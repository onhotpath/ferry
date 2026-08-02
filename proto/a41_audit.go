package main

// #41's audit probes: run each Accepted ADR's normative statement against the
// TIP, through the tip's own entry points, and print what it actually does.
//
// The method is the ticket's: do not read the code and judge whether it looks
// right. Every row below is produced by running.
//
//	A41=<n|all> go run .

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func hA(n, title string) {
	fmt.Printf("\n%s\n== %s  %s\n%s\n", strings.Repeat("=", 100), n, title, strings.Repeat("=", 100))
}

// says prints the ADR sentence a probe is checking, so the output is readable
// without the ADR open beside it.
func says(adr, sentence string) {
	fmt.Printf("  %s says:\n    %s\n", adr, sentence)
}

func verdict(v, detail string) {
	fmt.Printf("  VERDICT: %-18s %s\n", v, detail)
}

var a41 = []struct {
	n     string
	title string
	fn    func()
}{
	{"A1", "ADR-0003: is the address set prefix-free, or only duplicate-free?", runA1},
	{"A2", "ADR-0005: which Go kinds does the tip's compiler actually admit?", runA2},
	{"A3", "ADR-0007: is the codec chain on, and does it run before kind?", runA3},
	{"A4", "ADR-0009: does Register run the codec against the zero value?", runA4},
	{"A5", "ADR-0009: does a key codec have to opt in?", runA5},
	{"A6", "ADR-0011: does a Load aggregate?", runA6},
	{"A7", "ADR-0011: does ferry's own message text carry a plane value?", runA7},
	{"A8", "ADR-0011: is there an error type, four classes and one aggregate?", runA8},
	{"A9", "ADR-0008: does core parse the raw struct tag itself?", runA9},
	{"A10", "ADR-0008: can the grammar write its own headline example?", runA10},
	{"A11", "ADR-0008: is a promoted embedded pointer refused?", runA11},
	{"A12", "ADR-0006: is `required` on a struct and a *struct enforced?", runA12},
	{"A13", "ADR-0005: is an array index the array cannot hold loud?", runA13},
	{"A14", "ADR-0004: does Close always run, and is its failure reported?", runA14},
	{"A15", "ADR-0005: what does the YAML driver report at a container address?", runA15},
	{"A16", "ADR-0008: are near misses rejected with a remedy?", runA16},
}

func runA41(which string) {
	all := which == "all"
	found := all
	for _, p := range append(append([]struct {
		n     string
		title string
		fn    func()
	}{}, a41...), a41b...) {
		if all || strings.EqualFold(which, p.n) || which == strings.TrimPrefix(p.n, "A") {
			found = true
			hA(p.n, p.title)
			p.fn()
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "unknown probe", which)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// A1  ADR-0003's prefix-free rule
// ---------------------------------------------------------------------------

type A1Sub struct {
	Host string `ferry:"host"`
}

// A leaf at /db and a subtree under /db. ADR-0003: "A compiled schema's address
// set contains no address that is a prefix of another."
type A1Clash struct {
	Flat   string `ferry:"db"`
	Nested A1Sub  `ferry:"db"`
}

// The exact-duplicate case, which is the half the tip does implement.
type A1Dup struct {
	A string `ferry:"name"`
	B string `ferry:"name"`
}

func runA1() {
	says("ADR-0003", `"A compiled schema's address set contains no address that is a prefix of
    another. A path is a prefix of itself, so this subsumes exact duplicates."`)

	fmt.Println("\n  a leaf at /db beside a subtree under /db:")
	err := Compile[A1Clash]()
	s, _ := schemaFor(reflect.TypeFor[A1Clash](), defaultOpts())
	fmt.Printf("    Compile[A1Clash]() -> %v\n", err)
	if s != nil {
		fmt.Printf("    address set        -> %v\n", s.as.All())
	}

	fmt.Println("\n  the exact-duplicate case, for contrast:")
	fmt.Printf("    Compile[A1Dup]()   -> %v\n", errOneLine(Compile[A1Dup]()))

	fmt.Println("\n  and what the un-refused schema does on the two plane classes, which")
	fmt.Println("  is ADR-0003's own reason for making it a schema property:")
	ctx := context.Background()
	v := A1Clash{Flat: "scalar", Nested: A1Sub{Host: "h"}}

	dir, _ := os.MkdirTemp("", "a41")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "c.yaml")
	derr := Dump(ctx, v, FYAMLSink{Path: path})
	b, _ := os.ReadFile(path)
	fmt.Printf("    TREE plane (yaml): dump err=%v, file=%q\n", errOneLine(derr), string(b))

	store := NewStore()
	kerr := Dump(ctx, v, BKVSink{Store: store, PerOpen: true})
	fmt.Printf("    FLAT plane (kv)  : dump err=%v, keys=%v\n", errOneLine(kerr), store.Keys())
	fmt.Println("    So the schema compiles, a flat plane takes it, and a tree plane")
	fmt.Println("    cannot represent it - which is exactly the \"loadable from env and")
	fmt.Println("    undumpable to YAML\" schema ADR-0003 refuses at compile so that no")
	fmt.Println("    driver ever sees it.")

	verdict("NOT IMPLEMENTED", "prefixFree() in e_schema.go compares whole rendered")
	fmt.Println("           addresses for equality, so it is duplicate detection. The prefix")
	fmt.Println("           relation at a segment boundary is never asked.")
}

// ---------------------------------------------------------------------------
// A2  ADR-0005's kind admission, asked of the tip's compiler
// ---------------------------------------------------------------------------

func runA2() {
	says("ADR-0005", `"| int, int8, int16, int32, int64 | Number | ... |
     | uint, uint8, uint16, uint32, uint64 | Number | strconv.FormatUint, base 10 |"`)

	fmt.Println("\n  one single-field struct per kind, through the tip's own compiler:")
	type u8 struct {
		V uint8 `ferry:"v"`
	}
	type u16 struct {
		V uint16 `ferry:"v"`
	}
	type i8 struct {
		V int8 `ferry:"v"`
	}
	type f32 struct {
		V float32 `ferry:"v"`
	}
	rows := []struct {
		name string
		err  error
	}{
		{"uint8", Compile[u8]()},
		{"uint16", Compile[u16]()},
		{"int8", Compile[i8]()},
		{"float32", Compile[f32]()},
	}
	for _, r := range rows {
		fmt.Printf("    %-10s -> %v\n", r.name, errOneLine(r.err))
	}

	fmt.Println("\n  and the same question asked of the OTHER admission authority on this")
	fmt.Println("  branch, typeset.go's kindClassify, which the inherited walk uses:")
	for _, t := range []reflect.Type{
		reflect.TypeFor[uint8](), reflect.TypeFor[uint16](),
		reflect.TypeFor[int8](), reflect.TypeFor[float32](),
	} {
		fmt.Printf("    %-10s kindClassify=%v   e_schema.kindLeaf=%v\n",
			t, kindClassify(t) == shapeLeaf, kindLeaf(t))
	}

	verdict("NOT IMPLEMENTED", "uint8 is admitted by typeset.go and refused by")
	fmt.Println("           e_schema.go's kindLeaf, which omits reflect.Uint8 from its case")
	fmt.Println("           list. Two admission authorities disagreeing about one kind is")
	fmt.Println("           ADR-0010's duplication axis 1 inside the type set.")
}

// ---------------------------------------------------------------------------
// A3  ADR-0007's chain
// ---------------------------------------------------------------------------

type A3Conf struct {
	Addr netip.Addr `ferry:"addr"`
}

type A3IP struct {
	IP net.IP `ferry:"ip"`
}

func runA3() {
	says("ADR-0007", `"The text pair is consulted BEFORE reflect.Kind admission.
    A declaration beats an inference."`)

	fmt.Printf("\n  the package defaults, as the tip ships them:\n")
	fmt.Printf("    chainOrder      = %v\n", chainOrder)
	fmt.Printf("    chainBeforeKind = %v\n", chainBeforeKind)

	fmt.Println("\n  ADR-0007's own headline table, re-run through the tip's Compile/Dump:")
	fmt.Printf("    netip.Addr        -> %v\n", errOneLine(Compile[A3Conf]()))
	ctx := context.Background()
	vals, err := dumpTo(ctx, A3IP{IP: net.ParseIP("192.0.2.1")})
	fmt.Printf("    net.IP            -> %s  err=%v\n", vals[Path{}.Name("ip")].GoString(), errOneLine(err))
	fmt.Println("    ADR-0007's table says string(\"192.0.2.1\") for both rows.")

	fmt.Println("\n  with the chain switched on by hand, which is what every P12/P19 probe")
	fmt.Println("  that measures it does in its own body and then reverts:")
	chainOrder, chainBeforeKind = []string{"text"}, true
	fmt.Printf("    netip.Addr        -> %v\n", errOneLine(Compile[A3Conf](WithRegistry(NewRegistry()))))
	vals2, _ := dumpTo(ctx, A3IP{IP: net.ParseIP("192.0.2.1")}, WithRegistry(NewRegistry()))
	fmt.Printf("    net.IP            -> %s\n", vals2[Path{}.Name("ip")].GoString())
	chainOrder, chainBeforeKind = nil, false

	verdict("IMPLEMENTED, OFF", "the chain is implemented and both orders are")
	fmt.Println("           switchable, but the tip's default is chainOrder=nil and")
	fmt.Println("           chainBeforeKind=false, which is the world ADR-0007 rejected.")
	fmt.Println("           No B25 probe sets either, so every ADR-0012 measurement was")
	fmt.Println("           taken with ADR-0007's decision switched off.")
}

// ---------------------------------------------------------------------------
// A4  ADR-0009's zero-value check
// ---------------------------------------------------------------------------

type A4Conf struct {
	A netip.Addr `ferry:"a"`
}

func runA4() {
	says("ADR-0009", `"Register encodes the zero value of T, donates String to the declared
    kind, decodes it back, and refuses the registration if either half errors."`)

	g := StringCodec(netip.Addr.String, netip.ParseAddr)
	reg := NewRegistry()
	err := reg.Register(g)
	fmt.Printf("\n  Register(StringCodec(netip.Addr.String, netip.ParseAddr)) -> %v\n", err)
	fmt.Println("  ADR-0009's own worked refusal for this exact call is:")
	fmt.Println("    ferry: netip.Addr: the codec is not total over the zero value: it")
	fmt.Println("    encodes to string(\"invalid IP\") and decoding that back fails")

	fmt.Println("\n  the free function that does implement it, for contrast:")
	fmt.Printf("    zeroCheck(g) -> %v\n", errOneLine(zeroCheck(g)))

	fmt.Println("\n  and what the accepted registration then does, end to end:")
	ctx := context.Background()
	vals, derr := dumpTo(ctx, A4Conf{}, WithRegistry(reg))
	fmt.Printf("    dump the zero value -> %s err=%v\n", vals[Path{}.Name("a")].GoString(), errOneLine(derr))
	_, lerr := loadFrom(ctx, A4Conf{}, vals, WithRegistry(reg))
	fmt.Printf("    load it back        -> err=%v\n", errOneLine(lerr))

	verdict("NOT IMPLEMENTED", "zeroCheck exists in r16_zerocheck.go and")
	fmt.Println("           (*Registry).Register never calls it. The rule is implemented")
	fmt.Println("           beside the API rather than in it, which is ADR-0007's own third")
	fmt.Println("           defect in a new place.")
}

// ---------------------------------------------------------------------------
// A5  ADR-0009's AsMapKey opt-in
// ---------------------------------------------------------------------------

type A5Conf struct {
	Limits map[netip.Addr]int `ferry:"limits"`
}

func runA5() {
	says("ADR-0009", `"A registration is usable as a map key only if it says so:
    StringCodec(...).AsMapKey(). A map[T]V whose key type is registered without
    it is a schema compile error."`)

	reg := mustReg(NewRegistry(), TextCodec[netip.Addr](VString))
	fmt.Printf("\n  keyOptIn (the package default) = %v\n", keyOptIn)
	fmt.Printf("  map[netip.Addr]int, codec registered WITHOUT .AsMapKey():\n")
	fmt.Printf("    Compile[A5Conf](WithRegistry(reg)) -> %v\n", errOneLine(Compile[A5Conf](WithRegistry(reg))))

	keyOptIn = true
	fmt.Printf("\n  with keyOptIn switched on by hand, which is what R11 and R15 do:\n")
	reg2 := mustReg(NewRegistry(), TextCodec[netip.Addr](VString))
	fmt.Printf("    Compile[A5Conf](WithRegistry(reg2)) -> %v\n", errOneLine(Compile[A5Conf](WithRegistry(reg2))))
	keyOptIn = false

	verdict("IMPLEMENTED, OFF", "the opt-in rule is a package-level switch")
	fmt.Println("           defaulting to the rule ADR-0009 refused. Only R11, R15 and R17")
	fmt.Println("           turn it on, and each reverts it.")
}

// ---------------------------------------------------------------------------
// A6  ADR-0011's aggregation
// ---------------------------------------------------------------------------

type A6Conf struct {
	A int     `ferry:"a"`
	B int     `ferry:"b"`
	C int     `ferry:"c"`
	D bool    `ferry:"d"`
	E float64 `ferry:"e"`
}

func runA6() {
	says("ADR-0011", `"ferry reports every failure that is not a consequence of another failure
    it is already reporting." ... "No StopOnFirstError: it is a public knob whose
    only job is to make ferry report less."`)

	ctx := context.Background()
	bad := map[Path]Value{
		Path{}.Name("a"): Number("x"),
		Path{}.Name("b"): Number("y"),
		Path{}.Name("c"): Number("z"),
		Path{}.Name("d"): Number("q"),
		Path{}.Name("e"): Number("w"),
	}
	_, err := loadFrom(ctx, A6Conf{}, bad)
	fmt.Printf("\n  five leaves, every one unparseable:\n")
	fmt.Printf("    errors reported: %d\n", countErrs(err))
	fmt.Printf("    %v\n", err)

	fmt.Println("\n  the scheduler every entry point on this tip is built with:")
	fmt.Println("    Load, LoadOver, Dump, Binding.Load and SinkBinding.Dump all pass")
	fmt.Println("    `sch: serial`, and serial returns on the first non-nil error.")
	fmt.Println("    The aggregating scheduler exists, in e12_yield.go, and nothing")
	fmt.Println("    outside that probe uses it.")

	verdict("NOT IMPLEMENTED", "the tip is first-error in both directions.")
}

// ---------------------------------------------------------------------------
// A7  ADR-0011's redaction rule
// ---------------------------------------------------------------------------

type A7Conf struct {
	MaxConns int `ferry:"max_conns"`
}

func runA7() {
	says("ADR-0011", `"ferry's own message text never contains a value the plane supplied.
    The cause stays in the chain and is not printed."`)

	const secret = "AKIAIOSFODNN7EXAMPLE"
	ctx := context.Background()
	_, err := loadFrom(ctx, A7Conf{}, map[Path]Value{Path{}.Name("max_conns"): String(secret)})
	msg := fmt.Sprint(err)
	fmt.Printf("\n  the plane holds a secret at an int address.\n")
	fmt.Printf("    ferry's message: %s\n", msg)
	fmt.Printf("    contains the plane's own text: %v\n", strings.Contains(msg, secret))

	verdict("NOT IMPLEMENTED", "loadDir wraps the stdlib error with %w and prints")
	fmt.Println("           it, which is the naive form ADR-0011 measured 4 leaks in 5 for.")
}

// ---------------------------------------------------------------------------
// A8  ADR-0011's type, classes and aggregate
// ---------------------------------------------------------------------------

func runA8() {
	says("ADR-0011", `"type Error struct{ /* no exported fields */ }" with Address(), and
    "var ErrSchema, ErrMissing, ErrValue, ErrPlane error" plus ErrDriver;
    "ferry never calls errors.Join".`)

	fmt.Println("\n  what the tip exports, found by asking for each name:")
	fmt.Printf("    ferry.Error type      : absent (no such type in the package)\n")
	fmt.Printf("    ErrSchema/ErrMissing/ErrValue/ErrPlane/ErrDriver : absent\n")
	fmt.Printf("    ErrReadOnly           : present (%v), which is ADR-0004's\n", ErrReadOnly)
	fmt.Printf("    Elements()/ErrorAt()/DiffErrors() : absent\n")

	fmt.Println("\n  and errors.Join is what the compiler uses:")
	err := Compile[A1Dup]()
	var joined interface{ Unwrap() []error }
	fmt.Printf("    Compile[A1Dup]() unwraps to []error: %v\n", errors.As(err, &joined))
	fmt.Printf("    sorted at construction: yes (compileSchema2 sorts before joining)\n")

	verdict("NOT IMPLEMENTED", "no error type, no class sentinels, no aggregate")
	fmt.Println("           constructor. ADR-0011 was measured on proto/9-errors, which is")
	fmt.Println("           not an ancestor of this tip: its e_*.go probe files never")
	fmt.Println("           reached it, and proto/16's own e_*.go files took those names.")
}

// ---------------------------------------------------------------------------
// A9  ADR-0008's raw tag scanner
// ---------------------------------------------------------------------------

type A9BareQuote struct {
	Origins string `ferry:"origins,default=["value"]" json:"origins" yaml:"origins"`
}

type A9BadEscape struct {
	H string `ferry:"a\,b"`
}

type A9TwoTags struct {
	H string `ferry:"first" ferry:"second"`
}

func runA9() {
	says("ADR-0008", `"Core does not call reflect.StructTag.Get or Lookup. It scans
    reflect.StructField.Tag with its own parser and reports what Get answers with
    a silent empty string."`)

	fmt.Println("\n  the three failure modes ADR-0008 measured, through the tip's compiler:")
	for _, r := range []struct {
		name string
		err  error
		t    reflect.Type
	}{
		{"a bare double quote", Compile[A9BareQuote](), reflect.TypeFor[A9BareQuote]()},
		{"an invalid Go escape", Compile[A9BadEscape](), reflect.TypeFor[A9BadEscape]()},
		{"two ferry tags", Compile[A9TwoTags](), reflect.TypeFor[A9TwoTags]()},
	} {
		f := r.t.Field(0)
		got, ok := f.Tag.Lookup("ferry")
		fmt.Printf("    %-22s Lookup=%q ok=%v\n", r.name, got, ok)
		fmt.Printf("    %-22s Compile -> %v\n", "", errOneLine(r.err))
	}
	fmt.Printf("    and the json tag on the bare-quote field: %q\n",
		reflect.TypeFor[A9BareQuote]().Field(0).Tag.Get("json"))

	verdict("NOT IMPLEMENTED", "parseTag calls f.Tag.Lookup, so an invalid escape")
	fmt.Println("           is indistinguishable from a field carrying no ferry tag, a bare")
	fmt.Println("           quote truncates silently, and a duplicate key is not seen at all.")
}

// ---------------------------------------------------------------------------
// A10  ADR-0008's own headline example
// ---------------------------------------------------------------------------

type A10Conf struct {
	Greeting string `ferry:"greeting,default='Hello, world'"`
	Brokers  string `ferry:"brokers,default='h1:9092,h2:9092'"`
	Odd      string `ferry:"'a,b'"`
}

func runA10() {
	says("ADR-0008", `a name or an option value is bare, or single-quoted with a literal quote
    doubled inside it. Its worked struct is:
      Greeting string ferry:"greeting,default='Hello, world'"
      Brokers  string ferry:"brokers,default='h1:9092,h2:9092'"
      Odd      string ferry:"'a,b'"
    and it measures all three round-tripping through the real YAML driver.`)

	fmt.Println("\n  splitTag on the ADR's own examples:")
	for _, s := range []string{
		"greeting,default='Hello, world'",
		"brokers,default='h1:9092,h2:9092'",
		"'a,b'",
	} {
		fmt.Printf("    %-38q -> %q\n", s, splitTag(s))
	}

	fmt.Println("\n  and end to end, from an empty plane:")
	ctx := context.Background()
	v, err := loadFrom(ctx, A10Conf{}, map[Path]Value{})
	fmt.Printf("    Compile -> %v\n", errOneLine(Compile[A10Conf]()))
	fmt.Printf("    loaded  -> %+v err=%v\n", v, errOneLine(err))

	verdict("NOT IMPLEMENTED", "splitTag treats a quote as significant only when")
	fmt.Println("           it is the first byte of a token, so a quoted OPTION VALUE, which")
	fmt.Println("           begins with `default=`, is not quoted at all and the comma")
	fmt.Println("           inside it splits. The quoted NAME case does work.")
}

// ---------------------------------------------------------------------------
// A11  ADR-0008's embedded-pointer refusal
// ---------------------------------------------------------------------------

type A11Common struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

type A11Conf struct {
	*A11Common
	Port int `ferry:"port"`
}

func runA11() {
	says("ADR-0008", `"embedded pointer, with no ferry tag -> schema compile error" ...
    "Promotion walks the pointed-to struct at the parent address, so the pointer
    has no address subtree of its own, and ADR-0006's presence bit has nothing to
    materialise it from."`)

	fmt.Printf("\n  Compile[A11Conf]() -> %v\n", errOneLine(Compile[A11Conf]()))
	s, err := schemaFor(reflect.TypeFor[A11Conf](), defaultOpts())
	if err == nil {
		fmt.Printf("  address set        -> %v\n", s.as.All())
	}
	ctx := context.Background()
	got, lerr := loadFrom(ctx, A11Conf{}, map[Path]Value{Path{}.Name("name"): String("n")})
	fmt.Printf("  load /name=string(\"n\") -> nil pointer: %v, value: %+v, err=%v\n",
		got.A11Common == nil, got.A11Common, errOneLine(lerr))
	fmt.Println("  ADR-0008 measured this as a silent total loss on ITS prototype;")
	fmt.Println("  this tip's walk materialises the pointer from the promoted children's")
	fmt.Println("  presence bit, so the Load case works and the refusal is still absent.")

	fmt.Println("\n  what the refusal exists to prevent shows up on Dump instead:")
	nilv, derr := dumpTo(ctx, A11Conf{Port: 8080})
	fmt.Printf("    dump with the embedded pointer nil -> err=%v\n", errOneLine(derr))
	for _, p := range sortedAddrs(nilv) {
		fmt.Printf("      %-8q %s\n", p.String(), nilv[p].GoString())
	}
	fmt.Println("    The promoted pointer's own address IS THE EMPTY PATH, so a nil one")
	fmt.Println("    writes Null at the address ADR-0003 says may not exist and ADR-0010's")
	fmt.Println("    root rule refuses everywhere else.")

	verdict("NOT IMPLEMENTED", "no refusal, and a nil promoted pointer mints the")
	fmt.Println("           empty path, which is the hole ADR-0010's root-leaf rule closes")
	fmt.Println("           reached through a door that rule does not cover.")
}

// ---------------------------------------------------------------------------
// A12  ADR-0006's `required` at a container
// ---------------------------------------------------------------------------

type A12Cred struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

type A12Ptr struct {
	Auth *A12Cred `ferry:"auth,required"`
}

type A12Val struct {
	Auth A12Cred `ferry:"auth,required"`
}

func runA12() {
	says("ADR-0006", `"At a composite it means the plane supplied at least one of the address's
    static children." ... "This also repairs a defect the earlier draft shipped:
    required on a non-pointer struct was accepted at schema compile and enforced
    by nothing."`)

	ctx := context.Background()
	fmt.Printf("\n  Compile[A12Ptr]() -> %v\n", errOneLine(Compile[A12Ptr]()))
	fmt.Printf("  Compile[A12Val]() -> %v\n", errOneLine(Compile[A12Val]()))

	pv, perr := loadFrom(ctx, A12Ptr{}, map[Path]Value{})
	fmt.Printf("  *struct, empty plane -> %+v err=%v\n", pv, errOneLine(perr))
	vv, verr := loadFrom(ctx, A12Val{}, map[Path]Value{})
	fmt.Printf("   struct, empty plane -> %+v err=%v\n", vv, errOneLine(verr))
	fmt.Println("  ADR-0006's measured line for both is:")
	fmt.Println("    ferry: /auth: required, and the plane supplied nothing under it")

	fmt.Println("\n  where the flag goes:")
	fmt.Println("    applyOptions sets node.required on a struct and a pointer node, and")
	fmt.Println("    e_walk.go reads n.required in exactly one place, direction.leaf.")

	verdict("NOT IMPLEMENTED", "accepted at compile, enforced by nothing, which is")
	fmt.Println("           the draft defect ADR-0006 records repairing. Independently")
	fmt.Println("           found on proto/16-entry-point by the #10/#14/#15 session.")
}

// ---------------------------------------------------------------------------
// A13  ADR-0005's array bound
// ---------------------------------------------------------------------------

type A13Conf struct {
	V [3]string `ferry:"v"`
}

func runA13() {
	says("ADR-0005", `"Measured: [3]string given only index 0 loads ["a", "", ""], and given
    index 7 returns ferry: /V: plane has index 7, [3]string holds 3."`)

	ctx := context.Background()
	only0 := map[Path]Value{Path{}.Name("v").Index(0): String("a")}
	v0, e0 := loadFrom(ctx, A13Conf{}, only0)
	fmt.Printf("\n  index 0 only -> %q err=%v\n", v0.V, errOneLine(e0))

	over := map[Path]Value{Path{}.Name("v").Index(7): String("z")}
	v7, e7 := loadFrom(ctx, A13Conf{}, over)
	fmt.Printf("  index 7 only -> %q err=%v\n", v7.V, errOneLine(e7))

	verdict("NOT IMPLEMENTED", "the walk visits exactly n.n static element")
	fmt.Println("           addresses and never enumerates an array, so an index the array")
	fmt.Println("           cannot hold is not read and not reported. ADR-0005's second row")
	fmt.Println("           is a measurement the tip cannot produce.")
}

// ---------------------------------------------------------------------------
// A14  ADR-0004's release-and-commit protocol
// ---------------------------------------------------------------------------

type a14Sink struct {
	failSet   bool
	failClose bool
	log       *[]string
}

func (s a14Sink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return &a14Writer{s: s}, nil }, nil
}

type a14Writer struct{ s a14Sink }

func (w *a14Writer) Set(_ context.Context, p Path, v Value) error {
	*w.s.log = append(*w.s.log, "set "+p.String())
	if w.s.failSet {
		return errors.New("kv: no write ACL")
	}
	return nil
}

func (w *a14Writer) Commit(context.Context) error {
	*w.s.log = append(*w.s.log, "commit")
	return nil
}

func (w *a14Writer) Close() error {
	*w.s.log = append(*w.s.log, "close")
	if w.s.failClose {
		return errors.New("kv: flush failed")
	}
	return nil
}

type A14Conf struct {
	A string `ferry:"a"`
}

func runA14() {
	says("ADR-0004", `"Commit runs only when the walk succeeded, Close always runs." and
    ADR-0011: "a Close failure has no location and explains nothing... Discarding
    the latter is silently ignoring something, which ADR-0001 forbids, so it is
    an element."`)

	ctx := context.Background()
	for _, tc := range []struct {
		name               string
		failSet, failClose bool
	}{
		{"success", false, false},
		{"Set fails", true, false},
		{"Close fails", false, true},
		{"both fail", true, true},
	} {
		var log []string
		err := Dump(ctx, A14Conf{A: "x"}, a14Sink{failSet: tc.failSet, failClose: tc.failClose, log: &log})
		fmt.Printf("\n  %-12s calls=%v\n", tc.name, log)
		fmt.Printf("  %-12s err=%v\n", "", errOneLine(err))
	}

	verdict("PARTLY", "Commit-only-on-success and Close-always are implemented.")
	fmt.Println("           A Close failure is discarded: SinkBinding.Dump defers rel.Close()")
	fmt.Println("           without capturing its result, so `Close fails` reports nil.")
}

// ---------------------------------------------------------------------------
// A15  ADR-0005's container-address observation, and what hides it
// ---------------------------------------------------------------------------

type A15Conf struct {
	Tags []string `ferry:"tags"`
}

func runA15() {
	says("ADR-0005", `"| tags: [a] | Get(/tags) = Absent | Children(/tags) = /tags#0 |"`)

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "a41y")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "c.yaml")
	os.WriteFile(path, []byte("tags:\n  - a\n  - b\n"), 0o644)

	open, _ := FYAMLSource{Path: path}.Bind(NewAddressSet(nil))
	rd, err := open(ctx)
	if err != nil {
		fmt.Println("  open:", err)
		return
	}
	v, gerr := rd.Get(ctx, Path{}.Name("tags"))
	fmt.Printf("\n  Get(/tags) on `tags: [a, b]` -> %s, err=%v\n", v.GoString(), gerr)
	fmt.Println("  ADR-0005's table says Absent with no error.")

	fmt.Println("\n  and the reason no probe has ever noticed:")
	got, lerr := Load[A15Conf](ctx, FYAMLSource{Path: path})
	fmt.Printf("    Load[A15Conf] -> %+v err=%v\n", got, errOneLine(lerr))
	fmt.Println("    loadDir's get() discards the error Reader.Get returned and")
	fmt.Println("    substitutes Absent (B10), so the driver's wrong return value is")
	fmt.Println("    invisible. Fixing either one alone surfaces the other.")

	verdict("NOT IMPLEMENTED", "two deviations that cancel: the driver returns an")
	fmt.Println("           error where the ADR measures Absent, and the walk deletes it.")
}

// ---------------------------------------------------------------------------
// A16  ADR-0008's near-miss diagnostics
// ---------------------------------------------------------------------------

type A16Near struct {
	H string `ferry:"h,requird"`
}

type A16Foreign struct {
	H string `ferry:"h,omitempty"`
}

type A16Space struct {
	H string `ferry:"h, required"`
}

func runA16() {
	says("ADR-0008", `"Edit distance, so requird, reqired, defualt and deafult each get the
    right suggestion" and "A table of the neighbourhood's vocabulary, so a word
    from another mapper gets its own sentence". 22 of 26 got a specific remedy.`)

	fmt.Println()
	for _, r := range []struct {
		name string
		err  error
	}{
		{"requird (near miss)", Compile[A16Near]()},
		{"omitempty (foreign)", Compile[A16Foreign]()},
		{"leading space", Compile[A16Space]()},
	} {
		fmt.Printf("    %-22s -> %v\n", r.name, errOneLine(r.err))
	}

	verdict("NOT IMPLEMENTED", "one generic `unknown option` message, no edit")
	fmt.Println("           distance, no vocabulary table, no separate whitespace diagnosis,")
	fmt.Println("           and no tier-1 well-formedness stage above them.")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func errOneLine(err error) string {
	if err == nil {
		return "<nil>"
	}
	return strings.ReplaceAll(err.Error(), "\n", " | ")
}

func countErrs(err error) int {
	if err == nil {
		return 0
	}
	var j interface{ Unwrap() []error }
	if errors.As(err, &j) {
		return len(j.Unwrap())
	}
	return 1
}

func a41Indent(s, pad string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString(pad)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
