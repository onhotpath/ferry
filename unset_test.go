package ferry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// This file is ADR-0004's Unsetter clause: a dump replaces the composites it
// writes, and it is the only thing in a dump that deletes.
//
// Everything here goes through Dump. What the sink was asked, in which order, is
// the whole assertion, because that order is the contract: an unset arrives
// before the writes beneath the address it covers, so a member this dump does
// write survives having been forgotten.

// shrinkable is one sequence and one mapping beside a leaf, which are the two
// addresses whose membership comes from the value.
type shrinkable struct {
	Tags   []string          `ferry:"tags"`
	Labels map[string]string `ferry:"labels"`
	Leaf   string            `ferry:"leaf"`
}

// callSink records what a dump asked of a plane, in order, and can be either
// half of ADR-0011's policy: a writer that stages and one that does not.
//
// Staging is a second Writer type rather than a flag on one, for the reason
// [storeSink] uses two: Committer is discovered by assertion, so a writer
// holding the method cannot demonstrate what a writer without it does.
type callSink struct {
	staging bool
	failOn  Path

	// calls is every call the plane took, rendered, which is what the order
	// assertions read.
	calls []string
}

// errUnsetFailed is what a staged unset failure reports.
var errUnsetFailed = errors.New("the plane could not forget this address")

func (s *callSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) {
		if s.staging {
			return stagingUnsetter{s: s}, nil
		}

		return flatUnsetter{s: s}, nil
	}, nil
}

func (s *callSink) record(verb string, at Path) error {
	s.calls = append(s.calls, verb+" "+at.String())

	if at == s.failOn && verb == "unset" {
		return errUnsetFailed
	}

	return nil
}

// flatUnsetter is the writer with no Commit, so its dump runs through the staged
// phase and the calls below are a replay rather than the walk itself.
type flatUnsetter struct{ s *callSink }

func (w flatUnsetter) Set(_ context.Context, at LeafAddr, _ Value) error {
	return w.s.record("set", at.Path())
}

func (w flatUnsetter) Ensure(_ context.Context, at Container, _ Presence) error {
	return w.s.record("ensure", at.Path())
}

func (w flatUnsetter) Unset(_ context.Context, at CompositeAddr) error {
	return w.s.record("unset", at.Path())
}

// stagingUnsetter is the same plane with a Commit, so its dump is interleaved
// and the calls are the walk's own.
type stagingUnsetter struct{ s *callSink }

func (w stagingUnsetter) Set(_ context.Context, at LeafAddr, _ Value) error {
	return w.s.record("set", at.Path())
}

func (w stagingUnsetter) Ensure(_ context.Context, at Container, _ Presence) error {
	return w.s.record("ensure", at.Path())
}

func (w stagingUnsetter) Unset(_ context.Context, at CompositeAddr) error {
	return w.s.record("unset", at.Path())
}

func (stagingUnsetter) Commit(context.Context) error { return nil }

// TestADumpForgetsEveryCompositeItWrites is the clause itself, and it is asked
// of both halves of the write policy because the two reach the plane by
// different routes: an interleaved dump calls as it walks, and a staged one
// replays what it encoded.
func TestADumpForgetsEveryCompositeItWrites(t *testing.T) {
	t.Parallel()

	filled := shrinkable{Tags: []string{"a"}, Labels: map[string]string{"k": "v"}, Leaf: "x"}
	want := []string{"unset /tags", "set /tags#0", "unset /labels", "set /labels/k", "set /leaf"}

	t.Run("a sink that does not stage", func(t *testing.T) {
		t.Parallel()
		planeAsked(t, &callSink{}, filled, want)
	})

	t.Run("a sink that stages", func(t *testing.T) {
		t.Parallel()
		planeAsked(t, &callSink{staging: true}, filled, want)
	})
}

// asked dumps one value into one sink and compares what the plane was asked, in
// order, against what the clause says it should have been.
func planeAsked(t *testing.T, sink *callSink, v shrinkable, want []string) {
	t.Helper()

	if err := Dump(t.Context(), v, sink); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if !slices.Equal(sink.calls, want) {
		t.Errorf("the plane was asked\n%s\nwant\n%s", strings.Join(sink.calls, "\n"),
			strings.Join(want, "\n"))
	}
}

// TestAnEmptyCompositeIsForgottenBeforeItIsNulled is the arm that has no members
// at all, and it is the one a replacement most needs: a value that lost every
// element says so at the container's own address, and what the plane held
// beneath it has to go with it.
func TestAnEmptyCompositeIsForgottenBeforeItIsNulled(t *testing.T) {
	t.Parallel()

	want := []string{"unset /tags", "ensure /tags", "unset /labels", "ensure /labels", "set /leaf"}

	t.Run("a sink that does not stage", func(t *testing.T) {
		t.Parallel()
		planeAsked(t, &callSink{}, shrinkable{Leaf: "x"}, want)
	})

	t.Run("a sink that stages", func(t *testing.T) {
		t.Parallel()
		planeAsked(t, &callSink{staging: true}, shrinkable{Leaf: "x"}, want)
	})
}

// TestASchemaHoldingACompositeIsRefusedAtTheOpen is the other half of the
// clause: a dump that cannot replace is refused before it writes anything.
//
// The refusal is at the open because that is where both halves of the question
// are answered - the schema has held a composite since it compiled, and the
// writer is the first thing core sees that says whether the plane can forget an
// address. It is addressed at the composite, so the message names the field
// whose earlier members would have been kept.
//
// It is asked of a staging sink too, and that is the point of the second
// subtest: a plane that resolves what to remove at Commit still says so by
// implementing Unsetter, which is what both shipped sinks do. Committer is not
// an exemption from it.
func TestASchemaHoldingACompositeIsRefusedAtTheOpen(t *testing.T) {
	t.Parallel()

	t.Run("a sink that does not stage", func(t *testing.T) {
		t.Parallel()
		refusedAtOpen(t, newStoreSink(false))
	})

	t.Run("a sink that stages", func(t *testing.T) {
		t.Parallel()
		refusedAtOpen(t, newStoreSink(true))
	})
}

// refusedAtOpen dumps a schema holding two composites into a plane that cannot
// forget an address, and reads the refusal.
func refusedAtOpen(t *testing.T, sink *storeSink) {
	t.Helper()

	v := shrinkable{Tags: []string{"a"}, Labels: map[string]string{"k": "v"}, Leaf: "x"}

	err := Dump(t.Context(), v, sink)
	if err == nil {
		t.Fatal("a dump of a schema holding a composite into a sink that cannot forget an address succeeded")
	}

	if !errors.Is(err, ErrPlane) {
		t.Errorf("the refusal %v is not a plane refusal", err)
	}

	if got := fmt.Sprintf("%+v", err); !strings.Contains(got, "open, plane error") {
		t.Errorf("the refusal reads %s, and it has to be the open's", got)
	}

	if !strings.Contains(err.Error(), "/labels") || !strings.Contains(err.Error(), "ferry.Unsetter") {
		t.Errorf("the refusal %v names neither the composite it is about nor the capability it wanted", err)
	}

	if len(sink.attempts) != 0 {
		t.Errorf("the plane was asked for %v, want nothing: a refusal at the open is before any write",
			sink.attempts)
	}
}

// TestASchemaHoldingNoCompositeNeedsNoUnsetter is the boundary the refusal above
// has to have: the capability is wanted where the schema can need it and nowhere
// else, so a type of leaves and sections dumps into the same plane.
func TestASchemaHoldingNoCompositeNeedsNoUnsetter(t *testing.T) {
	t.Parallel()

	type flat struct {
		Leaf string `ferry:"leaf"`
	}

	sink := newStoreSink(false)
	if err := Dump(t.Context(), flat{Leaf: "x"}, sink); err != nil {
		t.Fatalf("a schema with no composite was refused a plane that cannot forget an address: %v", err)
	}

	if got := len(sink.accepted); got != 1 {
		t.Errorf("the plane took %d writes, want 1", got)
	}
}

// TestAnUnsetThatFailsIsReportedAtItsAddress keeps the refusal in the shape
// every other driver refusal has: the plane's own error, located at the address
// it was asked about, inside the dump's aggregate.
func TestAnUnsetThatFailsIsReportedAtItsAddress(t *testing.T) {
	t.Parallel()

	t.Run("a sink that does not stage", func(t *testing.T) {
		t.Parallel()
		refusedUnset(t, &callSink{failOn: At("tags")})
	})

	t.Run("a sink that stages", func(t *testing.T) {
		t.Parallel()
		refusedUnset(t, &callSink{staging: true, failOn: At("tags")})
	})
}

// refusedUnset dumps into a plane that cannot forget the address it was asked
// about, and reads the refusal.
func refusedUnset(t *testing.T, sink *callSink) {
	t.Helper()

	v := shrinkable{Tags: []string{"a"}, Labels: map[string]string{"k": "v"}, Leaf: "x"}

	err := Dump(t.Context(), v, sink)
	if err == nil {
		t.Fatal("a dump whose unset failed reported success")
	}

	if !errors.Is(err, errUnsetFailed) {
		t.Errorf("the refusal %v does not carry the plane's own error", err)
	}

	if !strings.Contains(err.Error(), "/tags") {
		t.Errorf("the refusal %v does not name the address it was asked about", err)
	}
}
