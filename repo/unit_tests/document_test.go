package unit_tests

import (
	"bytes"
	"testing"

	"github.com/harborworks/booking-hub/internal/service"
)

func TestRenderSimplePDFEmitsValidPDFHeader(t *testing.T) {
	pdf, err := service.RenderSimplePDF("Hello", map[string]string{"a": "1", "b": "2"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) {
		t.Fatalf("not a PDF: %q", pdf[:8])
	}
	if !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Fatal("missing EOF marker")
	}
}

func TestRenderCheckinPNGEmitsValidPNGHeader(t *testing.T) {
	png, err := service.RenderCheckinPNG("Pass", map[string]string{"name": "Alice"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !bytes.HasPrefix(png, pngMagic) {
		t.Fatalf("not a PNG: %v", png[:8])
	}
}
