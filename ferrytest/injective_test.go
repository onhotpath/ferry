package ferrytest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestInjectiveIsSilentOverKeysThatDoNotCollide is the ordinary answer: nothing
// to report is an empty result rather than a nil-versus-empty question, because
// a caller ranges over it.
func TestInjectiveIsSilentOverKeysThatDoNotCollide(t *testing.T) {
	got := ferrytest.Injective(nil, "a", "b", "c")

	if len(got) != 0 {
		t.Errorf("Injective reported %q over three distinct keys, want nothing", got)
	}
}

// TestInjectiveReportsBothValuesAndTheTextTheyShare is the check itself.
//
// Two keys rendering to one address are one entry, one of the two is lost with
// no error anywhere, and which one survives is which the walk writes last. So
// the report names both, because naming one leaves the reader with no pair to
// choose between.
func TestInjectiveReportsBothValuesAndTheTextTheyShare(t *testing.T) {
	got := ferrytest.Injective(foldingRegistry(t), lower("Ab"), lower("aB"))

	only := onlyString(t, got)
	for _, want := range []string{`"Ab"`, `"aB"`, `"ab"`} {
		if !strings.Contains(only, want) {
			t.Errorf("report = %q, want %s in it", only, want)
		}
	}
}

// TestInjectiveDisagreesWithTheKeyTypesOwnString is the correction #31 made, and
// the reason this check resolves the text through ferry rather than through a
// format function the caller supplies.
//
// Measured on one type through both routes: the registrant's own String() gives
// two distinct texts where ferry writes one twice. A check taking a func(T)
// string would therefore have reported no collision on a pair that collides,
// with nothing anywhere to say so.
func TestInjectiveDisagreesWithTheKeyTypesOwnString(t *testing.T) {
	values := []lower{"Ab", "aB"}

	own := map[string]bool{}
	for _, v := range values {
		own[v.String()] = true
	}

	if len(own) != len(values) {
		t.Fatalf("the key type's own String() gave %d texts for %d values, and the point of this test is that "+
			"it gives one each", len(own), len(values))
	}

	if got := ferrytest.Injective(foldingRegistry(t), values...); len(got) == 0 {
		t.Error("ferry's own key text collided and Injective reported nothing, so the two routes agree and the " +
			"reason this check does not take a format function has gone")
	}
}

// TestInjectiveSortsItsReport is ADR-0011's determinism invariant applied to a
// report: one string over repeated runs, so a diff of two CI logs is about the
// codec rather than about map iteration order.
func TestInjectiveSortsItsReport(t *testing.T) {
	reg := foldingRegistry(t)
	values := []lower{"Ab", "aB", "Cd", "cD", "Ef", "eF"}

	first := ferrytest.Injective(reg, values...)
	if !slices.IsSorted(first) {
		t.Errorf("Injective returned %q, which is not sorted", first)
	}

	for range 8 {
		if got := ferrytest.Injective(reg, values...); !slices.Equal(got, first) {
			t.Fatalf("Injective returned %q and then %q for one set of values", first, got)
		}
	}
}

// TestInjectiveAsksAboutOneValueOnce is what makes == the constraint's business
// rather than an incident: a value repeated in the call is one key in a Go map,
// so it cannot collide with itself.
func TestInjectiveAsksAboutOneValueOnce(t *testing.T) {
	if got := ferrytest.Injective(nil, "a", "a", "a"); len(got) != 0 {
		t.Errorf("Injective reported %q for one value written three times, want nothing", got)
	}
}

// TestInjectiveReportsAKeyItCannotResolve is the answer for a type ferry will
// not address a map with at all.
//
// A codec registered without AsMapKey is a schema compile error naming that
// method, and it is exactly the moment this check is being run to pre-empt. It
// is data rather than a panic, because everything this function returns is.
func TestInjectiveReportsAKeyItCannotResolve(t *testing.T) {
	reg := ferry.NewRegistry()
	if err := reg.Register(lowerCodec()); err != nil {
		t.Fatalf("registering the probe: %v", err)
	}

	only := onlyString(t, ferrytest.Injective(reg, lower("Ab")))
	if !strings.Contains(only, "AsMapKey") {
		t.Errorf("report = %q, want the method that declares the obligation named", only)
	}
}

// lower is a key type whose ferry text folds case and whose own String() does
// not, which is the disagreement this file rests on.
type lower string

// String is the type's own spelling, and it is injective.
func (l lower) String() string { return string(l) }

// lowerCodec is the registration, without the map-key declaration.
func lowerCodec() ferry.Reg {
	return ferry.StringCodec[lower](
		func(l lower) string { return strings.ToLower(string(l)) },
		func(s string) (lower, error) { return lower(s), nil },
	)
}

// foldingRegistry is lowerCodec declared usable as a map key, which is the claim
// AsMapKey makes and the one Injective exists to discharge over real values.
func foldingRegistry(t *testing.T) *ferry.Registry {
	t.Helper()

	reg := ferry.NewRegistry()
	if err := reg.Register(lowerCodec().AsMapKey()); err != nil {
		t.Fatalf("registering the probe: %v", err)
	}

	return reg
}

// onlyString is the single line a check was expected to report.
func onlyString(t *testing.T, got []string) string {
	t.Helper()

	if len(got) != 1 {
		t.Fatalf("reported %q, want exactly one line", got)
	}

	return got[0]
}
