package protect

import (
	"context"
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
)

// This file is the one place in this package that reaches past the seam, and the
// reason is that what it asserts has no seam.
//
// A shell's whole job is to declare a set of interfaces, and which interfaces a
// value declares is not observable through Load or Dump: core takes a different
// path for each of them, so a wrong entry in the table shows up as a schema that
// loads differently rather than as anything a load can be asked about. Ninety-six
// combinations also cannot be reached from outside, because producing a plane
// with an arbitrary subset of the capabilities is the very thing under test.
//
// So the assertion is made where it lives: every entry is there, and every one
// declares exactly the capabilities the plane it was handed had.

// everything is a plane with every optional interface on both sides, and a
// record of which of them were reached through a shell.
type everything struct {
	calls map[string]bool
}

func newEverything() *everything { return &everything{calls: map[string]bool{}} }

func (e *everything) called(what string) { e.calls[what] = true }

func (e *everything) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	e.called("Get")

	return ferry.Value{}, nil
}

func (e *everything) Probe(context.Context, ferry.Container) (ferry.SectionInfo, error) {
	e.called("Probe")

	return ferry.SectionAbsent, nil
}

func (e *everything) Children(context.Context, ferry.CompositeAddr) ([]ferry.Segment, error) {
	e.called("Children")

	return nil, nil
}

func (e *everything) Close() error {
	e.called("Close")

	return nil
}

func (e *everything) PlaneName(ferry.Path) (string, bool) {
	e.called("PlaneName")

	return "", false
}

func (e *everything) MaxConcurrent() int {
	e.called("MaxConcurrent")

	return 0
}

func (e *everything) Set(context.Context, ferry.LeafAddr, ferry.Value) error {
	e.called("Set")

	return nil
}

func (e *everything) Ensure(context.Context, ferry.Container, ferry.Presence) error {
	e.called("Ensure")

	return nil
}

func (e *everything) Unset(context.Context, ferry.CompositeAddr) error {
	e.called("Unset")

	return nil
}

func (e *everything) Prepare(context.Context, []ferry.Path) error {
	e.called("Prepare")

	return nil
}

func (e *everything) Commit(context.Context) error {
	e.called("Commit")

	return nil
}

// readerBits is one row per read-side capability: the bit, how it is put on a
// [readerCaps], and how to ask a built shell whether it declares it.
var readerBits = []struct {
	bit  int
	give func(*readerCaps, *everything)
	has  func(ferry.Reader) bool
}{
	{rProbe, func(c *readerCaps, e *everything) { c.probe = e }, func(r ferry.Reader) bool {
		_, ok := r.(ferry.Prober)

		return ok
	}},
	{rList, func(c *readerCaps, e *everything) { c.list = e }, func(r ferry.Reader) bool {
		_, ok := r.(ferry.Enumerator)

		return ok
	}},
	{rRelease, func(c *readerCaps, e *everything) { c.release = e }, func(r ferry.Reader) bool {
		_, ok := r.(ferry.Releaser)

		return ok
	}},
	{rName, func(c *readerCaps, e *everything) { c.name = e }, func(r ferry.Reader) bool {
		_, ok := r.(ferry.PlaneNamer)

		return ok
	}},
	{rBudget, func(c *readerCaps, e *everything) { c.budget = e }, func(r ferry.Reader) bool {
		_, ok := r.(ferry.Concurrent)

		return ok
	}},
}

// writerBits is [readerBits] for the write half.
var writerBits = []struct {
	bit  int
	give func(*writerCaps, *everything)
	has  func(ferry.Writer) bool
}{
	{wCommit, func(c *writerCaps, e *everything) { c.commit = e }, func(w ferry.Writer) bool {
		_, ok := w.(ferry.Committer)

		return ok
	}},
	{wRelease, func(c *writerCaps, e *everything) { c.release = e }, func(w ferry.Writer) bool {
		_, ok := w.(ferry.Releaser)

		return ok
	}},
	{wEnsure, func(c *writerCaps, e *everything) { c.ensure = e }, func(w ferry.Writer) bool {
		_, ok := w.(ferry.Ensurer)

		return ok
	}},
	{wUnset, func(c *writerCaps, e *everything) { c.unset = e }, func(w ferry.Writer) bool {
		_, ok := w.(ferry.Unsetter)

		return ok
	}},
	{wPrepare, func(c *writerCaps, e *everything) { c.prepare = e }, func(w ferry.Writer) bool {
		_, ok := w.(ferry.Preparer)

		return ok
	}},
	{wName, func(c *writerCaps, e *everything) { c.name = e }, func(w ferry.Writer) bool {
		_, ok := w.(ferry.PlaneNamer)

		return ok
	}},
}

// readerCapsFor is the capability set one mask names.
func readerCapsFor(mask int, e *everything) readerCaps {
	var c readerCaps

	for _, b := range readerBits {
		if mask&b.bit != 0 {
			b.give(&c, e)
		}
	}

	return c
}

// readerProfile is which capabilities a built shell declares.
func readerProfile(r ferry.Reader) int {
	var got int

	for _, b := range readerBits {
		if b.has(r) {
			got |= b.bit
		}
	}

	return got
}

func writerCapsFor(mask int, e *everything) writerCaps {
	var c writerCaps

	for _, b := range writerBits {
		if mask&b.bit != 0 {
			b.give(&c, e)
		}
	}

	return c
}

func writerProfile(w ferry.Writer) int {
	var got int

	for _, b := range writerBits {
		if b.has(w) {
			got |= b.bit
		}
	}

	return got
}

func TestEveryReaderShellDeclaresExactlyWhatThePlaneUnderneathDeclared(t *testing.T) {
	t.Parallel()

	e := newEverything()

	for mask, build := range readerShells {
		if build == nil {
			t.Errorf("mask %d has no shell, so a plane with those capabilities loses all of them", mask)

			continue
		}

		c := readerCapsFor(mask, e)
		if got := readerProfile(build(e, c)); got != mask {
			t.Errorf("the shell for mask %d declares %d: less than the plane had is a capability silently "+
				"dropped, and more is a wrapper answering a question the plane cannot", mask, got)
		}
	}
}

func TestEveryWriterShellDeclaresExactlyWhatThePlaneUnderneathDeclared(t *testing.T) {
	t.Parallel()

	e := newEverything()

	for mask, build := range writerShells {
		if build == nil {
			t.Errorf("mask %d has no shell, so a plane with those capabilities loses all of them", mask)

			continue
		}

		c := writerCapsFor(mask, e)
		if got := writerProfile(build(e, c)); got != mask {
			t.Errorf("the shell for mask %d declares %d", mask, got)
		}
	}
}

func TestAShellIsBuiltFromWhatThePlaneItWrapsCanDo(t *testing.T) {
	t.Parallel()

	e := newEverything()

	if got := readerProfile(shellReader(e, e)); got != len(readerShells)-1 {
		t.Errorf("a reader with every capability was shelled as %d, want %d", got, len(readerShells)-1)
	}

	if got := writerProfile(shellWriter(e, e)); got != len(writerShells)-1 {
		t.Errorf("a writer with every capability was shelled as %d, want %d", got, len(writerShells)-1)
	}
}

func TestEveryCallAShellCarriesReachesThePlaneUnderneath(t *testing.T) {
	t.Parallel()

	e := newEverything()

	reach(t, shellReader(e, e), shellWriter(e, e))

	for _, want := range []string{
		"Probe", "Children", "Close", "PlaneName", "MaxConcurrent", "Ensure", "Unset", "Prepare", "Commit",
	} {
		if !e.calls[want] {
			t.Errorf("%s did not reach the plane the shell wraps", want)
		}
	}
}

// reach calls everything a full shell carries, so the test above is one loop
// over what was recorded rather than nine assertions inline.
func reach(t *testing.T, r ferry.Reader, w ferry.Writer) {
	t.Helper()

	ctx := t.Context()

	callReader(ctx, t, r)
	callWriter(ctx, t, w)
}

func callReader(ctx context.Context, t *testing.T, r ferry.Reader) {
	t.Helper()

	p, _ := r.(ferry.Prober)
	l, _ := r.(ferry.Enumerator)
	c, _ := r.(ferry.Releaser)
	n, _ := r.(ferry.PlaneNamer)
	b, _ := r.(ferry.Concurrent)

	_, err := p.Probe(ctx, ferry.SectionAddr{})
	_, listErr := l.Children(ctx, ferry.CompositeAddr{})
	_, _ = n.PlaneName(ferry.Path{})
	_ = b.MaxConcurrent()

	if err := errors.Join(err, listErr, c.Close()); err != nil {
		t.Errorf("a call through the shell failed: %v", err)
	}
}

func callWriter(ctx context.Context, t *testing.T, w ferry.Writer) {
	t.Helper()

	e, _ := w.(ferry.Ensurer)
	u, _ := w.(ferry.Unsetter)
	p, _ := w.(ferry.Preparer)
	c, _ := w.(ferry.Committer)

	err := errors.Join(
		e.Ensure(ctx, ferry.SectionAddr{}, ferry.PresencePresent),
		u.Unset(ctx, ferry.CompositeAddr{}),
		p.Prepare(ctx, nil),
		c.Commit(ctx),
	)
	if err != nil {
		t.Errorf("a call through the shell failed: %v", err)
	}
}

func TestATagCarryingSomethingOtherThanTheOneWordIsRefused(t *testing.T) {
	t.Parallel()

	// Core validates a declared key's words against the declaration before a
	// driver ever sees them, so this guard cannot fire through a compiled schema
	// and is asserted where it lives.
	for _, tc := range []struct {
		name  string
		words map[string]string
	}{
		{"no word at all", map[string]string{}},
		{"a word this package does not declare", map[string]string{"plaintext": ""}},
		{"the word and something else", map[string]string{secretWord: "", "plaintext": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := checkWords(tc.words); !errors.Is(err, ferry.ErrPlane) {
				t.Errorf("it was refused with %v, want a plane refusal", err)
			}
		})
	}

	if err := checkWords(map[string]string{secretWord: ""}); err != nil {
		t.Errorf("the one word the vocabulary has was refused with %v", err)
	}
}
