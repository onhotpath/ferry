package main

// ferry's own sentences about a failure, PORTED from proto/9-errors's
// e_load.go, plus the hint table that lived in its e_probe5.go.
//
// This is the D7 half of the port: ADR-0011's rule is
//
//	ferry's own message text never contains a value the plane supplied.
//	The cause stays in the chain and is not printed.
//
// and the tip's walk wrapped with fmt.Errorf("ferry: %s: %w", at, err), which
// is exactly the naive form the ADR measured four leaks in five for, on a plane
// class where every value is a secret.
//
// What ferry may name is STRUCTURE - the observed Value kind, the target Go
// type, whether the failure was syntax or range, an array's length. What it may
// not name is the VALUE. The carve-out the ADR states rather than hides: a
// dynamic address segment is plane-supplied and IS printed, because ferry
// cannot name the address without it.

import (
	"errors"
	"reflect"
	"strconv"
)

// typeHints is the two-entry table ADR-0011 measured. Redaction costs 13
// authored messages over 20 reachable decode failures, and exactly 3 of those
// 20 lose a REASON the stdlib carried, collapsing to 2 types. Both are types
// ADR-0005 already owns in the identity table, so the obligation lands where
// the representation was already pinned. A registered codec adds no entry: it
// falls into the generic row, and ADR-0009's proof is where its representation
// is checked.
var typeHints = map[string]string{
	"time.Duration": "a duration needs a unit, as in 30s or 1h30m",
	"time.Time":     "a time is RFC 3339, as in 2026-08-02T12:00:00Z",
}

// safeDecodeMsg is FERRY'S OWN sentence about a decode failure, and it is the
// measured cost of the redaction rule: ferry has to author a message for every
// failure mode, because it cannot trust a stdlib error not to quote its input.
// strconv.NumError quotes its input unconditionally and time.ParseDuration puts
// the string in its message.
func safeDecodeMsg(val Value, t reflect.Type, err error) string {
	base := ""
	switch {
	case errors.Is(err, errKind):
		// The kind pair is structural on both sides, so it is printable in
		// full, and it is the message that reads best of the thirteen:
		// "the plane holds null and int cannot take one".
		return "the plane holds " + val.Kind().String() + " and " + t.String() + " cannot take one"
	case errors.Is(err, strconv.ErrRange):
		base = "is out of range for " + t.String()
	case errors.Is(err, strconv.ErrSyntax):
		base = "is not a valid " + t.String()
	default:
		// time.ParseDuration, time.Parse and every registered codec land here.
		// None of them can be passed through, for the same reason.
		base = "is not a valid " + t.String()
	}
	// The hint states the RULE where the stdlib echoed the input, which is why
	// ADR-0011 calls the hinted message better than the one it replaces.
	if h := typeHints[t.String()]; h != "" {
		return base + ": " + h
	}
	return base
}

// safeEncodeMsg is the Dump-side mirror. Nothing the plane supplied is in
// scope on a Dump - the value came from the caller's own struct - but the
// message is authored rather than passed through anyway, because a codec's
// error is third-party text and ferry makes no promise about it.
func safeEncodeMsg(t reflect.Type) string {
	return t.String() + " cannot be encoded"
}
