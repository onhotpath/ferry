package main

// The minimum of ADR-0011 #14 consumes - now ALIASES over the real model
// rather than a second copy of it.
//
// This file used to be a cut-down re-implementation of ADR-0011, written
// because the inherited branch predated ADR-0011 being implemented anywhere and
// #14 has to answer "which addresses must the user fill in" by reading the
// error set rather than by parsing message text.
//
// #41 D8 then ported the real model in from proto/9-errors (ferr_error.go), so
// the cut-down copy became a SECOND authority for the same thing - which is
// ADR-0007's own third defect and ADR-0010's duplication axis, arriving in the
// error model. What is left here is the name mapping #14's thirteen probes and
// the C10 and W15 suites already spell, pointing at the one implementation.
//
// The two behavioural differences the port brings, and both are ADR-0011's:
//
//   - the aggregate sorts on (moment, location, message) rather than on
//     (location, message). The moment is first because a Close failure has no
//     location and explains nothing.
//   - `Error()` is one line naming the addresses, and `%+v` is the report. The
//     copy here rendered the newline dump the ADR replaced.

import "errors"

// ADR-0011's classes. Aliases, so `errors.Is(e, tErrMissing)` and
// `errors.Is(e, ErrMissing)` are the same question.
var (
	tErrSchema  = ErrSchema
	tErrMissing = ErrMissing
	tErrValue   = ErrValue
	tErrPlane   = ErrPlane
)

// tErrAt is errAt at the walk moment. Kept as a name because #14's probes and
// e_walk.go's older call sites spell it.
func tErrAt(addr Path, class error, format string, a ...any) error {
	return errAt(mWalk, class, addr, format, a...)
}

// tNewAggregate, tElements, tAddress and tAggregating are the ported model's
// join, Elements, Address and the default scheduler.
func tNewAggregate(in []error) error { return join(in...) }
func tElements(err error) []error    { return Elements(err) }

func tAddress(err error) Path {
	var e *Error
	if errors.As(err, &e) {
		return e.loc
	}
	return Path{}
}

// tAggregating is now the DEFAULT scheduler, so every `WithSched(tAggregating)`
// in the T14, C10 and W15 suites is a no-op restating the default. They are
// left spelled out rather than deleted, because a probe that names the
// scheduler it measured stays readable when the default moves again.
var tAggregating = aggregating
