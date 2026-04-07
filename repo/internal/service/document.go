package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

type DocumentService struct {
	repo repository.DocumentRepository
	log  *slog.Logger
}

func NewDocumentService(repo repository.DocumentRepository, log *slog.Logger) *DocumentService {
	return &DocumentService{repo: repo, log: log}
}

// GenerateConfirmation produces a PDF confirmation document for a booking-like
// payload. Each call appends a new revision; the previous revision is
// auto-marked superseded by the repository.
func (s *DocumentService) GenerateConfirmation(ctx context.Context, ownerID uuid.UUID, relatedType string, relatedID uuid.UUID, title string, fields map[string]string) (*domain.Document, *domain.DocumentRevision, error) {
	pdf, err := RenderSimplePDF(title, fields)
	if err != nil {
		return nil, nil, err
	}
	return s.upsertDocument(ctx, ownerID, relatedType, relatedID, domain.DocConfirmationPDF, title, pdf, "application/pdf")
}

// GenerateCheckinPass renders a PNG check-in pass.
func (s *DocumentService) GenerateCheckinPass(ctx context.Context, ownerID uuid.UUID, relatedType string, relatedID uuid.UUID, title string, fields map[string]string) (*domain.Document, *domain.DocumentRevision, error) {
	pngBytes, err := RenderCheckinPNG(title, fields)
	if err != nil {
		return nil, nil, err
	}
	return s.upsertDocument(ctx, ownerID, relatedType, relatedID, domain.DocCheckinPassPNG, title, pngBytes, "image/png")
}

func (s *DocumentService) upsertDocument(ctx context.Context, ownerID uuid.UUID, relatedType string, relatedID uuid.UUID, dt domain.DocumentType, title string, content []byte, contentType string) (*domain.Document, *domain.DocumentRevision, error) {
	// Look for an existing document for this (owner, related, doc_type) and
	// append a revision; otherwise create with revision 1.
	existing, err := s.findExisting(ctx, ownerID, relatedType, relatedID, dt)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, nil, err
	}
	if existing != nil {
		rev, err := s.repo.AppendRevision(ctx, existing.ID, content, contentType)
		if err != nil {
			return nil, nil, err
		}
		existing.CurrentRevision = rev.Revision
		return existing, rev, nil
	}
	d := &domain.Document{
		OwnerUserID: ownerID,
		DocType:     dt,
		RelatedType: relatedType,
		RelatedID:   relatedID,
		Title:       title,
	}
	if err := s.repo.Create(ctx, d); err != nil {
		return nil, nil, err
	}
	rev, err := s.repo.AppendRevision(ctx, d.ID, content, contentType)
	if err != nil {
		return nil, nil, err
	}
	return d, rev, nil
}

func (s *DocumentService) findExisting(ctx context.Context, ownerID uuid.UUID, relatedType string, relatedID uuid.UUID, dt domain.DocumentType) (*domain.Document, error) {
	docs, err := s.repo.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		d := &docs[i]
		if d.RelatedType == relatedType && d.RelatedID == relatedID && d.DocType == dt {
			return d, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *DocumentService) Get(ctx context.Context, id uuid.UUID) (*domain.Document, error) {
	return s.repo.GetWithRevisions(ctx, id)
}

func (s *DocumentService) GetCurrent(ctx context.Context, id uuid.UUID) (*domain.DocumentRevision, error) {
	return s.repo.GetCurrentRevision(ctx, id)
}

func (s *DocumentService) GetRevision(ctx context.Context, docID uuid.UUID, rev int) (*domain.DocumentRevision, error) {
	return s.repo.GetRevision(ctx, docID, rev)
}

func (s *DocumentService) ListByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Document, error) {
	return s.repo.ListByOwner(ctx, ownerID)
}

// ---------- PDF generation (minimal hand-rolled) ----------

// RenderSimplePDF emits a single-page PDF with a heading and a key/value list.
// The output is intentionally minimal but a fully valid PDF 1.4 document so it
// renders in any viewer. No external dependencies.
func RenderSimplePDF(title string, fields map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	// Build the content stream.
	var content bytes.Buffer
	content.WriteString("BT\n")
	content.WriteString("/F1 18 Tf\n")
	content.WriteString("72 760 Td\n")
	content.WriteString("(" + escapePDFString(title) + ") Tj\n")
	content.WriteString("/F1 11 Tf\n")
	content.WriteString("0 -28 Td\n")
	content.WriteString("(HarborWorks confirmation document) Tj\n")
	content.WriteString("0 -22 Td\n")
	content.WriteString("(Generated: " + escapePDFString(time.Now().UTC().Format(time.RFC3339)) + ") Tj\n")
	for k, v := range fields {
		content.WriteString("0 -18 Td\n")
		content.WriteString("(" + escapePDFString(k+": "+v) + ") Tj\n")
	}
	content.WriteString("ET\n")

	stream := content.Bytes()
	streamLen := len(stream)

	// Object table — collect byte offsets as we write.
	offsets := make([]int, 0, 6)
	writeObj := func(s string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(s)
	}

	buf.WriteString("%PDF-1.4\n%\xff\xff\xff\xff\n")

	writeObj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	writeObj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	writeObj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	writeObj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n", streamLen))
	buf.Write(stream)
	buf.WriteString("\nendstream\nendobj\n")
	writeObj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	// Cross-reference table.
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefOffset)
	return buf.Bytes(), nil
}

func escapePDFString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return r.Replace(s)
}

// ---------- PNG check-in pass ----------

func RenderCheckinPNG(title string, fields map[string]string) ([]byte, error) {
	const w, h = 600, 320
	bg := color.RGBA{R: 11, G: 29, B: 42, A: 255}
	accent := color.RGBA{R: 43, G: 179, B: 192, A: 255}
	white := color.RGBA{R: 243, G: 247, B: 250, A: 255}
	muted := color.RGBA{R: 159, G: 180, B: 199, A: 255}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, w, 8), &image.Uniform{C: accent}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, h-8, w, h), &image.Uniform{C: accent}, image.Point{}, draw.Src)

	face := basicfont.Face7x13
	drawText(img, face, white, 24, 40, "HarborWorks Check-In Pass")
	drawText(img, face, accent, 24, 64, title)
	drawText(img, face, muted, 24, 84, "Generated: "+time.Now().UTC().Format(time.RFC3339))

	y := 120
	for k, v := range fields {
		drawText(img, face, muted, 24, y, k)
		drawText(img, face, white, 200, y, v)
		y += 20
		if y > h-30 {
			break
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawText(img *image.RGBA, face font.Face, col color.Color, x, y int, text string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: col},
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)},
	}
	d.DrawString(text)
}
