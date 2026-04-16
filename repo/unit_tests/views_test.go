package unit_tests

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/views"
)

// ─── HumanLabel ──────────────────────────────────────────────────────────────

func TestHumanLabel_Empty(t *testing.T) {
	if got := views.HumanLabel(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestHumanLabel_SingleWord(t *testing.T) {
	if got := views.HumanLabel("pending"); got != "Pending" {
		t.Errorf("expected Pending, got %q", got)
	}
}

func TestHumanLabel_SnakeCaseStatuses(t *testing.T) {
	cases := []struct{ input, want string }{
		{"pending_confirmation", "Pending Confirmation"},
		{"checked_in", "Checked In"},
		{"group_buy_threshold_met", "Group Buy Threshold Met"},
		{"canceled", "Canceled"},
		{"waitlisted", "Waitlisted"},
		{"completed", "Completed"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := views.HumanLabel(tc.input)
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestHumanLabel_DashSeparated(t *testing.T) {
	if got := views.HumanLabel("group-buy"); got != "Group Buy" {
		t.Errorf("expected Group Buy, got %q", got)
	}
}

func TestHumanLabel_AllBookingStatuses_NonEmpty(t *testing.T) {
	statuses := []domain.BookingStatus{
		domain.StatusPendingConfirmation,
		domain.StatusWaitlisted,
		domain.StatusCheckedIn,
		domain.StatusCompleted,
		domain.StatusCanceled,
	}
	for _, s := range statuses {
		label := views.HumanLabel(string(s))
		if label == "" {
			t.Errorf("HumanLabel(%q) should not be empty", s)
		}
		if label[0] < 'A' || label[0] > 'Z' {
			t.Errorf("HumanLabel(%q) should start with uppercase, got %q", s, label)
		}
	}
}

// ─── domain.MaskName ─────────────────────────────────────────────────────────

func TestMaskName_Empty(t *testing.T) {
	if got := domain.MaskName(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestMaskName_SingleChar(t *testing.T) {
	if got := domain.MaskName("A"); got != "A*" {
		t.Errorf("expected A*, got %q", got)
	}
}

func TestMaskName_MultiChar(t *testing.T) {
	if got := domain.MaskName("alice"); got != "a****" {
		t.Errorf("expected a****, got %q", got)
	}
}

func TestMaskName_TwoChars(t *testing.T) {
	got := domain.MaskName("ab")
	if got != "a*" {
		t.Errorf("expected a*, got %q", got)
	}
}

func TestMaskName_Unicode(t *testing.T) {
	got := domain.MaskName("Ão")
	runes := []rune(got)
	if runes[0] != 'Ã' {
		t.Errorf("first rune should be Ã, got %c", runes[0])
	}
	if runes[1] != '*' {
		t.Errorf("second rune should be *, got %c", runes[1])
	}
}

// ─── domain.MaskEmail ────────────────────────────────────────────────────────

func TestMaskEmail_Empty(t *testing.T) {
	if got := domain.MaskEmail(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestMaskEmail_ValidEmail_KeepsFirstCharAndDomain(t *testing.T) {
	got := domain.MaskEmail("alice@example.com")
	if len(got) == 0 || got[0] != 'a' {
		t.Errorf("expected first char 'a', got %q", got)
	}
	domain_ := "@example.com"
	if got[len(got)-len(domain_):] != domain_ {
		t.Errorf("expected domain preserved, got %q", got)
	}
}

func TestMaskEmail_SingleCharLocal(t *testing.T) {
	if got := domain.MaskEmail("a@example.com"); got != "a*@example.com" {
		t.Errorf("expected a*@example.com, got %q", got)
	}
}

func TestMaskEmail_NoAtSign_MasksLikeName(t *testing.T) {
	got := domain.MaskEmail("nodomain")
	if got == "" {
		t.Error("expected non-empty for no-@ input")
	}
	if got[0] != 'n' {
		t.Errorf("first char should be n, got %q", string(got[0]))
	}
}

// ─── domain.User helpers ──────────────────────────────────────────────────────

func TestUser_IsLocked_NilLockedUntil(t *testing.T) {
	u := &domain.User{}
	if u.IsLocked(time.Now().UTC()) {
		t.Error("nil LockedUntil should not be locked")
	}
}

func TestUser_IsLocked_PastLock(t *testing.T) {
	past := time.Now().UTC().Add(-15 * time.Minute)
	u := &domain.User{LockedUntil: &past}
	if u.IsLocked(time.Now().UTC()) {
		t.Error("should not be locked when LockedUntil is in the past")
	}
}

func TestUser_IsLocked_FutureLock(t *testing.T) {
	future := time.Now().UTC().Add(15 * time.Minute)
	u := &domain.User{LockedUntil: &future}
	if !u.IsLocked(time.Now().UTC()) {
		t.Error("should be locked when LockedUntil is in the future")
	}
}

func TestUser_CaptchaRequired_BelowThreshold(t *testing.T) {
	for _, attempts := range []int{0, 1} {
		u := &domain.User{FailedAttempts: attempts}
		if u.CaptchaRequired() {
			t.Errorf("FailedAttempts=%d: should not require captcha", attempts)
		}
	}
}

func TestUser_CaptchaRequired_AtOrAboveThreshold(t *testing.T) {
	for _, attempts := range []int{2, 5, 10} {
		u := &domain.User{FailedAttempts: attempts}
		if !u.CaptchaRequired() {
			t.Errorf("FailedAttempts=%d: should require captcha", attempts)
		}
	}
}

// ─── domain.BookingStatus helpers ────────────────────────────────────────────

func TestBookingStatus_IsActive(t *testing.T) {
	active := []domain.BookingStatus{
		domain.StatusPendingConfirmation,
		domain.StatusWaitlisted,
		domain.StatusCheckedIn,
	}
	for _, s := range active {
		if !s.IsActive() {
			t.Errorf("expected %s to be active", s)
		}
	}
	for _, s := range []domain.BookingStatus{domain.StatusCompleted, domain.StatusCanceled} {
		if s.IsActive() {
			t.Errorf("expected %s to be inactive", s)
		}
	}
}

func TestBookingStatus_IsTerminal(t *testing.T) {
	if !domain.StatusCompleted.IsTerminal() {
		t.Error("completed should be terminal")
	}
	if !domain.StatusCanceled.IsTerminal() {
		t.Error("canceled should be terminal")
	}
	if domain.StatusCheckedIn.IsTerminal() {
		t.Error("checked_in should not be terminal")
	}
	if domain.StatusPendingConfirmation.IsTerminal() {
		t.Error("pending_confirmation should not be terminal")
	}
}

// ─── domain.GroupReservation helpers ─────────────────────────────────────────

func TestGroupReservation_CurrentHeadcount_ExcludesCanceled(t *testing.T) {
	g := &domain.GroupReservation{
		Bookings: []domain.Booking{
			{PartySize: 3, Status: domain.StatusCheckedIn},
			{PartySize: 2, Status: domain.StatusCanceled}, // excluded
			{PartySize: 1, Status: domain.StatusPendingConfirmation},
		},
	}
	if got := g.CurrentHeadcount(); got != 4 {
		t.Errorf("expected headcount 4, got %d", got)
	}
}

func TestGroupReservation_CurrentHeadcount_Empty(t *testing.T) {
	g := &domain.GroupReservation{}
	if got := g.CurrentHeadcount(); got != 0 {
		t.Errorf("expected 0 for empty group, got %d", got)
	}
}

func TestGroupReservation_MaskedView_MasksNamesAndEmails(t *testing.T) {
	g := domain.GroupReservation{
		OrganizerName:  "Alice Smith",
		OrganizerEmail: "alice@example.com",
		Capacity:       10,
	}
	masked := g.MaskedView()
	if masked.OrganizerName == "Alice Smith" {
		t.Error("organizer name should be masked")
	}
	if masked.OrganizerEmail == "alice@example.com" {
		t.Error("organizer email should be masked")
	}
	// Capacity and other non-PII fields are unchanged.
	if masked.Capacity != 10 {
		t.Errorf("capacity should be unchanged, got %d", masked.Capacity)
	}
}

func TestGroupReservation_Validate_MissingName(t *testing.T) {
	g := &domain.GroupReservation{OrganizerEmail: "a@b.com", Capacity: 10}
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestGroupReservation_Validate_MissingEmail(t *testing.T) {
	g := &domain.GroupReservation{Name: "Group", Capacity: 10}
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for missing email")
	}
}

func TestGroupReservation_Validate_ZeroCapacity(t *testing.T) {
	g := &domain.GroupReservation{Name: "Group", OrganizerEmail: "a@b.com", Capacity: 0}
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for zero capacity")
	}
}

func TestGroupReservation_Validate_Valid(t *testing.T) {
	g := &domain.GroupReservation{Name: "Group", OrganizerEmail: "a@b.com", Capacity: 10}
	if err := g.Validate(); err != nil {
		t.Fatalf("expected no error for valid group: %v", err)
	}
}

// ─── domain.Booking.Validate ─────────────────────────────────────────────────

func TestBooking_Validate_MissingUserID(t *testing.T) {
	b := &domain.Booking{
		ResourceID: testUUID(),
		PartySize:  1,
		StartTime:  time.Now().Add(time.Hour),
		EndTime:    time.Now().Add(2 * time.Hour),
	}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for missing UserID")
	}
}

func TestBooking_Validate_MissingResourceID(t *testing.T) {
	b := &domain.Booking{
		UserID:    testUUID(),
		PartySize: 1,
		StartTime: time.Now().Add(time.Hour),
		EndTime:   time.Now().Add(2 * time.Hour),
	}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for missing ResourceID")
	}
}

func TestBooking_Validate_ZeroPartySize(t *testing.T) {
	b := &domain.Booking{
		UserID:     testUUID(),
		ResourceID: testUUID(),
		PartySize:  0,
		StartTime:  time.Now().Add(time.Hour),
		EndTime:    time.Now().Add(2 * time.Hour),
	}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for zero party size")
	}
}

func TestBooking_Validate_EndBeforeStart(t *testing.T) {
	start := time.Now().Add(2 * time.Hour)
	b := &domain.Booking{
		UserID:     testUUID(),
		ResourceID: testUUID(),
		PartySize:  1,
		StartTime:  start,
		EndTime:    start.Add(-time.Minute),
	}
	if err := b.Validate(); err == nil {
		t.Fatal("expected error for end before start")
	}
}

func TestBooking_Validate_Valid(t *testing.T) {
	b := &domain.Booking{
		UserID:     testUUID(),
		ResourceID: testUUID(),
		PartySize:  1,
		StartTime:  time.Now().Add(time.Hour),
		EndTime:    time.Now().Add(2 * time.Hour),
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("expected valid booking: %v", err)
	}
}

// testUUID returns a new random UUID for use in test fixtures.
func testUUID() uuid.UUID { return uuid.New() }
