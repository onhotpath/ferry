package ferrytest_test

import (
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// registryWith is one registry holding the codecs one test needs, built fresh
// per test because a registry is complete at birth and a registration claims its
// type only within one registry - so two tests over one type cannot share.
//
// [ferry.NewRegistry] refuses by panicking, having no error to return, and a
// probe this package can no longer register is a change to core's rules rather
// than a failure of the test that names it.
func registryWith(t *testing.T, codecs ...ferry.Registration) *ferry.Registry {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registering the probe: %v", r)
		}
	}()

	return ferry.NewRegistry(codecs...)
}

// addrProof discharges netip.Addr, which is the type ADR-0014's registrant call
// site uses and the one ADR-0009 measured the zero-value check against.
func addrProof() ferrytest.Proof {
	return ferrytest.Type("netip.Addr", ferrytest.Eq[netip.Addr],
		ferrytest.At(netip.Addr{}, ferry.String("")),
		ferrytest.At(netip.MustParseAddr("192.0.2.1"), ferry.String("192.0.2.1")),
	)
}

// TestCompleteOverCoreAlone is the call core makes about its own set, and the
// one ADR-0014 writes into a consumer's test verbatim.
func TestCompleteOverCoreAlone(t *testing.T) {
	t.Parallel()

	if got := ferrytest.Complete(nil, ferrytest.CoreTypes()...); len(got) != 0 {
		t.Errorf("Complete(nil, CoreTypes()...) reports %v, want nothing", got)
	}
}

// TestCompleteReportsEveryTable is the join over three tables, each seen on its
// own: a proof list missing a member of any one of them reports it, and the
// clause says which table it came from.
func TestCompleteReportsEveryTable(t *testing.T) {
	t.Parallel()

	reg := registryWith(t, ferry.StringText[netip.Addr]())

	cases := []struct {
		name   string
		proofs []ferrytest.Proof
		want   string
	}{
		{
			name:   "the identity table",
			proofs: without(reflect.TypeFor[time.Duration]()),
			want:   "time.Duration is in core's identity table and has no proof",
		},
		{
			name:   "a kind's representative",
			proofs: without(reflect.TypeFor[int16]()),
			want:   "int16 is core's representative for kind int16 and has no proof",
		},
		{
			name:   "a registration",
			proofs: ferrytest.CoreTypes(),
			want:   "netip.Addr has a registered codec and has no proof",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := ferrytest.Complete(reg, c.proofs...)
			if !slices.Contains(got, c.want) {
				t.Errorf("Complete reports\n\t%v\nwant it to contain\n\t%q", got, c.want)
			}
		})
	}
}

// without is core's table with one row removed, which is how a missing member
// is produced without writing a second table that could drift from the first.
func without(drop reflect.Type) []ferrytest.Proof {
	out := make([]ferrytest.Proof, 0, len(ferrytest.CoreTypes()))

	for _, p := range ferrytest.CoreTypes() {
		if p.Type() != drop {
			out = append(out, p)
		}
	}

	return out
}

// TestCompleteAcceptsARegistrantsOwnProof is the registrant's half of ADR-0001's
// transferred guarantee: a registration with a proof beside it is discharged by
// the same call core makes about its own set.
func TestCompleteAcceptsARegistrantsOwnProof(t *testing.T) {
	t.Parallel()

	reg := registryWith(t, ferry.StringText[netip.Addr]())

	proofs := append(ferrytest.CoreTypes(), addrProof())
	if got := ferrytest.Complete(reg, proofs...); len(got) != 0 {
		t.Errorf("Complete reports %v, want nothing", got)
	}
}

// TestCompleteSortsAndDeduplicates holds the two properties a report has that
// are not about which members are missing: it is one string over repeated runs
// (ADR-0011), and a type reached through two tables is reported once.
func TestCompleteSortsAndDeduplicates(t *testing.T) {
	t.Parallel()

	reg := registryWith(t,
		ferry.StringText[netip.Addr](),
		ferry.StringText[netip.Prefix](),
	)

	got := ferrytest.Complete(reg)
	if !slices.IsSorted(got) {
		t.Errorf("Complete reports %v, which is not sorted", got)
	}

	seen := map[string]bool{}
	for _, s := range got {
		head, _, _ := strings.Cut(s, " ")
		if seen[head] {
			t.Errorf("Complete reports %s twice in\n\t%v", head, got)
		}

		seen[head] = true
	}

	// Core's identity table is two members, its kind table sixteen, and both
	// registrations are missing too: twenty, with no proof supplied at all.
	const wantMembers = 20

	if len(got) != wantMembers {
		t.Errorf("Complete reports %d members with no proofs supplied, want %d:\n\t%v", len(got), wantMembers, got)
	}
}
