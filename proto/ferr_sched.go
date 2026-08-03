package main

// D6: aggregation is the DEFAULT, not an Option a caller has to ask for.
//
// ADR-0011, in as many words:
//
//	ferry reports every failure that is not a consequence of another failure
//	it is already reporting.
//
// and, on the knob the survey recommended and this ADR declined:
//
//	No `StopOnFirstError`: it is a public knob whose only job is to make ferry
//	report less.
//
// The tip shipped the opposite. Every entry point constructed `&walker{... sch:
// serial ...}`, `serial` returns on the first non-nil error, and the
// aggregating scheduler existed only inside one probe. So the tip WAS
// StopOnFirstError, which is the one Option ADR-0011 declines to ship, reached
// by defaulting rather than by deciding.
//
// `WithSched` stays, because ADR-0010 put aggregation in the scheduler and #20
// inherits that seam. What changes is which end of the seam you get without
// asking.

import "errors"

// aggregating is the default scheduler. Every task runs; every failure is
// collected; the aggregate is built by ferry's ONE constructor.
//
// It calls `join` and never errors.Join. ADR-0011 is emphatic about that, and
// the reason it is a rule rather than a preference is that breaking it was
// SILENT: an errors.Join result is invisible to Elements, is ordered by
// insertion, and renders as the newline dump. The prototype that broke it
// reported one element from Elements while two errors printed.
func aggregating(tasks []func() error) error {
	var errs []error
	for _, t := range tasks {
		if err := t(); err != nil {
			errs = append(errs, err)
		}
	}
	return join(errs...)
}

// dropMissingUnder removes ErrMissing elements from an error, returning nil if
// that leaves nothing. It is the mechanism half of the fix the #14/#15/#10
// session landed in e_walk.go's nPtr case: a `required` leaf beneath an
// ABSENT optional *T made the whole section mandatory, and a failure under a
// section that does not exist is a CONSEQUENCE of the section not existing
// rather than a failure of its own.
//
// It was already gated on the presence bit. What it was NOT gated on was the
// scheduler, and it is only sound if every sibling under the pointer actually
// RAN, because the presence bit is accumulated across siblings. Under `serial`
// the first missing child aborts, the later siblings never run, and the
// presence bit is a partial sum: a section with a present second field and an
// absent-and-required first field would have its real failure deleted.
//
// Making aggregation the default is what closes that. Z2 in the #14 suite
// measures the unsound half directly and is why this note exists.
func dropMissingUnder(err error) error {
	var kept []error
	for _, e := range Elements(err) {
		if errors.Is(e, ErrMissing) {
			continue
		}
		kept = append(kept, e)
	}
	return join(kept...)
}
