package ferrytest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase23TakesAPlaneThatNamesTheRoot is the positive half: the memory
// plane is keyed by the address itself, and the root address is an address, so
// it names it with nothing extra to say.
func TestDriverCase23TakesAPlaneThatNamesTheRoot(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	if len(c.lines) != 0 {
		t.Errorf("the suite reported %q against a plane that names the root", c.lines)
	}

	if anyLineContains(c.logs, "case 23 skipped") {
		t.Errorf("the suite logged %q, want case 23 running against a plane that names the root", c.logs)
	}
}

// TestDriverCase23SkipsAPlaneWithNoNameForTheRoot is the answer every driver
// gives whose keys are segments joined together: the root has none to join, so
// the schema is refused at Bind and that is where a caller can still act on it.
func TestDriverCase23SkipsAPlaneWithNoNameForTheRoot(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("rootless", func(inst *ferrytest.Instance) {
		inst.Sink = refusingSink{inner: inst.Sink, when: isRootSet, err: errNoRootName}
	}))

	if !anyLineContains(c.logs, "case 23 skipped") {
		t.Errorf("the suite logged %q, want case 23 skipped for a plane that has no name for the root", c.logs)
	}

	if anyLineContains(c.lines, "case 23") {
		t.Errorf("the suite reported %q, want case 23 silent about a refusal that is the expected shape",
			c.lines)
	}
}

// errNoRootName is a plane refusing the schema it cannot hold, spelled the way
// ADR-0004 says a driver spells one.
var errNoRootName = fmt.Errorf("%w: this plane has no key for the root address", ferry.ErrPlane)

// TestDriverCase23ReportsADumpThatLandedNowhere is the defect the case exists
// for: the plane took the bind, took the dump, returned no error and holds
// nothing, which is a total loss that reads as success on the write side alone.
func TestDriverCase23ReportsADumpThatLandedNowhere(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("swallowing", func(inst *ferrytest.Instance) {
		inst.Sink = swallowingSink{inner: inst.Sink, when: isRootSet}
	}))

	if !anyLineContains(c.lines, "case 23") {
		t.Errorf("the suite reported %q, want case 23 naming a dump at the root that landed nowhere", c.lines)
	}
}

// TestDriverCase23SaysNothingWithoutBothHalves is the pair the case cannot run
// without: it round trips, so a plane missing either half is not asked and says
// nothing.
func TestDriverCase23SaysNothingWithoutBothHalves(t *testing.T) {
	for _, half := range []struct {
		name string
		drop func(*ferrytest.Instance)
	}{
		{"write-only", func(inst *ferrytest.Instance) { inst.Source = nil }},
		{"read-only", func(inst *ferrytest.Instance) { inst.Sink = nil }},
	} {
		t.Run(half.name, func(t *testing.T) {
			c := &capture{}

			ferrytest.Driver(c, wrapPlane(half.name, half.drop))

			if anyLineContains(c.lines, "case 23") || anyLineContains(c.logs, "case 23") {
				t.Errorf("the suite said %q and %q about a plane with one half", c.lines, c.logs)
			}
		})
	}
}

// isRootSet is the address set a schema whose only address is the root compiles
// to: exactly one member, and it is at the root.
func isRootSet(addrs *ferry.AddressSet) bool {
	return addrs.Len() == 1 && hasPath(addrs, ferry.Path{})
}

// swallowingSink binds the set its predicate names and then drops every write
// made under that binding, which is what a driver does when it has no key to
// write at and says nothing about it.
type swallowingSink struct {
	inner ferry.Sink
	when  func(*ferry.AddressSet) bool
}

func (s swallowingSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil || !s.when(addrs) {
		return open, err
	}

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return swallowingWriter{inner: w}, nil
	}, nil
}

// swallowingWriter takes every write and keeps none of them.
type swallowingWriter struct{ inner ferry.Writer }

func (swallowingWriter) Set(context.Context, ferry.LeafAddr, ferry.Value) error { return nil }

func (w swallowingWriter) Close() error { return closeInner(w.inner) }
