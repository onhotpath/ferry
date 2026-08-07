package ferrytest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase18SkipsASinkThatDeclaresNeither is the capability scaling, on
// the reference plane.
//
// The memory plane neither stages nor asks to be handed the addresses a dump
// realised, and it renders every address to itself, so nothing obliges it to
// refuse anything before the writes. A case that stood over it anyway would be
// failing a plane for a claim it never made.
func TestDriverCase18SkipsASinkThatDeclaresNeither(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	if !anyLineContains(c.logs, "case 18 skipped: the plane's sink neither stages") {
		t.Errorf("the suite logged %q, want case 18 standing down for a sink that declared neither", c.logs)
	}
}

// TestDriverCase18SkipsAPlaneThatFoldsNothing is the other skip: a plane that
// takes both of two addresses one key function would fold has no refusal here to
// be held to, whatever it declared.
func TestDriverCase18SkipsAPlaneThatFoldsNothing(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("folds-nothing", func(inst *ferrytest.Instance) {
		inst.Sink = foldingSink{inner: inst.Sink}
	}))

	if !anyLineContains(c.logs, "case 18 skipped: the plane took both") {
		t.Errorf("the suite logged %q, want case 18 standing down for a plane that folds nothing", c.logs)
	}
}

// TestDriverCase18TakesASinkThatRefusesBeforeAnyWrite is the case passing, and
// it is the shape #135 asked for: the fold is found while the whole realised set
// is in hand, so the dump that fails leaves nothing behind.
func TestDriverCase18TakesASinkThatRefusesBeforeAnyWrite(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("prepares", func(inst *ferrytest.Instance) {
		inst.Sink = foldingSink{inner: inst.Sink, folds: true, early: true}
	}))

	for _, line := range c.lines {
		if strings.Contains(line, "case 18:") {
			t.Errorf("case 18 reported %q against a sink that refused before writing anything", line)
		}
	}

	if anyLineContains(c.logs, "case 18 skipped") {
		t.Errorf("the suite logged %q, and a case that stood down proves nothing about the sink", c.logs)
	}
}

// TestDriverCase18ReportsASinkThatRefusesAtTheCollidingWrite is the failure the
// case exists for, and the one nothing else in the suite reports.
//
// The sink declares that it can be told in time and then is not told: its
// Prepare takes the realised set and says nothing, so the fold is found inside
// the write that carries the second of the pair, by which time the leaf and the
// first of the pair are on the plane. The dump reports a failure and the plane
// has changed anyway.
func TestDriverCase18ReportsASinkThatRefusesAtTheCollidingWrite(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("prepares-late", func(inst *ferrytest.Instance) {
		inst.Sink = foldingSink{inner: inst.Sink, folds: true}
	}))

	if !anyLineContains(c.lines, "case 18: a dump refused over two map keys") {
		t.Errorf("the suite reported %q, want case 18 naming what the refused dump left behind", c.lines)
	}
}

// foldingSink is a flattening plane in the smallest shape that has the property:
// it renders an address by mapping every hyphen to an underscore, so two of the
// suite's map keys become one key and one of them has to be refused.
//
// Where it refuses is the knob. With early set it refuses out of Prepare, which
// is handed every address the value determined before the first write; without
// it, out of the Set that carries the second of the pair, which is where a plane
// that cannot be told in time finds out. With folds unset it refuses nothing and
// is an ordinary plane that happens to declare the capability.
type foldingSink struct {
	inner ferry.Sink
	folds bool
	early bool
}

func (s foldingSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return foldingWriter{inner: w, sink: s, by: map[string]ferry.Path{}}, nil
	}, nil
}

// foldingWriter is the open half. The minted keys belong to it and not to the
// sink, which is the rule every flattening driver is held to.
type foldingWriter struct {
	inner ferry.Writer
	sink  foldingSink
	by    map[string]ferry.Path
}

// Prepare refuses the fold where the sink was built to be told in time, and
// takes the set silently otherwise.
func (w foldingWriter) Prepare(_ context.Context, addrs []ferry.Path) error {
	if !w.sink.folds || !w.sink.early {
		return nil
	}

	errs := make([]error, 0, len(addrs))

	for _, addr := range addrs {
		if err := w.key(addr); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (w foldingWriter) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if w.sink.folds {
		if err := w.key(addr.Path()); err != nil {
			return err
		}
	}

	return w.inner.Set(ctx, addr, v)
}

// key is the flattening itself: an address that already holds its key is served
// from what this open minted, and one whose key another address took is refused
// naming both.
func (w foldingWriter) key(addr ferry.Path) error {
	key := strings.ReplaceAll(addr.String(), "-", "_")

	if other, taken := w.by[key]; taken && other != addr {
		return ferry.ErrorAt(addr, fmt.Errorf("%w: this plane renders %s and %s to one key",
			ferry.ErrPlane, other, addr))
	}

	w.by[key] = addr

	return nil
}

func (w foldingWriter) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	e, ok := w.inner.(ferry.Ensurer)
	if !ok {
		return ferry.ErrorAt(addr.Path(), errNoWrite)
	}

	return e.Ensure(ctx, addr, p)
}

func (w foldingWriter) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	u, ok := w.inner.(ferry.Unsetter)
	if !ok {
		return nil
	}

	return u.Unset(ctx, addr)
}

// TestDriverCase18SkipsAPlaneItCannotReadBack is the third way the case stands
// down: what a refused dump left cannot be compared against nothing if the plane
// cannot be read at all, and a source that will not bind is not case 18's defect
// to report.
func TestDriverCase18SkipsAPlaneItCannotReadBack(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("prepares-unreadable", func(inst *ferrytest.Instance) {
		inst.Sink = foldingSink{inner: inst.Sink, folds: true, early: true}
		inst.Source = unreadableSource{}
	}))

	if !anyLineContains(c.logs, "case 18 skipped: what the refused dump left could not be read back") {
		t.Errorf("the suite logged %q, want case 18 standing down for a plane it cannot read", c.logs)
	}
}

// unreadableSource refuses at Bind, which is the one refusal every case that
// reads runs into before any I/O.
type unreadableSource struct{}

func (unreadableSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) { return nil, errNoRead }
