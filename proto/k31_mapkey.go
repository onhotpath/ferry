package main

// #31: core's admissible map key set is not injective.
//
// ADR-0005 restricts a map key to `string`, the integer kinds, and a registered
// codec whose form is a `String`, and states the obligation on the registrant:
//
//	A key codec's text must be injective over the key type.
//	Two distinct keys producing one text collapse into one address, silently.
//
// It does not apply that rule to core's own identity table, which validMapKey
// admits wholesale, and `map[time.Time]string` therefore loses keys.
//
// Run: `K31=<n|all> GOTOOLCHAIN=go1.27rc2 go run .` from proto/.
//
// Every probe runs through the tip's own entry points - Dump, Load[T],
// Compile[T] - and never through the superseded walk.go, except where a probe
// is about walk.go.

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func runK31(sel string) {
	all := sel == "all"
	pick := func(n string) bool { return all || sel == n }

	if pick("1") {
		k31a()
	}
	if pick("2") {
		k31b()
	}
	if pick("3") {
		k31c()
	}
	if pick("4") {
		k31d()
	}
	if pick("5") {
		k31e()
	}
	if pick("6") {
		k31f()
	}
	if pick("7") {
		k31g()
	}
	if pick("8") {
		k31h()
	}
	if pick("9") {
		k31i()
	}
	if pick("10") {
		k31j()
	}
	if pick("11") {
		k31k()
	}
}

// asShipped runs f in the world the tip shipped: no key opt-in for core's own
// table and no mint-time collision check.
//
// It defers the TIP's OWN values rather than hardcoded ones, which is the leak
// section 8.3 of the audit found in A3 and A5 and which made thirteen verdicts
// describe a world that did not exist.
//
// The third seam it used to carry - keyCodecInstalled, the stopgap install
// around the two caller-facing walks - is gone under #58, which resolved the
// key codec into the compiled node so there is no registry for the walk to
// install and no second world to switch to. k31LegacyKeyText below is how that
// finding stays reproducible.
func asShipped(f func()) {
	defer func(a, b bool) { keyProvedOnly, keyCollisionCheck = a, b }(keyProvedOnly, keyCollisionCheck)
	keyProvedOnly, keyCollisionCheck = false, false
	f()
}

// withRule runs f with #31's decision on, which is also the package default.
func withRule(f func()) {
	defer func(a, b bool) { keyProvedOnly, keyCollisionCheck = a, b }(keyProvedOnly, keyCollisionCheck)
	keyProvedOnly, keyCollisionCheck = true, true
	f()
}

// k31LegacyKeyText is walk.go's deleted `mapKeyText`, verbatim, kept HERE so
// K31=10's finding stays reproducible after #58 deleted the function it is
// about.
//
// It is the three-step cascade run at WALK time - identity table, then the
// chain, then kind - with a last line that formats the key using the reflect
// fallback verb. That last line is the defect: it consults fmt.Stringer, which
// ADR-0005 refuses outright and by name, for a type nobody admitted through it.
//
// It lives in the probe rather than in the engine because a probe reproducing a
// deleted behaviour is evidence, and an engine retaining one is a second
// authority - which is the whole of what #58 removed.
func k31LegacyKeyText(k reflect.Value) string {
	if c, ok := identityLookup(k.Type()); ok {
		if v, err := c.enc(k); err == nil && v.Kind() == VString {
			return v.Text()
		}
	}
	if c, ok := activeChainCodec(k.Type()); ok {
		if v, err := c.enc(k); err == nil && v.Kind() == VString {
			return v.Text()
		}
	}
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(k.Uint(), 10)
	}
	return fmt.Sprintf("%v", k.Interface())
}

// ---------------------------------------------------------------------------
// K31=10  The key text is a third lookup, and it is the only one the compiled
//         schema does not carry.
// ---------------------------------------------------------------------------

func k31j() {
	hdr("K31=10  a registered key codec's text is not what Dump writes - FIXED under #58")

	fmt.Println(`  Found by K31=6 route (3): a registered codec whose text is deliberately
  non-injective produced TWO addresses instead of one. It is not that the
  collision went away. It is that the codec never ran.

  ADR-0009 resolves a codec into the compiled schema, and ADR-0007's own third
  defect is "the declared kind must live INSIDE the identity-table entry, not
  beside it ... two lookups for one decision is how a chain drifts". The key
  text WAS a THIRD lookup: mapKeyText consulted the identity table, then the
  chain, then the kind, at walk time, off the package-level activeReg.

  compileOnce installs the registry. dumpTo and loadFrom install it. The two
  entry points that a caller actually reaches - SinkBinding.Dump and
  Binding.LoadOver - did not, because the schema was supposed to carry
  everything the walk needs.

  FILED AS #58 AND FIXED THERE. resolveMapKey now answers admission and
  rendering in one lookup and the compiled node carries the pair, so the walk
  reaches no registry for a key at all. The old resolution is reproduced below
  by k31LegacyKeyText - walk.go's deleted mapKeyText, verbatim - so the finding
  survives the function it was about.`)

	ctx := context.Background()
	reg := NewRegistry()
	if err := reg.Register(TextCodec[netip.Addr](VString).AsMapKey()); err != nil {
		fmt.Println("  Register:", err)
		return
	}
	type addrMap struct {
		V map[netip.Addr]int `ferry:"v"`
	}
	in := addrMap{map[netip.Addr]int{
		netip.MustParseAddr("10.0.0.1"): 1,
		netip.MustParseAddr("10.0.0.2"): 2,
	}}

	fmt.Println("\n  map[netip.Addr]int, codec registered with .AsMapKey():")

	viaEntry := map[Path]Value{}
	var e1 error
	asShipped(func() { e1 = Dump(ctx, in, MemSink{viaEntry}, WithRegistry(reg)) })
	fmt.Printf("    through Dump      err=%v\n", e1)
	for _, p := range sortedAddrs(viaEntry) {
		fmt.Printf("      %-44s %s\n", p, viaEntry[p].GoString())
	}

	var viaProbe map[Path]Value
	var e2 error
	asShipped(func() { viaProbe, e2 = dumpTo(ctx, in, WithRegistry(reg)) })
	fmt.Printf("    through dumpTo    err=%v\n", e2)
	for _, p := range sortedAddrs(viaProbe) {
		fmt.Printf("      %-44s %s\n", p, viaProbe[p].GoString())
	}

	same := len(viaEntry) == len(viaProbe)
	if same {
		for p := range viaEntry {
			if _, ok := viaProbe[p]; !ok {
				same = false
			}
		}
	}
	fmt.Printf("    identical address sets: %v\n", same)

	fmt.Println(`
  Both were always legible here, because netip.Addr's kind fallback was
  unreachable and its ADR-0007 chain arm is off, so the two routes agreed by
  luck. The type that shows it is one whose registered text differs from what
  the fallback produces:`)

	reg2 := NewRegistry()
	_ = reg2.Register(StringCodec(
		func(h k31Host) string { return h.Name },
		func(s string) (k31Host, error) { return k31Host{s, 0}, nil }).AsMapKey())
	type hostMap struct {
		V map[k31Host]int `ferry:"v"`
	}
	hin := hostMap{map[k31Host]int{{"api", 80}: 1, {"api", 443}: 2}}

	// THE OLD RESOLUTION, run directly. With no registry installed - which is
	// the state the two entry points walked in - the cascade fell through to its
	// last line and formatted the struct with the reflect fallback verb.
	fmt.Println("\n    what the walk USED to resolve, with no registry installed:")
	for _, h := range []k31Host{{"api", 80}, {"api", 443}} {
		fmt.Printf("      k31LegacyKeyText(%v) -> %q\n", h, k31LegacyKeyText(reflect.ValueOf(h)))
	}
	fmt.Println("      ^ two distinct texts, so the collapse check saw no collision")
	fmt.Println("    what the registrant's codec says, and what ferry resolves now:")
	dn := reg2.install()
	for _, h := range []k31Host{{"api", 80}, {"api", 443}} {
		fmt.Printf("      k31Text(%v) -> %q\n", h, k31Text(h))
	}
	dn()
	fmt.Println("      ^ one text for both, which is the collision the registrant created")

	he := map[Path]Value{}
	var err error
	asShipped(func() { err = Dump(ctx, hin, MemSink{he}, WithRegistry(reg2)) })
	fmt.Printf("\n    through Dump      err=%v  addresses=%d\n", err, len(he))
	for _, p := range sortedAddrs(he) {
		fmt.Printf("      %-44s %s\n", p, he[p].GoString())
	}
	var hp map[Path]Value
	var err2 error
	asShipped(func() { hp, err2 = dumpTo(ctx, hin, WithRegistry(reg2)) })
	fmt.Printf("    through dumpTo    err=%v  addresses=%d\n", err2, len(hp))
	for _, p := range sortedAddrs(hp) {
		fmt.Printf("      %-44s %s\n", p, hp[p].GoString())
	}

	fmt.Println(`
  Both routes now write the codec's text, off one registry and one value.
  Before #58 the entry point wrote two addresses and the probe wrote one: the
  registrant said .AsMapKey(), the codec is what ferry promised to use, and the
  walk used the reflect fallback verb on a struct instead.

  Three things followed, and only the first was a bug fix.

  (1) The last line above formats the key with the reflect fallback verb, which
      is a representation nobody chose for a type nobody admitted - and it is
      worse than that, because the verb consults fmt.Stringer, which ADR-0005
      refuses outright and by name. validMapKey and mapKeyText were two
      authorities over one question, which is the pattern ADR-0005's
      identity-before-kind rule and ADR-0007's declared-kind rule each exist to
      stop. The key codec belongs in the compiled node. IT IS THERE NOW, and
      both authorities are one function, resolveMapKey.

  (2) EVERY injectivity statement about a registered or chain-admitted key type
      was a statement about the wrong function until it was. .AsMapKey() gated a
      codec the walk did not call.

  (3) It is the reason a key proof cannot take a format function from the
      prover. It has to ask ferry what ferry writes - see K31=7.

  And the second-order effect, which is why this mattered beyond a wrong
  string: the collapse check operates on that text, so with the fallback text
  being accidentally injective where the codec is not, the check was UNREACHABLE
  through Dump for exactly the class of type it was built for. It fires now:`)
	withRule(func() {
		out := map[Path]Value{}
		fmt.Printf("    %v\n", Dump(ctx, hin, MemSink{out}, WithRegistry(reg2)))
	})
}

// ---------------------------------------------------------------------------
// K31=1  The collision, through the tip's own Dump, in three shapes.
// ---------------------------------------------------------------------------

type k31Times struct {
	V map[time.Time]string `ferry:"v"`
}

func k31a() {
	hdr("K31=1  map[time.Time]string collapses, through Dump")

	fmt.Println(`  ADR-0007 found this with one pair. Three pairs, because the three have
  different remedies and only one of them is exotic.`)

	type shape struct {
		label string
		a, b  time.Time
		why   string
	}
	now := time.Now()
	fixedA := time.FixedZone("UTC", 0)
	fixedB := time.FixedZone("UTC", 0)
	shapes := []shape{
		{"zone name", time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0)),
			"two Locations, one offset. ADR-0007's own pair."},
		{"monotonic", now, now.Round(0),
			"time.Now() carries a monotonic reading; Round(0) strips it."},
		{"two FixedZones", time.Date(2026, 1, 15, 12, 0, 0, 0, fixedA),
			time.Date(2026, 1, 15, 12, 0, 0, 0, fixedB),
			"identical name, identical offset, two *Location values."},
	}

	fmt.Println("\n  AS THE TIP SHIPS: core's identity table admits any member as a key.")
	fmt.Printf("\n  %-16s %-8s %-9s %-9s %s\n", "pair", "a == b", "a.Equal(b)", "same text", "Go keys -> ferry addresses")
	asShipped(func() {
		for _, s := range shapes {
			ta, _ := s.a.MarshalText()
			tb, _ := s.b.MarshalText()
			m := map[time.Time]string{s.a: "a", s.b: "b"}
			got, err := k31DumpMap(k31Times{m})
			note := fmt.Sprintf("%d -> %d", len(m), len(got))
			if err != nil {
				note += "  err=" + shorten2(fmt.Sprint(err), 40)
			} else {
				note += "  err=<nil>"
			}
			fmt.Printf("  %-16s %-8v %-9v %-9v %s\n", s.label, s.a == s.b, s.a.Equal(s.b), string(ta) == string(tb), note)
		}
	})
	for _, s := range shapes {
		fmt.Printf("\n  %s: %s\n", s.label, s.why)
	}

	fmt.Println(`
  Two Go keys, one address, nil error, on all three. Which entry survives is
  map iteration order, and P12 and P19 each carry one line that is flaky across
  runs of the same binary for exactly this reason.

  The third pair is the one that decides #31, and section K31=2 is why.`)

	fmt.Println("\n  WITH THE RULE, which is this branch's default:")
	withRule(func() {
		m := map[time.Time]string{shapes[0].a: "a", shapes[0].b: "b"}
		_, err := k31DumpMap(k31Times{m})
		fmt.Printf("    %v\n", err)
	})
}

// k31DumpMap dumps through the entry point and returns what reached the sink.
func k31DumpMap[T any](v T) (map[Path]Value, error) {
	m := map[Path]Value{}
	err := Dump(context.Background(), v, MemSink{m})
	return m, err
}

// ---------------------------------------------------------------------------
// K31=2  There is no injective text form for time.Time, and that is a property
//        of the type rather than of RFC 3339.
// ---------------------------------------------------------------------------

func k31b() {
	hdr("K31=2  no text can be injective over time.Time")

	fmt.Println(`  "Keep it with a stated caveat" is only available if SOME text form is
  injective, so that a registrant could supply one. Measured, none is, and the
  reason is structural:

    type Time struct { wall uint64; ext int64; loc *Location }

  == compares the loc POINTER. time.FixedZone allocates a fresh *Location on
  every call, so two calls with the same name and the same offset produce two
  values that are distinct under == and identical under every encoding in the
  standard library.`)

	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("UTC", 0))
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("UTC", 0))

	mt := func(t time.Time) string { s, _ := t.MarshalText(); return string(s) }
	mb := func(t time.Time) string { s, _ := t.MarshalBinary(); return fmt.Sprintf("%x", s) }
	gb := func(t time.Time) string { s, _ := t.GobEncode(); return fmt.Sprintf("%x", s) }
	mj := func(t time.Time) string { s, _ := t.MarshalJSON(); return string(s) }

	rows := []struct {
		name string
		f    func(time.Time) string
	}{
		{"MarshalText (ferry's, RFC 3339)", mt},
		{"MarshalJSON", mj},
		{"MarshalBinary", mb},
		{"GobEncode", gb},
		{"Format(RFC3339Nano)", func(t time.Time) string { return t.Format(time.RFC3339Nano) }},
		{`Format("...MST")`, func(t time.Time) string { return t.Format("2006-01-02T15:04:05.999999999Z07:00 MST") }},
		{"UnixNano, base 10", func(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }},
		{"String()", time.Time.String}, {"GoString()", time.Time.GoString},
		{"%#v", func(t time.Time) string { return fmt.Sprintf("%#v", t) }},
		{"Location().String()", func(t time.Time) string { return t.Location().String() }},
		{"Zone() name+offset", func(t time.Time) string {
			n, o := t.Zone()
			return n + "/" + strconv.Itoa(o)
		}},
	}

	fmt.Printf("\n  a == b: %v      a.Equal(b): %v\n\n", a == b, a.Equal(b))
	fmt.Printf("  %-34s %-9s %s\n", "encoding", "distinct", "what both produce")
	distinct := 0
	for _, r := range rows {
		sa, sb := r.f(a), r.f(b)
		d := sa != sb
		if d {
			distinct++
		}
		fmt.Printf("  %-34s %-9v %s\n", r.name, d, shorten2(sa, 44))
	}
	fmt.Printf("\n  %d of %d encodings distinguish them.\n", distinct, len(rows))

	fmt.Println(`
  So the honest statement is stronger than "RFC 3339 is not injective over
  time.Time". It is:

      No text form is injective over time.Time, because == compares a pointer
      and no text carries one.

  That kills the option of keeping time.Time as a key type with a registrant
  supplying a better codec: there is no better codec. It also means a
  diagnostic that says "register an injective codec for it" would be asking
  for something that does not exist.`)
}

// ---------------------------------------------------------------------------
// K31=3  Injectivity is over ==, not over the proof relation, and the two
//        disagree for exactly one member of core's set.
// ---------------------------------------------------------------------------

func k31c() {
	hdr("K31=3  the relation the obligation is stated over")

	fmt.Println(`  ADR-0005 says "a key codec's text must be injective over the key type"
  and does not say under which equality. It has to say, because ADR-0005 also
  makes the equality relation PER TYPE and required:

      Type[T](name string, eq func(a, b T) bool, cases ...Case[T]) Proof

  Two candidate readings, and they give different answers for time.Time:`)

	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))
	ta, _ := a.MarshalText()
	tb, _ := b.MarshalText()
	m := map[time.Time]string{a: "a", b: "b"}

	fmt.Printf("\n  under the type's PROOF relation (time.Time.Equal):\n")
	fmt.Printf("    a.Equal(b) = %v, so a and b are ONE key, and one text is correct.\n", a.Equal(b))
	fmt.Printf("    the proof relation says the codec is injective here.\n")
	fmt.Printf("\n  under Go's ==:\n")
	fmt.Printf("    a == b = %v, so a and b are TWO keys, and one text loses one.\n", a == b)
	fmt.Printf("    len(map[time.Time]string{a:..., b:...}) = %d\n", len(m))
	fmt.Printf("    texts: %q and %q\n", ta, tb)

	fmt.Println(`
  == is the relation that decides, and it is not a preference:

      The Go map's own key identity is ==. It is == that decides how many
      entries the map holds, so it is == the address set has to be injective
      over. A weaker relation cannot see an entry disappear, because under it
      the entry was never there.

  That is the whole of #31 in one sentence, and it explains the shape of the
  bug rather than only its existence: ADR-0005 chose .Equal for time.Time's
  LEAF proof, correctly and for a measured reason, and then admitted the same
  type as a KEY, where the relation is not negotiable.

  So a key type must satisfy a STRICTER relation than its own leaf proof, and
  a type can be a legal leaf and an illegal key. time.Time is the first
  instance and the design had no place to say so.`)

	fmt.Println(`
  One consequence for ADR-0009's helper as proposed:

      func Injective[T any](format func(T) string, values ...T) error

  T is unconstrained, so it compiles for a type Go cannot even use as a map
  key, and it has no relation to compare under. K31=7 measures what that costs.`)
}

// ---------------------------------------------------------------------------
// K31=4  Does ADR-0003's dynamic tier catch it? Two checks, and only one of
//        them can.
// ---------------------------------------------------------------------------

func k31d() {
	hdr("K31=4  the two dynamic checks, and which one can see this")

	fmt.Println(`  #31's own constraints say the loud half may be free:

      ADR-0003 already runs a dynamic collision check as each map-key address
      is minted, before the write it belongs to.

  There are two such checks and they are not the same check.`)

	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))

	// --- the driver-side check (ADR-0003 driver side, ADR-0004's Keys) ---
	fmt.Println(`
  (a) THE DRIVER-SIDE CHECK: "a driver's mapping from ferry address to plane
      key must be injective over the address set". ADR-0004 implements it in
      the key function, and it is keyed by Path:`)

	as := NewAddressSet([]Path{Path{}.Name("v")})
	ks, err := NewKeys(as, "env", envKeyFunc)
	if err != nil {
		fmt.Println("      NewKeys:", err)
		return
	}
	addr := Path{}.Name("v").Name(string(k31Text(a)))
	k1, e1 := ks.Key(addr)
	k2, e2 := ks.Key(addr) // the SAME Path: both Go keys mint one address
	fmt.Printf("      Key(%s) -> %q err=%v\n", addr, k1, e1)
	fmt.Printf("      Key(%s) -> %q err=%v   <- the second Go key, same Path\n", addr, k2, e2)
	fmt.Println(`      No error, and there cannot be one. The check asks whether two ADDRESSES
      collapse to one KEY. Here two Go map keys collapsed to one ADDRESS before
      the driver was asked anything, so the driver sees one address once and
      answers correctly. The collision is upstream of the only check that
      exists today.`)

	// --- the core-side check (ADR-0003 core side, prefix-free) ---
	fmt.Println(`
  (b) THE CORE-SIDE CHECK: "a compiled schema's address set contains no address
      that is a prefix of another. A path is a prefix of itself, so this
      subsumes exact duplicates", run "as each is minted, before the write it
      belongs to".

      A repeated address IS a prefix of itself, so this rule already covers the
      case in terms. It is not implemented on the dynamic tier: the walk writes
      out[at] = v into a map, and a repeat overwrites.`)

	m := map[time.Time]string{a: "a", b: "b"}
	asShipped(func() {
		got, derr := k31DumpMap(k31Times{m})
		fmt.Printf("\n      as the tip ships:  %d Go keys -> %d addresses, err=%v\n", len(m), len(got), derr)
		for _, p := range sortedAddrs(got) {
			fmt.Printf("        %-40s %s\n", p, got[p].GoString())
		}
	})

	// The rule, implemented over the member list the walk already builds.
	fmt.Println(`
      With the rule applied at mint time, over the member list the walk already
      builds:`)
	// keyProvedOnly stays OFF here so the walk reaches the map at all: this
	// half of the decision is what catches a key type nobody proved, and it has
	// to be measurable independently of the compile-time refusal.
	defer func(a, b bool) { keyProvedOnly, keyCollisionCheck = a, b }(keyProvedOnly, keyCollisionCheck)
	keyProvedOnly, keyCollisionCheck = false, true
	_, mintErr := k31DumpMap(k31Times{m})
	fmt.Printf("        %v\n", mintErr)

	fmt.Println(`
      COST. The walk already sorts a map's members by their key text, because
      ADR-0001's determinism invariant requires it:

          slices.SortFunc(keys, func(a, b) int { return cmpStr(mapKeyText(a), mapKeyText(b)) })

      so a duplicate is an adjacent-equal comparison on a list that is already
      in text order. Three implementations, benchmarked:`)

	k31MintBench()

	fmt.Println(`
  The check is not merely free, it arrives with a speedup, and the reason is
  the same fact that makes it cheap. The tip calls mapKeyText inside the
  COMPARATOR, so it recomputes a key's text O(n log n) times. Computing it once
  per key is what a duplicate check needs anyway, so the check and the
  optimisation are one edit.

  What is NOT free is the diagnostic. A Path cannot say which two Go values
  produced it, so the message has to name the keys rather than the address,
  which means the check belongs in the walk's member step and not in an address
  set the walk hands over.`)
}

// k31Conv is ferry's own key rendering, asked the way the engine asks it.
//
// UPDATED UNDER #58, which deleted `mapKeyText`. The key text is now
// `resolveMapKey`'s resolved pair, taken once where the key type is admitted
// and carried on the compiled node, rather than a three-step cascade re-run per
// key against a package global the caller-facing entry points do not install.
// Every probe here that asked `mapKeyText` asks this, so "what ferry writes"
// stays ONE function and a probe still cannot disagree with the engine.
//
// It resolves per call so the benchmarks below keep their shape: what they
// compare is rendering per COMPARISON against rendering once per KEY, and
// hoisting the resolution out of them would erase the axis being measured.
func k31TextOf(k reflect.Value) string {
	if kc, ok := resolveMapKey(k.Type()); ok {
		if s, err := kc.text(k); err == nil {
			return s
		}
	}
	// The type is not an admissible key under the rules currently in force,
	// which for map[time.Time]V is #31's own fix. Several probes below run in
	// exactly that state on purpose and still need the text the collapse was
	// MADE of, so this falls back to the resolution the tip shipped rather than
	// refusing to answer a question about history.
	return k31LegacyKeyText(k)
}

// k31Text is the walk's own key text, so a probe cannot disagree with it.
func k31Text(v any) string { return k31TextOf(reflect.ValueOf(v)) }

// k31MintCheck is ADR-0003's dynamic tier applied where the walk mints, using
// the sort the walk already does.
func k31MintCheck(m reflect.Value) (dup bool, first, second string) {
	keys := m.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		return cmpStr(k31TextOf(a), k31TextOf(b))
	})
	for i := 1; i < len(keys); i++ {
		if k31TextOf(keys[i]) == k31TextOf(keys[i-1]) {
			return true, fmt.Sprintf("%v", keys[i-1]), fmt.Sprintf("%v", keys[i])
		}
	}
	return false, "", ""
}

type k31Member struct {
	text string
	key  reflect.Value
}

// k31AsShips is the tip's member step: the key text inside the comparator.
func k31AsShips(v reflect.Value) []reflect.Value {
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		return cmpStr(k31TextOf(a), k31TextOf(b))
	})
	return keys
}

// k31Precomputed sorts (text, key) pairs, so the render runs once per key.
func k31Precomputed(v reflect.Value) []k31Member {
	keys := v.MapKeys()
	ms := make([]k31Member, len(keys))
	for i, k := range keys {
		ms[i] = k31Member{k31TextOf(k), k}
	}
	slices.SortFunc(ms, func(a, b k31Member) int { return cmpStr(a.text, b.text) })
	return ms
}

// k31Checked is k31Precomputed plus ADR-0003's dynamic tier.
func k31Checked(v reflect.Value) ([]k31Member, error) {
	ms := k31Precomputed(v)
	for i := 1; i < len(ms); i++ {
		if ms[i].text == ms[i-1].text {
			return nil, fmt.Errorf("two map keys address %q", ms[i].text)
		}
	}
	return ms, nil
}

func k31MintBench() {
	mk := func(n int) map[string]int {
		m := make(map[string]int, n)
		for i := range n {
			m["k"+strconv.Itoa(i)] = i
		}
		return m
	}
	fmt.Printf("\n        %-6s %-22s %-22s %s\n", "keys", "as the tip ships", "text computed once", "+ the duplicate check")
	for _, n := range []int{8, 64, 512} {
		v := reflect.ValueOf(mk(n))
		a := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				k31AsShips(v)
			}
		})
		p := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				k31Precomputed(v)
			}
		})
		c := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				_, _ = k31Checked(v)
			}
		})
		fmt.Printf("        %-6d %-22s %-22s %s\n", n,
			fmt.Sprintf("%d ns", a.NsPerOp()),
			fmt.Sprintf("%d ns", p.NsPerOp()),
			fmt.Sprintf("%d ns", c.NsPerOp()))
	}
}

// ---------------------------------------------------------------------------
// K31=5  Load cannot collide, so Dump is the whole exposure.
// ---------------------------------------------------------------------------

func k31e() {
	hdr("K31=5  the exposure is Dump-only, and that is what makes a mint-time check complete")

	ctx := context.Background()
	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))
	in := k31Times{map[time.Time]string{a: "a", b: "b"}}

	asShipped(func() {
		m := map[Path]Value{}
		derr := Dump(ctx, in, MemSink{m})
		out, lerr := Load[k31Times](ctx, MemSource{m})
		fmt.Printf("  in : %d keys\n  wire: %d addresses (dump err=%v)\n  out: %d keys (load err=%v)\n",
			len(in.V), len(m), derr, len(out.V), lerr)
		// The surviving VALUE is a draw from the collapse race and is not
		// printed: this line flipped between runs of the same binary, which is
		// the defect this file argues against, in this file. The address and
		// the Location are stable and are what the finding rests on.
		for k := range out.V {
			fmt.Printf("       %v  Location=%q  (the value that survives is a draw; see K31=11)\n",
				k.Format(time.RFC3339), k.Location())
		}
	})

	fmt.Println(`
  A round trip that loses an entry, with two nil errors.

  On the LOAD side no collision is possible, and the reason is structural: a
  plane's addresses are already a set, so enumeration cannot hand the walk the
  same address twice. Load's failure here is not a collision, it is that the
  entry it needed was never written.

  That is the argument that a mint-time check is a COMPLETE backstop rather than
  a partial one:

      A key collision can only be created where keys are minted, and keys are
      only minted on Dump.

  It is also the argument that the backstop is not sufficient on its own: it
  fires on the plane being written, and the entry is already gone from the
  artefact the user loaded from before that.`)
}

// ---------------------------------------------------------------------------
// K31=6  What refusing time.Time at compile costs, and what the remedy is.
// ---------------------------------------------------------------------------

type k31Named time.Time

func k31f() {
	hdr("K31=6  refusing map[time.Time]V at schema compile: the cost and the remedy")

	asShipped(func() {
		fmt.Println("  As the tip ships, this compiles:")
		fmt.Printf("    Compile[struct{ V map[time.Time]string }]  -> %v\n", Compile[k31Times]())
	})
	fmt.Println("\n  With the rule:")
	fmt.Printf("    Compile[struct{ V map[time.Time]string }]  -> %v\n", Compile[k31Times]())

	fmt.Println(`
  Under the rule this ticket proposes it does not, and the diagnostic has to
  name a remedy that exists. K31=2 measured that "register an injective codec
  for time.Time" is not one. The three remedies that ARE available:`)

	ctx := context.Background()

	// (1) key by the instant.
	type byNano struct {
		V map[int64]string `ferry:"v"`
	}
	m1 := map[Path]Value{}
	e1 := Dump(ctx, byNano{map[int64]string{
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).UnixNano(): "a",
	}}, MemSink{m1})
	fmt.Printf("\n  (1) map[int64]V keyed by UnixNano   compile+dump err=%v  %s\n", e1, fmtVals(m1))
	fmt.Println("      Lossy on purpose and honest about it: the zone is not in the key,")
	fmt.Println("      which is exactly the information no text could have carried.")

	// (2) key by the text, value carries the time.
	type byText struct {
		V map[string]string `ferry:"v"`
	}
	m2 := map[Path]Value{}
	e2 := Dump(ctx, byText{map[string]string{"2026-01-15T12:00:00Z": "a"}}, MemSink{m2})
	fmt.Printf("\n  (2) map[string]V keyed by RFC 3339   compile+dump err=%v  %s\n", e2, fmtVals(m2))
	fmt.Println("      The user does the conversion, so the collapse is theirs and visible.")

	// (3) a named type over time.Time with a registered codec.
	fmt.Println("\n  (3) type Instant time.Time + a registered codec + .AsMapKey()")
	reg := NewRegistry()
	err := reg.Register(StringCodec(
		func(t k31Named) string { s, _ := time.Time(t).MarshalText(); return string(s) },
		func(s string) (k31Named, error) {
			var t time.Time
			err := t.UnmarshalText([]byte(s))
			return k31Named(t), err
		}).AsMapKey())
	fmt.Printf("      Register -> %v\n", err)
	type byNamed struct {
		V map[k31Named]string `ferry:"v"`
	}
	na := k31Named(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	nb := k31Named(time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0)))
	mm := map[k31Named]string{na: "a", nb: "b"}
	m3 := map[Path]Value{}
	e3 := Dump(ctx, byNamed{mm}, MemSink{m3}, WithRegistry(reg))
	fmt.Printf("      compile: accepted, because .AsMapKey() was said\n")
	fmt.Printf("      dump:    %d Go keys -> %d addresses, err=%v\n", len(mm), len(m3), e3)
	fmt.Println(`      The compile refusal is DEFEATED - .AsMapKey() is a claim the registrant
      makes and core cannot check, and K31=2 says this particular claim is
      unprovable for any codec over this type. And the mint-time check of
      K31=4 catches them anyway, which is the whole argument for shipping both
      halves rather than either.

      This is the same shape as #45, arriving from the other side: there the
      refusal is lifted by DELETING a registration, here by ADDING a keyword.
      Neither is a hole in the compile-time rule; both are the reason the
      compile-time rule cannot be the only rule.`)

	fmt.Println(`
  So the cost of the refusal is one real use case - a map keyed by an instant -
  and the remedy is one line of user code that is more honest than what ferry
  was doing for them. The cost of NOT refusing is that core ships a member of
  its own key set that violates the rule core states, and the ADR would have to
  say so in the documentation of the set rather than in a compile error.`)
}

// ---------------------------------------------------------------------------
// K31=7  What a key proof is, and what ADR-0009's Injective can and cannot do.
// ---------------------------------------------------------------------------

// KeyProof is the fourth thing #31 says a proof needs: a key type's obligation
// is over PAIRS, not over values.
type KeyProof interface {
	Name() string
	Size() int
	check() []string
}

type keyProof[T comparable] struct {
	name   string
	values []T
	reg    *Registry
}

func (k keyProof[T]) Name() string { return k.name }
func (k keyProof[T]) Size() int    { return len(k.values) }

// check is the whole of it: distinct under ==, distinct in the text ferry's own
// walk produces. Not a format function the prover supplies - see below.
func (k keyProof[T]) check() []string {
	type seen struct {
		v T
	}
	byText := map[string]seen{}
	var bad []string
	done := k.reg.install()
	defer done()
	for _, v := range k.values {
		t := k31TextOf(reflect.ValueOf(v))
		if prev, dup := byText[t]; dup && prev.v != v {
			bad = append(bad, fmt.Sprintf("%s: %#v and %#v both address %q", k.name, prev.v, v, t))
			continue
		}
		byText[t] = seen{v}
	}
	sort.Strings(bad)
	return bad
}

// KeyType is the constructor. T is `comparable` rather than `any`, which is the
// correction K31=3 argues for: a type Go cannot use as a map key cannot be a
// ferry key type, and the constraint says so at compile time.
func KeyType[T comparable](name string, reg *Registry, values ...T) KeyProof {
	return keyProof[T]{name, values, reg}
}

func k31g() {
	hdr("K31=7  a key proof is a pair obligation, and it needs ferry's own key text")

	fmt.Println(`  ADR-0005's proof is a triple over VALUES of a type: values, a relation,
  and a golden Value. A key type needs a fourth thing, and it is not a fourth
  column on the same row - it is a different obligation:

      A value proof asks: does this value survive a round trip?
      A key  proof asks: do these two values stay two?

  No value proof can see the second, because the collision is BETWEEN values
  and every case in a value proof is run alone. Measured: core's time.Time
  proof passes on all three planes, today, with the defect live.`)

	dir, _ := os.MkdirTemp("", "k31")
	defer os.RemoveAll(dir)
	for _, pl := range []Plane{memoryPlane(), yamlPlane(dir), flatPlane()} {
		var tt Proof
		for _, p := range CoreTypes() {
			if p.Name() == "time.Time" {
				tt = p
			}
		}
		r := RoundTrip(pl, tt)
		fmt.Printf("    %-38s time.Time: %s\n", pl.Name, r.summary())
	}

	fmt.Println(`
  The key proof, run over core's candidate key set. Each row carries the
  adversarial pair rather than only ordinary values, because a table is only as
  good as its values and this is the one place the hazard is a PAIR:`)

	reg := NewRegistry()
	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))
	proofs := []KeyProof{
		KeyType("string", reg, "", "a", "b,c", "\x00", "  ", "/", "a/b"),
		KeyType("int", reg, 0, 1, -1, math.MaxInt, math.MinInt),
		KeyType("uint64", reg, uint64(0), math.MaxUint64),
		KeyType("time.Duration", reg, time.Duration(0), time.Second, -time.Second,
			time.Duration(math.MaxInt64), time.Duration(math.MinInt64)),
		KeyType("time.Time", reg, a, b, time.Time{}, time.Unix(0, 0).UTC()),
	}
	for _, p := range proofs {
		bad := p.check()
		if len(bad) == 0 {
			fmt.Printf("    ok   %-16s %d values, %d pairs checked\n", p.Name(), p.Size(), p.Size()*(p.Size()-1)/2)
			continue
		}
		fmt.Printf("    FAIL %-16s\n", p.Name())
		for _, s := range bad {
			fmt.Println("         " + s)
		}
	}

	fmt.Println(`
  Two things this measures about ADR-0009's proposed helper:

      func Injective[T any](format func(T) string, values ...T) error

  (a) T is ` + "`any`" + `, so it accepts a type Go cannot key a map with, and it has
      no == to test distinctness with. ` + "`comparable`" + ` is the constraint that makes
      the signature say what the obligation is.

  (b) The prover supplies ` + "`format`" + `, and ferry does not use it. What addresses
      the plane is mapKeyText, which consults the identity table, then the
      chain, then the kind - and a registrant's format function is none of
      those three. Measured, the same type through both routes:`)

	{
		r2 := NewRegistry()
		_ = r2.Register(StringCodec(
			func(h k31Host) string { return h.Name },
			func(s string) (k31Host, error) { return k31Host{s, 0}, nil }).AsMapKey())
		done := r2.install()
		hosts := []k31Host{{"api", 80}, {"api", 443}}
		fmt.Printf("\n      registrant's own format:  %q and %q\n",
			hosts[0].String(), hosts[1].String())
		fmt.Printf("      ferry's mapKeyText:       %q and %q\n",
			k31TextOf(reflect.ValueOf(hosts[0])), k31TextOf(reflect.ValueOf(hosts[1])))
		done()
	}
	fmt.Println(`
      A registrant who proves their String() injective has proved nothing about
      what ferry writes, because the codec they registered is what ferry uses.
      So the key check takes the registry and asks ferry, which is the same
      correction ADR-0007 made for the declared kind: one lookup, not two.`)
}

// k31Host is a type whose String() and whose registered codec disagree.
type k31Host struct {
	Name string
	Port int
}

func (h k31Host) String() string { return h.Name + ":" + strconv.Itoa(h.Port) }

// ---------------------------------------------------------------------------
// K31=8  Which types are admissible as keys today, and which have a proof.
// ---------------------------------------------------------------------------

func k31h() {
	hdr("K31=8  the admissible key set, enumerated, against what is proved")

	fmt.Println(`  D18 did this for the LEAF set and found eleven proof rows against eighteen
  admitted members. The same question for keys, run through validMapKey itself.`)

	reg := NewRegistry()
	_ = reg.Register(TextCodec[netip.Addr](VString).AsMapKey())
	_ = reg.Register(StringCodec(
		func(h k31Host) string { return h.String() },
		func(s string) (k31Host, error) { return k31Host{s, 0}, nil }))

	chainOn := func(f func()) {
		co, cb := chainOrder, chainBeforeKind
		chainOrder, chainBeforeKind = []string{"text"}, true
		defer func() { chainOrder, chainBeforeKind = co, cb }()
		f()
	}

	type cand struct {
		name string
		t    reflect.Type
		how  string
	}
	cands := []cand{
		{"string", reflect.TypeFor[string](), "kind"},
		{"type Env string", reflect.TypeFor[k31Env](), "kind"},
		{"int", reflect.TypeFor[int](), "kind"},
		{"uint8", reflect.TypeFor[uint8](), "kind"},
		{"int64", reflect.TypeFor[int64](), "kind"},
		{"bool", reflect.TypeFor[bool](), "-"},
		{"float64", reflect.TypeFor[float64](), "-"},
		{"[4]byte", reflect.TypeFor[[4]byte](), "-"},
		{"time.Duration", reflect.TypeFor[time.Duration](), "identity"},
		{"time.Time", reflect.TypeFor[time.Time](), "identity"},
		{"netip.Addr", reflect.TypeFor[netip.Addr](), "registered/chain"},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix](), "chain"},
		{"k31Host", reflect.TypeFor[k31Host](), "registered, no AsMapKey"},
	}

	done := reg.install()
	fmt.Printf("\n  %-24s %-24s %-10s %s\n", "type", "how it would be admitted", "admitted", "injective under ==")
	chainOn(func() {
		for _, c := range cands {
			ok := validMapKey(c.t)
			fmt.Printf("  %-24s %-24s %-10v %s\n", c.name, c.how, ok, k31Injectivity(c.t))
		}
	})
	done()

	fmt.Println(`
  The "injective under ==" column is the one core has never computed. Two rows
  are proved by construction, one row is proved by search (K31=9), one row is
  DISPROVED, and the rest are somebody else's claim.

  So the completeness question for keys has the same shape D18 found for
  leaves, and a worse answer: core admits five key routes and has proved two.`)
}

type k31Env string

// k31Injectivity is a one-line verdict per type, sourced from the probes below
// rather than computed here, so the table cannot claim more than was measured.
func k31Injectivity(t reflect.Type) string {
	switch t {
	case reflect.TypeFor[string](), reflect.TypeFor[k31Env]():
		return "yes, the text IS the value"
	case reflect.TypeFor[int](), reflect.TypeFor[int64](), reflect.TypeFor[uint8]():
		return "yes, base 10 is a bijection on the width"
	case reflect.TypeFor[time.Duration]():
		return "yes, by search: see K31=9"
	case reflect.TypeFor[time.Time]():
		return "NO, and no text form is: see K31=2"
	case reflect.TypeFor[netip.Addr](), reflect.TypeFor[netip.Prefix]():
		return "the registrant's claim; see K31=9"
	}
	return "n/a"
}

// ---------------------------------------------------------------------------
// K31=9  The search: which of the plausible key types survive an adversarial
//        hunt for a colliding pair.
// ---------------------------------------------------------------------------

func k31i() {
	hdr("K31=9  hunting for a colliding pair, per candidate key type")

	fmt.Println(`  "Injectivity is not checkable in general" is true and is not the same as
  "nothing can be checked". A randomised hunt over a type's own value space is
  a cheap way to turn a claim into either a counterexample or a bounded
  statement, and it is what core owes for the types it ships.`)

	const n = 1 << 20

	fmt.Printf("\n  %-18s %-12s %s\n", "type", "values", "result")

	// time.Duration
	{
		seen := make(map[string]int64, n)
		var hit string
		for range n {
			d := time.Duration(rand.Int64())
			if rand.IntN(2) == 0 {
				d = -d
			}
			s := k31Text(d)
			if prev, ok := seen[s]; ok && prev != int64(d) {
				hit = fmt.Sprintf("COLLISION %d and %d -> %q", prev, int64(d), s)
				break
			}
			seen[s] = int64(d)
		}
		for _, d := range []time.Duration{0, math.MaxInt64, math.MinInt64, 1, -1, time.Second, -time.Second} {
			s := k31Text(d)
			if prev, ok := seen[s]; ok && prev != int64(d) {
				hit = fmt.Sprintf("COLLISION %d and %d -> %q", prev, int64(d), s)
			}
			seen[s] = int64(d)
		}
		if hit == "" {
			hit = fmt.Sprintf("no collision in %d random + 7 extreme values", n)
		}
		fmt.Printf("  %-18s %-12d %s\n", "time.Duration", n, hit)
	}

	// int64 through the kind arm
	{
		seen := make(map[string]int64, n)
		hit := ""
		for range n {
			v := rand.Int64()
			s := k31Text(v)
			if prev, ok := seen[s]; ok && prev != v {
				hit = fmt.Sprintf("COLLISION %d and %d", prev, v)
				break
			}
			seen[s] = v
		}
		if hit == "" {
			hit = fmt.Sprintf("no collision in %d random values", n)
		}
		fmt.Printf("  %-18s %-12d %s\n", "int64", n, hit)
	}

	// netip.Addr through the chain
	{
		co, cb := chainOrder, chainBeforeKind
		chainOrder, chainBeforeKind = []string{"text"}, true
		seen := make(map[string]netip.Addr, n)
		hit := ""
		add := func(a netip.Addr) {
			if hit != "" {
				return
			}
			s := k31Text(a)
			if prev, ok := seen[s]; ok && prev != a {
				hit = fmt.Sprintf("COLLISION %v and %v -> %q", prev, a, s)
			}
			seen[s] = a
		}
		var b4 [4]byte
		var b16 [16]byte
		for range n / 2 {
			for i := range b4 {
				b4[i] = byte(rand.UintN(256))
			}
			add(netip.AddrFrom4(b4))
			for i := range b16 {
				b16[i] = byte(rand.UintN(256))
			}
			add(netip.AddrFrom16(b16))
		}
		// The adversarial values: v4, the v4-in-v6 form of the same bytes, the
		// zero Addr, and a zoned address.
		v4 := netip.AddrFrom4([4]byte{192, 0, 2, 1})
		add(v4)
		add(netip.AddrFrom16([16]byte{10: 0xff, 11: 0xff, 12: 192, 13: 0, 14: 2, 15: 1}))
		add(netip.Addr{})
		add(netip.MustParseAddr("fe80::1%eth0"))
		add(netip.MustParseAddr("fe80::1%eth1"))
		add(netip.MustParseAddr("fe80::1"))
		if hit == "" {
			hit = fmt.Sprintf("no collision in %d random + 6 adversarial values", n)
		}
		fmt.Printf("  %-18s %-12d %s\n", "netip.Addr", n, hit)
		chainOrder, chainBeforeKind = co, cb
	}

	// time.Time, which needs no search at all.
	{
		a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("UTC", 0))
		b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("UTC", 0))
		fmt.Printf("  %-18s %-12s COLLISION at the 2nd value: %v and %v -> %q\n",
			"time.Time", "2", a.Location(), b.Location(), k31Text(a))
	}

	fmt.Println(`
  What a hunt of this shape is worth, stated exactly, because it is easy to
  read as more than it is:

    - A hit is a PROOF that the type is not injective, and it is the strongest
      result available. time.Time needed two values.
    - A miss over 2^20 values is not a proof of injectivity. It is a bound, and
      the honest statement is "no counterexample was found", which is what a
      registrant can say and what core should not have to.
    - For string and the integer kinds core does not need the hunt, and that is
      the difference: the text IS the value, or base 10 is a bijection on the
      width. Those two are the only rows core can PROVE, and they are exactly
      the two ADR-0005 named.

  So the hunt is a tool for a registrant, not a substitute for core's rule.`)

	fmt.Println(strings.Repeat("-", 72))
}

func init() {
	prev := runAtEnd
	_ = prev
	if p := os.Getenv("K31"); p != "" {
		k31Selected = p
	}
}

var k31Selected string

// envKeyFunc is the flat driver's key function, reused so the driver-side check
// in K31=4 is a real one.
func envKeyFunc(p Path) (string, error) {
	var b strings.Builder
	for i, s := range p.Segments() {
		if i > 0 {
			b.WriteByte('_')
		}
		b.WriteString(strings.ToUpper(strings.Map(func(r rune) rune {
			if r == '-' || r == '.' || r == ':' || r == '/' {
				return '_'
			}
			return r
		}, s.Text)))
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("empty address")
	}
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// K31=11  ADR-0001's determinism invariant already requires the refusal.
// ---------------------------------------------------------------------------

func k31k() {
	hdr("K31=11  a colliding dump cannot be deterministic, so the invariant decides")

	fmt.Println(`  ADR-0001 makes determinism a package-wide invariant, and ADR-0003 measured
  it at 1 distinct error string over 300 runs. A collapsing map key breaks it,
  and not as a side effect: which entry survives is which one the walk writes
  last, and with two equal texts the order is Go's map iteration order.

  So the choice is not "silent but stable" against "loud". There is no stable
  option. 300 dumps of one value:`)

	a := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	b := time.Date(2026, 1, 15, 12, 0, 0, 0, time.FixedZone("GMT", 0))
	in := k31Times{map[time.Time]string{a: "a", b: "b"}}

	count := func(label string) {
		seen := map[string]int{}
		for range 300 {
			out, err := k31DumpMap(in)
			var k string
			if err != nil {
				k = "err: " + err.Error()
			} else {
				k = fmtVals(out)
			}
			seen[k]++
		}
		keys := make([]string, 0, len(seen))
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// The frequencies are NOT printed. They are a sample of a distribution
		// the program does not control, so they moved between runs - measured
		// at 29/300 to 46/300 for the same outcome over 8 runs - and the line
		// above already states the fact stably. Which outcomes occur is the
		// finding; how often each won is noise with a number on it.
		fmt.Printf("\n    %s: %d distinct outcome(s) over 300 dumps\n", label, len(seen))
		for _, k := range keys {
			fmt.Printf("      %s\n", shorten2(k, 78))
		}
	}

	asShipped(func() { count("as the tip ships") })
	withRule(func() { count("with the rule") })

	fmt.Println(`
  Two outcomes at 300 runs, split by nothing the program controls. That is
  ADR-0001's invariant broken by a value rather than by an ordering bug, and
  it is why P12 and P19 each carry a line the audit records as flaky across
  runs of the same binary and attributes to this ticket.

  The invariant is therefore an independent argument for the mint-time check,
  and it is the stronger one, because it does not depend on anybody agreeing
  that a lost entry matters:

      A dump that collapses two keys has no deterministic answer to give, so
      the only outcome consistent with ADR-0001 is a refusal.

  Both of the audit's flaky lines are on walk.go, the superseded walk, which
  the audit's own open list says is not retired. The tip's walk is now
  deterministic on this input in the only way available to it.`)
}

// k31Outcomes is what a probe about a COLLAPSE has to print instead of one
// sample. Which entry survives is Go's map iteration order, so a probe that
// prints the winner is not byte-stable across runs of the same binary - which
// is what the audit records for one line in P12 and one in P19 and attributes
// to this ticket.
//
// The set of outcomes is stable and is also the better measurement: "2 distinct
// outcomes over 200 runs" states the defect, where "/m/api number(2)" states
// one draw from it.
func k31Outcomes(n int, f func() (map[Path]Value, error)) []string {
	seen := map[string]bool{}
	for range n {
		d, err := f()
		s := fmtVals(d)
		if err != nil {
			s = "err=" + err.Error()
		}
		seen[strings.TrimSpace(s)] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
