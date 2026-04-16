package unit_tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

func newTestBookingService(bookings *mockBookingRepo, resources *mockResourceRepo, users *mockUserRepo, enc *stubEncrypter) *service.BookingService {
	return service.NewBookingService(bookings, resources, users, enc, slog.Default(), service.DefaultBookingPolicy())
}

// makeResource returns a seeded resource with the given capacity.
func makeResource(repo *mockResourceRepo, capacity int) *domain.Resource {
	r := &domain.Resource{
		ID:       uuid.New(),
		Name:     "Test Slip",
		Capacity: capacity,
	}
	repo.seed(r)
	return r
}

// makeUser seeds a normal (non-blacklisted) user.
func makeUser(repo *mockUserRepo) *domain.User {
	u := &domain.User{ID: uuid.New(), Username: "testuser"}
	repo.seed(u)
	return u
}

// futureWindow returns a [start, end] pair that satisfies the 2h lead time.
func futureWindow() (time.Time, time.Time) {
	start := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Hour)
	end := start.Add(time.Hour)
	return start, end
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_Success(t *testing.T) {
	users := newMockUserRepo()
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	u := makeUser(users)
	r := makeResource(resources, 4)
	start, end := futureWindow()

	svc := newTestBookingService(bookings, resources, users, nil)
	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		PartySize:  1,
		StartTime:  start,
		EndTime:    end,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Status != domain.StatusPendingConfirmation {
		t.Errorf("status: want pending_confirmation, got %s", b.Status)
	}
	if b.UserID != u.ID {
		t.Error("UserID mismatch")
	}
}

func TestCreate_BlacklistedUser(t *testing.T) {
	users := newMockUserRepo()
	users.seed(&domain.User{ID: uuid.New(), Username: "bad", IsBlacklisted: true, BlacklistReason: "spam"})
	badUser, _ := users.GetByUsername(context.Background(), "bad")
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	start, end := futureWindow()

	svc := newTestBookingService(newMockBookingRepo(), resources, users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     badUser.ID,
		ResourceID: r.ID,
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrBlacklisted) {
		t.Errorf("expected ErrBlacklisted, got %v", err)
	}
}

func TestCreate_ResourceNotFound(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	start, end := futureWindow()

	svc := newTestBookingService(newMockBookingRepo(), newMockResourceRepo(), users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: uuid.New(), // not in repo
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCreate_EndBeforeStart(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	start := time.Now().UTC().Add(3 * time.Hour)
	end := start.Add(-time.Minute) // end before start

	svc := newTestBookingService(newMockBookingRepo(), resources, users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCreate_PartySizeTooLarge(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 2) // capacity = 2
	start, end := futureWindow()

	svc := newTestBookingService(newMockBookingRepo(), resources, users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		PartySize:  5, // > capacity
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrCapacityExceed) {
		t.Errorf("expected ErrCapacityExceed, got %v", err)
	}
}

func TestCreate_LeadTimeViolation(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	// Only 30 minutes from now — violates 2h lead time.
	start := time.Now().UTC().Add(30 * time.Minute)
	end := start.Add(time.Hour)

	svc := newTestBookingService(newMockBookingRepo(), resources, users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrLeadTime) {
		t.Errorf("expected ErrLeadTime, got %v", err)
	}
}

func TestCreate_DailyLimitExceeded(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	bookings := newMockBookingRepo()
	bookings.countActiveReturn = 3 // at cap (MaxActivePerDay = 3)
	start, end := futureWindow()

	svc := newTestBookingService(bookings, resources, users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrDailyLimit) {
		t.Errorf("expected ErrDailyLimit, got %v", err)
	}
}

func TestCreate_OverlapBlocked(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	bookings := newMockBookingRepo()
	bookings.userOverlapReturn = true // user already has an overlapping booking
	start, end := futureWindow()

	svc := newTestBookingService(bookings, resources, users, nil)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		StartTime:  start,
		EndTime:    end,
	})
	if !errors.Is(err, domain.ErrOverlap) {
		t.Errorf("expected ErrOverlap, got %v", err)
	}
}

func TestCreate_WaitlistWhenCapacityFull(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 2) // capacity = 2
	bookings := newMockBookingRepo()
	// Simulate 2 seats already occupied for this window.
	resources.sumActiveParty = 2 // capacity - 2 = 0 remaining

	start, end := futureWindow()
	svc := newTestBookingService(bookings, resources, users, nil)
	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID:     u.ID,
		ResourceID: r.ID,
		PartySize:  1,
		StartTime:  start,
		EndTime:    end,
	})
	if err != nil {
		t.Fatalf("Create should succeed with waitlist: %v", err)
	}
	if b.Status != domain.StatusWaitlisted {
		t.Errorf("expected waitlisted, got %s", b.Status)
	}
}

func TestCreate_WithSecureNotesEncryption(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	bookings := newMockBookingRepo()
	enc := &stubEncrypter{}
	start, end := futureWindow()

	svc := newTestBookingService(bookings, resources, users, enc)
	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID:      u.ID,
		ResourceID:  r.ID,
		StartTime:   start,
		EndTime:     end,
		SecureNotes: "secret notes here",
	})
	if err != nil {
		t.Fatalf("Create with secure notes: %v", err)
	}
	if len(b.SecureNotes) == 0 {
		t.Error("expected encrypted notes to be stored")
	}
}

func TestCreate_SecureNotes_EncryptionFails(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	r := makeResource(resources, 4)
	start, end := futureWindow()

	// encrypter that always returns an error simulates encryption failure.
	enc := &stubEncrypter{encryptErr: domain.ErrInvalidInput}
	svc := newTestBookingService(newMockBookingRepo(), resources, users, enc)
	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID:      u.ID,
		ResourceID:  r.ID,
		StartTime:   start,
		EndTime:     end,
		SecureNotes: "secret",
	})
	if err == nil {
		t.Fatal("expected error when encryption fails")
	}
}

// ─── GetForOwner ─────────────────────────────────────────────────────────────

func TestGetForOwner_Owner_GetDecryptedNotes(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	enc := &stubEncrypter{}
	start, end := futureWindow()

	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, enc)

	// Create a booking with secure notes.
	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID:      u.ID,
		ResourceID:  makeResource(resources, 4).ID,
		StartTime:   start,
		EndTime:     end,
		SecureNotes: "private note",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Owner fetches: notes should be decrypted.
	got, err := svc.GetForOwner(context.Background(), u.ID, b.ID)
	if err != nil {
		t.Fatalf("GetForOwner: %v", err)
	}
	if got.SecureNotesPlain != "private note" {
		t.Errorf("expected decrypted notes, got %q", got.SecureNotesPlain)
	}
}

func TestGetForOwner_NonOwner_NoNotes(t *testing.T) {
	users := newMockUserRepo()
	owner := makeUser(users)
	other := makeUser(users)
	resources := newMockResourceRepo()
	enc := &stubEncrypter{}
	start, end := futureWindow()

	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, enc)

	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID:      owner.ID,
		ResourceID:  makeResource(resources, 4).ID,
		StartTime:   start,
		EndTime:     end,
		SecureNotes: "private note",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Another user fetches: SecureNotesPlain should remain empty.
	got, err := svc.GetForOwner(context.Background(), other.ID, b.ID)
	if err != nil {
		t.Fatalf("GetForOwner: %v", err)
	}
	if got.SecureNotesPlain != "" {
		t.Errorf("non-owner should not see decrypted notes, got %q", got.SecureNotesPlain)
	}
}

// ─── Transition ──────────────────────────────────────────────────────────────

func TestTransition_PendingToCheckedIn(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, nil)
	start, end := futureWindow()

	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID: u.ID, ResourceID: makeResource(resources, 4).ID,
		StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Transition(context.Background(), u.ID, b.ID, domain.StatusCheckedIn)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if updated.Status != domain.StatusCheckedIn {
		t.Errorf("expected checked_in, got %s", updated.Status)
	}
}

func TestTransition_WrongActor(t *testing.T) {
	users := newMockUserRepo()
	owner := makeUser(users)
	other := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, nil)
	start, end := futureWindow()

	b, err := svc.Create(context.Background(), service.CreateInput{
		UserID: owner.ID, ResourceID: makeResource(resources, 4).ID,
		StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Transition(context.Background(), other.ID, b.ID, domain.StatusCheckedIn)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestTransition_InvalidTransition(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, nil)
	start, end := futureWindow()

	// Create booking (pending_confirmation), then check-in.
	b, _ := svc.Create(context.Background(), service.CreateInput{
		UserID: u.ID, ResourceID: makeResource(resources, 4).ID,
		StartTime: start, EndTime: end,
	})
	svc.Transition(context.Background(), u.ID, b.ID, domain.StatusCheckedIn)

	// Trying to go back to pending_confirmation from checked_in is illegal.
	_, err := svc.Transition(context.Background(), u.ID, b.ID, domain.StatusPendingConfirmation)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestTransition_CutoffWindow(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()

	// Create the booking directly in the repo, starting in 5 minutes (within 10m cutoff).
	bookingID := uuid.New()
	bookings.bookings[bookingID] = &domain.Booking{
		ID:         bookingID,
		UserID:     u.ID,
		ResourceID: makeResource(resources, 4).ID,
		Status:     domain.StatusPendingConfirmation,
		StartTime:  time.Now().UTC().Add(5 * time.Minute), // inside 10-minute cutoff
		EndTime:    time.Now().UTC().Add(65 * time.Minute),
		PartySize:  1,
	}

	svc := newTestBookingService(bookings, resources, users, nil)
	_, err := svc.Transition(context.Background(), u.ID, bookingID, domain.StatusCanceled)
	if !errors.Is(err, domain.ErrCutoff) {
		t.Errorf("expected ErrCutoff, got %v", err)
	}
}

func TestTransition_CancelOutsideCutoff(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, nil)
	start, end := futureWindow() // 3h from now — well outside cutoff

	b, _ := svc.Create(context.Background(), service.CreateInput{
		UserID: u.ID, ResourceID: makeResource(resources, 4).ID,
		StartTime: start, EndTime: end,
	})
	updated, err := svc.Transition(context.Background(), u.ID, b.ID, domain.StatusCanceled)
	if err != nil {
		t.Fatalf("cancel outside cutoff should succeed: %v", err)
	}
	if updated.Status != domain.StatusCanceled {
		t.Errorf("expected canceled, got %s", updated.Status)
	}
}

// ─── Policy ──────────────────────────────────────────────────────────────────

func TestBookingService_Policy_DefaultValues(t *testing.T) {
	svc := newTestBookingService(newMockBookingRepo(), newMockResourceRepo(), newMockUserRepo(), nil)
	p := svc.Policy()
	if p.MinLeadTime != 2*time.Hour {
		t.Errorf("MinLeadTime: want 2h, got %v", p.MinLeadTime)
	}
	if p.MaxActivePerDay != 3 {
		t.Errorf("MaxActivePerDay: want 3, got %d", p.MaxActivePerDay)
	}
	if p.ChangeCutoff != 10*time.Minute {
		t.Errorf("ChangeCutoff: want 10m, got %v", p.ChangeCutoff)
	}
}

// ─── ListByUser / Get ────────────────────────────────────────────────────────

func TestListByUser_ReturnsOwnerBookings(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, nil)
	start, end := futureWindow()

	_, err := svc.Create(context.Background(), service.CreateInput{
		UserID: u.ID, ResourceID: makeResource(resources, 4).ID,
		StartTime: start, EndTime: end,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := svc.ListByUser(context.Background(), u.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 booking, got %d", len(list))
	}
}

func TestGet_ExistingBooking(t *testing.T) {
	users := newMockUserRepo()
	u := makeUser(users)
	resources := newMockResourceRepo()
	bookings := newMockBookingRepo()
	svc := newTestBookingService(bookings, resources, users, nil)
	start, end := futureWindow()

	b, _ := svc.Create(context.Background(), service.CreateInput{
		UserID: u.ID, ResourceID: makeResource(resources, 4).ID,
		StartTime: start, EndTime: end,
	})

	got, err := svc.Get(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != b.ID {
		t.Errorf("ID mismatch: want %s, got %s", b.ID, got.ID)
	}
}
