package ferrytest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestDriverCase22SkipsAPlaneThatNamesNothing is the scaling half: the memory
// plane is keyed by the address itself, so it has nothing to add to a report and
// correctly implements nothing.
func TestDriverCase22SkipsAPlaneThatNamesNothing(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, ferrytest.MemPlane())

	if !anyLineContains(c.logs, "case 22 skipped") {
		t.Errorf("the suite logged %q, want case 22 skipped for a plane with no name of its own", c.logs)
	}

	if len(c.lines) != 0 {
		t.Errorf("the suite reported %q against the reference plane", c.lines)
	}
}

// TestDriverCase22TakesAPureNamer is the positive half, and it is what keeps the
// three failing fixtures below from passing for the wrong reason.
func TestDriverCase22TakesAPureNamer(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("named", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, name: pureName}
	}))

	if len(c.lines) != 0 {
		t.Errorf("the suite reported %q against a plane whose names are a function of the address", c.lines)
	}
}

// TestDriverCase22ReportsANameThatDependsOnThePlane is the defect the case
// exists for: a name computed from what the plane holds puts a plane value into
// a report whose text promises to carry none.
func TestDriverCase22ReportsANameThatDependsOnThePlane(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("leaky", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, held: true, name: pureName}
	}))

	if !anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 naming a spelling that moved once the plane held "+
			"something", c.lines)
	}
}

// TestDriverCase22ReportsANameThatChangesBetweenTwoCalls is the determinism
// half: a report is composed once, and a name that is not the same twice makes
// two runs of one failure read as two failures.
func TestDriverCase22ReportsANameThatChangesBetweenTwoCalls(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("restless", func(inst *ferrytest.Instance) {
		n := 0
		inst.Source = namingSource{inner: inst.Source, name: func(addr ferry.Path, _ bool) (string, bool) {
			n++

			return pureNameOf(addr) + string(rune('a'+n%3)), true
		}}
	}))

	if !anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 naming a spelling that changed between two calls", c.lines)
	}
}

// TestDriverCase22ReportsAPlaneThatStopsNaming is the third shape: a reader that
// names an address while the plane is empty and refuses once it is not, which is
// the same dependence stated as an absence.
func TestDriverCase22ReportsAPlaneThatStopsNaming(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("forgetful", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, held: true,
			name: func(addr ferry.Path, held bool) (string, bool) {
				return pureNameOf(addr), !held
			}}
	}))

	if !anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 naming a plane that stopped naming its addresses", c.lines)
	}
}

// TestDriverCase22ReportsAReaderThatOnlyNamesAnEmptyPlane is the same
// dependence stated as a capability rather than as an answer: the read half
// implements the interface over an empty plane and not over a populated one, so
// what a report opens with depends on what the plane turned out to hold.
func TestDriverCase22ReportsAReaderThatOnlyNamesAnEmptyPlane(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("half-named", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, drops: true, name: pureName}
	}))

	if !anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 naming a reader that stopped implementing the "+
			"interface once the plane held something", c.lines)
	}
}

// TestDriverCase22ReportsAPlaneThatOnlyNamesWhatItHolds is the dependence the
// other way round: nothing is named until the plane holds it, so an empty plane
// and a populated one disagree about every address.
func TestDriverCase22ReportsAPlaneThatOnlyNamesWhatItHolds(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("held-only", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, held: true,
			name: func(addr ferry.Path, held bool) (string, bool) {
				return pureNameOf(addr), held
			}}
	}))

	if !anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 naming a plane that names only what it holds", c.lines)
	}
}

// TestDriverCase22ReportsTheDumpItCouldNotMake is the purity half with no
// populated plane to compare against: the case says so rather than passing on
// the determinism half alone.
func TestDriverCase22ReportsTheDumpItCouldNotMake(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("unwritable", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, name: pureName}
		inst.Sink = refusingSink{inner: inst.Sink, when: isLeafSet, err: errNoLeaf}
	}))

	if !anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 naming the dump it could not make", c.lines)
	}
}

// errNoLeaf is the refusal the fixture above stages, distinct so that the report
// can be traced back to it.
var errNoLeaf = errors.New("this plane will not take that schema")

// TestDriverCase22SaysNothingWithoutAReadHalf is the first of the two halves
// this case cannot run without: a plane that mints no source is not asked, and
// says nothing, because there is no reader for the interface to be on.
func TestDriverCase22SaysNothingWithoutAReadHalf(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("write-only", func(inst *ferrytest.Instance) {
		inst.Source = nil
	}))

	if anyLineContains(c.lines, "case 22") || anyLineContains(c.logs, "case 22") {
		t.Errorf("the suite said %q and %q about a plane with no read half at all", c.lines, c.logs)
	}
}

// TestDriverCase22SkipsWithoutAWriteHalf is the second: a plane that names its
// addresses and mints no sink runs the determinism half and says out loud that
// the purity half had no populated plane to run against.
func TestDriverCase22SkipsWithoutAWriteHalf(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("read-only", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, name: pureName}
		inst.Sink = nil
	}))

	if !anyLineContains(c.logs, "case 22 skipped") {
		t.Errorf("the suite logged %q, want case 22 saying which half it could not run", c.logs)
	}

	if anyLineContains(c.lines, "case 22") {
		t.Errorf("the suite reported %q, want case 22 silent about a half the plane does not have", c.lines)
	}
}

// TestDriverCase22SkipsAPlaneThatWillNotOpenOverWhatItHolds is the other open
// this case makes: the plane took the fixture and would not be read back, which
// is a broken open and is cases 4, 6 and 10's failure rather than this one's.
func TestDriverCase22SkipsAPlaneThatWillNotOpenOverWhatItHolds(t *testing.T) {
	c := &capture{}

	ferrytest.Driver(c, wrapPlane("shut", func(inst *ferrytest.Instance) {
		inst.Source = namingSource{inner: inst.Source, shut: true, name: pureName}
	}))

	if !anyLineContains(c.logs, "case 22 skipped") {
		t.Errorf("the suite logged %q, want case 22 saying it could not open the plane it had written", c.logs)
	}
}

// namingSource wraps a plane's read half and gives it a name of its own for an
// address.
//
// held is what makes a fixture impure: the wrapped reader is asked whether the
// plane holds anything at the address, and a namer that consults it is naming
// out of the plane's contents rather than out of the address.
type namingSource struct {
	inner ferry.Source
	held  bool

	// drops makes the read half implement the interface only while the plane
	// holds nothing, which is the same dependence expressed as a method set.
	drops bool

	// shut refuses the open once the plane holds something, which is the one
	// shape that separates this case's two opens: the first succeeds and the
	// second, over the plane the fixture was written to, does not.
	shut bool

	name func(addr ferry.Path, held bool) (string, bool)
}

func (s namingSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		named := namingReader{inner: r, set: addrs, consults: s.held, name: s.name}
		if !named.holdsAnything() {
			return named, nil
		}

		return s.overHeld(named)
	}, nil
}

// overHeld is what this source hands back once the plane holds something, which
// is where the two fixtures that depend on the contents differ from the honest
// one.
func (s namingSource) overHeld(named namingReader) (ferry.Reader, error) {
	switch {
	case s.shut:
		return nil, errShutOverHeld
	case s.drops:
		return plainReader{namingReader: named}, nil
	default:
		return named, nil
	}
}

// errShutOverHeld is the refusal the shut fixture stages at its second open.
var errShutOverHeld = errors.New("this plane will not open once it holds anything")

// plainReader is [namingReader] with the naming method hidden, which is how a
// fixture drops one optional interface and keeps the rest.
type plainReader struct{ namingReader }

func (plainReader) PlaneName() {}

// namingReader is the read half of [namingSource], and it forwards the inner
// reader's own optional interfaces rather than dropping them: the whole suite
// runs against this shell, so a reader that stopped enumerating would report the
// other cases' failures instead of this one's.
type namingReader struct {
	inner    ferry.Reader
	set      *ferry.AddressSet
	consults bool
	name     func(addr ferry.Path, held bool) (string, bool)
}

func (r namingReader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

func (r namingReader) Close() error { return closeInner(r.inner) }

func (r namingReader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	p, ok := r.inner.(ferry.Prober)
	if !ok {
		return ferry.SectionAbsent, nil
	}

	return p.Probe(ctx, addr)
}

func (r namingReader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	e, ok := r.inner.(ferry.Enumerator)
	if !ok {
		return nil, nil
	}

	return e.Children(ctx, addr)
}

// PlaneName names the address, and asks the plane about it first where the
// fixture is the impure one.
func (r namingReader) PlaneName(addr ferry.Path) (string, bool) {
	return r.name(addr, r.consults && r.holds(addr))
}

// holdsAnything reads the whole set, which is what decides at the open whether
// this reader names anything at all.
func (r namingReader) holdsAnything() bool {
	for m := range r.set.Seq() {
		if leaf, ok := m.(ferry.LeafAddr); ok && r.holds(leaf.Path()) {
			return true
		}
	}

	return false
}

// holds reads the plane at the address, which is exactly what a namer may not
// do and what the fixtures that do it are made of.
func (r namingReader) holds(addr ferry.Path) bool {
	for m := range r.set.Seq() {
		leaf, ok := m.(ferry.LeafAddr)
		if !ok || leaf.Path() != addr {
			continue
		}

		v, err := r.inner.Get(context.Background(), leaf)

		return err == nil && v.Kind() != ferry.KindAbsent
	}

	return false
}

// pureName is the honest namer: the address, uppercased and joined, and nothing
// else.
func pureName(addr ferry.Path, held bool) (string, bool) {
	return pureNameOf(addr) + heldMark[held], true
}

// heldMark is what the impure fixture appends where the plane holds the address,
// as a table rather than as a branch on the flag.
var heldMark = map[bool]string{true: "_SET"}

func pureNameOf(addr ferry.Path) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(addr.String(), "/"), "/", "_"))
}

// closeInner releases a wrapped reader that holds a resource, on the terms the
// rest of this package's shells use.
func closeInner(inner any) error {
	c, ok := inner.(ferry.Releaser)
	if !ok {
		return nil
	}

	return c.Close()
}
