package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// GovernanceService bundles every cross-cutting compliance feature: data
// dictionary, tags, consent, deletion, and CSV import/export.
type GovernanceService struct {
	repo      repository.GovernanceRepository
	users     repository.UserRepository
	bookings  repository.BookingRepository
	resources repository.ResourceRepository
	analytics *AnalyticsService
	log       *slog.Logger
}

func NewGovernanceService(
	repo repository.GovernanceRepository,
	users repository.UserRepository,
	bookings repository.BookingRepository,
	resources repository.ResourceRepository,
	analytics *AnalyticsService,
	log *slog.Logger,
) *GovernanceService {
	return &GovernanceService{repo: repo, users: users, bookings: bookings, resources: resources, analytics: analytics, log: log}
}

// ---------- data dictionary / tags ----------

func (s *GovernanceService) Dictionary(ctx context.Context) ([]domain.DataDictionaryEntry, error) {
	return s.repo.ListDictionary(ctx)
}

func (s *GovernanceService) UpsertDictionaryEntry(ctx context.Context, e *domain.DataDictionaryEntry) error {
	if e.Entity == "" || e.Field == "" {
		return errors.Join(domain.ErrInvalidInput, errors.New("entity and field are required"))
	}
	return s.repo.UpsertDictionary(ctx, e)
}

func (s *GovernanceService) Tags(ctx context.Context) ([]domain.Tag, error) {
	return s.repo.ListTags(ctx)
}

func (s *GovernanceService) CreateTag(ctx context.Context, name, description string) (*domain.Tag, error) {
	t := &domain.Tag{Name: name, Description: description}
	if err := s.repo.CreateTag(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *GovernanceService) Tag(ctx context.Context, tagID uuid.UUID, targetType string, targetID uuid.UUID) error {
	return s.repo.Tag(ctx, tagID, targetType, targetID)
}

func (s *GovernanceService) Taggings(ctx context.Context, targetType string, targetID uuid.UUID) ([]domain.Tag, error) {
	return s.repo.ListTaggings(ctx, targetType, targetID)
}

// ---------- consent ----------

func (s *GovernanceService) GrantConsent(ctx context.Context, userID uuid.UUID, scope, version string) error {
	now := time.Now().UTC()
	return s.repo.UpsertConsent(ctx, &domain.ConsentRecord{
		UserID: userID, Scope: scope, Granted: true, Version: version, GrantedAt: &now,
	})
}

func (s *GovernanceService) WithdrawConsent(ctx context.Context, userID uuid.UUID, scope, version string) error {
	now := time.Now().UTC()
	return s.repo.UpsertConsent(ctx, &domain.ConsentRecord{
		UserID: userID, Scope: scope, Granted: false, Version: version, WithdrawnAt: &now,
	})
}

func (s *GovernanceService) ConsentHistory(ctx context.Context, userID uuid.UUID) ([]domain.ConsentRecord, error) {
	return s.repo.ListConsent(ctx, userID)
}

// ---------- deletion ----------

const DeletionDelay = 7 * 24 * time.Hour

func (s *GovernanceService) RequestDeletion(ctx context.Context, userID uuid.UUID) (*domain.DeletionRequest, error) {
	d := &domain.DeletionRequest{
		UserID:       userID,
		RequestedAt:  time.Now().UTC(),
		ProcessAfter: time.Now().UTC().Add(DeletionDelay),
		Status:       "pending",
	}
	if err := s.repo.CreateDeletionRequest(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *GovernanceService) CancelDeletion(ctx context.Context, userID uuid.UUID) error {
	return s.repo.CancelDeletion(ctx, userID)
}

func (s *GovernanceService) PendingDeletion(ctx context.Context, userID uuid.UUID) (*domain.DeletionRequest, error) {
	return s.repo.GetDeletionRequest(ctx, userID)
}

// RunDeletionExecutor is the background-job step that processes every
// deletion request whose 7-day grace window has elapsed. For each request:
//
//  1. Analytics events are detached: the per-user salted hash is set to NULL
//     so the rows survive as anonymized aggregates with no link back.
//  2. The user row is hard-deleted. Migration 0003 makes every dependent
//     FK either CASCADE (sessions, bookings, documents/revisions, todos,
//     notifications, consent_records, group_buy_participants, deletion_
//     requests) or SET NULL (group_buys.organizer_id), so a single DELETE
//     removes every personal-data row in one transaction.
//
// The deletion is logged with the user id only (no PII).
func (s *GovernanceService) RunDeletionExecutor(ctx context.Context) error {
	now := time.Now().UTC()
	due, err := s.repo.ListDuePending(ctx, now)
	if err != nil {
		return err
	}
	for _, d := range due {
		if err := s.hardDeleteOne(ctx, d); err != nil {
			s.log.Warn("deletion executor failed", "user_id", d.UserID, "error", err)
		}
	}
	return nil
}

func (s *GovernanceService) hardDeleteOne(ctx context.Context, d domain.DeletionRequest) error {
	// 1. Detach analytics first (uses the salted hash, not the user_id, so
	//    it must run before the user row is gone).
	if err := s.analytics.AnonymiseUserEvents(ctx, d.UserID); err != nil {
		return fmt.Errorf("anonymise analytics: %w", err)
	}
	// 2. Hard delete. Cascades + SET NULL handle every dependent table.
	if err := s.users.HardDelete(ctx, d.UserID); err != nil {
		return fmt.Errorf("hard delete user: %w", err)
	}
	// 3. The deletion_requests row is gone via cascade — log the event so
	//    auditors can correlate. Only the user id (a UUID) appears here.
	s.log.Info("user hard-deleted", "user_id", d.UserID, "request_id", d.ID)
	return nil
}

// ---------- CSV import/export ----------

// CSVValidationError describes one row that failed bulk validation.
type CSVValidationError struct {
	Row    int    `json:"row"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ParsedResourceRow is one row that survived bulk validation. Exported so
// unit tests can assert on the parsed form without round-tripping through DB.
type ParsedResourceRow struct {
	Name        string
	Description string
	Capacity    int
}

// ValidateResourcesCSV is a pure function that performs all schema and row
// checks for a resource CSV import. It is intentionally side-effect free so
// unit tests can exercise every branch without a database. The contract is
// "all-or-nothing": when any row fails, the slice of validation errors is
// returned and `parsed` is empty.
func ValidateResourcesCSV(body io.Reader) (parsed []ParsedResourceRow, errs []CSVValidationError, fatal error) {
	r := csv.NewReader(body)
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("parse csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, errors.New("csv is empty")
	}
	header := rows[0]
	colIdx := map[string]int{}
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	required := []string{"name", "description", "capacity"}
	for _, c := range required {
		if _, ok := colIdx[c]; !ok {
			return nil, nil, fmt.Errorf("missing column %q", c)
		}
	}

	parsed = make([]ParsedResourceRow, 0, len(rows)-1)
	seen := map[string]bool{}
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < len(header) {
			errs = append(errs, CSVValidationError{Row: i + 1, Field: "*", Reason: "short row"})
			continue
		}
		name := strings.TrimSpace(row[colIdx["name"]])
		desc := strings.TrimSpace(row[colIdx["description"]])
		capStr := strings.TrimSpace(row[colIdx["capacity"]])

		if name == "" {
			errs = append(errs, CSVValidationError{Row: i + 1, Field: "name", Reason: "required"})
			continue
		}
		if seen[strings.ToLower(name)] {
			errs = append(errs, CSVValidationError{Row: i + 1, Field: "name", Reason: "duplicate within file"})
			continue
		}
		seen[strings.ToLower(name)] = true

		capVal, err := strconv.Atoi(capStr)
		if err != nil || capVal <= 0 {
			errs = append(errs, CSVValidationError{Row: i + 1, Field: "capacity", Reason: "must be a positive integer"})
			continue
		}
		parsed = append(parsed, ParsedResourceRow{Name: name, Description: desc, Capacity: capVal})
	}

	if len(errs) > 0 {
		return nil, errs, nil
	}
	return parsed, nil, nil
}

// ImportResourcesCSV parses a CSV with the columns name,description,capacity,
// validates every row, and persists the surviving rows inside a single
// transaction. Any validation error or insert error rolls back the whole
// transaction; the returned `inserted` count is the number of rows actually
// committed (always 0 on the failure paths).
func (s *GovernanceService) ImportResourcesCSV(ctx context.Context, body io.Reader) (inserted int, errs []CSVValidationError, fatal error) {
	parsed, validationErrs, fatal := ValidateResourcesCSV(body)
	if fatal != nil {
		return 0, nil, fatal
	}
	if len(validationErrs) > 0 {
		return 0, validationErrs, nil
	}

	rows := make([]domain.Resource, 0, len(parsed))
	for _, p := range parsed {
		rows = append(rows, domain.Resource{
			Name:        p.Name,
			Description: p.Description,
			Capacity:    p.Capacity,
		})
	}

	// All-or-nothing transactional insert. If a row collides with an existing
	// resource (unique name), the whole transaction is rolled back and the
	// caller learns about it via fatal.
	n, err := s.resources.InsertManyTx(ctx, rows)
	if err != nil {
		s.log.Warn("csv import rolled back", "error", err)
		return 0, nil, fmt.Errorf("import rolled back: %w", err)
	}
	s.log.Info("csv import committed", "rows", n)
	return n, nil, nil
}

// ExportResourcesCSV writes resources as CSV to the supplied writer.
func (s *GovernanceService) ExportResourcesCSV(ctx context.Context, w io.Writer) error {
	resources, err := s.resources.List(ctx)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{"id", "name", "description", "capacity"}); err != nil {
		return err
	}
	for _, r := range resources {
		if err := cw.Write([]string{r.ID.String(), r.Name, r.Description, strconv.Itoa(r.Capacity)}); err != nil {
			return err
		}
	}
	return cw.Error()
}
