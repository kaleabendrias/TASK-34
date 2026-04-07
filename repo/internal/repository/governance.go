package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type GovernanceRepository interface {
	// Data dictionary
	ListDictionary(ctx context.Context) ([]domain.DataDictionaryEntry, error)
	UpsertDictionary(ctx context.Context, e *domain.DataDictionaryEntry) error

	// Tags
	CreateTag(ctx context.Context, t *domain.Tag) error
	ListTags(ctx context.Context) ([]domain.Tag, error)
	Tag(ctx context.Context, tagID uuid.UUID, targetType string, targetID uuid.UUID) error
	ListTaggings(ctx context.Context, targetType string, targetID uuid.UUID) ([]domain.Tag, error)

	// Consent
	UpsertConsent(ctx context.Context, c *domain.ConsentRecord) error
	ListConsent(ctx context.Context, userID uuid.UUID) ([]domain.ConsentRecord, error)

	// Deletion
	CreateDeletionRequest(ctx context.Context, d *domain.DeletionRequest) error
	GetDeletionRequest(ctx context.Context, userID uuid.UUID) (*domain.DeletionRequest, error)
	ListDuePending(ctx context.Context, now time.Time) ([]domain.DeletionRequest, error)
	MarkDeletionComplete(ctx context.Context, id uuid.UUID, at time.Time) error
	CancelDeletion(ctx context.Context, userID uuid.UUID) error
}

type govRepo struct{ pool *pgxpool.Pool }

func NewGovernanceRepository(pool *pgxpool.Pool) GovernanceRepository {
	return &govRepo{pool: pool}
}

func (r *govRepo) ListDictionary(ctx context.Context) ([]domain.DataDictionaryEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, entity, field, data_type, description, sensitive, tags
		FROM data_dictionary ORDER BY entity, field
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.DataDictionaryEntry, 0)
	for rows.Next() {
		var e domain.DataDictionaryEntry
		if err := rows.Scan(&e.ID, &e.Entity, &e.Field, &e.DataType, &e.Description, &e.Sensitive, &e.Tags); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *govRepo) UpsertDictionary(ctx context.Context, e *domain.DataDictionaryEntry) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	const q = `
		INSERT INTO data_dictionary (id, entity, field, data_type, description, sensitive, tags)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (entity, field) DO UPDATE SET
			data_type   = EXCLUDED.data_type,
			description = EXCLUDED.description,
			sensitive   = EXCLUDED.sensitive,
			tags        = EXCLUDED.tags
	`
	_, err := r.pool.Exec(ctx, q, e.ID, e.Entity, e.Field, e.DataType, e.Description, e.Sensitive, e.Tags)
	return err
}

func (r *govRepo) CreateTag(ctx context.Context, t *domain.Tag) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tags (id, name, description) VALUES ($1,$2,$3)
		ON CONFLICT (name) DO NOTHING
	`, t.ID, t.Name, t.Description)
	return err
}

func (r *govRepo) ListTags(ctx context.Context) ([]domain.Tag, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, description FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Tag, 0)
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Description); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *govRepo) Tag(ctx context.Context, tagID uuid.UUID, targetType string, targetID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO taggings (id, tag_id, target_type, target_id)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (tag_id, target_type, target_id) DO NOTHING
	`, uuid.New(), tagID, targetType, targetID)
	return err
}

func (r *govRepo) ListTaggings(ctx context.Context, targetType string, targetID uuid.UUID) ([]domain.Tag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.name, t.description
		FROM tags t
		JOIN taggings g ON g.tag_id = t.id
		WHERE g.target_type = $1 AND g.target_id = $2
		ORDER BY t.name
	`, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Tag, 0)
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Description); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *govRepo) UpsertConsent(ctx context.Context, c *domain.ConsentRecord) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO consent_records (id, user_id, scope, granted, version, granted_at, withdrawn_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, c.ID, c.UserID, c.Scope, c.Granted, c.Version, c.GrantedAt, c.WithdrawnAt, c.CreatedAt)
	return err
}

func (r *govRepo) ListConsent(ctx context.Context, userID uuid.UUID) ([]domain.ConsentRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, scope, granted, version, granted_at, withdrawn_at, created_at
		FROM consent_records WHERE user_id = $1 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ConsentRecord, 0)
	for rows.Next() {
		var c domain.ConsentRecord
		if err := rows.Scan(&c.ID, &c.UserID, &c.Scope, &c.Granted, &c.Version, &c.GrantedAt, &c.WithdrawnAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *govRepo) CreateDeletionRequest(ctx context.Context, d *domain.DeletionRequest) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.RequestedAt.IsZero() {
		d.RequestedAt = time.Now().UTC()
	}
	if d.Status == "" {
		d.Status = "pending"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO deletion_requests (id, user_id, requested_at, process_after, status, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, d.ID, d.UserID, d.RequestedAt, d.ProcessAfter, d.Status, d.CompletedAt)
	if err != nil {
		return fmt.Errorf("insert deletion: %w", err)
	}
	return nil
}

func (r *govRepo) GetDeletionRequest(ctx context.Context, userID uuid.UUID) (*domain.DeletionRequest, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, requested_at, process_after, status, completed_at
		FROM deletion_requests WHERE user_id = $1 AND status = 'pending'
		ORDER BY requested_at DESC LIMIT 1
	`, userID)
	var d domain.DeletionRequest
	if err := row.Scan(&d.ID, &d.UserID, &d.RequestedAt, &d.ProcessAfter, &d.Status, &d.CompletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (r *govRepo) ListDuePending(ctx context.Context, now time.Time) ([]domain.DeletionRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, requested_at, process_after, status, completed_at
		FROM deletion_requests WHERE status = 'pending' AND process_after <= $1
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.DeletionRequest, 0)
	for rows.Next() {
		var d domain.DeletionRequest
		if err := rows.Scan(&d.ID, &d.UserID, &d.RequestedAt, &d.ProcessAfter, &d.Status, &d.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *govRepo) MarkDeletionComplete(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE deletion_requests SET status = 'completed', completed_at = $2 WHERE id = $1`, id, at)
	return err
}

func (r *govRepo) CancelDeletion(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE deletion_requests SET status = 'canceled' WHERE user_id = $1 AND status = 'pending'`, userID)
	return err
}

// Convenience: marshal/unmarshal field mapping for webhooks. Kept here so the
// repo file is the only one that has to import encoding/json for this.
func MarshalFieldMapping(m map[string]string) ([]byte, error) { return json.Marshal(m) }
