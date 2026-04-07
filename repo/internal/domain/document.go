package domain

import (
	"time"

	"github.com/google/uuid"
)

type DocumentType string

const (
	DocConfirmationPDF DocumentType = "confirmation_pdf"
	DocCheckinPassPNG  DocumentType = "checkin_pass_png"
)

// Document is the logical artifact users can download. The actual bytes live
// in DocumentRevision rows so a full revision history is preserved; superseded
// revisions are kept and labelled rather than deleted.
type Document struct {
	ID              uuid.UUID    `json:"id"`
	OwnerUserID     uuid.UUID    `json:"owner_user_id"`
	DocType         DocumentType `json:"doc_type"`
	RelatedType     string       `json:"related_type"`
	RelatedID       uuid.UUID    `json:"related_id"`
	CurrentRevision int          `json:"current_revision"`
	Title           string       `json:"title"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`

	Revisions []DocumentRevision `json:"revisions,omitempty"`
}

type DocumentRevision struct {
	ID           uuid.UUID  `json:"id"`
	DocumentID   uuid.UUID  `json:"document_id"`
	Revision     int        `json:"revision"`
	Content      []byte     `json:"-"`
	ContentType  string     `json:"content_type"`
	Superseded   bool       `json:"superseded"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	SupersededBy *uuid.UUID `json:"superseded_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
