package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// BackupService writes JSON exports to a local "removable media" directory
// (mounted via docker volume). Daily incrementals only export rows whose
// updated_at column changed since the last successful backup; weekly fulls
// dump every table. Restore reads the latest full + every incremental newer
// than that full. The 4-hour SLA is honored simply by being O(rows).
type BackupService struct {
	pool   *pgxpool.Pool
	repo   repository.BackupRepository
	target string
	log    *slog.Logger
}

func NewBackupService(pool *pgxpool.Pool, repo repository.BackupRepository, targetDir string, log *slog.Logger) *BackupService {
	return &BackupService{pool: pool, repo: repo, target: targetDir, log: log}
}

// Tables that participate in backup/restore. Order matters for restore so
// foreign keys resolve cleanly.
var backupTables = []string{
	"resources",
	"group_reservations",
	"users",
	"bookings",
	"group_buys",
	"group_buy_participants",
	"documents",
	"document_revisions",
	"notifications",
	"todos",
	"notification_deliveries",
	"data_dictionary",
	"tags",
	"taggings",
	"consent_records",
	"webhooks",
	"webhook_deliveries",
	"analytics_events",
	"analytics_hourly",
	"anomaly_alerts",
}

// TakeFull writes a full snapshot of all backupTables.
func (s *BackupService) TakeFull(ctx context.Context) (*domain.Backup, error) {
	return s.take(ctx, "full", time.Time{})
}

// TakeIncremental writes only rows whose updated_at is newer than the
// previous successful backup. The baseline is the most recent backup of
// either kind (full OR incremental) — using the last full snapshot as
// the baseline would re-emit every row that already shipped in earlier
// incrementals between fulls. Falls back to "no since" (full export) if
// the table is empty so the very first incremental still produces a
// usable file.
func (s *BackupService) TakeIncremental(ctx context.Context) (*domain.Backup, error) {
	last, err := s.repo.LastSuccessful(ctx)
	since := time.Time{}
	if err == nil && last != nil {
		since = last.TakenAt
	}
	return s.take(ctx, "incremental", since)
}

func (s *BackupService) take(ctx context.Context, kind string, since time.Time) (*domain.Backup, error) {
	if err := os.MkdirAll(s.target, 0o755); err != nil {
		return nil, fmt.Errorf("ensure backup dir: %w", err)
	}
	now := time.Now().UTC()
	name := fmt.Sprintf("harborworks-%s-%s.json", kind, now.Format("20060102T150405Z"))
	path := filepath.Join(s.target, name)

	dump := map[string][]map[string]any{}
	for _, table := range backupTables {
		rows, err := s.dumpTable(ctx, table, since)
		if err != nil {
			s.log.Warn("table dump failed", "table", table, "error", err)
			continue
		}
		dump[table] = rows
	}
	body, err := json.MarshalIndent(map[string]any{
		"kind":       kind,
		"taken_at":   now,
		"since":      since,
		"row_counts": rowCounts(dump),
		"tables":     dump,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return nil, err
	}
	b := &domain.Backup{
		Kind:      kind,
		Path:      path,
		SizeBytes: int64(len(body)),
		TakenAt:   now,
		Detail:    fmt.Sprintf("tables=%d", len(dump)),
	}
	if err := s.repo.Insert(ctx, b); err != nil {
		return nil, err
	}
	s.log.Info("backup written", "kind", kind, "path", path, "bytes", len(body))
	return b, nil
}

// dumpTable selects all rows from the given table; if the table has an
// updated_at column and `since` is non-zero, only newer rows are returned.
func (s *BackupService) dumpTable(ctx context.Context, table string, since time.Time) ([]map[string]any, error) {
	q := "SELECT row_to_json(t) FROM " + table + " t"
	args := []any{}
	if !since.IsZero() && tableHasUpdatedAt(table) {
		q += " WHERE updated_at > $1"
		args = append(args, since)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, 64)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func tableHasUpdatedAt(table string) bool {
	switch table {
	case "users", "bookings", "group_reservations", "group_buys", "documents", "todos":
		return true
	}
	return false
}

func rowCounts(dump map[string][]map[string]any) map[string]int {
	out := map[string]int{}
	for k, v := range dump {
		out[k] = len(v)
	}
	return out
}

// List returns the backup index for admin pages.
func (s *BackupService) List(ctx context.Context, limit int) ([]domain.Backup, error) {
	return s.repo.List(ctx, limit)
}

// PlanRestore returns the chain of files needed to restore to "now": the
// most recent full plus every incremental written after it. The caller can
// inspect this list before invoking ApplyRestore.
func (s *BackupService) PlanRestore(ctx context.Context) ([]domain.Backup, error) {
	full, err := s.repo.LastFull(ctx)
	if err != nil {
		return nil, errors.New("no full backup available")
	}
	incrementals, err := s.repo.IncrementalsAfter(ctx, full.TakenAt)
	if err != nil {
		return nil, err
	}
	plan := []domain.Backup{*full}
	plan = append(plan, incrementals...)
	return plan, nil
}
