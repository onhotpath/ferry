package ferry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// planeKey is the second planted token: a name the plane mints rather than a
// value it holds.
//
// It is the map key half of the rule, and the two halves are not the same rule.
// A value is never printed; a key is a name, ferry cannot name the address
// without it, and errors.md records that carve-out. So this token is expected
// in a location and in a plane's own spelling of one, and forbidden in ferry's
// own message text, where the address already says where.
const planeKey = "K3YFR0MTHEPLANE"

// leakRows is how many failure families TestNoMessageRepeatsWhatThePlaneHeld
// drives, asserted against the table so that a family which stops firing fails
// loudly rather than passing silently.
const leakRows = 14

// leakCase is one family of failure, driven through a real Load or Dump over a
// plane whose every value is the planted secret and whose every minted name
// carries the planted key.
type leakCase struct {
	name string
	run  func() error
}

// TestNoMessageRepeatsWhatThePlaneHeld is ADR-0011's redaction rule asserted
// over what a run produces rather than over messages somebody remembered to
// write down (#159).
//
// TestFerryComposesNoPlaneText next door is the rendering assertion over
// hand-built errors, and it covers exactly the rows its table names. This is the
// reachability assertion: every family core can author, reached through the
// entry point, with the plane's own text planted in everything the plane hands
// over.
//
// Three things are asserted of every element, and the third is the one the
// plane-naming channel added. ferry's own text carries neither token. The
// location and the plane's own name for it may carry the key, which is the map
// key carve-out, and may never carry the value - which is the executable form of
// the obligation [PlaneNamer] states. And a rendering carries the value only
// where a driver's own error is being printed, which is the driver's obligation
// and not core's.
func TestNoMessageRepeatsWhatThePlaneHeld(t *testing.T) {
	rows := leakCases()

	if len(rows) != leakRows {
		t.Fatalf("the table drives %d families and the count says %d: a family that stopped firing has to "+
			"fail here rather than pass silently", len(rows), leakRows)
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := row.run()
			if err == nil {
				t.Fatal("this family reported nothing, so the assertion below ran over no element at all")
			}

			checkNoLeakedText(t, err)
		})
	}
}

// checkNoLeakedText asserts over every element the failure reports, and asserts
// there is at least one, which is the anti-vacuity half.
func checkNoLeakedText(t *testing.T, err error) {
	t.Helper()

	elems := Elements(err)
	if len(elems) == 0 {
		t.Fatalf("the failure holds no element: %v", err)
	}

	for i, e := range elems {
		fe, ok := errors.AsType[*Error](e)
		if !ok {
			t.Fatalf("element %d is not one of ferry's own: %v", i, e)
		}

		checkElementIsClean(t, fe)
	}
}

func checkElementIsClean(t *testing.T, e *Error) {
	t.Helper()

	// ferry's own text, total and with no exception: neither the value nor the
	// name reaches it, because the location already says where.
	for _, planted := range []string{planeSecret, planeKey} {
		if strings.Contains(e.msg, planted) {
			t.Errorf("ferry's own text repeats what the plane held: %s", e.msg)
		}
	}

	// The location and the plane's own spelling of it may carry a minted name
	// and may never carry a value.
	for _, where := range []string{e.loc.String(), e.spelled} {
		if strings.Contains(where, planeSecret) {
			t.Errorf("the location repeats a value the plane held: %s", where)
		}
	}

	checkRenderings(t, e)
}

// checkRenderings is the whole printed form, where a driver's own text is the
// one thing that may carry the value: core prints a cause it was handed and the
// obligation not to leak through it is the driver's, in the same shape as every
// other driver obligation.
func checkRenderings(t *testing.T, e *Error) {
	t.Helper()

	if e.driver && e.cause != nil {
		return
	}

	for _, form := range []string{"%v", "%+v", "%s", "%q"} {
		if got := fmt.Sprintf(form, e); strings.Contains(got, planeSecret) {
			t.Errorf("%s repeats a value the plane held: %s", form, got)
		}
	}
}

// The fixtures, one per shape a family needs. Every leaf that is read holds the
// planted secret, so a message that quotes what it was handed quotes it.
type (
	leakLeaf struct {
		N int `ferry:"n"`
	}
	leakSmall struct {
		N int8 `ferry:"n"`
	}
	leakRequired struct {
		N string `ferry:"n,required"`
	}
	leakSection struct {
		DB *leakRequired `ferry:"db"`
	}
	leakList struct {
		Tags []string `ferry:"tags"`
	}
	leakMap struct {
		M map[string]string `ferry:"m"`
	}
)

// leakCases is the matrix: one row per failure family core can author, each
// driven through the entry point over a plane holding the planted text.
func leakCases() []leakCase {
	rows := []leakCase{
		{"a value the plane holds is not the type's", loadOverPlanted[leakLeaf](String(planeSecret))},
		{"a value the plane holds is out of range", loadOverPlanted[leakSmall](Number(planeNumber))},
		{"a value the plane holds is of the wrong kind", loadOverPlanted[leakLeaf](Bool(true))},
	}

	return append(rows, leakEnumerated()...)
}

// loadOverPlanted is one row's whole body for a family raised at a leaf: the
// plane holds v at the fixture's one address, and the load refuses it.
func loadOverPlanted[T any](v Value) func() error {
	return func() error {
		p := newPlane(map[Path]Value{At("n"): v})

		_, err := Load[T](context.Background(), planeSource{p: p})

		return err
	}
}

// leakEnumerated is the rest of the matrix: the families that need a plane which
// lists, a plane that refuses, a writer that cannot forget, or a name of its own
// for an address.
func leakEnumerated() []leakCase {
	return []leakCase{
		{"an address the schema marks required is unset", func() error {
			_, err := Load[leakRequired](context.Background(), planeSource{p: newPlane(nil)})

			return err
		}},
		{"a container the schema marks required holds nothing", func() error {
			p := newPlane(nil)
			p.presence[At("db")] = PresencePresent

			_, err := Load[leakSection](context.Background(), planeSource{p: p})

			return err
		}},
		{"the plane lists a name under a sequence", func() error {
			p := newPlane(map[Path]Value{At("tags").At(planeKey): String(planeSecret)})

			_, err := Load[leakList](context.Background(), treeSource{p: p})

			return err
		}},
		{"the plane lists a position under a mapping", func() error {
			p := newPlane(map[Path]Value{At("m").Elem(0): String(planeSecret)})

			_, err := Load[leakMap](context.Background(), treeSource{p: p})

			return err
		}},
		{"the plane lists a member with no name", func() error {
			p := newPlane(map[Path]Value{At("m").At(""): String(planeSecret)})

			_, err := Load[leakMap](context.Background(), treeSource{p: p})

			return err
		}},
		{"the value carries a key that is empty text", func() error {
			return Dump(context.Background(), leakMap{M: map[string]string{"": planeSecret}},
				planeSink{p: newPlane(nil)})
		}},
		{"the source cannot list what it holds", func() error {
			p := newPlane(map[Path]Value{At("tags").Elem(0): String(planeSecret)})

			_, err := Load[leakList](context.Background(), planeSource{p: p})

			return err
		}},
		{"the sink cannot forget a composite", func() error {
			return Dump(context.Background(), leakMap{M: map[string]string{planeKey: planeSecret}},
				leakSink{p: newPlane(nil)})
		}},
		{"the driver refused a read", func() error {
			p := newPlane(nil)
			p.fail[At("n")] = fmt.Errorf("the store answered %s", planeSecret)

			_, err := Load[leakLeaf](context.Background(), planeSource{p: p})

			return err
		}},
		{"two addresses render to one plane key", func() error {
			_, err := Load[folding](context.Background(), collidingKeys{})

			return err
		}},
		{"the plane names the addresses it refuses", func() error {
			p := newPlane(map[Path]Value{At("m").At(planeKey): Bool(true)})

			_, err := Load[leakMapOfInt](context.Background(), namingSource{p: p})

			return err
		}},
	}
}

// leakMapOfInt is the fixture of the naming row: a mapping whose one minted key
// is the planted name and whose value the type cannot take, so the refusal is
// located at an address that carries the key and printed under the plane's own
// spelling of it.
type leakMapOfInt struct {
	M map[string]int `ferry:"m"`
}

// leakSink is a write half with nothing optional on it at all: no Unsetter, no
// Ensurer, no Committer and no Releaser, which is the sink a schema holding a
// composite is refused against.
type leakSink struct{ p *plane }

func (s leakSink) Bind(addrs *AddressSet) (OpenWriterFunc, error) {
	s.p.bound = addrs

	return func(context.Context) (Writer, error) { return leakWriter{p: s.p}, nil }, nil
}

type leakWriter struct{ p *plane }

func (w leakWriter) Set(ctx context.Context, addr LeafAddr, v Value) error {
	return w.p.Set(ctx, addr, v)
}

// namingSource is the read half with a name of its own for an address, which is
// what puts the plane-naming channel inside this test's domain.
type namingSource struct{ p *plane }

func (s namingSource) Bind(addrs *AddressSet) (OpenFunc, error) {
	s.p.bound = addrs

	return func(context.Context) (Reader, error) { return namingReader{treeReader{p: s.p}}, nil }, nil
}

// namingReader names an address out of the address and nothing else, which is
// what the interface obliges: a name computed from what the plane held would put
// a plane value into a message that promises to carry none.
type namingReader struct{ treeReader }

func (namingReader) PlaneName(addr Path) (string, bool) {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(addr.String(), "/"), "/", "_")), true
}
