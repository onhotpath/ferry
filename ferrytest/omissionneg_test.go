package ferrytest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase21ReportsAnAnswerAtAnAddressTheDumpWasSilentAt is case 21 from
// three sides, and each plane below is broken in exactly one way.
//
// The first is the defect ADR-0004 recorded on its own prototype: a sink that
// answers for an omission with an explicit null, which reads back as a value the
// plane never held. The second stores an empty string instead, which is the same
// mistake without the loud refusal. The third takes neither write, which is the
// plane that would otherwise pass the case by holding nothing at all.
func TestDriverCase21ReportsAnAnswerAtAnAddressTheDumpWasSilentAt(t *testing.T) {
	cases := map[string]struct {
		mode omissionDefect
		want string
	}{
		"a stored null":    {mode: omissionNull, want: "a plane that answers for the silence"},
		"a stored empty":   {mode: omissionEmpty, want: "an answer ferry never gave"},
		"nothing at all":   {mode: omissionSilent, want: "would pass the half below"},
		"nothing is wrong": {mode: omissionNone, want: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Driver(c, omittingPlane(tc.mode))

			assertCase21(t, c, tc.want)
		})
	}
}

// assertCase21 holds the run to reporting case 21 and nothing else.
func assertCase21(t *testing.T, c *capture, want string) {
	t.Helper()

	if want == "" {
		if len(c.lines) != 0 {
			t.Errorf("a conforming plane reported %q, want nothing", c.lines)
		}

		return
	}

	only := onlyLine(t, c)
	if !strings.Contains(only, "case 21") {
		t.Errorf("report = %q, want case 21 and only case 21", only)
	}

	if !strings.Contains(only, want) {
		t.Errorf("report = %q, want %q in it", only, want)
	}
}

// omissionDefect is which of the three ways a sink can answer for an omission
// the plane below takes.
type omissionDefect int

const (
	omissionNone omissionDefect = iota
	omissionNull
	omissionEmpty
	omissionSilent
)

// omitAddr is the address case 21's dump is silent at, and the only address
// these planes behave differently at.
var omitAddr = ferry.At("gone")

// omittingPlane is the memory plane with one defect, scoped to the one schema
// that names [omitAddr] so that every other case runs against the plane the rest
// of these tests already trust.
func omittingPlane(mode omissionDefect) ferrytest.Plane {
	mem := ferrytest.MemPlane()
	p := mem

	p.Name = "omitting"
	p.Open = func() ferrytest.Instance {
		inst := mem.Open()
		inst.Sink = omittingSink{inner: inst.Sink, mode: mode}

		return inst
	}

	return p
}

type omittingSink struct {
	inner ferry.Sink
	mode  omissionDefect
}

func (s omittingSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	at, scoped := leafIn(addrs, omitAddr)
	if !scoped || s.mode == omissionNone {
		return open, nil
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return &omittingWriter{inner: w, at: at, mode: s.mode}, nil
	}, nil
}

// leafIn finds one leaf of a bound set by address, which is how these planes ask
// whether they were handed the schema they are broken for.
func leafIn(addrs *ferry.AddressSet, at ferry.Path) (ferry.LeafAddr, bool) {
	for m := range addrs.Seq() {
		if a, ok := m.(ferry.LeafAddr); ok && a.Path() == at {
			return a, true
		}
	}

	return ferry.LeafAddr{}, false
}

// omittingWriter answers for the address the dump is silent at, on the first
// write the dump does make.
type omittingWriter struct {
	inner ferry.Writer
	at    ferry.LeafAddr
	mode  omissionDefect
	done  bool
}

func (w *omittingWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if w.mode == omissionSilent {
		return nil
	}

	if !w.done {
		w.done = true

		if err := w.inner.Set(ctx, w.at, w.filler()); err != nil {
			return err
		}
	}

	return w.inner.Set(ctx, addr, v)
}

// filler is what this plane invents at the silent address.
func (w *omittingWriter) filler() ferry.Value {
	if w.mode == omissionNull {
		return ferry.Null
	}

	return ferry.String("")
}

func (w *omittingWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	return ensureThrough(ctx, w.inner, addr, p)
}

// Unset forwards a retraction, so that the sink this shell wraps keeps the
// capability it declared and case 17 is still asked its question.
func (w *omittingWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	u, ok := w.inner.(ferry.Unsetter)
	if !ok {
		return errors.New("the plane underneath cannot forget an address")
	}

	return u.Unset(ctx, addr)
}
