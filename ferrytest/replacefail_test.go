package ferrytest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase17SkipsASinkWhoseFirstDumpFailed is the attribution rule case 15
// already carries, on the case that came after it.
//
// Case 17 asks what a sink does about a member the new value no longer has, and
// a sink that could not take the first dump has not answered that badly, it has
// not been asked. Case 1 owns a dump that cannot happen, so this case stands
// down and says so.
func TestDriverCase17SkipsASinkWhoseFirstDumpFailed(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("no-first-dump-17", func(inst *ferrytest.Instance) {
		inst.Sink = forgetfulSink{inner: inst.Sink, refuse: ferry.String("a")}
	}))

	if !anyLineContains(c.logs, "case 17 skipped: the first dump failed") {
		t.Errorf("the suite logged %q, want case 17 standing down for a first dump that failed", c.logs)
	}

	for _, line := range c.lines {
		if strings.Contains(line, "case 17:") {
			t.Errorf("case 17 reported %q, and a case that stood down reports nothing", line)
		}
	}
}

// TestDriverCase17ReportsASinkThatCannotTakeTheShorterDump is the other refusal:
// a value whose sequence lost a member is an ordinary value, so a sink that
// declared it can forget an address and then cannot write one has made a
// mistake this case owns.
func TestDriverCase17ReportsASinkThatCannotTakeTheShorterDump(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("no-second-dump-17", func(inst *ferrytest.Instance) {
		inst.Sink = forgetfulSink{inner: inst.Sink, refuse: ferry.String("z")}
	}))

	if !anyLineContains(c.lines, "case 17: the second, shorter dump failed") {
		t.Errorf("the suite reported %q, want case 17 naming the refused shorter dump", c.lines)
	}
}

// forgetfulSink refuses one value wherever it is written, and keeps every
// optional interface the sink underneath had.
//
// Keeping [ferry.Unsetter] is the whole point of a second wrapper: the ones
// beside it drop it, which is correct for the cases they serve and makes case 17
// skip, and a case that skips cannot be asked to fail.
type forgetfulSink struct {
	inner  ferry.Sink
	refuse ferry.Value
}

func (s forgetfulSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return forgetfulWriter{inner: w, refuse: s.refuse}, nil
	}, nil
}

// forgetfulWriter is the open half, and the one value it refuses is what
// separates the two dumps: the suite's populated fixture carries it and its
// shorter one does not, or the other way round.
type forgetfulWriter struct {
	inner  ferry.Writer
	refuse ferry.Value
}

func (w forgetfulWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if v == w.refuse {
		return ferry.ErrorAt(addr.Path(), errNoWrite)
	}

	return w.inner.Set(ctx, addr, v)
}

func (w forgetfulWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	e, ok := w.inner.(ferry.Ensurer)
	if !ok {
		return ferry.ErrorAt(addr.Path(), errNoWrite)
	}

	return e.Ensure(ctx, addr, p)
}

func (w forgetfulWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	u, ok := w.inner.(ferry.Unsetter)
	if !ok {
		return nil
	}

	return u.Unset(ctx, addr)
}

// TestDriverCase17ReportsASinkThatKeepsTheEarlierMembers is the failure the case
// exists for, and the one nothing else reports: the shorter dump is taken, every
// value it wrote is where it should be, and the members it did not write are
// still there.
func TestDriverCase17ReportsASinkThatKeepsTheEarlierMembers(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("additive", func(inst *ferrytest.Instance) {
		inst.Sink = additiveSink{inner: inst.Sink}
	}))

	if !anyLineContains(c.lines, "case 17: after a dump of") {
		t.Errorf("the suite reported %q, want case 17 naming the list it found", c.lines)
	}
}

// additiveSink declares it can forget an address and then forgets nothing,
// which is a sink whose dumps accumulate while saying they replace.
type additiveSink struct{ inner ferry.Sink }

func (s additiveSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return additiveWriter{inner: w}, nil
	}, nil
}

type additiveWriter struct{ inner ferry.Writer }

func (w additiveWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	return w.inner.Set(ctx, addr, v)
}

func (w additiveWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	e, ok := w.inner.(ferry.Ensurer)
	if !ok {
		return ferry.ErrorAt(addr.Path(), errNoWrite)
	}

	return e.Ensure(ctx, addr, p)
}

// Unset takes the call and does nothing with it, which is the declaration
// without the behaviour.
func (additiveWriter) Unset(context.Context, ferry.CompositeAddr) error { return nil }
