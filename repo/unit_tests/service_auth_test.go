package unit_tests

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

// testAuthSettings returns a fast test configuration. MinCost (4) cuts bcrypt
// time from ~100ms to ~1ms so the test suite remains snappy.
func testAuthSettings() service.AuthSettings {
	return service.AuthSettings{
		SessionInactivity:  30 * time.Minute,
		SessionAbsoluteTTL: 12 * time.Hour,
		LockoutThreshold:   5,
		LockoutDuration:    15 * time.Minute,
		CaptchaThreshold:   2,
		CaptchaTTL:         5 * time.Minute,
		BcryptCost:         bcrypt.MinCost,
		CookieSecure:       false,
	}
}

func newTestAuthService(users *mockUserRepo, sessions *mockSessionRepo, captchas *mockCaptchaRepo) *service.AuthService {
	return service.NewAuthService(users, sessions, captchas, slog.Default(), testAuthSettings())
}

// hashPw wraps bcrypt at MinCost for test fixtures.
func hashPw(t testing.TB, plain string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hashPw: %v", err)
	}
	return string(h)
}

// seedUserPw seeds a user with a bcrypt hash and returns the user ID.
func seedUserPw(repo *mockUserRepo, username, hash string) uuid.UUID {
	u := &domain.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: hash,
	}
	repo.seed(u)
	return u.ID
}

// ─── Register ────────────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	users := newMockUserRepo()
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	u, err := svc.Register(context.Background(), "alice", "Harbor@Test2026!")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("username: want alice, got %s", u.Username)
	}
	if u.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
}

func TestRegister_ShortUsername(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, err := svc.Register(context.Background(), "ab", "Harbor@Test2026!")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestRegister_LongUsername(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, err := svc.Register(context.Background(), strings.Repeat("a", 65), "Harbor@Test2026!")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestRegister_WeakPassword(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, err := svc.Register(context.Background(), "alice", "short")
	if !errors.Is(err, domain.ErrPasswordPolicy) {
		t.Errorf("expected ErrPasswordPolicy, got %v", err)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	users := newMockUserRepo()
	users.seed(&domain.User{ID: uuid.New(), Username: "alice"})
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	_, err := svc.Register(context.Background(), "alice", "Harbor@Test2026!")
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestRegister_TrimsWhitespace(t *testing.T) {
	// After trimming, "  ab  " → "ab" which is only 2 chars (< 3).
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, err := svc.Register(context.Background(), "  ab  ", "Harbor@Test2026!")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput after trim, got %v", err)
	}
}

// ─── Login ───────────────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	users := newMockUserRepo()
	seedUserPw(users, "alice", hashPw(t, "Harbor@Test2026!"))
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	result, err := svc.Login(context.Background(), service.LoginInput{
		Username: "alice",
		Password: "Harbor@Test2026!",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.Session.ID == "" {
		t.Error("expected a session ID")
	}
	if result.User.Username != "alice" {
		t.Errorf("expected alice, got %s", result.User.Username)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, err := svc.Login(context.Background(), service.LoginInput{
		Username: "ghost",
		Password: "Harbor@Test2026!",
	})
	if !errors.Is(err, domain.ErrCredentialInvalid) {
		t.Errorf("expected ErrCredentialInvalid, got %v", err)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	users := newMockUserRepo()
	seedUserPw(users, "alice", hashPw(t, "Harbor@Test2026!"))
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	_, err := svc.Login(context.Background(), service.LoginInput{
		Username: "alice",
		Password: "WrongPassword123!",
	})
	if !errors.Is(err, domain.ErrCredentialInvalid) {
		t.Errorf("expected ErrCredentialInvalid, got %v", err)
	}
}

func TestLogin_LockedAccount(t *testing.T) {
	users := newMockUserRepo()
	lockUntil := time.Now().UTC().Add(15 * time.Minute)
	users.seed(&domain.User{
		ID:           uuid.New(),
		Username:     "alice",
		PasswordHash: hashPw(t, "Harbor@Test2026!"),
		LockedUntil:  &lockUntil,
	})
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	_, err := svc.Login(context.Background(), service.LoginInput{
		Username: "alice",
		Password: "Harbor@Test2026!",
	})
	if !errors.Is(err, domain.ErrLocked) {
		t.Errorf("expected ErrLocked, got %v", err)
	}
}

func TestLogin_WrongPassword_TriggersLockout(t *testing.T) {
	users := newMockUserRepo()
	captchas := newMockCaptchaRepo()
	// At failedAttempts=4, one more wrong attempt crosses threshold (5) → locked.
	// FailedAttempts=4 ≥ CaptchaThreshold=2, so a valid captcha must be provided
	// first, otherwise the service short-circuits with ErrCaptchaRequired.
	users.seed(&domain.User{
		ID:             uuid.New(),
		Username:       "alice",
		PasswordHash:   hashPw(t, "Harbor@Test2026!"),
		FailedAttempts: 4,
	})
	svc := newTestAuthService(users, newMockSessionRepo(), captchas)

	ch, err := svc.IssueCaptcha(context.Background())
	if err != nil {
		t.Fatalf("IssueCaptcha: %v", err)
	}

	_, err = svc.Login(context.Background(), service.LoginInput{
		Username:      "alice",
		Password:      "WrongPassword123!",
		CaptchaToken:  ch.Token,
		CaptchaAnswer: ch.Answer,
	})
	if !errors.Is(err, domain.ErrLocked) {
		t.Errorf("expected ErrLocked after threshold exceeded, got %v", err)
	}
}

func TestLogin_CaptchaRequired_MissingToken(t *testing.T) {
	users := newMockUserRepo()
	users.seed(&domain.User{
		ID:             uuid.New(),
		Username:       "alice",
		PasswordHash:   hashPw(t, "Harbor@Test2026!"),
		FailedAttempts: 2, // ≥ CaptchaThreshold (2) → captcha required
	})
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	_, err := svc.Login(context.Background(), service.LoginInput{
		Username: "alice",
		Password: "Harbor@Test2026!",
		// CaptchaToken and CaptchaAnswer are empty
	})
	if !errors.Is(err, domain.ErrCaptchaRequired) {
		t.Errorf("expected ErrCaptchaRequired, got %v", err)
	}
}

func TestLogin_WithValidCaptcha(t *testing.T) {
	users := newMockUserRepo()
	users.seed(&domain.User{
		ID:             uuid.New(),
		Username:       "alice",
		PasswordHash:   hashPw(t, "Harbor@Test2026!"),
		FailedAttempts: 2,
	})
	captchas := newMockCaptchaRepo()
	captchas.captchas["tok123"] = &domain.CaptchaChallenge{
		Token:     "tok123",
		Answer:    "7",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	svc := newTestAuthService(users, newMockSessionRepo(), captchas)

	result, err := svc.Login(context.Background(), service.LoginInput{
		Username:      "alice",
		Password:      "Harbor@Test2026!",
		CaptchaToken:  "tok123",
		CaptchaAnswer: "7",
	})
	if err != nil {
		t.Fatalf("Login with valid captcha: %v", err)
	}
	if result.Session.ID == "" {
		t.Error("expected session ID")
	}
}

func TestLogin_WithWrongCaptchaAnswer(t *testing.T) {
	users := newMockUserRepo()
	users.seed(&domain.User{
		ID:             uuid.New(),
		Username:       "alice",
		PasswordHash:   hashPw(t, "Harbor@Test2026!"),
		FailedAttempts: 2,
	})
	captchas := newMockCaptchaRepo()
	captchas.captchas["tok456"] = &domain.CaptchaChallenge{
		Token:     "tok456",
		Answer:    "7",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	svc := newTestAuthService(users, newMockSessionRepo(), captchas)

	_, err := svc.Login(context.Background(), service.LoginInput{
		Username:      "alice",
		Password:      "Harbor@Test2026!",
		CaptchaToken:  "tok456",
		CaptchaAnswer: "99", // wrong
	})
	if !errors.Is(err, domain.ErrCaptchaInvalid) {
		t.Errorf("expected ErrCaptchaInvalid, got %v", err)
	}
}

func TestLogin_WithExpiredCaptcha(t *testing.T) {
	users := newMockUserRepo()
	users.seed(&domain.User{
		ID:             uuid.New(),
		Username:       "alice",
		PasswordHash:   hashPw(t, "Harbor@Test2026!"),
		FailedAttempts: 2,
	})
	captchas := newMockCaptchaRepo()
	captchas.captchas["oldtok"] = &domain.CaptchaChallenge{
		Token:     "oldtok",
		Answer:    "7",
		ExpiresAt: time.Now().UTC().Add(-time.Minute), // expired
	}
	svc := newTestAuthService(users, newMockSessionRepo(), captchas)

	_, err := svc.Login(context.Background(), service.LoginInput{
		Username:      "alice",
		Password:      "Harbor@Test2026!",
		CaptchaToken:  "oldtok",
		CaptchaAnswer: "7",
	})
	if !errors.Is(err, domain.ErrCaptchaInvalid) {
		t.Errorf("expected ErrCaptchaInvalid for expired captcha, got %v", err)
	}
}

// ─── ResolveSession ──────────────────────────────────────────────────────────

func TestResolveSession_Valid(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice"})
	sessions := newMockSessionRepo()
	sessions.seed(&domain.Session{
		ID:             "sess-abc",
		UserID:         uid,
		CreatedAt:      time.Now().UTC(),
		LastActivityAt: time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(30 * time.Minute),
	})
	svc := newTestAuthService(users, sessions, newMockCaptchaRepo())

	u, sess, err := svc.ResolveSession(context.Background(), "sess-abc")
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("expected alice, got %s", u.Username)
	}
	if sess.ID != "sess-abc" {
		t.Errorf("expected sess-abc, got %s", sess.ID)
	}
}

func TestResolveSession_EmptyID(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, _, err := svc.ResolveSession(context.Background(), "")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestResolveSession_NotFound(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	_, _, err := svc.ResolveSession(context.Background(), "ghost-session")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestResolveSession_Expired(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice"})
	sessions := newMockSessionRepo()
	sessions.seed(&domain.Session{
		ID:             "old-sess",
		UserID:         uid,
		CreatedAt:      time.Now().UTC().Add(-2 * time.Hour),
		LastActivityAt: time.Now().UTC().Add(-35 * time.Minute),
		ExpiresAt:      time.Now().UTC().Add(-5 * time.Minute), // already past
	})
	svc := newTestAuthService(users, sessions, newMockCaptchaRepo())

	_, _, err := svc.ResolveSession(context.Background(), "old-sess")
	if !errors.Is(err, domain.ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}
}

func TestResolveSession_AbsoluteTTL(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice"})
	sessions := newMockSessionRepo()
	// Session is still active (ExpiresAt in future) but was created 13h ago
	// which exceeds the 12h absolute TTL.
	sessions.seed(&domain.Session{
		ID:             "stale-sess",
		UserID:         uid,
		CreatedAt:      time.Now().UTC().Add(-13 * time.Hour),
		LastActivityAt: time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(30 * time.Minute),
	})
	svc := newTestAuthService(users, sessions, newMockCaptchaRepo())

	_, _, err := svc.ResolveSession(context.Background(), "stale-sess")
	if !errors.Is(err, domain.ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired for absolute TTL, got %v", err)
	}
}

// ─── ChangePassword ──────────────────────────────────────────────────────────

func TestChangePassword_Success(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice", PasswordHash: hashPw(t, "OldPass@2026!")})
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	err := svc.ChangePassword(context.Background(), uid, "OldPass@2026!", "NewPass@2026!")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	u, _ := users.GetByID(context.Background(), uid)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("NewPass@2026!")) != nil {
		t.Error("new password hash not stored")
	}
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice", PasswordHash: hashPw(t, "OldPass@2026!")})
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	err := svc.ChangePassword(context.Background(), uid, "WrongOld@2026!", "NewPass@2026!")
	if !errors.Is(err, domain.ErrCredentialInvalid) {
		t.Errorf("expected ErrCredentialInvalid, got %v", err)
	}
}

func TestChangePassword_WeakNew(t *testing.T) {
	users := newMockUserRepo()
	uid := uuid.New()
	users.seed(&domain.User{ID: uid, Username: "alice", PasswordHash: hashPw(t, "OldPass@2026!")})
	svc := newTestAuthService(users, newMockSessionRepo(), newMockCaptchaRepo())

	err := svc.ChangePassword(context.Background(), uid, "OldPass@2026!", "weak")
	if !errors.Is(err, domain.ErrPasswordPolicy) {
		t.Errorf("expected ErrPasswordPolicy, got %v", err)
	}
}

func TestChangePassword_UserNotFound(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	err := svc.ChangePassword(context.Background(), uuid.New(), "any", "Harbor@New2026!")
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
}

// ─── IssueCaptcha ────────────────────────────────────────────────────────────

func TestIssueCaptcha_Success(t *testing.T) {
	captchas := newMockCaptchaRepo()
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), captchas)

	c, err := svc.IssueCaptcha(context.Background())
	if err != nil {
		t.Fatalf("IssueCaptcha: %v", err)
	}
	if c.Token == "" {
		t.Error("expected non-empty token")
	}
	if c.Question == "" {
		t.Error("expected non-empty question")
	}
	if c.ExpiresAt.IsZero() {
		t.Error("expected non-zero expiry")
	}
	stored, ok := captchas.captchas[c.Token]
	if !ok {
		t.Fatal("captcha not persisted")
	}
	if stored.Answer == "" {
		t.Error("answer should be stored")
	}
}

// ─── Logout ──────────────────────────────────────────────────────────────────

func TestLogout_DeletesSession(t *testing.T) {
	sessions := newMockSessionRepo()
	sessions.seed(&domain.Session{ID: "sess-x"})
	svc := newTestAuthService(newMockUserRepo(), sessions, newMockCaptchaRepo())

	if err := svc.Logout(context.Background(), "sess-x"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := sessions.Get(context.Background(), "sess-x"); !errors.Is(err, domain.ErrNotFound) {
		t.Error("session should be deleted after logout")
	}
}

func TestLogout_EmptyID_NoOp(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout with empty ID should be no-op: %v", err)
	}
}

// ─── Settings ────────────────────────────────────────────────────────────────

func TestAuthService_Settings_ReturnsConfiguredValues(t *testing.T) {
	svc := newTestAuthService(newMockUserRepo(), newMockSessionRepo(), newMockCaptchaRepo())
	s := svc.Settings()
	if s.LockoutThreshold != 5 {
		t.Errorf("LockoutThreshold: want 5, got %d", s.LockoutThreshold)
	}
	if s.CaptchaThreshold != 2 {
		t.Errorf("CaptchaThreshold: want 2, got %d", s.CaptchaThreshold)
	}
	if s.SessionInactivity != 30*time.Minute {
		t.Errorf("SessionInactivity: want 30m, got %v", s.SessionInactivity)
	}
}
