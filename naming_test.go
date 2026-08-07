package ferry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// flatKeys is a source that flattens the way an environment driver does: it
// builds core's key table at Bind and names an address out of it afterwards.
//
// It is a source of its own rather than a method on the test plane, because
// PlaneNamer is discovered by assertion and a plane that has it cannot
// demonstrate what a plane without it does.
type flatKeys struct{ p *plane }

func (s flatKeys) Bind(addrs *AddressSet) (OpenFunc, error) {
	s.p.bound = addrs

	keys, err := NewKeys(addrs, "flat", flatKey)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (Reader, error) {
		return flatReader{treeReader: treeReader{p: s.p}, keys: keys}, nil
	}, nil
}

// flatSink is the same table on the write side.
type flatSink struct{ p *plane }

func (s flatSink) Bind(addrs *AddressSet) (OpenWriterFunc, error) {
	s.p.bound = addrs

	keys, err := NewKeys(addrs, "flat", flatKey)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (Writer, error) { return namingWriter{p: s.p, keys: keys}, nil }, nil
}

// flatKey uppercases and joins with _, and refuses a name this plane cannot
// spell, which is what a key function does and what makes the false arm of
// PlaneName reachable through the entry point.
func flatKey(addr Path) (string, error) {
	out := strings.ToUpper(strings.TrimPrefix(addr.String(), "/"))
	if strings.Contains(out, "BAD") {
		return "", fmt.Errorf("%w: this plane has no name for it", ErrPlane)
	}

	return strings.ReplaceAll(out, "/", "_"), nil
}

type flatReader struct {
	treeReader
	keys *Keys
}

func (r flatReader) PlaneName(addr Path) (string, bool) { return r.keys.PlaneName(addr) }

type namingWriter struct {
	p    *plane
	keys *Keys
}

func (w namingWriter) Set(ctx context.Context, addr LeafAddr, v Value) error {
	return w.p.Set(ctx, addr, v)
}

func (w namingWriter) Unset(ctx context.Context, addr CompositeAddr) error {
	return w.p.Unset(ctx, addr)
}

func (w namingWriter) PlaneName(addr Path) (string, bool) { return w.keys.PlaneName(addr) }

// blankSource names every address as empty text, which is a plane that answered
// and said nothing.
type blankSource struct{ p *plane }

func (s blankSource) Bind(addrs *AddressSet) (OpenFunc, error) {
	s.p.bound = addrs

	return func(context.Context) (Reader, error) { return blankReader{treeReader{p: s.p}}, nil }, nil
}

type blankReader struct{ treeReader }

func (blankReader) PlaneName(Path) (string, bool) { return "", true }

// namedLeaf and namedMap are the fixtures: one address the type determines, and
// one mapping whose keys the value determines.
type (
	namedLeaf struct {
		N int `ferry:"n"`
	}
	namedMap struct {
		M map[string]int `ferry:"m"`
	}
)

// TestAReportOpensWithThePlanesOwnName is the whole of what the naming channel
// is for: the line names the thing somebody can go and change.
func TestAReportOpensWithThePlanesOwnName(t *testing.T) {
	p := newPlane(map[Path]Value{At("n"): String("nope")})

	_, err := Load[namedLeaf](t.Context(), flatKeys{p: p})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: N: ") {
		t.Errorf("the report does not open with the plane's own name for the address: %s", got)
	}

	// The address is unmoved, which is what a caller matches on.
	fe, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the failure is not one of ferry's own: %v", err)
	}

	if fe.Address() != At("n") {
		t.Errorf("Address is %v, want %v: the spelling is what is printed and never what is returned",
			fe.Address(), At("n"))
	}
}

// TestAPlaneWithNoNameOfItsOwnPrintsFerrys is the no-namer path, which is every
// plane keyed by the address itself.
func TestAPlaneWithNoNameOfItsOwnPrintsFerrys(t *testing.T) {
	p := newPlane(map[Path]Value{At("n"): String("nope")})

	_, err := Load[namedLeaf](t.Context(), planeSource{p: p})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: /n: ") {
		t.Errorf("a plane that names nothing moved the report: %s", got)
	}
}

// TestANameThePlaneRefusesLeavesFerrysRendering is the escape hatch, and it is
// the case where ferry's own rendering is the only honest thing to print.
func TestANameThePlaneRefusesLeavesFerrysRendering(t *testing.T) {
	p := newPlane(map[Path]Value{At("m").At("bad"): Bool(true)})

	_, err := Load[namedMap](t.Context(), flatKeys{p: p})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: /m/bad: ") {
		t.Errorf("an address the plane cannot name did not keep ferry's rendering: %s", got)
	}
}

// TestAnAddressAValueMintedIsNamedToo is the dynamic tier: the name is a
// function of the address, so an address that did not exist at Bind is named
// exactly as one that did.
func TestAnAddressAValueMintedIsNamedToo(t *testing.T) {
	p := newPlane(map[Path]Value{At("m").At("k"): Bool(true)})

	_, err := Load[namedMap](t.Context(), flatKeys{p: p})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: M_K: ") {
		t.Errorf("an address the value minted was not named: %s", got)
	}
}

// TestANameThatIsEmptyTextIsNotAName states the boundary: a plane that answered
// and said nothing says less than ferry's own rendering of the address.
func TestANameThatIsEmptyTextIsNotAName(t *testing.T) {
	p := newPlane(map[Path]Value{At("n"): String("nope")})

	_, err := Load[namedLeaf](t.Context(), blankSource{p: p})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: /n: ") {
		t.Errorf("an empty name replaced ferry's rendering: %s", got)
	}
}

// TestADumpNamesTheAddressesItRefuses is the same channel on the write side.
func TestADumpNamesTheAddressesItRefuses(t *testing.T) {
	p := newPlane(nil)
	p.fail[At("n")] = errors.New("the store would not take it")

	err := Dump(t.Context(), namedLeaf{N: 1}, flatSink{p: p})
	if err == nil {
		t.Fatal("the dump succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: N: ") {
		t.Errorf("the dump's report does not open with the plane's own name: %s", got)
	}
}

// TestAReportIsOrderedByAddressAndDisplayedByName is the consequence worth
// stating: the sort key does not move, so two runs of one failure produce one
// report whatever the plane calls its addresses.
func TestAReportIsOrderedByAddressAndDisplayedByName(t *testing.T) {
	p := newPlane(map[Path]Value{At("a"): String("nope"), At("z"): String("nope")})

	_, err := Load[namedPair](t.Context(), flatKeys{p: p})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := fmt.Sprintf("%v", err); !strings.Contains(got, "A, Z") {
		t.Errorf("the summary is not ordered by address and displayed by name: %s", got)
	}
}

// namedPair is two leaves whose addresses and whose names sort alike, which is
// what lets the case above assert the order without asserting a fold.
type namedPair struct {
	A int `ferry:"a"`
	Z int `ferry:"z"`
}

// TestAFailureWithNoLocationIsUnnamed is the close failure: there is no address
// for a name to stand in for, and the element is left exactly as it was.
func TestAFailureWithNoLocationIsUnnamed(t *testing.T) {
	p := newPlane(map[Path]Value{At("n"): String("nope")})
	p.closeErr = errors.New("the connection would not close")

	_, err := Load[namedLeaf](t.Context(), closingFlat{flatKeys{p: p}})
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	got := fmt.Sprintf("%+v", err)
	if !strings.Contains(got, "N: ") || !strings.Contains(got, "closing the plane") {
		t.Errorf("the report lost one of its two failures: %s", got)
	}
}

// closingFlat is the flat plane with a reader that releases, so that a close
// failure joins the report the walk already built.
type closingFlat struct{ flatKeys }

func (s closingFlat) Bind(addrs *AddressSet) (OpenFunc, error) {
	open, err := s.flatKeys.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		named, ok := r.(flatReader)
		if !ok {
			return r, nil
		}

		return closingReader{flatReader: named, p: s.p}, nil
	}, nil
}

type closingReader struct {
	flatReader
	p *plane
}

func (r closingReader) Close() error { return r.p.closeErr }
