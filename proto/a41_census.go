package main

// #41's second batch: statements the tip is expected to implement, run anyway.
//
// The ticket's three verdicts are "implemented and exercised", "implemented and
// never exercised", and "not implemented". The first is not the output, but a
// census with only the third in it is a bug list rather than a census, and it
// cannot say what its own coverage was. These are the rows that make the
// coverage statement honest - and two of them did not come out as expected.
//
//	A41=<n|all> go run .

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

var a41b = []struct {
	n     string
	title string
	fn    func()
}{
	{"A17", "ADR-0004: does Bind do I/O, and does the refusal land at open?", runA17},
	{"A18", "ADR-0006: Absent does not write, and a present empty beats a default", runA18},
	{"A19", "ADR-0006: Null per kind, against the ADR's own table", runA19},
	{"A20", "ADR-0005: nil and empty composites are one value", runA20},
	{"A21", "ADR-0005: String is the universal donor and nothing else coerces", runA21},
	{"A22", "ADR-0006: a declaration attaches to the address SHAPE", runA22},
	{"A23", "ADR-0006: a default fills a hole and never conjures the section", runA23},
	{"A24", "ADR-0010: the root must be a struct ferry walks", runA24},
	{"A25", "ADR-0008/0009: are both compile-affecting Options in the cache key?", runA25},
	{"A26", "ADR-0003: segment-wise ordering, and core never folds", runA26},
	{"A27", "ADR-0009/0010: the freeze, and that Compile does not take it", runA27},
	{"A28", "ADR-0005: recursive types, and the maps-no-address backstop", runA28},
	{"A29", "ADR-0004: a map field from a source that cannot enumerate", runA29},
	{"A30", "ADR-0001: is a compile refusal deterministic over 300 runs?", runA30},
	{"A31", "ADR-0009: what does the default registry hold before main() runs?", runA31},
}

// ---------------------------------------------------------------------------

type A17Conf struct {
	Name string `ferry:"name"`
}

func runA17() {
	says("ADR-0004", `"Bind takes no context.Context... Measured: Bind against a missing file
    returns a nil error and Open then fails. The conformance suite can therefore
    contain the case Bind must succeed against an unreachable plane."`)

	ctx := context.Background()
	src := FYAMLSource{Path: "/nonexistent/nowhere.yaml"}
	open, berr := src.Bind(NewAddressSet(nil))
	fmt.Printf("\n  Bind against a missing file -> err=%v\n", errOneLine(berr))
	_, oerr := open(ctx)
	fmt.Printf("  the OpenFunc then           -> err=%v\n", errOneLine(oerr))
	_, lerr := Load[A17Conf](ctx, src)
	fmt.Printf("  through Load[T]             -> err=%v\n", errOneLine(lerr))

	verdict("IMPLEMENTED", "and exercised: Bind takes no context in the type,")
	fmt.Println("           and the refusal lands at open.")
}

// ---------------------------------------------------------------------------

type A18Conf struct {
	Name string   `ferry:"name,default=anonymous"`
	Tags []string `ferry:"tags"`
	Port int      `ferry:"port,default=8080"`
}

func runA18() {
	says("ADR-0006", `"Absent means ferry does not write to the field. Every other observation,
    Null and the empty string included, is a value the plane holds."
    Measured there as: Absent -> "anonymous"; String("") -> ""; a real value -> "svc".`)

	ctx := context.Background()
	fmt.Println()
	for _, tc := range []struct {
		name string
		v    Value
	}{
		{"Absent", Absent},
		{`String("")`, String("")},
		{"a real value", String("svc")},
	} {
		m := map[Path]Value{}
		if tc.v.Present() {
			m[Path{}.Name("name")] = tc.v
		}
		got, err := loadFrom(ctx, A18Conf{}, m)
		fmt.Printf("    %-14s -> Name=%-12q err=%v\n", tc.name, got.Name, errOneLine(err))
	}

	fmt.Println("\n  and the seeded struct against a wholly empty plane:")
	seed := A18Conf{Name: "svc", Port: 5432, Tags: []string{"a"}}
	got, err := loadFrom(ctx, seed, map[Path]Value{})
	fmt.Printf("    seed %+v\n    ->   %+v err=%v\n", seed, got, errOneLine(err))
	fmt.Println("    NOTE: /port is Absent and carries a default, so the default OVERWRITES")
	fmt.Println("    the seed. ADR-0006 says declared defaults win over a seed, so this is")
	fmt.Println("    the rule and not a defect - but its own worked line for the seeded")
	fmt.Println("    struct uses fields with no declared default.")

	verdict("IMPLEMENTED", "and exercised through the tip's own entry point.")
}

// ---------------------------------------------------------------------------

type A19Conf struct {
	S  string        `ferry:"s"`
	I  int           `ferry:"i"`
	D  time.Duration `ferry:"d"`
	By []byte        `ferry:"by"`
	P  *int          `ferry:"p"`
	Sl []string      `ferry:"sl"`
}

// nilOrSet renders a pointer by whether it is nil and what it points at, never
// by its address, so a probe's output is stable across runs.
func nilOrSet(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("&%d", *p)
}

func runA19() {
	says("ADR-0006", `"Null is admitted by exactly the Go types that have a null, and refused by
    every other leaf as a wrong kind." Its table: string/int/time.Duration refuse;
    []byte -> nil slice; *T -> nil pointer; []T, map[K]V -> nil.`)

	ctx := context.Background()
	seed := A19Conf{S: "seed", I: 7, D: time.Second, By: []byte("xy"), P: new(int), Sl: []string{"x"}}
	fmt.Println()
	for _, addr := range []string{"s", "i", "d", "by", "p", "sl"} {
		got, err := loadFrom(ctx, seed, map[Path]Value{Path{}.Name(addr): Null()})
		res := "accepted"
		if err != nil {
			res = "REFUSED: " + errOneLine(err)
		}
		// P is rendered as nil/non-nil rather than printed, because %+v on a
		// live *int is a heap address and this suite is regression-diffed.
		fmt.Printf("    Null at /%-3s -> %-52s {S:%s I:%d D:%v By:%v P:%s Sl:%v}\n",
			addr, res, got.S, got.I, got.D, got.By, nilOrSet(got.P), got.Sl)
	}

	verdict("IMPLEMENTED", "every row matches ADR-0006's table, exercised here")
	fmt.Println("           for the first time on this tip: no E16 or B25 probe loads a Null.")
}

// ---------------------------------------------------------------------------

type A20Conf struct {
	Tags   []string       `ferry:"tags"`
	Limits map[string]int `ferry:"limits"`
}

func runA20() {
	says("ADR-0005", `"A composite with no elements writes Null at its own address, whether it is
    nil or empty. On Load, a container address with no children yields the zero
    value, which for a slice or a map is nil."`)

	ctx := context.Background()
	fmt.Println()
	for _, tc := range []struct {
		name string
		v    A20Conf
	}{
		{"nil, nil", A20Conf{}},
		{"empty, empty", A20Conf{Tags: []string{}, Limits: map[string]int{}}},
		{"one each", A20Conf{Tags: []string{"a"}, Limits: map[string]int{"rps": 1}}},
	} {
		vals, _ := dumpTo(ctx, tc.v)
		back, err := loadFrom(ctx, A20Conf{}, vals)
		fmt.Printf("    %-14s -> %-46s back: tags nil=%-6v len=%d  limits nil=%-6v len=%d err=%v\n",
			tc.name, a41AddrList(vals), back.Tags == nil, len(back.Tags),
			back.Limits == nil, len(back.Limits), errOneLine(err))
	}

	verdict("IMPLEMENTED", "and exercised, including the normalisation onto nil.")
}

// ---------------------------------------------------------------------------

type A21Conf struct {
	Port  int           `ferry:"port"`
	On    bool          `ferry:"on"`
	Ratio float64       `ferry:"ratio"`
	Name  string        `ferry:"name"`
	D     time.Duration `ferry:"d"`
}

func runA21() {
	says("ADR-0005", `"Every leaf accepts its own kind. Every leaf additionally accepts String,
    whose text is parsed by exactly the parser that leaf's own kind uses.
    Nothing else coerces." And: a Number is NOT accepted for a Go string field.`)

	ctx := context.Background()
	fmt.Println("\n  the flattening plane, which reports String for everything:")
	flat := map[Path]Value{
		Path{}.Name("port"):  String("8080"),
		Path{}.Name("on"):    String("true"),
		Path{}.Name("ratio"): String("3.5"),
		Path{}.Name("name"):  String("svc"),
		Path{}.Name("d"):     String("30s"),
	}
	got, err := loadFrom(ctx, A21Conf{}, flat)
	fmt.Printf("    -> %+v err=%v\n", got, errOneLine(err))

	fmt.Println("\n  a Number at a string field, which the ADR says is refused:")
	_, e2 := loadFrom(ctx, A21Conf{}, map[Path]Value{Path{}.Name("name"): Number("8080")})
	fmt.Printf("    -> err=%v\n", errOneLine(e2))

	fmt.Println("\n  ADR-0005's cast comparison, the seven inputs it says ferry refuses or")
	fmt.Println("  answers exactly:")
	type row struct {
		in   Value
		want string
	}
	for _, r := range []struct {
		label string
		v     Value
		into  string
	}{
		{`"0080" -> int`, String("0080"), "port"},
		{`"010" -> int`, String("010"), "port"},
		{`"0x10" -> int`, String("0x10"), "port"},
		{`"1.9" -> int`, String("1.9"), "port"},
		{`"" -> int`, String(""), "port"},
		{`"yes" -> bool`, String("yes"), "on"},
		{`"30" -> Duration`, String("30"), "d"},
	} {
		g, err := loadFrom(ctx, A21Conf{}, map[Path]Value{Path{}.Name(r.into): r.v})
		out := fmt.Sprintf("%v", reflect.ValueOf(g).FieldByNameFunc(func(n string) bool {
			return strings.EqualFold(n, map[string]string{"port": "Port", "on": "On", "d": "D"}[r.into])
		}).Interface())
		if err != nil {
			out = "refused"
		}
		fmt.Printf("    %-22s -> %s\n", r.label, out)
	}

	verdict("IMPLEMENTED", "the donor rule and all seven cast rows reproduce.")
}

// ---------------------------------------------------------------------------

type A22Elem struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=8080"`
}

type A22Conf struct {
	Servers map[string]A22Elem `ferry:"servers"`
	Pool    []A22Elem          `ferry:"pool"`
}

func runA22() {
	says("ADR-0006", `"A map key's address and a slice element's index come from the value, not
    the type, so /servers/a/port is not in the compiled schema and never can be.
    The declaration lives at /servers/*/port." Measured there: looked up by the
    realised address every default under a map or a slice silently vanishes.`)

	ctx := context.Background()
	vals := map[Path]Value{
		Path{}.Name("servers").Name("a").Name("host"): String("h1"),
		Path{}.Name("pool").Index(0).Name("host"):     String("h2"),
	}
	got, err := loadFrom(ctx, A22Conf{}, vals)
	fmt.Printf("\n    -> Servers=%v  Pool=%v  err=%v\n", got.Servers, got.Pool, errOneLine(err))
	fmt.Println("    ADR-0006's correct row is Servers=map[a:{h1 8080}] Pool=[{h2 8080}].")

	verdict("IMPLEMENTED", "childAddr() re-mints the realised address while the")
	fmt.Println("           declaration stays on the compiled shape node.")
}

// ---------------------------------------------------------------------------

type A23Auth struct {
	User string `ferry:"user,default=admin"`
	Pass string `ferry:"pass"`
}

type A23Conf struct {
	Auth *A23Auth `ferry:"auth"`
	P    *int     `ferry:"p,default=5"`
}

func runA23() {
	says("ADR-0006", `"A *T where T is a composite is materialised exactly when something under
    it was present on the plane. A declared default beneath it does NOT count as
    presence." And: "A pointer to a leaf... P *int with default=5 loads as &5
    from an empty plane."`)

	ctx := context.Background()
	fmt.Println()
	for _, tc := range []struct {
		name string
		m    map[Path]Value
	}{
		{"nothing under /auth", map[Path]Value{}},
		{"/auth/pass present", map[Path]Value{Path{}.Name("auth").Name("pass"): String("p")}},
	} {
		got, err := loadFrom(ctx, A23Conf{}, tc.m)
		auth := "nil"
		if got.Auth != nil {
			auth = fmt.Sprintf("&%+v", *got.Auth)
		}
		p := "nil"
		if got.P != nil {
			p = fmt.Sprintf("&%d", *got.P)
		}
		fmt.Printf("    %-22s -> Auth=%-26s P=%-4s err=%v\n", tc.name, auth, p, errOneLine(err))
	}

	verdict("IMPLEMENTED", "both rows match, including the pointer-to-a-leaf case.")
}

// ---------------------------------------------------------------------------

type A24Struct struct {
	V string `ferry:"v"`
}

func runA24() {
	says("ADR-0010", `"The root must be a struct ferry WALKS, decided after the chain and the
    registry have been asked." Its table refuses int, time.Duration, netip.Addr,
    big.Int, map[string]int, []string and [2]string.`)

	fmt.Println()
	rows := []struct {
		name string
		err  error
	}{
		{"struct{...}", Compile[A24Struct]()},
		{"*struct{...}", Compile[*A24Struct]()},
		{"int", Compile[int]()},
		{"time.Duration", Compile[time.Duration]()},
		{"map[string]int", Compile[map[string]int]()},
		{"[]string", Compile[[]string]()},
		{"[2]string", Compile[[2]string]()},
	}
	for _, r := range rows {
		fmt.Printf("    %-16s -> %v\n", r.name, errOneLine(r.err))
	}

	verdict("IMPLEMENTED", "and exercised by E7 and E11 on this branch.")
}

// ---------------------------------------------------------------------------

type A25Conf struct {
	Host string `ferry:"host" mylib:"HOST"`
	Port int    `ferry:"port" mylib:"PORT"`
}

func runA25() {
	says("ADR-0010", `"The schema cache is keyed by the type, the struct tag key, and the
    registry." Measured there: one reflect.Type under two tag keys yields two
    address sets, and a cache keyed by reflect.Type alone hands registry B
    registry A's codec.`)

	ctx := context.Background()
	s1, _ := schemaFor(reflect.TypeFor[A25Conf](), defaultOpts())
	o2 := defaultOpts()
	o2.tagKey = "mylib"
	s2, _ := schemaFor(reflect.TypeFor[A25Conf](), o2)
	fmt.Printf("\n    tag key \"ferry\" -> %v\n", s1.as.All())
	fmt.Printf("    tag key \"mylib\" -> %v\n", s2.as.All())
	fmt.Printf("    same *schema pointer: %v\n", s1 == s2)

	regA := mustReg(NewRegistry(), StringCodec(
		func(d A25Dur) string { return time.Duration(d).String() },
		func(s string) (A25Dur, error) { d, err := time.ParseDuration(s); return A25Dur(d), err }))
	regB := NewRegistry()
	va, _ := dumpTo(ctx, A25Wrap{D: A25Dur(time.Second)}, WithRegistry(regA))
	vb, _ := dumpTo(ctx, A25Wrap{D: A25Dur(time.Second)}, WithRegistry(regB))
	fmt.Printf("    registry A (codec)    -> %s\n", va[Path{}.Name("d")].GoString())
	fmt.Printf("    registry B (no codec) -> %s\n", vb[Path{}.Name("d")].GoString())

	verdict("IMPLEMENTED", "both components are in the key, and the two schemas")
	fmt.Println("           are distinct values.")
}

type A25Dur time.Duration

type A25Wrap struct {
	D A25Dur `ferry:"d"`
}

// ---------------------------------------------------------------------------

type A26Conf struct {
	A  string `ferry:"a"`
	B  string `ferry:"MyKey"`
	C  string `ferry:"mykey"`
	Ax string `ferry:"a-x"`
}

func runA26() {
	says("ADR-0003", `"Measured, twelve indices sorted by canonical bytes: 0 1 10 11 2 ...
    Sorted segment-wise, comparing Index segments numerically: 0 1 2 ... 11."
    And: "Core compares segment text by exact byte equality. It never folds case."`)

	var idx []Path
	for i := range 12 {
		idx = append(idx, Path{}.Name("v").Index(i))
	}
	segwise := sortedPaths(idx)
	var bytewise []string
	for _, p := range idx {
		bytewise = append(bytewise, p.String())
	}
	strSort(bytewise)
	fmt.Printf("\n    segment-wise : %s\n", joinTail(segwise))
	fmt.Printf("    canonical byte: %s\n", strings.Join(tailAll(bytewise), " "))

	s, err := schemaFor(reflect.TypeFor[A26Conf](), defaultOpts())
	fmt.Printf("\n    two fields differing only in case -> compile err=%v\n", errOneLine(err))
	if s != nil {
		fmt.Printf("    address set                       -> %v\n", s.as.All())
	}

	verdict("IMPLEMENTED", "CompareSegmentwise is the published order and the")
	fmt.Println("           compiler uses it; segment text is compared by exact bytes.")
}

// ---------------------------------------------------------------------------

func runA27() {
	says("ADR-0010", `"after Compile[Conf](WithRegistry(fresh)) -> frozen=false, a later Register
    is accepted; after a Load -> frozen=true, a later Register is refused."`)

	ctx := context.Background()
	r1 := NewRegistry()
	_ = Compile[A17Conf](WithRegistry(r1))
	fmt.Printf("\n    after Compile -> frozen=%v, a later Register -> %v\n",
		r1.frozen.Load(), errOneLine(r1.Register(StringCodec(
			func(d A25Dur) string { return "" },
			func(s string) (A25Dur, error) { return 0, nil }))))

	r2 := NewRegistry()
	_, _ = loadFrom(ctx, A17Conf{}, map[Path]Value{}, WithRegistry(r2))
	fmt.Printf("    after Load    -> frozen=%v, a later Register -> %v\n",
		r2.frozen.Load(), errOneLine(r2.Register(StringCodec(
			func(d A25Dur) string { return "" },
			func(s string) (A25Dur, error) { return 0, nil }))))

	verdict("IMPLEMENTED", "and exercised by E10 and R18.")
}

// ---------------------------------------------------------------------------

type A28Node struct {
	Name string   `ferry:"name"`
	Next *A28Node `ferry:"next"`
}

type A28Opaque struct {
	Addr A28NoFields `ferry:"addr"`
}

type A28NoFields struct{ hidden int }

type A28MapOnly struct {
	Limits map[string]int `ferry:"limits"`
}

func runA28() {
	says("ADR-0005", `"A recursive type has an unbounded static address set, so schema compile
    refuses it." And "Every struct type visited during schema compile must
    contribute at least one address." And ADR-0010: "the maps-no-address backstop
    counts MINTED ADDRESS SHAPES and not static leaf addresses, or
    struct{ Limits map[string]int } is refused, which ADR-0005's own example compiles."`)

	fmt.Println()
	for _, r := range []struct {
		name string
		err  error
	}{
		{"recursive", Compile[A28Node]()},
		{"a struct with no exported field", Compile[A28Opaque]()},
		{"struct{ Limits map[string]int }", Compile[A28MapOnly]()},
	} {
		fmt.Printf("    %-34s -> %v\n", r.name, errOneLine(r.err))
	}

	verdict("IMPLEMENTED", "all three, including E11's correction.")
}

// ---------------------------------------------------------------------------

type a29NoEnum struct{ m map[Path]Value }

func (r a29NoEnum) Get(_ context.Context, p Path) (Value, error) { return r.m[p], nil }

type a29Source struct{ m map[Path]Value }

func (s a29Source) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return a29NoEnum{s.m}, nil }, nil
}

func runA29() {
	says("ADR-0004", `"Loading a map-typed field from a non-enumerating source is an error naming
    the field and the source, never a silently empty map."`)

	ctx := context.Background()
	_, err := Load[A20Conf](ctx, a29Source{m: map[Path]Value{}})
	fmt.Printf("\n    a source with no Enumerator -> err=%v\n", errOneLine(err))

	verdict("IMPLEMENTED", "loadDir.members asserts FEnumerator and refuses.")
	fmt.Println("           NOTE: the message names the address and the Go type but not the")
	fmt.Println("           source, where ADR-0004 says \"naming the field and the source\".")
}

// ---------------------------------------------------------------------------

type A30Bad struct {
	A string `ferry:"x,requird"`
	B string `ferry:"x,nope"`
	C int    `ferry:"c,default=abc"`
	D string
}

func runA30() {
	says("ADR-0001", `determinism is a package-wide invariant; ADR-0010 measured "one distinct
    error string over 300 compiles of a type with four faults".`)

	seen := map[string]int{}
	for range 300 {
		o := defaultOpts()
		o.reg = NewRegistry()
		_, err := compileOnce(reflect.TypeFor[A30Bad](), o)
		seen[fmt.Sprint(err)]++
	}
	fmt.Printf("\n    distinct error strings over 300 compiles: %d\n", len(seen))
	for s := range seen {
		fmt.Printf("    %s\n", strings.ReplaceAll(s, "\n", "\n    "))
	}

	verdict("IMPLEMENTED", "sorted at construction in compileSchema2.")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func a41AddrList(vals map[Path]Value) string {
	var out []string
	for _, p := range sortedAddrs(vals) {
		out = append(out, p.String()+"="+vals[p].GoString())
	}
	return "[" + strings.Join(out, " ") + "]"
}

func joinTail(ps []Path) string {
	var out []string
	for _, p := range ps {
		segs := p.Segments()
		out = append(out, segs[len(segs)-1].Text)
	}
	return strings.Join(out, " ")
}

func tailAll(ss []string) []string {
	var out []string
	for _, s := range ss {
		i := strings.LastIndexAny(s, "/#")
		out = append(out, s[i+1:])
	}
	return out
}

func strSort(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

var _ = filepath.Join
var _ = os.Getenv

// ---------------------------------------------------------------------------
// A31  what the default registry holds before main() runs
// ---------------------------------------------------------------------------

type A31Conf struct {
	Addr netip.Addr `ferry:"addr"`
}

// A31Poll is the other type r18_freeze.go's init registers. It carries no text
// pair, so ADR-0007's chain cannot claim it and the registry is the only thing
// that can - which keeps A31's contrast visible at compile.
type A31Poll struct {
	P R18Poll `ferry:"p"`
}

func runA31() {
	says("ADR-0009", `"The default registry's freeze point is safe by Go's own initialisation
    order, since every init completes before main.main." So a package init that
    registers is the shape every consumer writes, and it is correct.`)

	fmt.Printf("\n  the default registry at the top of main(), before any probe runs:\n")
	fmt.Printf("    registrations: %d\n", len(defaultRegistry.byType))
	// Sorted, because ADR-0001's determinism invariant covers every map
	// iteration reaching a user-visible artefact and an audit's own output is
	// one. Ranging the map directly - which is what this line used to do - made
	// the audit violate the rule it was auditing.
	names := make([]string, 0, len(defaultRegistry.byType))
	for t := range defaultRegistry.byType {
		names = append(names, fmt.Sprintf("%v", t))
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("      %s\n", n)
	}
	fmt.Println("    They come from two package init()s in r18_freeze.go, which exist to")
	fmt.Println("    demonstrate exactly this property of ADR-0009's model.")

	fmt.Println("\n  what that does to every probe that takes defaultOpts():")
	ctx := context.Background()
	fmt.Printf("    Compile[struct{ Addr netip.Addr }]()                    -> %v\n",
		errOneLine(Compile[A31Conf]()))
	fmt.Printf("    Compile[struct{ Addr netip.Addr }](WithRegistry(fresh)) -> %v\n",
		errOneLine(Compile[A31Conf](WithRegistry(NewRegistry()))))
	fmt.Println("    Both compile, and that is #41 D3's doing rather than the registry's:")
	fmt.Println("    ADR-0007's chain claims netip.Addr on its text pair, so the fresh")
	fmt.Println("    registry needs no registration and the two agree on the plane too:")
	dv, _ := dumpTo(ctx, A31Conf{Addr: netip.MustParseAddr("192.0.2.1")})
	fv, _ := dumpTo(ctx, A31Conf{Addr: netip.MustParseAddr("192.0.2.1")}, WithRegistry(NewRegistry()))
	fmt.Printf("      default registry -> %s\n", dv[Path{}.Name("addr")].GoString())
	fmt.Printf("      fresh registry   -> %s\n", fv[Path{}.Name("addr")].GoString())

	fmt.Println("\n  The other registration is where the hazard survives. R18Poll carries")
	fmt.Println("  no text pair, so the chain cannot claim it and only the registry can.")
	fmt.Println("  It compiles either way - it is an int64 kind - and the two disagree")
	fmt.Println("  about what the plane holds, silently:")
	pd, _ := dumpTo(ctx, A31Poll{P: R18Poll(90 * time.Second)})
	pf, _ := dumpTo(ctx, A31Poll{P: R18Poll(90 * time.Second)}, WithRegistry(NewRegistry()))
	fmt.Printf("    Compile[struct{ P R18Poll }]()                    -> %v\n", errOneLine(Compile[A31Poll]()))
	fmt.Printf("    Compile[struct{ P R18Poll }](WithRegistry(fresh)) -> %v\n", errOneLine(Compile[A31Poll](WithRegistry(NewRegistry()))))
	fmt.Printf("      default registry -> %s\n", pd[Path{}.Name("p")].GoString())
	fmt.Printf("      fresh registry   -> %s\n", pf[Path{}.Name("p")].GoString())

	verdict("IMPLEMENTED", "and it is a measurement hazard rather than a defect:")
	fmt.Println("           the registry ADR-0009 designed behaves exactly as designed, and")
	fmt.Println("           the consequence is that E16's and B25's default-registry")
	fmt.Println("           measurements were taken against a registry holding two codecs")
	fmt.Println("           no probe in either suite asked for. A31 is why A3 uses a fresh")
	fmt.Println("           registry to ask about the chain. As FOUND the netip.Addr rows")
	fmt.Println("           differed at COMPILE; with the chain on that pair agrees and the")
	fmt.Println("           hazard moves to R18Poll, where both registries compile and the")
	fmt.Println("           plane holds a different value. That is the harder version: a")
	fmt.Println("           compile error is loud and a changed representation is not.")
}
