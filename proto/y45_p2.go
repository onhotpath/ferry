package main

// Y5 to Y8: what each candidate rule costs, and the recommendation.

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Y5  what refusing would cost
// ---------------------------------------------------------------------------

func runY5() {
	fmt.Println(`  #45's first question: may the chain claim a map key at all, or is
  keying a map registration-only? The ticket already names the cost of
  refusing - "map[netip.Addr]int needs a registration where a netip.Addr
  LEAF does not, an asymmetry that needs stating rather than discovering".
  Measured, so the asymmetry is a size rather than a word.`)

	fmt.Println("\n  (1) WHAT REFUSING WOULD REFUSE, over Y2's population:")
	reg := NewRegistry()
	done := reg.install()
	for _, p := range yPop {
		c, claims := activeChainCodec(p.t)
		_, core := byIdentity[p.t]
		if !(p.t.Comparable() && claims && c.kind == VString && !core) {
			continue
		}
		fmt.Printf("    map[%-24s]V  leaf: still admitted   key: would need a registration\n", p.name)
	}
	done()

	fmt.Println(`
  (2) THE REMEDY, AND WHAT IT COSTS THE USER. One line, at a call site the
  author does not have yet, and it is the SAME line ADR-0009 already asks
  for. R1 is now the engine, so both columns are measured:`)
	fmt.Printf("    %-30s %-12s %s\n", "", "as FOUND", "under R1, measured")
	fmt.Printf("    %-30s %-12s %s\n", "nothing registered", "<nil>",
		shortenY(errOneLine(Compile[yMap[netip.Addr]](WithRegistry(NewRegistry())))))
	fmt.Printf("    %-30s %-12s %s\n", "TextCodec(...).AsMapKey()", "<nil>",
		errOneLine(Compile[yMap[netip.Addr]](WithRegistry(yAddrReg(true)))))

	fmt.Println(`
  (3) THE ASYMMETRY, STATED AS A SIZE. Under R1 there is exactly one thing
  a chain-claimed type cannot do that a registered one can, and it is
  keying a map. Every row below is measured against a FRESH registry, so
  the four that compile are the chain still claiming the type:`)
	ctx := context.Background()
	fresh := NewRegistry()
	for _, row := range []struct {
		what    string
		err     error
		underR1 string
	}{
		{"leaf field", Compile[yLeaf[netip.Addr]](WithRegistry(fresh)), "unchanged"},
		{"slice element", Compile[yLeaf[[]netip.Addr]](WithRegistry(fresh)), "unchanged"},
		{"pointer to leaf", Compile[yLeaf[*netip.Addr]](WithRegistry(fresh)), "unchanged"},
		{"map VALUE", Compile[yLeaf[map[string]netip.Addr]](WithRegistry(fresh)), "unchanged"},
		{"map KEY", Compile[yMap[netip.Addr]](WithRegistry(fresh)), "the one that moved"},
	} {
		fmt.Printf("    %-18s %-10s %s\n", row.what, shortenY(errOneLine(row.err)), row.underR1)
	}
	v, _ := dumpTo(ctx, yLeaf[netip.Addr]{netip.MustParseAddr("192.0.2.1")}, WithRegistry(fresh))
	fmt.Printf("    and the leaf still lands as %s, unregistered\n", v[Path{}.Name("v")].GoString())

	fmt.Println(`
  So the asymmetry is one position out of five, and it is the one position
  where a lossy text form costs a user DATA rather than legibility. That is
  the trade, and it is smaller than "the chain may not claim the type".`)
}

// ---------------------------------------------------------------------------
// Y6  the four candidate rules
// ---------------------------------------------------------------------------

// yRule is a candidate admission rule for a map key, written as a predicate so
// each can be applied to the population without touching the engine.
type yRule struct {
	name  string
	admit func(reflect.Type) bool
}

func runY6() {
	fmt.Println(`  #45's second question: if the chain may claim a key, where is the
  obligation communicated, given there is no call site? Four candidate
  answers, each built and applied rather than argued.`)

	// Installed only for the rule table. Held any longer it would deadlock the
	// loadFrom at the end of this probe, because install() takes a mutex that
	// the entry points take again.
	reg := NewRegistry()
	done := reg.install()

	rules := []yRule{
		{"R0 status quo: the chain keys a map", func(t reflect.Type) bool {
			c, ok := activeChainCodec(t)
			return ok && c.kind == VString
		}},
		{"R1 registration-only (DECIDED, shipped)", func(reflect.Type) bool { return false }},
		{"R2 compile-time warning", func(t reflect.Type) bool {
			c, ok := activeChainCodec(t)
			return ok && c.kind == VString
		}},
		{"R3 detect the collapse at Dump (SHIPPED)", func(t reflect.Type) bool {
			c, ok := activeChainCodec(t)
			return ok && c.kind == VString
		}},
	}

	fmt.Printf("\n  %-38s %s\n", "rule", "what it admits, over Y2's population")
	for _, r := range rules {
		var in []string
		for _, p := range yPop {
			_, core := byIdentity[p.t]
			if p.t.Comparable() && !core && r.admit(p.t) {
				in = append(in, p.name)
			}
		}
		fmt.Printf("  %-38s %d: %s\n", r.name, len(in), strings.Join(in, " "))
	}
	done()

	fmt.Println(`
  R0 is what ferry shipped as FOUND. Y4 is its measurement: two entries
  dropped, no error, winner by map iteration order. It is out, because
  ADR-0001 rules out silently ignoring anything and this ignores a map
  entry. R1 is the decided rule and is now the engine, which is why Y1's
  third row and Y4's first line both refuse.

  R2 needs a channel ferry does not have. Measured against the codebase:`)
	fmt.Println("    ferry.Error classes                 : ErrSchema ErrMissing ErrValue ErrPlane ErrDriver")
	fmt.Println("    a non-fatal class among them        : none")
	fmt.Println("    a logger, a Warn hook, an io.Writer : none in the API")
	fmt.Println(`    ADR-0011 defines an error model and no warning model, and adding
    one to communicate ONE obligation is a large surface for a small rule.
    A diagnostic that fires as an ERROR on a type the author never
    mentioned is R1 with a worse message, not a third option.

  R3 is the one nobody had costed, and it is the interesting one, because
  ferry can detect the collapse EXACTLY - with no registrant, no value
  list and no injectivity proof - simply by noticing that two keys of one
  map produced one address.

  IT IS NOW SHIPPED, alongside R1 rather than instead of it, which is
  what ADR-0007 records. Under R1 the only keys that reach a dump at all
  are core's own and ones a registrant declared with .AsMapKey(), so R3
  is the check that catches a WRONG .AsMapKey() - the case ADR-0009 says
  it leaves to the registrant's own tests. The rule column below is
  therefore what each rule ADMITS at compile; R3 admits what R0 does and
  refuses later, which is why it reads the same.`)

	fmt.Println("\n  (a) DOES IT CATCH THE CASE? Y4's map, through the same detection:")
	for _, tc := range []struct {
		name string
		keys []YID
	}{
		{"three keys, one text", []YID{{"Prod"}, {"prod"}, {"PROD"}}},
		{"three keys, three texts", []YID{{"a"}, {"b"}, {"c"}}},
	} {
		m := reflect.MakeMap(reflect.TypeFor[map[YID]int]())
		for i, k := range tc.keys {
			m.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(i))
		}
		fmt.Printf("    %-26s -> %s\n", tc.name, errOneLine(yCollapse(m)))
	}

	fmt.Println(`
  (b) WHAT IT COSTS. The walk ALREADY renders every key to text and sorts
  the keys by that text, in e_walk.go's members(). So the check is an
  adjacent-pair scan over a list that is already built and already sorted.
  Priced as the scan ALONE against the work the walk already does, so the
  ratio is the answer rather than the two absolute numbers:`)
	for _, n := range []int{8, 64, 512} {
		texts := yKeyTexts(yBigMap(n))
		already := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				yKeyTexts(yBigMapCached(n))
			}
		})
		scan := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				yScan(texts)
			}
		})
		fmt.Printf("    %4d keys   the walk already does %8d ns %2d allocs   the scan adds %6d ns %d allocs   %.2f%%\n",
			n, already.NsPerOp(), already.AllocsPerOp(), scan.NsPerOp(), scan.AllocsPerOp(),
			100*float64(scan.NsPerOp())/float64(already.NsPerOp()))
	}

	fmt.Println(`
  (c) AND WHAT IT DOES NOT REACH, which is the argument against it on its
  own. A collapse is only visible on DUMP, because on Load the plane holds
  one address and there is nothing to compare it with:`)
	ctx := context.Background()
	back, lerr := loadFrom(ctx, yMap[YID]{}, map[Path]Value{Path{}.Name("m").Name("prod"): Number("1")})
	fmt.Printf("    Load a map with one address -> %d key(s), err=%v\n", len(back.M), errOneLine(lerr))
	fmt.Println(`    Nothing was lost on that Load, and nothing could be: the loss
    happened on whichever Dump wrote the file. So R3 is a detection at the
    moment of loss, which is the right moment, but it is a RUNTIME error
    where ADR-0009 chose a SCHEMA one - and ADR-0005's whole framing is
    that a type outside the set is loud at schema compile rather than at
    the first bad value.`)
}

// yCollapse is R3, written as a free function over a map value so it can be
// measured without touching the engine. It mirrors e_walk.go's members()
// exactly: render each key, sort by the text, and compare adjacent pairs.
func yCollapse(m reflect.Value) error {
	if i := yScan(yKeyTexts(m)); i >= 0 {
		return fmt.Errorf(
			"ferry: two keys of this %s address %q; the key type's text is not "+
				"injective, so one entry would be lost", m.Type(), yKeyTexts(m)[i])
	}
	return nil
}

// yScan is the whole of R3's added work: one pass over a list the walk has
// already rendered and already sorted. It returns the index of the first
// duplicate, or -1.
func yScan(texts []string) int {
	for i := 1; i < len(texts); i++ {
		if texts[i] == texts[i-1] {
			return i
		}
	}
	return -1
}

// yBigMapCached keeps the map out of the benchmark's own timing, so what is
// measured is the render-and-sort the walk does and not the map construction.
var yBigMapCache = map[int]reflect.Value{}

func yBigMapCached(n int) reflect.Value {
	if v, ok := yBigMapCache[n]; ok {
		return v
	}
	v := yBigMap(n)
	yBigMapCache[n] = v
	return v
}

// yKeyTexts asks THE CHAIN, by name, for the text it would give a key.
//
// It used to call the engine's `mapKeyText`, which reached a package global to
// run the identity/chain/kind cascade at walk time. #58 deleted that: the key
// text is now `resolveMapKey`'s pair, taken at compile and carried on the
// compiled node. Routing this probe through `resolveMapKey` would make it
// measure nothing, because ADR-0007's #45 reversal refuses a chain-claimed key
// outright and both YID and YPair are exactly that.
//
// And measuring nothing is the wrong answer here: R3's cost is the cost of the
// render-and-sort the walk already does, and the counterfactual R0 needs the
// text a chain arm WOULD have produced. So the probe names its own source
// rather than borrowing the engine's, which is also what stops it silently
// re-becoming a second authority over the key text.
func yKeyTexts(m reflect.Value) []string {
	c, ok := activeChainCodec(m.Type().Key())
	if !ok {
		panic(fmt.Sprintf("y45: %s is not chain-claimed, so there is no counterfactual to measure", m.Type().Key()))
	}
	keys := m.MapKeys()
	texts := make([]string, len(keys))
	for i, k := range keys {
		v, err := c.enc(k)
		if err != nil {
			panic(err)
		}
		texts[i] = v.Text()
	}
	slices.Sort(texts)
	return texts
}

func yBigMap(n int) reflect.Value {
	m := reflect.MakeMap(reflect.TypeFor[map[YPair]int]())
	for i := range n {
		m.SetMapIndex(reflect.ValueOf(YPair{uint8(i / 256), uint8(i % 256)}), reflect.ValueOf(i))
	}
	return m
}

// ---------------------------------------------------------------------------
// Y7  can injectivity be checked with no registrant
// ---------------------------------------------------------------------------

func runY7() {
	saysY("ADR-0009", `It proposes ferrytest.Injective over a registrant's value list.
	#45 asks whether that has anything to say when there is no registrant.`)

	fmt.Println(`  Injectivity is a property of a function over a DOMAIN, so checking it
  needs values. Three sources of values, and what each can reach:`)

	fmt.Println("\n  (1) THE TYPE ALONE, at schema compile. Nothing. Measured by asking")
	fmt.Println("      what a compiler has in hand for the two Y3 types:")
	reg := NewRegistry()
	done := reg.install()
	for _, p := range []struct {
		name string
		t    reflect.Type
	}{{"main.YID (lossy)", reflect.TypeFor[YID]()}, {"main.YPair (total)", reflect.TypeFor[YPair]()}} {
		c, _ := activeChainCodec(p.t)
		z, _ := c.enc(reflect.New(p.t).Elem())
		fmt.Printf("      %-20s kind=%v  zero encodes to %q  cardinality=unknown\n", p.name, c.kind, z.Text())
	}
	done()
	fmt.Println(`      The two are indistinguishable from the type. Both are comparable
      structs with a text pair, both encode their zero to a string, and
      neither exposes anything a compiler could count. Enumerating the
      domain is not available either: main.YPair has 65536 inhabitants and
      main.YID has as many as there are strings.`)

	fmt.Println(`
  (2) A REGISTRANT'S VALUE LIST, which is ADR-0009's answer. Unavailable
      by construction here: the whole premise of #45 is that nobody
      registered, so there is no registrant and no list. ferrytest.Injective
      has nothing to be called on and nowhere to be called from.

  (3) THE VALUES A REAL DUMP HOLDS, which is Y6's R3. Available, exact for
      the map in hand, and available at no cost - but only for that map,
      and only on Dump. It proves nothing about the type.

  So the answer to #45's third question is NO for the check as ADR-0009
  frames it, and the reason is not an implementation gap: injectivity over
  a type is not decidable from the type, and the only party who can supply
  a domain is the party who did not show up.`)

	fmt.Println(`
  ONE THING WORTH RECORDING, because it says the obligation cannot simply
  be moved to the harness. ADR-0001's route (b) is "registration carries
  the proof". A chain arm is a codec nobody registered, so there is no
  registration to carry it, and route (a) - core's own table - does not
  cover it either, because core does not own the type. A chain-claimed map
  key is outside BOTH of ADR-0001's routes to a guarantee.`)
}

// ---------------------------------------------------------------------------
// Y8  the recommendation
// ---------------------------------------------------------------------------

func runY8() {
	fmt.Println(`  DECIDED, and shipped on this branch: R1, keying a map is
  registration-only. The chain may claim a type at a leaf, in a slice,
  behind a pointer and as a map VALUE, and it may not claim a map KEY.
  ADR-0007 reversed its own sentence; typeset.go's validMapKey no longer
  has a chain arm, and mapKeyRefusal carries the message below.

  The four things it rests on, each measured above:

  (1) IT IS THE REVERSIBLE DIRECTION, which #45 already says and Y5 sizes.
      Refusing costs one position out of five, with a one-line remedy that
      is the SAME line ADR-0009 already asks for. Admitting and being wrong
      costs a dropped map entry with no error, which is unrecoverable
      because the plane never held the lost key.

  (2) IT REPAIRS AN INVERSION RATHER THAN ADDING AN ASYMMETRY. Y1: today
      registering a type makes it LESS usable than not registering it, and
      that is the only place in ferry where that is true. Under R1 the
      refusal is lifted by ADDING .AsMapKey(), which is the direction
      ADR-0009 chose and the direction every other rule in ferry runs in.

  (3) NEITHER OF THE OTHER TWO CANDIDATES SURVIVES ITS OWN COST. R2 needs a
      warning channel ADR-0011 does not define, to communicate one
      obligation. R3 is free to run and detects the loss exactly, but it is
      a RUNTIME refusal where ADR-0005's framing is that a type outside the
      set is loud at SCHEMA COMPILE, and it cannot see the Load side at all.

  (4) ADR-0001 HAS NO ROUTE TO THE GUARANTEE HERE (Y7). Core's table does
      not cover the type and there is no registration to carry the proof,
      so admitting the key admits a member whose proof cannot be written -
      which is the exact phrase ADR-0005 uses for what it refuses.

  WHAT R1 IS NOT, and this matters for the diagnostic. It is not a claim
  that the chain's text is lossy. Y3 measured the opposite: every stdlib
  type the chain claims is injective on every adversarial value the probe
  could build, 4-in-6 and zoned addresses included. R1 refuses because
  nobody can be ASKED, not because the answer would be no.

  THE DIAGNOSTIC, which is where ADR-0009 says the obligation lives, and
  which has to name a type the author never mentioned. Read out of the
  engine rather than out of this file, so the two cannot drift:`)

	fmt.Printf("\n    %s\n", errOneLine(Compile[yMap[netip.Addr]](WithRegistry(NewRegistry()))))

	fmt.Println(`
  It names the type, why the rule exists, and the exact remedy, and the
  remedy is a call the author can write without understanding injectivity.
  That is the most ADR-0009's "the diagnostic is the only moment a
  registrant is guaranteed to read" can be made to do for a non-registrant.

  WHAT THIS DOES NOT CLOSE:

  - #31 is untouched, and deliberately. map[time.Time]string collapses on
    core's OWN pre-seeded entry, which ADR-0009 says its opt-in
    "deliberately does not reach". R1 changes nothing there.
  - R3 IS implemented, alongside R1. It is free, it is exact, and under R1
    the only types that can reach it are core's own and ones a registrant
    declared with .AsMapKey(). In other words R1 makes R3 the check that
    catches a WRONG .AsMapKey(), which is the case ADR-0009 currently
    leaves entirely to the registrant's own tests.
  - Whether the leaf/key split should be spelled in the chain arm rather
    than at the map node. That is a shape question for whoever writes the
    ADR, not a decision this measurement forces.`)
}

// shortenY keeps a refusal readable in a table column. The full text is printed
// once, in Y8, and read out of the engine there.
func shortenY(s string) string {
	if len(s) > 26 {
		return s[:23] + "..."
	}
	return s
}

var _ = context.Background
