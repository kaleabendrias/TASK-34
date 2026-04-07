package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type DocumentRepository interface {
	Create(ctx context.Context, d *domain.Document) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Document, error)
	GetWithRevisions(ctx context.Context, id uuid.UUID) (*domain.Document, error)
	GetCurrentRevision(ctx context.Context, docID uuid.UUID) (*domain.DocumentRevision, error)
	GetRevision(ctx context.Context, docID uuid.UUID, revision int) (*domain.DocumentRevision, error)
	AppendRevision(ctx context.Context, docID uuid.UUID, content []byte, contentType string) (*domain.DocumentRevision, error)
	ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Document, error)
}

type docRepo struct{ pool *pgxpool.Pool }

func NewDocumentRepository(pool *pgxpool.Pool) DocumentRepository {
	return &docRepo{pool: pool}
}

const documentColumns = `
	id, owner_user_id, doc_type, related_type, related_id, current_revision, title, created_at, updated_at
`

// Create inserts the parent row with current_revision = 0. The first revision
// is appended in a separate call (which bumps to 1) so the supersession
// logic stays uniform.
func (r *docRepo) Create(ctx context.Context, d *domain.Document) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	d.CurrentRevision = 0
	const q = `
		INSERT INTO documents (id, owner_user_id, doc_type, related_type, related_id, current_revision, title, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`
	_, err := r.pool.Exec(ctx, q,
		d.ID, d.OwnerUserID, string(d.DocType), d.RelatedType, d.RelatedID,
		d.CurrentRevision, d.Title, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (r *docRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Document, error) {
	q := `SELECT ` + documentColumns + ` FROM documents WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	d, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func (r *docRepo) GetWithRevisions(ctx context.Context, id uuid.UUID) (*domain.Document, error) {
	d, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, document_id, revision, content_type, superseded, superseded_at, superseded_by, created_at
		FROM document_revisions WHERE document_id = $1 ORDER BY revision DESC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rev domain.DocumentRevision
		if err := rows.Scan(&rev.ID, &rev.DocumentID, &rev.Revision, &rev.ContentType, &rev.Superseded, &rev.SupersededAt, &rev.SupersededBy, &rev.CreatedAt); err != nil {
			return nil, err
		}
		d.Revisions = append(d.Revisions, rev)
	}
	return d, rows.Err()
}

func (r *docRepo) GetCurrentRevision(ctx context.Context, docID uuid.UUID) (*domain.DocumentRevision, error) {
	const q = `
		SELECT id, document_id, revision, content, content_type, superseded, superseded_at, superseded_by, created_at
		FROM document_revisions
		WHERE document_id = $1
		ORDER BY revision DESC LIMIT 1
	`
	row := r.pool.QueryRow(ctx, q, docID)
	var rev domain.DocumentRevision
	if err := row.Scan(&rev.ID, &rev.DocumentID, &rev.Revision, &rev.Content, &rev.ContentType, &rev.Superseded, &rev.SupersededAt, &rev.SupersededBy, &rev.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &rev, nil
}

func (r *docRepo) GetRevision(ctx context.Context, docID uuid.UUID, revision int) (*domain.DocumentRevision, error) {
	const q = `
		SELECT id, document_id, revision, content, content_type, superseded, superseded_at, superseded_by, created_at
		FROM document_revisions WHERE document_id = $1 AND revision = $2
	`
	row := r.pool.QueryRow(ctx, q, docID, revision)
	var rev domain.DocumentRevision
	if err := row.Scan(&rev.ID, &rev.DocumentID, &rev.Revision, &rev.Content, &rev.ContentType, &rev.Superseded, &rev.SupersededAt, &rev.SupersededBy, &rev.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &rev, nil
}

// AppendRevision creates a new revision, marks the previous one as superseded,
// and bumps current_revision on the parent document. Done in a single tx so
// readers either see the old or the new state, never a half-update.
func (r *docRepo) AppendRevision(ctx context.Context, docID uuid.UUID, content []byte, contentType string) (*domain.DocumentRevision, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int
	if err := tx.QueryRow(ctx, `SELECT current_revision FROM documents WHERE id = $1 FOR UPDATE`, docID).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	next := current + 1
	newID := uuid.New()
	now := time.Now().UTC()

	if _, err := tx.Exec(ctx, `
		UPDATE document_revisions
		SET superseded = TRUE, superseded_at = $2, superseded_by = $3
		WHERE document_id = $1 AND revision = $4
	`, docID, now, newID, current); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO document_revisions (id, document_id, revision, content, content_type, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, newID, docID, next, content, contentType, now); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE documents SET current_revision = $2, updated_at = $3 WHERE id = $1
	`, docID, next, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.DocumentRevision{
		ID:          newID,
		DocumentID:  docID,
		Revision:    next,
		Content:     content,
		ContentType: contentType,
		CreatedAt:   now,
	}, nil
}

func (r *docRepo) ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Document, error) {
	q := `SELECT ` + documentColumns + ` FROM documents WHERE owner_user_id = $1 ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Document, 0)
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func scanDocument(s rowScanner) (*domain.Document, error) {
	var (
		d       domain.Document
		docType string
	)
	if err := s.Scan(&d.ID, &d.OwnerUserID, &docType, &d.RelatedType, &d.RelatedID, &d.CurrentRevision, &d.Title, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.DocType = domain.DocumentType(docType)
	return &d, nil
}

// CreateDocumentWithFirstRevision is a convenience helper used by the document
// service: it inserts the parent row and the first revision in a single tx.
func CreateDocumentWithFirstRevision(ctx context.Context, pool *pgxpool.Pool, d *domain.Document, content []byte, contentType string) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	d.CurrentRevision = 1

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO documents (id, owner_user_id, doc_type, related_type, related_id, current_revision, title, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, d.ID, d.OwnerUserID, string(d.DocType), d.RelatedType, d.RelatedID, d.CurrentRevision, d.Title, d.CreatedAt, d.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO document_revisions (id, document_id, revision, content, content_type, created_at)
		VALUES ($1,$2,1,$3,$4,$5)
	`, uuid.New(), d.ID, content, contentType, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
