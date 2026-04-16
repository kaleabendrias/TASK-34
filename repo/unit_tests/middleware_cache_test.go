package unit_tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/infrastructure/cache"
)

func newCacheTestRouter(c *cache.Cache, adminUser *domain.User) *gin.Engine {
	r := gin.New()
	mw := []gin.HandlerFunc{middleware.ReadThroughCache(c)}
	if adminUser != nil {
		mw = append([]gin.HandlerFunc{injectUser(adminUser)}, mw...)
	}
	r.Use(mw...)
	r.GET("/api/resources", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"resources": []string{"Slip A1"}})
	})
	r.POST("/api/bookings", func(ctx *gin.Context) {
		ctx.JSON(http.StatusCreated, gin.H{"id": uuid.New().String()})
	})
	return r
}

// getReq is a shorthand for performing a GET request and returning status + headers.
func getReq(r http.Handler, path string, extraHeaders map[string]string) (int, http.Header) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Header()
}

// postReq performs a POST request.
func postReq(r http.Handler, path string) int {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// ─── Read-through cache tests ─────────────────────────────────────────────────

func TestCache_NonGetPassesThrough(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	r := newCacheTestRouter(c, nil)

	code := postReq(r, "/api/bookings")
	// POST responses are never cached; the handler returns 201.
	if code != http.StatusCreated {
		t.Errorf("expected 201 for POST, got %d", code)
	}
}

func TestCache_FirstGet_Miss(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	r := newCacheTestRouter(c, nil)

	code, headers := getReq(r, "/api/resources", nil)
	if code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
	if headers.Get("X-Cache") != "MISS" {
		t.Errorf("expected X-Cache: MISS, got %q", headers.Get("X-Cache"))
	}
}

func TestCache_SecondGet_Hit(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	r := newCacheTestRouter(c, nil)

	// First request populates the cache.
	getReq(r, "/api/resources", nil)

	// Second request should be a cache hit.
	code, headers := getReq(r, "/api/resources", nil)
	if code != http.StatusOK {
		t.Errorf("expected 200 on cache hit, got %d", code)
	}
	if headers.Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", headers.Get("X-Cache"))
	}
}

func TestCache_DifferentPaths_IndependentKeys(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	r := gin.New()
	r.Use(middleware.ReadThroughCache(c))
	callCount := 0
	r.GET("/api/path1", func(ctx *gin.Context) {
		callCount++
		ctx.JSON(http.StatusOK, gin.H{"path": 1})
	})
	r.GET("/api/path2", func(ctx *gin.Context) {
		callCount++
		ctx.JSON(http.StatusOK, gin.H{"path": 2})
	})

	// Hit path1 twice → first MISS, second HIT; path2 is a different key.
	req1 := httptest.NewRequest(http.MethodGet, "/api/path1", nil)
	req1.Header.Set("Accept", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Header().Get("X-Cache") != "MISS" {
		t.Errorf("path1 first: want MISS, got %s", w1.Header().Get("X-Cache"))
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/path1", nil)
	req2.Header.Set("Accept", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("path1 second: want HIT, got %s", w2.Header().Get("X-Cache"))
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/path2", nil)
	req3.Header.Set("Accept", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	// path2 was never hit → MISS
	if w3.Header().Get("X-Cache") != "MISS" {
		t.Errorf("path2 first: want MISS, got %s", w3.Header().Get("X-Cache"))
	}
}

func TestCache_AdminBypass_SkipsCache(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	admin := &domain.User{ID: uuid.New(), IsAdmin: true}
	r := newCacheTestRouter(c, admin)

	// Pre-warm cache with a non-admin request (no user in context).
	noAdminRouter := newCacheTestRouter(c, nil)
	getReq(noAdminRouter, "/api/resources", nil) // MISS → stores in cache

	// Admin requests with bypass header should get BYPASS, not HIT.
	code, headers := getReq(r, "/api/resources", map[string]string{
		middleware.CacheBypassHeader: "true",
	})
	if code != http.StatusOK {
		t.Errorf("expected 200 for bypass, got %d", code)
	}
	if headers.Get("X-Cache") != "BYPASS" {
		t.Errorf("expected X-Cache: BYPASS, got %q", headers.Get("X-Cache"))
	}
}

func TestCache_NonAdminBypassIgnored(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	normalUser := &domain.User{ID: uuid.New(), IsAdmin: false}
	r := newCacheTestRouter(c, normalUser)

	// First request: MISS (normal user bypass header is ignored).
	_, h1 := getReq(r, "/api/resources", map[string]string{middleware.CacheBypassHeader: "true"})
	if h1.Get("X-Cache") != "MISS" {
		t.Errorf("non-admin bypass should be ignored → MISS, got %s", h1.Get("X-Cache"))
	}

	// Second request: HIT (cached from previous).
	_, h2 := getReq(r, "/api/resources", map[string]string{middleware.CacheBypassHeader: "true"})
	if h2.Get("X-Cache") != "HIT" {
		t.Errorf("second request should be HIT, got %s", h2.Get("X-Cache"))
	}
}

func TestCache_QueryStringIsPartOfKey(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	r := gin.New()
	r.Use(middleware.ReadThroughCache(c))
	r.GET("/api/items", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"q": ctx.Query("q")})
	})

	doGet := func(qs string) string {
		req := httptest.NewRequest(http.MethodGet, "/api/items?"+qs, nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Header().Get("X-Cache")
	}

	if v := doGet("q=foo"); v != "MISS" {
		t.Errorf("first request ?q=foo: want MISS, got %s", v)
	}
	if v := doGet("q=foo"); v != "HIT" {
		t.Errorf("second request ?q=foo: want HIT, got %s", v)
	}
	if v := doGet("q=bar"); v != "MISS" {
		t.Errorf("first request ?q=bar: want MISS, got %s", v)
	}
}

func TestCache_ErrorResponse_NotCached(t *testing.T) {
	c := cache.New(cache.DefaultTTL)
	r := gin.New()
	r.Use(middleware.ReadThroughCache(c))
	r.GET("/api/broken", func(ctx *gin.Context) {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "oops"})
	})

	req1 := httptest.NewRequest(http.MethodGet, "/api/broken", nil)
	req1.Header.Set("Accept", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	// Error response (5xx) should never be cached, so the second request
	// should also be a MISS.
	req2 := httptest.NewRequest(http.MethodGet, "/api/broken", nil)
	req2.Header.Set("Accept", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Cache") != "MISS" {
		t.Errorf("error response should not be cached: got %s", w2.Header().Get("X-Cache"))
	}
}
