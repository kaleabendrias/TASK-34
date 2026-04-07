package unit_tests

import (
	"sort"
	"testing"

	"github.com/harborworks/booking-hub/internal/domain"
)

// allowed lists every legal transition that the state machine must permit.
// Anything outside this set must be rejected.
var allowed = []struct {
	from, to domain.BookingStatus
}{
	{domain.StatusPendingConfirmation, domain.StatusWaitlisted},
	{domain.StatusPendingConfirmation, domain.StatusCheckedIn},
	{domain.StatusPendingConfirmation, domain.StatusCanceled},
	{domain.StatusWaitlisted, domain.StatusPendingConfirmation},
	{domain.StatusWaitlisted, domain.StatusCanceled},
	{domain.StatusCheckedIn, domain.StatusCompleted},
	{domain.StatusCheckedIn, domain.StatusCanceled},
}

func TestCanTransition_Allowed(t *testing.T) {
	for _, tr := range allowed {
		if !domain.CanTransition(tr.from, tr.to) {
			t.Errorf("expected %s -> %s to be allowed", tr.from, tr.to)
		}
	}
}

func TestCanTransition_DeniedAndTerminal(t *testing.T) {
	// Terminal states allow nothing.
	terminals := []domain.BookingStatus{domain.StatusCompleted, domain.StatusCanceled}
	all := []domain.BookingStatus{
		domain.StatusPendingConfirmation,
		domain.StatusWaitlisted,
		domain.StatusCheckedIn,
		domain.StatusCompleted,
		domain.StatusCanceled,
	}
	for _, from := range terminals {
		for _, to := range all {
			if domain.CanTransition(from, to) {
				t.Errorf("terminal %s -> %s should be denied", from, to)
			}
		}
	}
	// Unknown source state denied.
	if domain.CanTransition("nonsense", domain.StatusCanceled) {
		t.Error("unknown source should be denied")
	}
	// Pending → Completed is not direct; must go via CheckedIn.
	if domain.CanTransition(domain.StatusPendingConfirmation, domain.StatusCompleted) {
		t.Error("direct pending->completed should be denied")
	}
	// Waitlisted -> CheckedIn must go via PendingConfirmation.
	if domain.CanTransition(domain.StatusWaitlisted, domain.StatusCheckedIn) {
		t.Error("waitlisted->checked_in should be denied")
	}
}

func TestAllowedNext(t *testing.T) {
	got := domain.AllowedNext(domain.StatusPendingConfirmation)
	want := []string{string(domain.StatusCanceled), string(domain.StatusCheckedIn), string(domain.StatusWaitlisted)}
	gotS := make([]string, len(got))
	for i, s := range got {
		gotS[i] = string(s)
	}
	sort.Strings(gotS)
	sort.Strings(want)
	if len(gotS) != len(want) {
		t.Fatalf("len mismatch: %v vs %v", gotS, want)
	}
	for i := range gotS {
		if gotS[i] != want[i] {
			t.Fatalf("%v != %v", gotS, want)
		}
	}
	if got := domain.AllowedNext(domain.StatusCompleted); len(got) != 0 {
		t.Errorf("terminal should have no next, got %v", got)
	}
}
