package unit_tests

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// makeAuthRouter builds a router with the given middleware chain and a single
// GET /test endpoint that returns 200 {"ok":true}.
func makeAuthRouter(mw ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(mw...)
	r.GET("/test", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

// injectUser is a middleware that pre-sets a domain.User in the gin context,
// simulating a preceding Authenticator that already resolved the session.
func injectUser(u *domain.User) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("auth.user", u)
		c.Next()
	}
}

// do sends a GET /test request to the given router and returns the status.
func do(r http.Handler, cookie string) int {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if cookie != "" {
		req.Header.Set("Cookie", middleware.SessionCookieName+"="+cookie)
	}
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// ─── Authenticator ───────────────────────────────────────────────────────────

func TestAuthenticator_NoCookie_Optional(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	authSvc := newTestAuthService(users, sessions, newMockCaptchaRepo())
	r := makeAuthRouter(middleware.Authenticator(authSvc, false))

	// No cookie, not required → 200.
	if code := do(r, ""); code != http.StatusOK {
		t.Errorf("expected 200, got %d", code)
	}
}

func TestAuthenticator_NoCookie_Required_ReturnsUnauthorized(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	authSvc := newTestAuthService(users, sessions, newMockCaptchaRepo())
	r := makeAuthRouter(middleware.Authenticator(authSvc, true))

	if code := do(r, ""); code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestAuthenticator_ValidSession_SetsUser(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice"})
	sessions := newMockSessionRepo()
	sessions.seed(&domain.Session{
		ID:             "valid-sess",
		UserID:         uid,
		CreatedAt:      time.Now().UTC(),
		LastActivityAt: time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(30 * time.Minute),
	})
	authSvc := newTestAuthService(users, sessions, newMockCaptchaRepo())

	// Add a handler that checks the user is set in context.
	var capturedUser *domain.User
	r := gin.New()
	r.Use(middleware.Authenticator(authSvc, true))
	r.GET("/test", func(c *gin.Context) {
		capturedUser = middleware.CurrentUser(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Cookie", middleware.SessionCookieName+"=valid-sess")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedUser == nil {
		t.Fatal("user not set in context")
	}
	if capturedUser.Username != "alice" {
		t.Errorf("expected alice, got %s", capturedUser.Username)
	}
}

func TestAuthenticator_ExpiredSession_ReturnsUnauthorized(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice"})
	sessions := newMockSessionRepo()
	sessions.seed(&domain.Session{
		ID:             "expired-sess",
		UserID:         uid,
		CreatedAt:      time.Now().UTC().Add(-2 * time.Hour),
		LastActivityAt: time.Now().UTC().Add(-35 * time.Minute),
		ExpiresAt:      time.Now().UTC().Add(-5 * time.Minute), // in the past
	})
	authSvc := newTestAuthService(users, sessions, newMockCaptchaRepo())
	r := makeAuthRouter(middleware.Authenticator(authSvc, true))

	if code := do(r, "expired-sess"); code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired session, got %d", code)
	}
}

func TestAuthenticator_BadCookie_ClearedAndUnauthorized(t *testing.T) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	authSvc := newTestAuthService(users, sessions, newMockCaptchaRepo())
	r := makeAuthRouter(middleware.Authenticator(authSvc, true))

	if code := do(r, "nonexistent-session-id"); code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unknown session, got %d", code)
	}
}

// ─── CurrentUser ─────────────────────────────────────────────────────────────

func TestCurrentUser_ReturnsNilWhenNotSet(t *testing.T) {
	var captured *domain.User
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		captured = middleware.CurrentUser(c)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if captured != nil {
		t.Errorf("expected nil, got %v", captured)
	}
}

// ─── RequireAdmin ─────────────────────────────────────────────────────────────

func TestRequireAdmin_AdminPasses(t *testing.T) {
	admin := &domain.User{ID: uuid.New(), IsAdmin: true}
	r := makeAuthRouter(injectUser(admin), middleware.RequireAdmin())
	if code := do(r, ""); code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", code)
	}
}

func TestRequireAdmin_NonAdminForbidden(t *testing.T) {
	user := &domain.User{ID: uuid.New(), IsAdmin: false}
	r := makeAuthRouter(injectUser(user), middleware.RequireAdmin())
	if code := do(r, ""); code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", code)
	}
}

func TestRequireAdmin_NoUser_Unauthorized(t *testing.T) {
	r := makeAuthRouter(middleware.RequireAdmin())
	if code := do(r, ""); code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no user in context, got %d", code)
	}
}

// ─── RequireNotBlacklisted ────────────────────────────────────────────────────

func TestRequireNotBlacklisted_NormalUserPasses(t *testing.T) {
	user := &domain.User{ID: uuid.New(), IsBlacklisted: false}
	r := makeAuthRouter(injectUser(user), middleware.RequireNotBlacklisted())
	if code := do(r, ""); code != http.StatusOK {
		t.Errorf("expected 200 for normal user, got %d", code)
	}
}

func TestRequireNotBlacklisted_BlacklistedForbidden(t *testing.T) {
	user := &domain.User{ID: uuid.New(), IsBlacklisted: true, BlacklistReason: "spam"}
	r := makeAuthRouter(injectUser(user), middleware.RequireNotBlacklisted())
	if code := do(r, ""); code != http.StatusForbidden {
		t.Errorf("expected 403 for blacklisted user, got %d", code)
	}
}

func TestRequireNotBlacklisted_NoUser_Unauthorized(t *testing.T) {
	r := makeAuthRouter(middleware.RequireNotBlacklisted())
	if code := do(r, ""); code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no user, got %d", code)
	}
}

// ─── RequirePasswordRotated ──────────────────────────────────────────────────

func TestRequirePasswordRotated_NoRotation_Passes(t *testing.T) {
	user := &domain.User{ID: uuid.New(), MustRotatePassword: false}
	r := makeAuthRouter(injectUser(user), middleware.RequirePasswordRotated())
	if code := do(r, ""); code != http.StatusOK {
		t.Errorf("expected 200 when no rotation needed, got %d", code)
	}
}

func TestRequirePasswordRotated_RotationNeeded_BlocksNormalRoutes(t *testing.T) {
	user := &domain.User{ID: uuid.New(), MustRotatePassword: true}
	r := makeAuthRouter(injectUser(user), middleware.RequirePasswordRotated())
	if code := do(r, ""); code != http.StatusForbidden {
		t.Errorf("expected 403 when rotation needed, got %d", code)
	}
}

func TestRequirePasswordRotated_AllowsChangePasswordPath(t *testing.T) {
	user := &domain.User{ID: uuid.New(), MustRotatePassword: true}
	r := gin.New()
	r.Use(injectUser(user), middleware.RequirePasswordRotated())
	r.POST("/api/auth/change-password", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("change-password should be allowed: got %d", w.Code)
	}
}

func TestRequirePasswordRotated_AllowsLogoutPath(t *testing.T) {
	user := &domain.User{ID: uuid.New(), MustRotatePassword: true}
	r := gin.New()
	r.Use(injectUser(user), middleware.RequirePasswordRotated())
	r.POST("/api/auth/logout", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("logout should be allowed: got %d", w.Code)
	}
}

func TestRequirePasswordRotated_AllowsMePath(t *testing.T) {
	user := &domain.User{ID: uuid.New(), MustRotatePassword: true}
	r := gin.New()
	r.Use(injectUser(user), middleware.RequirePasswordRotated())
	r.GET("/api/auth/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/me should be allowed: got %d", w.Code)
	}
}

func TestRequirePasswordRotated_NilUser_Passes(t *testing.T) {
	// No user in context → middleware passes through (guard is noop).
	r := makeAuthRouter(middleware.RequirePasswordRotated())
	if code := do(r, ""); code != http.StatusOK {
		t.Errorf("expected 200 when no user, got %d", code)
	}
}
