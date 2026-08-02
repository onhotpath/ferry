package main

// The minimum of ADR-0011 this ticket needs, and the reason it needs any of it.
//
// #14 has to answer "which addresses must the user fill in", and ADR-0001's
// stated route to that is to read the error set. ADR-0011 makes that route
// exist: an error carries an address and a class, Elements ranges an
// aggregate, and errors.Is matches the class. The inherited branch
// (proto/16-entry-point) predates ADR-0011 being implemented anywhere, so its
// walk reports `fmt.Errorf("ferry: %s: required, ...")` and the only way to
// get the address back out is to parse the message - which is precisely what
// ADR-0011 forbids ("message text is not API").
//
// So this file is not a design proposal. It is ADR-0011's already-accepted
// shape, cut down to the three things #14 consumes, so that the probes use the
// mechanism rather than string matching. If it were absent, template
// generation would not be buildable from outside core at all, which is itself
// one of this ticket's findings.

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ADR-0011's classes, the two #14 reads.
var (
	tErrSchema  = errors.New("schema error")
	tErrMissing = errors.New("missing")
	tErrValue   = errors.New("invalid value")
	tErrPlane   = errors.New("plane error")
)

// tError is ADR-0011's `Error`: one exported name, no exported fields, a
// pointer receiver so the 5.14 value-receiver defect cannot arrive.
type tError struct {
	addr  Path
	class error
	msg   string
	cause error
}

func (e *tError) Error() string {
	if e.addr.IsRoot() {
		return "ferry: " + e.msg
	}
	return "ferry: " + e.addr.String() + ": " + e.msg
}
func (e *tError) Unwrap() error   { return e.class }
func (e *tError) Address() Path   { return e.addr }
func (e *tError) Is(t error) bool { return t == e.class }

func tErrAt(addr Path, class error, format string, a ...any) error {
	return &tError{addr: addr, class: class, msg: fmt.Sprintf(format, a...)}
}

// tAggregate is ADR-0011's aggregate: flat, never nested, sorted AT
// CONSTRUCTION so errors.AsType's tree-order pick is deterministic too.
type tAggregate struct{ elems []error }

func (a *tAggregate) Error() string {
	if len(a.elems) == 1 {
		return a.elems[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ferry: %d errors:", len(a.elems))
	for _, e := range a.elems {
		b.WriteString("\n  " + strings.TrimPrefix(e.Error(), "ferry: "))
	}
	return b.String()
}
func (a *tAggregate) Unwrap() []error { return a.elems }

// tNewAggregate is ferry's ONE aggregate constructor. ADR-0011: ferry never
// calls errors.Join, because a Join result is invisible to Elements, is
// ordered by insertion, and renders as the newline dump.
func tNewAggregate(in []error) error {
	// Flat, never nested: the walk hands one scheduler's aggregate up to the
	// next scheduler as a single task error, so without this the tree ferry's
	// own recursion builds is exactly the left-leaning pairwise tree survey
	// item 5.4 warns about.
	var errs []error
	for _, e := range in {
		switch {
		case e == nil:
		case isAgg(e):
			errs = append(errs, e.(*tAggregate).elems...)
		default:
			errs = append(errs, e)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	slices.SortFunc(errs, func(x, y error) int {
		ax, ay := tAddress(x), tAddress(y)
		if c := CompareSegmentwise(ax, ay); c != 0 {
			return c
		}
		return cmp.Compare(x.Error(), y.Error())
	})
	if len(errs) == 1 {
		return errs[0]
	}
	return &tAggregate{elems: errs}
}

// tElements is ADR-0011's Elements: one element for a bare leaf, so a caller's
// loop reads the same whether one field failed or forty.
func isAgg(e error) bool { _, ok := e.(*tAggregate); return ok }

func tElements(err error) []error {
	if err == nil {
		return nil
	}
	if a, ok := err.(*tAggregate); ok {
		return a.elems
	}
	return []error{err}
}

func tAddress(err error) Path {
	var e *tError
	if errors.As(err, &e) {
		return e.addr
	}
	return Path{}
}

// --- the aggregating scheduler ----------------------------------------------
//
// ADR-0010 puts aggregation in the SCHEDULER and not in the walk, and measures
// the same walk function byte-identical under both. #14 needs the aggregating
// one for one reason only: with first-error scheduling the required set comes
// back one address per Load, and T2 prices exactly that.

func tAggregating(tasks []func() error) error {
	var errs []error
	for _, t := range tasks {
		if err := t(); err != nil {
			errs = append(errs, err)
		}
	}
	return tNewAggregate(errs)
}

// dropMissingUnder removes ErrMissing elements from an error, returning nil if
// that leaves nothing. It is the mechanism half of the candidate fix in
// e_walk.go's nPtr case.
//
// It is only sound if every sibling under the pointer actually RAN, because
// the presence bit it is gated on is accumulated across siblings. Z2 measures
// what happens when they do not.
func dropMissingUnder(err error) error {
	var kept []error
	for _, e := range tElements(err) {
		if errors.Is(e, tErrMissing) {
			continue
		}
		kept = append(kept, e)
	}
	return tNewAggregate(kept)
}
