package keyform

import (
	"testing"

	"github.com/onhotpath/ferry"
)

// TestQueryTables prints, for every schema issue #184 names and both forms, the
// address-to-plane-key table NewKeys computes, or the refusal it makes instead.
func TestQueryTables(t *testing.T) {
	sets := map[string]*ferry.AddressSet{
		"1. flat      Q,Page":        addrsOf[Flat1](t),
		"2a. nested   DB.Host/Port":  addrsOf[Nested2](t),
		"2b. nested   DB.Auth.User":  addrsOf[Nested3](t),
		"3. slice     Tags []string": addrsOf[Slicey](t),
		"4. map       Limits":        addrsOf[Mappy](t),
		"6a. header   x-request-id":  addrsOf[Header1](t),
		"6b. header   x-forwarded/*": addrsOf[HeaderNested](t),
	}

	for _, name := range sortedKeys(sets) {
		t.Logf("\n=== schema %s ===", name)

		for _, f := range []Form{Bracket, Flat} {
			report(t, "query/"+f.String(), queryPlane(f, DefaultQuerySeparator), sets[name])
		}

		for _, f := range []Form{Bracket, Flat} {
			report(t, "header/"+f.String(), headerPlane(f), sets[name])
		}

		report(t, "header/depth1", NewHeaderDepth1Source().p, sets[name])
	}
}

func report(t *testing.T, label string, p plane, addrs *ferry.AddressSet) {
	t.Helper()

	table, err := Keys(p, addrs)
	if err != nil {
		t.Logf("  %-22s REFUSED: %v", label, err)

		return
	}

	for _, addr := range sortedAddrs(addrs) {
		t.Logf("  %-22s %-22s -> %q", label, addr.String(), table[addr])
		label = ""
	}
}

// TestDynamicKeys runs the map keys and sequence indices a value mints through
// Keys.Open, which is the tier that checks them as they are minted.
func TestDynamicKeys(t *testing.T) {
	addrs := addrsOf[Mappy](t)

	minted := []ferry.Path{
		ferry.At("limits", "rps"),
		ferry.At("limits", "burst"),
		ferry.At("limits", "a.b"),
		ferry.At("limits", "a[b]"),
		ferry.At("limits", "a b"),
		ferry.At("limits", "a-b"),
		ferry.At("limits", "]["),
		ferry.At("limits", ""),
		ferry.At("limits", "0"),
	}

	for _, f := range []Form{Bracket, Flat} {
		for _, p := range []plane{queryPlane(f, DefaultQuerySeparator), headerPlane(f)} {
			keys, err := ferry.NewKeys(addrs, p.name, p.keyf)
			if err != nil {
				t.Fatalf("%s/%s: %v", p.name, f, err)
			}

			open := keys.Open()
			t.Logf("\n--- %s/%s, map keys minted from the value ---", p.name, f)

			for _, addr := range minted {
				key, kerr := open(addr)
				if kerr != nil {
					t.Logf("  %-26s REFUSED %v", addr.String(), kerr)

					continue
				}

				t.Logf("  %-26s -> %q", addr.String(), key)
			}
		}
	}
}

// TestSequenceKeys is question 3: what tags[0] versus a flat join produces.
func TestSequenceKeys(t *testing.T) {
	addrs := addrsOf[Slicey](t)

	for _, f := range []Form{Bracket, Flat} {
		p := queryPlane(f, DefaultQuerySeparator)

		keys, err := ferry.NewKeys(addrs, p.name, p.keyf)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}

		open := keys.Open()
		t.Logf("--- query/%s, sequence positions ---", f)

		for _, addr := range []ferry.Path{
			ferry.At("tags"),
			ferry.At("tags").Elem(0),
			ferry.At("tags").Elem(1),
			ferry.At("tags").Elem(10),
		} {
			key, kerr := open(addr)
			if kerr != nil {
				t.Logf("  %-16s REFUSED %v", addr.String(), kerr)

				continue
			}

			t.Logf("  %-16s -> %q", addr.String(), key)
		}
	}
}

// TestCollisions is question 5: a schema that collides under one form and not
// the other, in both directions, plus what the loose forms show the refusals
// are buying.
func TestCollisions(t *testing.T) {
	cases := []struct {
		name  string
		addrs *ferry.AddressSet
	}{
		{"a nested `a` plus a leaf tagged `a[host]`", addrsOf[BracketCollider](t)},
		{"a nested `a` plus a leaf tagged `a.host`", addrsOf[FlatCollider](t)},
		{"a nested `x/request/id` plus a leaf tagged `x-request-id`", addrsOf[HeaderHyphenCollider](t)},
	}

	for _, c := range cases {
		t.Logf("\n=== %s ===", c.name)

		for _, f := range []Form{Bracket, BracketStrict, Flat, FlatStrict} {
			report(t, "query/"+f.String(), queryPlane(f, DefaultQuerySeparator), c.addrs)
		}

		for _, f := range []Form{Bracket, Flat} {
			report(t, "header/"+f.String(), headerPlane(f), c.addrs)
		}
	}
}

// TestLooseBracketIsNotInjective is the measured reason the [ and ] refusal
// exists at all: without it, two different addresses render alike.
func TestLooseBracketIsNotInjective(t *testing.T) {
	three := ferry.At("x", "y", "z")
	two := ferry.At("x", "y][z")

	loose := Bracket.Query("")

	a, err := loose(three)
	if err != nil {
		t.Fatal(err)
	}

	b, err := loose(two)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("bracket: %-14s -> %q", three.String(), a)
	t.Logf("bracket: %-14s -> %q", two.String(), b)

	if a != b {
		t.Fatalf("expected a collision, got %q and %q", a, b)
	}

	set := ferry.NewAddressSet(three, two)
	if _, err = ferry.NewKeys(set, "query", loose); err == nil {
		t.Fatal("NewKeys accepted a non-injective key function")
	} else {
		t.Logf("NewKeys(bracket) over both: %v", err)
	}

	if _, err = ferry.NewKeys(set, "query", BracketStrict.Query("")); err == nil {
		t.Fatal("strict bracket accepted a segment holding ]")
	} else {
		t.Logf("NewKeys(bracket!) over both: %v", err)
	}

	if _, err = ferry.NewKeys(set, "query", Flat.Query(DefaultQuerySeparator)); err != nil {
		t.Logf("NewKeys(flat) over both: %v", err)
	} else {
		t.Logf("NewKeys(flat) over both: accepted, x.y.z and x.y][z")
	}
}

// TestStrictnessIsOrthogonal shows that the strict/loose axis is the same
// question on both forms, and that a strict header join refuses the single most
// ordinary header a config struct names.
func TestStrictnessIsOrthogonal(t *testing.T) {
	for _, f := range []Form{Bracket, BracketStrict, Flat, FlatStrict} {
		report(t, "header/"+f.String(), headerPlane(f), addrsOf[Header1](t))
	}

	for _, f := range []Form{Bracket, BracketStrict, Flat, FlatStrict} {
		report(t, "query/"+f.String(), queryPlane(f, DefaultQuerySeparator), addrsOf[Header1](t))
	}
}
