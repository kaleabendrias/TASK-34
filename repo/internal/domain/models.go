package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Domain-level errors. Layers above translate these to HTTP statuses.
var (
	ErrNotFound          = errors.New("resource not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrCapacityExceed    = errors.New("capacity exceeded")
	ErrConflict          = errors.New("conflicting state")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrBlacklisted       = errors.New("user is blacklisted")
	ErrLocked            = errors.New("account temporarily locked")
	ErrCaptchaRequired   = errors.New("captcha required")
	ErrCaptchaInvalid    = errors.New("captcha answer is invalid")
	ErrPasswordPolicy    = errors.New("password does not meet policy")
	ErrCredentialInvalid = errors.New("invalid credentials")
	ErrSessionExpired    = errors.New("session expired")
	ErrIdempotencyMismatch = errors.New("idempotency key was reused with a different request")
	ErrOversold            = errors.New("insufficient remaining capacity")
	ErrOptimisticLock      = errors.New("optimistic lock conflict, please retry")
	ErrDeadlinePassed      = errors.New("group buy deadline has passed")
	ErrAlreadyJoined       = errors.New("user already joined this group buy")
	ErrLeadTime            = errors.New("booking violates minimum lead time")
	ErrCutoff            = errors.New("booking change blocked by cutoff window")
	ErrDailyLimit        = errors.New("daily active booking limit exceeded")
	ErrOverlap           = errors.New("booking overlaps an existing reservation")
	ErrInvalidTransition = errors.New("invalid status transition")
)

// ---------- USER ----------

type User struct {
	ID              uuid.UUID  `json:"id"`
	Username        string     `json:"username"`
	PasswordHash    string     `json:"-"`
	IsBlacklisted   bool       `json:"is_blacklisted"`
	BlacklistReason string     `json:"blacklist_reason,omitempty"`
	IsAdmin         bool       `json:"is_admin"`
	AnonymizedAt    *time.Time `json:"anonymized_at,omitempty"`
	FailedAttempts  int        `json:"-"`
	LockedUntil     *time.Time `json:"-"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// MaskName returns a privacy-preserving version of a username for "shared
// views". For multi-character names, all but the first character are replaced
// with asterisks. Field-level masking is centralised here so callers cannot
// forget to apply it.
func MaskName(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	if len(runes) == 1 {
		return string(runes[0]) + "*"
	}
	out := make([]rune, len(runes))
	out[0] = runes[0]
	for i := 1; i < len(runes); i++ {
		out[i] = '*'
	}
	return string(out)
}

// CaptchaRequired reports whether the user must solve a CAPTCHA on the next
// login attempt. Triggered from the 3rd attempt onward (failed_attempts >= 2).
func (u *User) CaptchaRequired() bool {
	return u.FailedAttempts >= 2
}

// IsLocked reports whether the user is currently inside the lockout window.
func (u *User) IsLocked(now time.Time) bool {
	return u.LockedUntil != nil && u.LockedUntil.After(now)
}

// ---------- SESSION ----------

type Session struct {
	ID             string    `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	CreatedAt      time.Time `json:"created_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	UserAgent      string    `json:"user_agent,omitempty"`
	IP             string    `json:"ip,omitempty"`
}

// ---------- CAPTCHA ----------

type CaptchaChallenge struct {
	Token     string    `json:"token"`
	Question  string    `json:"question"`
	Answer    string    `json:"-"`
	CreatedAt time.Time `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	Consumed  bool      `json:"-"`
}

// ---------- RESOURCE ----------

type Resource struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Capacity    int       `json:"capacity"`
	CreatedAt   time.Time `json:"created_at"`
}

// ---------- BOOKING ----------

type BookingStatus string

const (
	StatusPendingConfirmation BookingStatus = "pending_confirmation"
	StatusWaitlisted          BookingStatus = "waitlisted"
	StatusCheckedIn           BookingStatus = "checked_in"
	StatusCompleted           BookingStatus = "completed"
	StatusCanceled            BookingStatus = "canceled"
)

// IsActive reports whether a booking still consumes inventory and counts
// against per-user daily limits.
func (s BookingStatus) IsActive() bool {
	switch s {
	case StatusPendingConfirmation, StatusWaitlisted, StatusCheckedIn:
		return true
	}
	return false
}

func (s BookingStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCanceled
}

type Booking struct {
	ID         uuid.UUID     `json:"id"`
	UserID     uuid.UUID     `json:"user_id"`
	ResourceID uuid.UUID     `json:"resource_id"`
	GroupID    *uuid.UUID    `json:"group_id,omitempty"`
	PartySize  int           `json:"party_size"`
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Status     BookingStatus `json:"status"`
	Notes      string        `json:"notes,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// Validate enforces invariants that hold regardless of persistence layer.
func (b *Booking) Validate() error {
	if b.UserID == uuid.Nil {
		return errors.Join(ErrInvalidInput, errors.New("user_id is required"))
	}
	if b.ResourceID == uuid.Nil {
		return errors.Join(ErrInvalidInput, errors.New("resource_id is required"))
	}
	if b.PartySize <= 0 {
		return errors.Join(ErrInvalidInput, errors.New("party_size must be > 0"))
	}
	if !b.EndTime.After(b.StartTime) {
		return errors.Join(ErrInvalidInput, errors.New("end_time must be after start_time"))
	}
	return nil
}

// ---------- GROUP RESERVATION ----------

type GroupReservation struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	OrganizerName  string    `json:"organizer_name"`
	OrganizerEmail string    `json:"organizer_email"`
	Capacity       int       `json:"capacity"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	Bookings []Booking `json:"bookings,omitempty"`
}

func (g *GroupReservation) Validate() error {
	if g.Name == "" {
		return errors.Join(ErrInvalidInput, errors.New("name is required"))
	}
	if g.OrganizerEmail == "" {
		return errors.Join(ErrInvalidInput, errors.New("organizer_email is required"))
	}
	if g.Capacity <= 0 {
		return errors.Join(ErrInvalidInput, errors.New("capacity must be > 0"))
	}
	return nil
}

// CurrentHeadcount sums non-canceled child bookings.
func (g *GroupReservation) CurrentHeadcount() int {
	total := 0
	for _, b := range g.Bookings {
		if b.Status == StatusCanceled {
			continue
		}
		total += b.PartySize
	}
	return total
}
