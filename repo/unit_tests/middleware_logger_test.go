package unit_tests

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/harborworks/booking-hub/internal/api/middleware"
)

func TestRequestLogger_200(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := gin.New()
	r.Use(middleware.RequestLogger(log))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test?q=1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequestLogger_400(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := gin.New()
	r.Use(middleware.RequestLogger(log))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusBadRequest) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequestLogger_500(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := gin.New()
	r.Use(middleware.RequestLogger(log))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestRecovery_RegistersMiddleware(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := gin.New()
	r.Use(middleware.Recovery(log))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
