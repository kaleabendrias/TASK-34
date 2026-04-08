package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// AuthSettings groups all timing knobs for authentication. They are constants
// in the spec, but exposing them as a struct keeps the service testable.
type AuthSettings struct {
	SessionInactivity time.Duration // 30m: time-since-last-activity that ends a session
	SessionAbsoluteTTL time.Duration // hard upper bound on a session's lifespan
	LockoutThreshold  int           // 5: failed attempts before lockout
	LockoutDuration   time.Duration // 15m
	CaptchaThreshold  int           // 2: failed attempts after which captcha is required
	CaptchaTTL        time.Duration // 5m
	BcryptCost        int           // 10–12 typical
	CookieSecure      bool          // emit Secure flag on session cookies
}

func DefaultAuthSettings() AuthSettings {
	return AuthSettings{
		SessionInactivity:  30 * time.Minute,
		SessionAbsoluteTTL: 12 * time.Hour,
		LockoutThreshold:   5,
		LockoutDuration:    15 * time.Minute,
		CaptchaThreshold:   2,
		CaptchaTTL:         5 * time.Minute,
		BcryptCost:         bcrypt.DefaultCost,
		CookieSecure:       true,
	}
}

type AuthService struct {
	users    repository.UserRepository
	sessions repository.SessionRepository
	captchas repository.CaptchaRepository
	log      *slog.Logger
	cfg      AuthSettings
}

func NewAuthService(
	users repository.UserRepository,
	sessions repository.SessionRepository,
	captchas repository.CaptchaRepository,
	log *slog.Logger,
	cfg AuthSettings,
) *AuthService {
	return &AuthService{users: users, sessions: sessions, captchas: captchas, log: log, cfg: cfg}
}

// Settings exposes the configured timings (read-only).
func (s *AuthService) Settings() AuthSettings { return s.cfg }

// ---------- registration ----------

func (s *AuthService) Register(ctx context.Context, username, password string) (*domain.User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return nil, errors.Join(domain.ErrInvalidInput, errors.New("username must be 3–64 characters"))
	}
	if err := domain.ValidatePassword(password); err != nil {
		return nil, err
	}

	if existing, err := s.users.GetByUsername(ctx, username); err == nil && existing != nil {
		return nil, errors.Join(domain.ErrConflict, errors.New("username is already taken"))
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &domain.User{
		Username:     username,
		PasswordHash: string(hash),
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	s.log.Info("user registered", "user_id", u.ID, "username", u.Username)
	return u, nil
}

// ---------- login ----------

// LoginInput captures everything the login endpoint may receive.
type LoginInput struct {
	Username      string
	Password      string
	CaptchaToken  string
	CaptchaAnswer string
	UserAgent     string
	IP            string
}

// LoginResult is returned on successful authentication.
type LoginResult struct {
	Session *domain.Session
	User    *domain.User
}

// Login validates credentials and produces a session. It implements:
//   - lockout (5 failed attempts → 15 minute block)
//   - CAPTCHA from the 3rd attempt
//   - failed-attempt accounting
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	now := time.Now().UTC()
	in.Username = strings.TrimSpace(in.Username)

	user, err := s.users.GetByUsername(ctx, in.Username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Constant-time-ish: still consume bcrypt cost on a dummy hash
			_ = bcrypt.CompareHashAndPassword(
				[]byte("$2a$10$abcdefghijklmnopqrstuv0123456789ABCDEFGHIJKLMNOPQRSTUV"),
				[]byte(in.Password),
			)
			return nil, domain.ErrCredentialInvalid
		}
		return nil, err
	}

	if user.IsLocked(now) {
		return nil, errors.Join(domain.ErrLocked,
			fmt.Errorf("locked until %s", user.LockedUntil.Format(time.RFC3339)))
	}

	// CAPTCHA gate. Required from the 3rd attempt onward.
	if user.CaptchaRequired() {
		if in.CaptchaToken == "" || in.CaptchaAnswer == "" {
			return nil, domain.ErrCaptchaRequired
		}
		if err := s.verifyCaptcha(ctx, in.CaptchaToken, in.CaptchaAnswer, now); err != nil {
			return nil, err
		}
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		// Failed credentials: increment counter and (maybe) lock.
		var lockUntil *time.Time
		if user.FailedAttempts+1 >= s.cfg.LockoutThreshold {
			until := now.Add(s.cfg.LockoutDuration)
			lockUntil = &until
		}
		if err := s.users.RecordFailedLogin(ctx, user.ID, lockUntil); err != nil {
			s.log.Error("record failed login", "error", err, "user_id", user.ID)
		}
		s.log.Warn("login failed",
			"username", in.Username,
			"failed_attempts_was", user.FailedAttempts,
			"locked_after", lockUntil != nil)
		if lockUntil != nil {
			return nil, errors.Join(domain.ErrLocked,
				fmt.Errorf("locked until %s", lockUntil.Format(time.RFC3339)))
		}
		return nil, domain.ErrCredentialInvalid
	}

	// Success: reset counters and create a session.
	if err := s.users.ResetFailedLogin(ctx, user.ID, now); err != nil {
		s.log.Error("reset login counter", "error", err, "user_id", user.ID)
	}
	user.FailedAttempts = 0
	user.LockedUntil = nil
	user.LastLoginAt = &now

	sessID, err := generateToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}
	sess := &domain.Session{
		ID:             sessID,
		UserID:         user.ID,
		CreatedAt:      now,
		LastActivityAt: now,
		ExpiresAt:      now.Add(s.cfg.SessionInactivity),
		UserAgent:      in.UserAgent,
		IP:             in.IP,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}
	s.log.Info("login success", "user_id", user.ID, "username", user.Username, "session_id", sess.ID[:8]+"...")
	return &LoginResult{Session: sess, User: user}, nil
}

// ResolveSession validates a session token, enforces inactivity timeout, and
// slides the expiry forward. Returns the user attached to the session.
func (s *AuthService) ResolveSession(ctx context.Context, sessionID string) (*domain.User, *domain.Session, error) {
	if sessionID == "" {
		return nil, nil, domain.ErrUnauthorized
	}
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, domain.ErrUnauthorized
		}
		return nil, nil, err
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		_ = s.sessions.Delete(ctx, sessionID)
		return nil, nil, domain.ErrSessionExpired
	}
	// Absolute lifetime cap
	if now.Sub(sess.CreatedAt) > s.cfg.SessionAbsoluteTTL {
		_ = s.sessions.Delete(ctx, sessionID)
		return nil, nil, domain.ErrSessionExpired
	}

	user, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, nil, err
	}

	// Slide the inactivity window forward.
	newExpires := now.Add(s.cfg.SessionInactivity)
	if err := s.sessions.Touch(ctx, sess.ID, now, newExpires); err != nil {
		s.log.Warn("touch session", "error", err)
	}
	sess.LastActivityAt = now
	sess.ExpiresAt = newExpires
	return user, sess, nil
}

// ChangePassword validates the current password, the new password against
// the policy, and bcrypt-hashes the new value before storing it. The
// must_rotate_password flag is cleared as a side effect of UpdatePasswordHash.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
		return domain.ErrCredentialInvalid
	}
	if err := domain.ValidatePassword(next); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	if err := s.users.UpdatePasswordHash(ctx, userID, string(hash)); err != nil {
		return err
	}
	s.log.Info("password rotated", "user_id", userID)
	return nil
}

func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return s.sessions.Delete(ctx, sessionID)
}

// ---------- captcha ----------

// IssueCaptcha mints a new math challenge "What is X + Y?" and returns the
// public token+question. The answer is stored server-side only.
func (s *AuthService) IssueCaptcha(ctx context.Context) (*domain.CaptchaChallenge, error) {
	a, err := randInt(1, 9)
	if err != nil {
		return nil, err
	}
	b, err := randInt(1, 9)
	if err != nil {
		return nil, err
	}
	tok, err := generateToken(16)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	c := &domain.CaptchaChallenge{
		Token:     tok,
		Question:  fmt.Sprintf("What is %d + %d?", a, b),
		Answer:    fmt.Sprintf("%d", a+b),
		CreatedAt: now,
		ExpiresAt: now.Add(s.cfg.CaptchaTTL),
	}
	if err := s.captchas.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *AuthService) verifyCaptcha(ctx context.Context, token, answer string, now time.Time) error {
	c, err := s.captchas.Get(ctx, token)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrCaptchaInvalid
		}
		return err
	}
	if c.Consumed || now.After(c.ExpiresAt) {
		return domain.ErrCaptchaInvalid
	}
	if strings.TrimSpace(answer) != c.Answer {
		// Burn the challenge so it can't be brute-forced.
		_ = s.captchas.Consume(ctx, token)
		return domain.ErrCaptchaInvalid
	}
	if err := s.captchas.Consume(ctx, token); err != nil {
		return err
	}
	return nil
}

// ---------- helpers ----------

func generateToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randInt(min, max int64) (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(max-min+1))
	if err != nil {
		return 0, err
	}
	return n.Int64() + min, nil
}
