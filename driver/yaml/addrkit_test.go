package yaml_test

import (
	"context"
	"testing"

	"github.com/onhotpath/ferry"
)

// Holding a typed address from outside core.
//
// The three address kinds are sealed and the schema compiler is the only thing
// that mints one (ADR-0016), so a test that hands an address to a driver asks
// the compiler for it rather than writing it. Binding a fixture type through a
// source that keeps what it was handed is the whole mechanism.
//
// That is not a hoop. It is the property under test one level down: a driver is
// only ever asked a question the schema said was admissible at the address, so a
// test that could invent an address would be testing something no walk can do.

// addrs is one compiled fixture's address set, with the lookups the cases need.
type addrs struct{ set *ferry.AddressSet }

// addressesOf compiles T and keeps the address set core hands a driver's Bind.
func addressesOf[T any](t *testing.T) addrs {
	t.Helper()

	spy := &setSpy{}
	if _, err := ferry.Bind[T](spy); err != nil {
		t.Fatalf("compiling the address fixture: %v", err)
	}

	return addrs{set: spy.set}
}

// leaf is the address of a place a value can be, and fails the test where the
// fixture does not name one there.
func (a addrs) leaf(t *testing.T, at ferry.Path) ferry.LeafAddr {
	t.Helper()

	for m := range a.set.Seq() {
		if l, ok := m.(ferry.LeafAddr); ok && l.Path() == at {
			return l
		}
	}

	t.Fatalf("the fixture names no leaf at %s", at)

	return ferry.LeafAddr{}
}

// section is the address of a place whose children come from the type.
func (a addrs) section(t *testing.T, at ferry.Path) ferry.SectionAddr {
	t.Helper()

	for m := range a.set.Seq() {
		if s, ok := m.(ferry.SectionAddr); ok && s.Path() == at {
			return s
		}
	}

	t.Fatalf("the fixture names no section at %s", at)

	return ferry.SectionAddr{}
}

// composite is the address of a place whose children come from the value.
func (a addrs) composite(t *testing.T, at ferry.Path) ferry.CompositeAddr {
	t.Helper()

	for m := range a.set.Seq() {
		if c, ok := m.(ferry.CompositeAddr); ok && c.Path() == at {
			return c
		}
	}

	t.Fatalf("the fixture names no composite at %s", at)

	return ferry.CompositeAddr{}
}

// setSpy keeps the address set a Bind was handed, and opens onto a reader that
// answers nothing: the fixture is compiled for its addresses and never read.
type setSpy struct{ set *ferry.AddressSet }

func (s *setSpy) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	s.set = addrs

	return func(context.Context) (ferry.Reader, error) { return silent{}, nil }, nil
}

type silent struct{}

func (silent) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) { return ferry.Value{}, nil }

// The fixtures the cases take their addresses from.
//
// Which Go type named an address is not something a driver can see, so a
// fixture is chosen for the addresses it mints and not for what the document
// under test actually holds: an array names its positions statically, which is
// how a case reads one element of a sequence without a value in hand.
type (
	// scalars names one leaf per observation the read-side document carries,
	// plus one position of a sequence and one member of a mapping.
	scalars struct {
		Nul      string    `ferry:"nul"`
		Empty    string    `ferry:"empty"`
		Value    string    `ferry:"value"`
		Missing  string    `ferry:"missing"`
		Quoted   string    `ferry:"quoted"`
		Yes      string    `ferry:"yes"`
		Ratio    string    `ferry:"ratio"`
		Bin      string    `ferry:"bin"`
		When     string    `ferry:"when"`
		List     [2]string `ferry:"list"`
		Mapping  mapMember `ferry:"map"`
		Anything string    `ferry:"anything"`
		Next     nextHost  `ferry:"next"`
		V        string    `ferry:"v"`
		Key      string    `ferry:"key"`
		Unused   string    `ferry:"unused"`
		Port     string    `ferry:"port"`
		Kept     string    `ferry:"kept"`
		Gone     string    `ferry:"gone"`
	}

	mapMember struct {
		K string `ferry:"k"`
	}

	nextHost struct {
		Host string `ferry:"host"`
	}

	// leafShapes names as leaves the two addresses the read-side document holds
	// containers at, which is the disagreement #252 is about.
	leafShapes struct {
		List string `ferry:"list"`
		Map  string `ferry:"map"`
	}

	// wideList names five positions of a sequence statically, which is how a
	// case reads past the end of a shorter document.
	wideList struct {
		List [5]string `ferry:"list"`
	}

	// colliding names the three address pairs a flattening key function folds
	// together, which are six distinct paths through a document.
	colliding struct {
		DBHost string   `ferry:"db_host"`
		DB     hostOnly `ferry:"db"`
		Hyphen string   `ferry:"feature-flags"`
		Under  string   `ferry:"feature_flags"`
		Upper  string   `ferry:"Host"`
		Lower  string   `ferry:"host"`
	}

	hostOnly struct {
		Host string `ferry:"host"`
	}

	// containers names the addresses whose children come from the value or from
	// the type, which is what a probe and an enumeration are asked about.
	containers struct {
		List []string          `ferry:"list"`
		Map  map[string]string `ferry:"map"`
		Sect *nextHost         `ferry:"section"`
		Miss []string          `ferry:"missing"`
		Nul  []string          `ferry:"nul"`
		Val  []string          `ferry:"value"`
	}
)
