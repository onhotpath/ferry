package ferry

import (
	"context"
	"errors"
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

// TestAPlaneThatCannotForgetIsRefusedNothing is the one place this capability
// differs from [Ensurer], and the difference is deliberate.
//
// What a value has to say at a container's own address must be spellable, so a
// plane that cannot spell it refuses the dump. What a plane already holds is
// beyond core's knowledge, so a sink with no Unsetter is additive at a composite
// rather than a dump that fails - which is what lets a sink replacing its whole
// plane on every dump implement nothing.
func TestAPlaneThatCannotForgetIsRefusedNothing(t *testing.T) {
	t.Parallel()

	sink := newStoreSink(false)

	v := shrinkable{Tags: []string{"a"}, Labels: map[string]string{"k": "v"}, Leaf: "x"}
	if err := Dump(t.Context(), v, sink); err != nil {
		t.Fatalf("a dump into a sink that cannot forget an address was refused: %v", err)
	}

	if got := len(sink.accepted); got != 3 {
		t.Errorf("the plane took %d writes, want 3: a sink with no Unsetter is asked for the same writes as "+
			"one with it", got)
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
