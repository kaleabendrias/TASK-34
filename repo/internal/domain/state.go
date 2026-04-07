package domain

// allowedTransitions is the authoritative booking state machine. Every
// transition the system permits must appear here; everything else is rejected
// with ErrInvalidTransition.
var allowedTransitions = map[BookingStatus]map[BookingStatus]struct{}{
	StatusPendingConfirmation: {
		StatusWaitlisted: {},
		StatusCheckedIn:  {},
		StatusCanceled:   {},
	},
	StatusWaitlisted: {
		StatusPendingConfirmation: {},
		StatusCanceled:            {},
	},
	StatusCheckedIn: {
		StatusCompleted: {},
		StatusCanceled:  {},
	},
	StatusCompleted: {}, // terminal
	StatusCanceled:  {}, // terminal
}

// CanTransition reports whether moving a booking from -> to is allowed by the
// state machine. It does NOT enforce time-based business rules (lead time,
// cutoff, etc); the service layer composes both checks.
func CanTransition(from, to BookingStatus) bool {
	dests, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = dests[to]
	return ok
}

// AllowedNext lists the legal next states from the given current state.
func AllowedNext(from BookingStatus) []BookingStatus {
	dests := allowedTransitions[from]
	out := make([]BookingStatus, 0, len(dests))
	for s := range dests {
		out = append(out, s)
	}
	return out
}
