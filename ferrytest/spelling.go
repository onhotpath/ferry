package ferrytest

import (
	"reflect"

	"github.com/onhotpath/ferry"
)

// Spelling holds one of a plane's spellings to the rules every spelling obeys,
// over the payloads and the refusals you supply.
//
//	ferrytest.Spelling(t, onOff, ferrytest.Eq[bool],
//	    []bool{true, false},
//	    []string{"yes", "1", ""},
//	)
//
// The first slice is payloads this spelling must carry, and the second is
// carriers it must refuse. Each payload is rendered, rendered again, parsed back
// and parsed again, which is what proves at once that what the spelling writes
// is something it reads, that it writes one spelling per value, and that neither
// half answers differently the second time. Each refusal is parsed twice, and a
// spelling that returns a value instead of an error for one is reported: a
// carrier with no reading is a failure and never a zero value. What a refusal
// says is not asserted here, because message text is not API - but a spelling
// that quotes what it refused, bounded and escaped to one line, is what the
// contract asks for, and that is yours to check where you author the message.
//
// The relation is positional and there is no default, for [Type]'s reason: a
// payload type knows its own identity and reflect.DeepEqual is wrong for
// several. Carriers are compared with reflect.DeepEqual, which is right for
// them, because a carrier is the plane's own bytes or text and nothing else.
//
// What no test can prove is that the two halves are pure functions, and the
// contract requires it. This probes for the observable half of it - the same
// input twice, the same answer - and a spelling that consults something else
// only sometimes will pass here and fail in production. Build a spelling over
// words and numbers it owns, and it cannot.
func Spelling[P, C any](t T, s ferry.Spelling[P, C], eq func(a, b P) bool, payloads []P, refused []C) {
	t.Helper()

	if s == nil {
		t.Errorf("there is no spelling to prove: %v is not one", s)

		return
	}

	for _, p := range payloads {
		spellingCarries(t, s, eq, p)
	}

	for _, c := range refused {
		spellingRefuses(t, s, c)
	}
}

// spellingCarries is laws 1, 2, 3 and the render half of 6 over one payload:
// round-trip closure, the write form inside the accept set, one spelling per
// value, and the same answer twice (ADR-0018).
func spellingCarries[P, C any](t T, s ferry.Spelling[P, C], eq func(a, b P) bool, p P) {
	t.Helper()

	first, err := s.Render(p)
	if err != nil {
		t.Errorf("Render(%#v) refused the payload: %v", p, err)

		return
	}

	if again, err := s.Render(p); err != nil || !reflect.DeepEqual(first, again) {
		t.Errorf("Render(%#v) answered %#v then %#v (%v), and one value has one spelling", p, first, again, err)

		return
	}

	spellingReads(t, s, eq, p, first)
}

// spellingReads is the parse half: what Render wrote must read back as the
// payload it came from, twice (ADR-0018, laws 1 and 6).
func spellingReads[P, C any](t T, s ferry.Spelling[P, C], eq func(a, b P) bool, p P, c C) {
	t.Helper()

	got, err := s.Parse(c)
	if err != nil {
		t.Errorf("Parse(%#v) refused what Render(%#v) wrote: %v", c, p, err)

		return
	}

	if !eq(got, p) {
		t.Errorf("Parse(Render(%#v)) is %#v, and a round trip through a spelling returns what it started from", p, got)
	}

	if again, err := s.Parse(c); err != nil || !eq(got, again) {
		t.Errorf("Parse(%#v) answered %#v then %#v (%v), and a spelling is a pure function", c, got, again, err)
	}
}

// spellingRefuses is law 4 and the parse half of law 6: a carrier this plane has
// no reading for is an error and never a zero value, both times (ADR-0018).
func spellingRefuses[P, C any](t T, s ferry.Spelling[P, C], c C) {
	t.Helper()

	got, err := s.Parse(c)
	if err == nil {
		t.Errorf("Parse(%#v) answered %#v, and a carrier with no reading is a refusal", c, got)

		return
	}

	if _, err := s.Parse(c); err == nil {
		t.Errorf("Parse(%#v) refused once and answered once, and a spelling is a pure function", c)
	}
}
