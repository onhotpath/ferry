package main

// The walk's side of #9, added as a SWITCH rather than a rewrite so the
// fail-fast behaviour the base prototype had stays measurable against the
// aggregating one. Every rule with a live alternative is a switch, which is the
// convention proto/8-defaults set.
//
// With sink == nil the walk behaves exactly as it did before (5.4 live: first
// error wins), so #8's and #11's probes are unaffected. With a sink it records
// and keeps walking.

import (
	"errors"
	"reflect"
	"strconv"
)

type errSink struct{ errs []error }

func (s *errSink) add(e error) { s.errs = append(s.errs, e) }
func (s *errSink) result() error {
	if s == nil {
		return nil
	}
	return join(s.errs...)
}

// emit is the one seam. Aggregating means the walk continues, so the caller
// gets a nil error back and the failure lives in the sink.
func (o loadOpts) emit(e *Error) error {
	if o.sink != nil {
		o.sink.add(e)
		return nil
	}
	return e
}

func (o loadOpts) miss(p Path, msg string) error {
	return o.emit(errAt(mWalk, ErrMissing, p, "%s", msg))
}

func (o loadOpts) valf(p Path, format string, args ...any) error {
	return o.emit(errAt(mWalk, ErrValue, p, format, args...))
}

func (o loadOpts) planef(p Path, format string, args ...any) error {
	return o.emit(errAt(mWalk, ErrPlane, p, format, args...))
}

// decErr is the redaction rule at the one place plane text actually enters
// ferry's errors: a leaf that did not decode.
//
// The cause stays in the chain, so errors.Is(err, strconv.ErrRange) still
// works. It is never PRINTED, because strconv.NumError quotes its input and
// time.ParseDuration puts the string in its message, and on a Vault or Consul
// plane that input is a secret. ferry cannot know which addresses hold secrets
// without knowing what the plane is for, which ADR-0001 forbids by name, so the
// rule has to be total.
func (o loadOpts) decErr(p Path, val Value, t reflect.Type, cause error) error {
	return o.emit(errAt(mWalk, ErrValue, p, "%s", safeDecodeMsg(val, t, cause)).withCause(cause))
}

// safeDecodeMsg is FERRY'S OWN sentence about a decode failure, and it is the
// measured cost of the redaction rule: ferry has to author a message for every
// failure mode, because it cannot trust a stdlib error not to quote its input.
//
// What it may name is STRUCTURE - the observed kind, the target type, whether
// the failure was syntax or range. What it may not name is the VALUE.
func safeDecodeMsg(val Value, t reflect.Type, err error) string {
	switch {
	case errors.Is(err, errKind):
		return "the plane holds " + val.Kind().String() + " and " + t.String() + " cannot take one"
	case errors.Is(err, strconv.ErrRange):
		return "is out of range for " + t.String()
	case errors.Is(err, strconv.ErrSyntax):
		return "is not a valid " + t.String()
	default:
		// time.ParseDuration, time.Parse and every registered codec land here.
		// None of them can be passed through, for the same reason.
		return "is not a valid " + t.String()
	}
}
