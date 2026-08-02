package main

// R10: can registration REQUIRE a proof, or only enable one?
//
// ADR-0001: "Core ships the round-trip property harness as a public testing
// package so a registrant can discharge that obligation in their own tests.
// Registering without proving is permitted and forfeits the guarantee."
//
// That sentence is the one #19 exists to implement, and it decides the API's
// shape more than any other. This probe runs both readings.

import (
	"fmt"
	"math/big"
	"net/netip"
	"reflect"
	"strings"
)

// --- the proof, as ADR-0005 specifies it: values, a relation, a golden ------

type RCase[T any] struct {
	Value T
	Want  Value // the boundary Value ferry must produce
}

type RProof interface {
	name() string
	run(reg *Registry, plane func(map[Path]Value) (map[Path]Value, error)) []string
}

type rproof[T any] struct {
	label string
	eq    func(a, b T) bool
	cases []RCase[T]
}

func (p rproof[T]) name() string { return p.label }

// Prove is the harness constructor. Note what it does NOT take: the codec.
func Prove[T any](name string, eq func(a, b T) bool, cases ...RCase[T]) RProof {
	return rproof[T]{name, eq, cases}
}

func (p rproof[T]) run(reg *Registry, plane func(map[Path]Value) (map[Path]Value, error)) []string {
	var fails []string
	withRegistry(reg, func() {
		for i, c := range p.cases {
			type holder struct{ V T }
			in := holder{c.Value}
			dumped, err := dump(reflect.ValueOf(in))
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s[%d]: dump: %v", p.label, i, err))
				continue
			}
			// Column three: the representation. ADR-0005's own measurement is
			// that the property alone is blind to it.
			if got := dumped[Path{}.Name("V")]; got != c.Want {
				fails = append(fails, fmt.Sprintf("%s[%d]: golden: got %s want %s",
					p.label, i, got.GoString(), c.Want.GoString()))
			}
			crossed, err := plane(dumped)
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s[%d]: plane: %v", p.label, i, err))
				continue
			}
			var out holder
			if err := load(crossed, reflect.ValueOf(&out).Elem()); err != nil {
				fails = append(fails, fmt.Sprintf("%s[%d]: load: %v", p.label, i, err))
				continue
			}
			if !p.eq(in.V, out.V) {
				fails = append(fails, fmt.Sprintf("%s[%d]: %#v -> %#v", p.label, i, in.V, out.V))
			}
		}
	})
	return fails
}

func runR10() {
	fmt.Println("--- R10a: the harness needs NO accessor on a registration ---")
	fmt.Println("    A proof exercises the codec through the ordinary walk, so the harness")
	fmt.Println("    takes a registry and a proof and never opens the Reg. That is what")
	fmt.Println("    keeps Reg opaque, which is what keeps `a codec is a pair` structural.")
	fmt.Println("    Signature, in full:")
	fmt.Println("      func RoundTrip(t *testing.T, r *ferry.Registry, p Plane, proofs ...Proof)")
	fmt.Println("    ferrytest is a separate package from ferry, so anything it needed from")
	fmt.Println("    a Reg would be EXPORTED SURFACE FOREVER. It needs nothing.")

	good := NewRegistry()
	_ = good.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	addrProof := Prove("netip.Addr", func(a, b netip.Addr) bool { return a == b },
		RCase[netip.Addr]{netip.Addr{}, String("")},
		RCase[netip.Addr]{netip.MustParseAddr("192.0.2.1"), String("192.0.2.1")},
		RCase[netip.Addr]{netip.MustParseAddr("2001:db8::1"), String("2001:db8::1")},
		RCase[netip.Addr]{netip.MustParseAddr("::ffff:192.0.2.1"), String("::ffff:192.0.2.1")},
	)
	report("a correct codec", addrProof.run(good, identityPlane))

	fmt.Println("\n--- R10b: what the triple catches that the pair does not ---")

	// (1) lossy text: the round-trip property alone catches this one.
	lossy := NewRegistry()
	_ = lossy.Register(StringCodec(
		func(a netip.Addr) string { return strings.SplitN(a.String(), ":", 2)[0] },
		netip.ParseAddr))
	report("lossy text (property catches it)", addrProof.run(lossy, identityPlane))

	// (2) right value, wrong representation: only the golden column catches it.
	nano := NewRegistry()
	_ = nano.Register(TypeCodec(VNumber,
		func(x big.Int) (Value, error) { return Number(x.String()), nil },
		func(v Value) (big.Int, error) {
			var x big.Int
			s, err := v.AsNumber()
			if err != nil {
				return x, err
			}
			x.SetString(s, 10)
			return x, nil
		}))
	bigStringProof := Prove("big.Int as String",
		func(a, b big.Int) bool { return a.Cmp(&b) == 0 },
		RCase[big.Int]{*big.NewInt(0), String("0")},
		RCase[big.Int]{*big.NewInt(1 << 40), String("1099511627776")},
	)
	report("declared Number where the proof says String", bigStringProof.run(nano, identityPlane))
	fmt.Println("      ^ the VALUE round-trips perfectly. Only column three sees it, and")
	fmt.Println("        it is the difference between a codec that works on YAML and one")
	fmt.Println("        that works on env, which is #12's most consequential finding.")

	// (3) not total over the zero value.
	partial := NewRegistry()
	_ = partial.Register(StringCodec(
		func(a netip.Addr) string { return a.String() },
		func(s string) (netip.Addr, error) {
			if s == "" {
				return netip.Addr{}, fmt.Errorf("empty address")
			}
			return netip.ParseAddr(s)
		}))
	report("not total over the zero value", addrProof.run(partial, identityPlane))
	fmt.Println("      ^ ADR-0007: `the zero value is the value a codec sees most often,")
	fmt.Println("        because an unset field is dumped`. This is that, caught.")

	fmt.Println("\n--- R10c: what a proof CANNOT be written for ---")
	fmt.Println("    R3d registered a chan int codec and the table took it. The proof is")
	fmt.Println("    where it stops, and the failure is in the RELATION rather than in the")
	fmt.Println("    values or the golden:")
	fmt.Println("      Prove(\"chan int\", ???, RCase{make(chan int), String(\"ch\")})")
	fmt.Println("      -> == is false for two distinct channels, always")
	fmt.Println("      -> any relation that returns true is a relation that says every")
	fmt.Println("         channel is every other channel")
	fmt.Println("    ADR-0005's `the value does not exist outside the process` is exactly")
	fmt.Println("    this, and it means ferry does not need a mechanism to refuse the four")
	fmt.Println("    permanent kinds at registration: the proof already cannot be written,")
	fmt.Println("    and ADR-0001 already says registering without proving forfeits the")
	fmt.Println("    guarantee. Adding a kind check would refuse a registrant who has")
	fmt.Println("    knowingly forfeited it, which is not core's call to make.")

	fmt.Println("\n--- R10d: so can registration REQUIRE the proof? Priced both ways ---")
	fmt.Println("    Required means the proof is an argument to Register, which puts this")
	fmt.Println("    in production code:")
	fmt.Println("      ferry.Register(ferry.StringCodec(netip.Addr.String, netip.ParseAddr),")
	fmt.Println("        ferrytest.Prove(\"netip.Addr\", ferrytest.Eq[netip.Addr],")
	fmt.Println("          ferrytest.Case{netip.Addr{}, ferry.String(\"\")}, ... ))")
	fmt.Println("    Three measured costs, in order of weight:")
	fmt.Println("      1. main() imports a testing package, and its test fixtures ship in")
	fmt.Println("         the binary. ADR-0002 puts the harness in ferrytest precisely so")
	fmt.Println("         that it is not in core's import graph.")
	fmt.Println("      2. It does not close the hole. ADR-0005 measured a knowingly lossy")
	fmt.Println("         float codec caught by 1 of 4 values, so a required proof with")
	fmt.Println("         one case is a green check that proves nothing, and nothing can")
	fmt.Println("         check the value list.")
	oneCase := Prove("netip.Addr, one case",
		func(a, b netip.Addr) bool { return a == b },
		RCase[netip.Addr]{netip.MustParseAddr("192.0.2.1"), String("192.0.2.1")})
	report("      the lossy codec against a ONE-CASE proof", oneCase.run(lossy, identityPlane))
	fmt.Println("      3. Register returns an error, so a failing proof would be a runtime")
	fmt.Println("         failure at startup rather than a test failure in CI, which is")
	fmt.Println("         the wrong place: ADR-0001 makes the harness route (b) AUTHORITY,")
	fmt.Println("         and authority that fires in production is an outage.")
	fmt.Println("    So: registration ENABLES a proof and cannot require one, which is")
	fmt.Println("    ADR-0001's sentence taken literally rather than strengthened.")

	fmt.Println("\n--- R10e: what core CAN do instead, and it is not nothing ---")
	fmt.Println("    Three things, all measured elsewhere in this prototype:")
	fmt.Println("      - the declared-kind check, one comparison per encode (R2e)")
	fmt.Println("      - a build error for a half pair (R1c)")
	fmt.Println("      - ferrytest ships the harness taking a *Registry, so discharging")
	fmt.Println("        the obligation is four lines rather than a project")
	fmt.Println("    And one thing it can do that it does not do for core's own set:")
	fmt.Println("      ADR-0005's completeness check iterates core's table and asserts")
	fmt.Println("      every member has a proof. A REGISTRY is enumerable in exactly the")
	fmt.Println("      same way, which the text arm is not (ADR-0007's weakest point).")
	fmt.Println("      So ferrytest can offer the same check over a user's registry:")
	r10Completeness(good, []RProof{addrProof})
}

func r10Completeness(r *Registry, proofs []RProof) {
	have := map[string]bool{}
	for _, p := range proofs {
		have[p.name()] = true
	}
	var missing []string
	for t := range r.byType {
		if !have[t.String()] {
			missing = append(missing, t.String())
		}
	}
	sortStrings(missing)
	fmt.Printf("      ferrytest.Complete(registry, proofs...) -> missing=%v\n", missing)
	r2 := NewRegistry()
	_ = r2.Register(
		StringCodec(netip.Addr.String, netip.ParseAddr),
		StringCodec(netip.Prefix.String, netip.ParsePrefix))
	var missing2 []string
	for t := range r2.byType {
		if !have[t.String()] {
			missing2 = append(missing2, t.String())
		}
	}
	sortStrings(missing2)
	fmt.Printf("      after adding one registration and no proof   -> missing=%v\n", missing2)
	fmt.Println("      ^ that is ADR-0005's own completeness check, available to a")
	fmt.Println("        registrant because a registry is enumerable. It is opt-in, like")
	fmt.Println("        the proof it checks for, and it is the difference between")
	fmt.Println("        `permitted and forfeits the guarantee` being a sentence and")
	fmt.Println("        being something a registrant's CI can hold them to.")
}

func report(label string, fails []string) {
	if len(fails) == 0 {
		fmt.Printf("    %-46s PASS\n", label)
		return
	}
	fmt.Printf("    %-46s %d failure(s)\n", label, len(fails))
	for _, f := range fails {
		fmt.Printf("        %s\n", f)
	}
}
