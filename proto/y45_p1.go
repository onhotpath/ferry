package main

// Y1 to Y4: the hole itself, its population, and what it costs.

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

// yMap is the shape every probe below compiles: one map field, so the key type
// is the only thing under test.
type yMap[K comparable] struct {
	M map[K]int `ferry:"m"`
}

// yLeaf is the same type at a LEAF, which is the asymmetry Y5 prices.
type yLeaf[T any] struct {
	V T `ferry:"v"`
}

// ---------------------------------------------------------------------------
// Y1  the three rows of #45's table, run
// ---------------------------------------------------------------------------

func runY1() {
	saysY("ADR-0009", `"A registration is usable as a map key only if it says so:
	StringCodec(...).AsMapKey(). A map[T]V whose key type is registered without
	it is a schema compile error."
	and "the diagnostic is where the obligation gets communicated, which is the
	point: it is the only moment a registrant is guaranteed to read".`)

	saysY("ADR-0007", `"A type the chain claims with declared kind String may key a map,
	on the same terms as a registered codec."
	and "A chain arm is a codec nobody registered, so the obligation needs a home."`)

	fmt.Println("  #45's table, with the third row run rather than reasoned:")
	fmt.Printf("\n  chainOrder=%v chainBeforeKind=%v keyOptIn=%v\n\n", chainOrder, chainBeforeKind, keyOptIn)

	for _, r := range []struct {
		what string
		reg  *Registry
	}{
		{"registers a codec, no .AsMapKey()", yAddrReg(false)},
		{"registers a codec with .AsMapKey()", yAddrReg(true)},
		{"registers NOTHING at all", NewRegistry()},
	} {
		err := Compile[yMap[netip.Addr]](WithRegistry(r.reg))
		fmt.Printf("    %-36s -> %s\n", r.what, errOneLine(err))
	}

	fmt.Println(`
    Row three is the hole. Nobody was asked, and the way to reach it from
    row one is to DELETE the registration rather than to add a word to it.`)

	fmt.Println("\n  and the same three rows on the LEAF, where no obligation exists at all:")
	for _, r := range []struct {
		what string
		reg  *Registry
	}{
		{"registers a codec, no .AsMapKey()", yAddrReg(false)},
		{"registers a codec with .AsMapKey()", yAddrReg(true)},
		{"registers NOTHING at all", NewRegistry()},
	} {
		err := Compile[yLeaf[netip.Addr]](WithRegistry(r.reg))
		fmt.Printf("    %-36s -> %s\n", r.what, errOneLine(err))
	}
	fmt.Println(`
    So the opt-in is the ONLY place in ferry where registering a type makes
    it less usable than not registering it. That is the shape of the defect,
    stated without reference to injectivity at all.`)
}

// yAddrReg builds ADR-0009's own example registration for netip.Addr, with and
// without the opt-in. TextCodec rather than StringCodec because #41 D4 made
// Register run the zero-value check, which StringCodec(String, ParseAddr)
// fails - that is ADR-0009's own worked refusal.
func yAddrReg(asKey bool) *Registry {
	g := TextCodec[netip.Addr](VString)
	if asKey {
		g = g.AsMapKey()
	}
	return mustReg(NewRegistry(), g)
}

// ---------------------------------------------------------------------------
// Y2  the population
// ---------------------------------------------------------------------------

// yPop is every named type an ADR mentions plus the config-shaped ones, which
// is X3's census re-asked with the map-key question instead of the leaf one.
var yPop = []struct {
	name string
	t    reflect.Type
}{
	{"netip.Addr", reflect.TypeFor[netip.Addr]()},
	{"netip.AddrPort", reflect.TypeFor[netip.AddrPort]()},
	{"netip.Prefix", reflect.TypeFor[netip.Prefix]()},
	{"time.Time", reflect.TypeFor[time.Time]()},
	{"time.Duration", reflect.TypeFor[time.Duration]()},
	{"big.Int", reflect.TypeFor[big.Int]()},
	{"net.IP", reflect.TypeFor[net.IP]()},
	{"net.IPNet", reflect.TypeFor[net.IPNet]()},
	{"net.TCPAddr", reflect.TypeFor[net.TCPAddr]()},
	{"url.URL", reflect.TypeFor[url.URL]()},
	{"sql.NullString", reflect.TypeFor[sql.NullString]()},
	{"tls.Config", reflect.TypeFor[tls.Config]()},
	{"[16]byte UUID", reflect.TypeFor[[16]byte]()},
	{"main.YID (user, lossy)", reflect.TypeFor[YID]()},
	{"main.YPair (user, total)", reflect.TypeFor[YPair]()},
}

func runY2() {
	fmt.Println(`  A type reaches #45's hole only if all three hold:
    (a) Go will let it key a map at all, i.e. reflect says Comparable
    (b) ADR-0007's chain claims it, at declared kind String
    (c) core does not already own it in the identity table

  (c) matters because a core-owned type is #31's, which ADR-0009 says its
  opt-in "deliberately does not reach". Measured over the census:`)

	fmt.Printf("\n  %-24s %-12s %-14s %-12s %s\n", "type", "comparable", "chain claims", "core owns", "in #45's population")
	fmt.Printf("  %-24s %-12s %-14s %-12s %s\n", strings.Repeat("-", 24), strings.Repeat("-", 12), strings.Repeat("-", 14), strings.Repeat("-", 12), strings.Repeat("-", 19))
	reg := NewRegistry()
	done := reg.install()
	var in []string
	for _, p := range yPop {
		cmp := p.t.Comparable()
		c, claims := activeChainCodec(p.t)
		claims = claims && c.kind == VString
		_, core := byIdentity[p.t]
		hit := cmp && claims && !core
		if hit {
			in = append(in, p.name)
		}
		fmt.Printf("  %-24s %-12v %-14v %-12v %v\n", p.name, cmp, claims, core, hit)
	}
	done()
	fmt.Printf("\n  in the population (%d): %s\n", len(in), strings.Join(in, " "))
	fmt.Println(`
  The stdlib half is small and it is not the interesting half. Every type a
  USER writes with a text pair is in it too, and ADR-0007's own census found
  13 complete text pairs in 29 config types. The population is "types whose
  authors chose to have a text form", which is unbounded by construction.`)
}

// ---------------------------------------------------------------------------
// Y3  is the chain's text actually injective
// ---------------------------------------------------------------------------

// YID is the shape the risk actually takes: a user type whose text form is
// case-insensitive, so two distinct Go values render identically. Nothing
// about it is exotic - it is a tenant or region identifier, and its author
// wrote MarshalText for logging rather than for ferry.
type YID struct{ S string }

func (i YID) MarshalText() ([]byte, error)  { return []byte(strings.ToLower(i.S)), nil }
func (i *YID) UnmarshalText(b []byte) error { i.S = string(b); return nil }

// YPair is the control: a text form that is total and injective.
type YPair struct{ A, B uint8 }

func (p YPair) MarshalText() ([]byte, error) { return fmt.Appendf(nil, "%d.%d", p.A, p.B), nil }
func (p *YPair) UnmarshalText(b []byte) error {
	_, err := fmt.Sscanf(string(b), "%d.%d", &p.A, &p.B)
	return err
}

func runY3() {
	fmt.Println(`  Injectivity is a property of the ENCODER over the key type, so it can be
  measured directly: encode a list of distinct values and count distinct texts.`)

	reg := NewRegistry()
	done := reg.install()
	defer done()

	yInj("netip.Addr", []any{
		netip.MustParseAddr("192.0.2.1"),
		netip.MustParseAddr("::ffff:192.0.2.1"), // 4-in-6, a DIFFERENT Go value
		netip.MustParseAddr("fe80::1"),
		netip.MustParseAddr("fe80::1%eth0"), // same address, different zone
		netip.Addr{},
	})
	yInj("netip.Prefix", []any{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("10.1.2.3/8"), // host bits kept, distinct value
		netip.MustParsePrefix("10.0.0.0/24"),
	})
	yInj("netip.AddrPort", []any{
		netip.MustParseAddrPort("192.0.2.1:80"),
		netip.MustParseAddrPort("192.0.2.1:443"),
	})
	yInj("main.YPair", []any{YPair{1, 2}, YPair{1, 20}, YPair{12, 0}})
	yInj("main.YID", []any{YID{"Prod"}, YID{"prod"}, YID{"PROD"}})

	fmt.Println(`
  So the stdlib types the chain claims are injective on every adversarial
  value the probe could build, INCLUDING the two that look like traps: a
  4-in-6 address and a zoned one both keep their distinction in the text.
  That is worth stating plainly, because it means the hole is not currently
  reachable through anything ferry ships.

  main.YID is three distinct Go values and one text. Nothing about it is
  contrived: a case-insensitive identifier with a MarshalText written for
  logs is the ordinary way a user arrives here, and its author never read
  ADR-0009 because they never called Register.`)
}

func yInj(name string, vals []any) {
	if len(vals) == 0 {
		return
	}
	t := reflect.TypeOf(vals[0])
	c, ok := activeChainCodec(t)
	if !ok {
		fmt.Printf("\n  %-16s the chain does not claim it\n", name)
		return
	}
	texts := map[string]int{}
	var order []string
	for _, v := range vals {
		val, err := c.enc(reflect.ValueOf(v))
		if err != nil {
			fmt.Printf("    %-30v -> encode error %v\n", v, err)
			continue
		}
		if texts[val.Text()] == 0 {
			order = append(order, val.Text())
		}
		texts[val.Text()]++
	}
	fmt.Printf("\n  %-16s %d distinct value(s) -> %d distinct text(s)  injective=%v\n",
		name, len(vals), len(texts), len(texts) == len(vals))
	for _, s := range order {
		if texts[s] > 1 {
			fmt.Printf("    %-30q <- %d values collapse here\n", s, texts[s])
		}
	}
}

// ---------------------------------------------------------------------------
// Y4  what the collapse costs, end to end
// ---------------------------------------------------------------------------

func runY4() {
	saysY("ADR-0009", `Its own measurement for the REGISTERED case, which the opt-in exists
	to prevent: "Go map holds 2 keys -> ferry dumps 1 address".`)

	saysY("ADR-0001", `The determinism invariant covers "every map iteration reaching a
	user-visible artefact", and nothing may be ignored silently.`)

	ctx := context.Background()
	v := yMap[YID]{M: map[YID]int{{"Prod"}: 1, {"prod"}: 2, {"PROD"}: 3}}

	fmt.Printf("\n  Compile[struct{ M map[main.YID]int }] -> %s\n", errOneLine(Compile[yMap[YID]]()))
	fmt.Printf("  the Go map holds %d keys\n", len(v.M))

	got, derr := dumpTo(ctx, v)
	fmt.Printf("  ferry dumps %d address(es), err=%v\n", len(got), errOneLine(derr))
	for _, p := range sortedAddrs(got) {
		fmt.Printf("    %-12s %s\n", p.String(), got[p].GoString())
	}

	fmt.Println("\n  WHICH entry survives, over 200 dumps of the same value:")
	seen := map[string]int{}
	for i := 0; i < 200; i++ {
		g, _ := dumpTo(ctx, v)
		var parts []string
		for _, p := range sortedAddrs(g) {
			parts = append(parts, p.String()+"="+g[p].GoString())
		}
		seen[strings.Join(parts, " ")]++
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("    %d distinct outcome(s) over 200 runs\n", len(seen))
	for _, k := range keys {
		fmt.Printf("      %-24s %d/200\n", k, seen[k])
	}

	back, lerr := loadFrom(ctx, yMap[YID]{}, got)
	fmt.Printf("\n  and loading it back: %d key(s), err=%v\n", len(back.M), errOneLine(lerr))
	fmt.Println(`
  Two entries dropped, no error, and the winner decided by map iteration
  order. That is ADR-0001's determinism invariant broken and its
  "never silently ignore" rule broken, in one dump, with nobody warned.

  This is the same failure ADR-0009 measured and refused for the registered
  case. The only difference is that here the author never called Register,
  so there was no diagnostic to read.`)
}
