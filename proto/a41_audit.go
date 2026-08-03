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
	"strconv"
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

	fmt.Println("\n  and what the schema does on the two plane classes, which is")
	fmt.Println("  ADR-0003's own reason for making it a schema property:")
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
	fmt.Println("    The refusal reaches both drivers as a compile error, so neither")
	fmt.Println("    plane ever sees the schema - which is exactly the \"loadable from")
	fmt.Println("    env and undumpable to YAML\" case ADR-0003 refuses at compile.")

	verdict("IMPLEMENTED", "#41 D2 made prefixFree() ask the prefix relation at a")
	fmt.Println("           segment boundary. As FOUND it compared whole rendered addresses")
	fmt.Println("           for equality, so it was duplicate detection and the clash above")
	fmt.Println("           compiled, dumped to a flat plane and could not reach a tree one.")
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

	verdict("IMPLEMENTED", "#41 D18 put reflect.Uint8 back into e_schema.go's")
	fmt.Println("           kindLeaf case list, so both admission authorities now give the")
	fmt.Println("           same answer for every kind above. As FOUND uint8 was admitted by")
	fmt.Println("           typeset.go and refused by kindLeaf, which is ADR-0010's")
	fmt.Println("           duplication axis 1 inside the type set. The two authorities still")
	fmt.Println("           disagree about 12 of 26 named third-party types (X3=4), for a")
	fmt.Println("           different reason: ADR-0008's field rule is on one and not the other.")
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

type A3Pfx struct {
	P netip.Prefix `ferry:"p"`
}

func runA3() {
	says("ADR-0007", `"The text pair is consulted BEFORE reflect.Kind admission.
    A declaration beats an inference."`)

	// This probe drives the two chain globals through both orders, so it must
	// hand the tip's own defaults back. Restoring a hardcoded nil/false at the
	// end - which is what it used to do - left every later probe running in the
	// world before #41 D3 flipped the default.
	defer func(o []string, b bool) { chainOrder, chainBeforeKind = o, b }(chainOrder, chainBeforeKind)

	fmt.Printf("\n  the package defaults, as the tip ships them:\n")
	fmt.Printf("    chainOrder      = %v (len %d)\n", chainOrder, len(chainOrder))
	fmt.Printf("    chainBeforeKind = %v\n", chainBeforeKind)

	fmt.Println("\n  ADR-0007's own headline table, re-run against a FRESH registry so the")
	fmt.Println("  chain is the only thing answering (see A31 for why that matters):")
	ctx := context.Background()
	for _, m := range []struct {
		label  string
		order  []string
		before bool
	}{
		{"as the tip ships", nil, false},
		{"chain before kind", []string{"text"}, true},
	} {
		chainOrder, chainBeforeKind = m.order, m.before
		fmt.Printf("    %-20s netip.Addr    -> %v\n", m.label, errOneLine(Compile[A3Conf](WithRegistry(NewRegistry()))))
		fmt.Printf("    %-20s netip.Prefix  -> %v\n", "", errOneLine(Compile[A3Pfx](WithRegistry(NewRegistry()))))
		vals, _ := dumpTo(ctx, A3IP{IP: net.ParseIP("192.0.2.1")}, WithRegistry(NewRegistry()))
		fmt.Printf("    %-20s net.IP        -> %s\n", "", vals[Path{}.Name("ip")].GoString())
	}
	fmt.Println("    ADR-0007's chosen column is string(\"...\") for all three.")

	verdict("IMPLEMENTED, ON", "the chain is implemented, both orders are")
	fmt.Println("           switchable, and #41 D3 made chainOrder=[text] and")
	fmt.Println("           chainBeforeKind=true the tip's default, which is the world")
	fmt.Println("           ADR-0007 chose. As FOUND the default was nil/false, so every")
	fmt.Println("           ADR-0012 measurement was taken with the decision switched off.")
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

	fmt.Printf("\n  the registry the refusal leaves behind: %d registration(s)\n", len(reg.byType))
	fmt.Println("  so the dump below is answered by ADR-0007's chain, not by the codec:")
	ctx := context.Background()
	vals, derr := dumpTo(ctx, A4Conf{}, WithRegistry(reg))
	fmt.Printf("    dump the zero value -> %s err=%v\n", vals[Path{}.Name("a")].GoString(), errOneLine(derr))
	_, lerr := loadFrom(ctx, A4Conf{}, vals, WithRegistry(reg))
	fmt.Printf("    load it back        -> err=%v\n", errOneLine(lerr))

	fmt.Println("\n  and a codec that IS total over the zero value, for contrast:")
	reg2 := NewRegistry()
	fmt.Printf("    Register(TextCodec[netip.Addr](VString)) -> %v\n",
		errOneLine(reg2.Register(TextCodec[netip.Addr](VString))))
	vals2, derr2 := dumpTo(ctx, A4Conf{}, WithRegistry(reg2))
	fmt.Printf("    dump the zero value -> %s err=%v\n", vals2[Path{}.Name("a")].GoString(), errOneLine(derr2))
	_, lerr2 := loadFrom(ctx, A4Conf{}, vals2, WithRegistry(reg2))
	fmt.Printf("    load it back        -> err=%v\n", errOneLine(lerr2))

	verdict("IMPLEMENTED", "#41 D4 moved the check inside (*Registry).Register,")
	fmt.Println("           where ADR-0009 puts it, and the refusal above is the ADR's own")
	fmt.Println("           worked text. As FOUND zeroCheck lived in r16_zerocheck.go and")
	fmt.Println("           Register never called it: the rule was implemented beside the API")
	fmt.Println("           rather than in it. The half Register still cannot check is")
	fmt.Println("           equality, which ADR-0005 keeps with the registrant (X3=3).")
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

	// Same reason as A3: this probe drives keyOptIn, so it hands the tip's own
	// default back rather than a hardcoded false.
	defer func(k bool) { keyOptIn = k }(keyOptIn)

	reg := mustReg(NewRegistry(), TextCodec[netip.Addr](VString))
	fmt.Printf("\n  keyOptIn (the package default) = %v\n", keyOptIn)
	fmt.Printf("  map[netip.Addr]int, codec registered WITHOUT .AsMapKey():\n")
	fmt.Printf("    Compile[A5Conf](WithRegistry(reg)) -> %v\n", errOneLine(Compile[A5Conf](WithRegistry(reg))))

	keyOptIn = false
	fmt.Printf("\n  with keyOptIn switched OFF, which is the world as FOUND:\n")
	reg2 := mustReg(NewRegistry(), TextCodec[netip.Addr](VString))
	fmt.Printf("    Compile[A5Conf](WithRegistry(reg2)) -> %v\n", errOneLine(Compile[A5Conf](WithRegistry(reg2))))

	verdict("IMPLEMENTED, ON", "the opt-in rule is a package-level switch and")
	fmt.Println("           #41 D5 made it default to the rule ADR-0009 chose. As FOUND it")
	fmt.Println("           defaulted to the rule ADR-0009 refused, and only R11, R15 and")
	fmt.Println("           R17 turned it on. #45 records the hole it still does not close:")
	fmt.Println("           an unregistered type the chain claims at kind String keys a map")
	fmt.Println("           with nobody asked, so the refusal is lifted by DELETING a line.")
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

	fmt.Println("\n  and the Dump direction, on a plane that refuses every address:")
	derr := Dump(ctx, A6Conf{}, refuseAllSink{})
	fmt.Printf("    errors reported: %d\n", countErrs(derr))

	verdict("IMPLEMENTED", "#41 landed the aggregating scheduler on the entry")
	fmt.Println("           points in both directions. As FOUND every entry point passed")
	fmt.Println("           `sch: serial`, which returns on the first non-nil error, and the")
	fmt.Println("           aggregating scheduler existed only inside e12_yield.go's probe.")
	fmt.Println("           `serial` and `aggregating` are both unexported and WithSched takes")
	fmt.Println("           an unexported parameter type, so no importer can select either")
	fmt.Println("           (X4=5); aggregating is what ferry does, not what it offers.")
}

// refuseAllSink is the smallest plane that fails every Set, so A6 can ask the
// Dump direction the question it asks the Load one.
type refuseAllSink struct{}

func (s refuseAllSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return refuseAllWriter{}, nil }, nil
}

type refuseAllWriter struct{}

func (refuseAllWriter) Set(context.Context, Path, Value) error {
	return errors.New("this plane refuses everything")
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
	fmt.Printf("    and the cause is still in the chain: errors.Is(err, strconv.ErrSyntax) = %v\n",
		errors.Is(err, strconv.ErrSyntax))

	verdict("IMPLEMENTED", "#41 gave ferry its own message text (ferr_msg.go) and")
	fmt.Println("           kept the plane's own words in the cause without printing them.")
	fmt.Println("           As FOUND loadDir wrapped the stdlib error with %w and printed it,")
	fmt.Println("           which is the naive form ADR-0011 measured 4 leaks in 5 for.")
}

// ---------------------------------------------------------------------------
// A8  ADR-0011's type, classes and aggregate
// ---------------------------------------------------------------------------

// A8Multi carries two independent schema refusals - an unknown option and a
// default that does not parse - so the compiler is forced to build an aggregate
// rather than return a single error bare.
type A8Multi struct {
	A string `ferry:"a,nope"`
	B int    `ferry:"b,default=abc"`
}

func runA8() {
	says("ADR-0011", `"type Error struct{ /* no exported fields */ }" with Address(), and
    "var ErrSchema, ErrMissing, ErrValue, ErrPlane error" plus ErrDriver;
    "ferry never calls errors.Join".`)

	// Every name below is referenced rather than described, so this block is a
	// compile-time proof that the identifier exists and a run-time measurement
	// of what it does. As FOUND, none of them existed and these lines were
	// literals reading `absent`.
	fmt.Println("\n  what the tip exports, by using each name:")
	ctx := context.Background()
	_, verr := loadFrom(ctx, A7Conf{}, map[Path]Value{Path{}.Name("max_conns"): String("nope")})
	var fe *Error
	fmt.Printf("    *ferry.Error          : present, errors.As -> %v, Address() = %v\n",
		errors.As(verr, &fe), func() Path {
			if fe != nil {
				return fe.Address()
			}
			return Path{}
		}())
	for _, c := range []struct {
		name string
		err  error
	}{
		{"ErrSchema", ErrSchema}, {"ErrMissing", ErrMissing}, {"ErrValue", ErrValue},
		{"ErrPlane", ErrPlane}, {"ErrDriver", ErrDriver}, {"ErrReadOnly", ErrReadOnly},
	} {
		fmt.Printf("    %-21s : %q   this error Is it: %v\n", c.name, c.err, errors.Is(verr, c.err))
	}
	fmt.Printf("    Elements(err)         : %d element(s)\n", len(Elements(verr)))
	fmt.Printf("    ErrorAt(/x, err)      : %v\n", errOneLine(ErrorAt(Path{}.Name("x"), verr)))
	fmt.Printf("    DiffErrors(...)       : %v\n",
		DiffErrors(verr, Want{Address: Path{}.Name("max_conns"), Class: ErrValue}))

	fmt.Println("\n  and where errors.Join survives, which is the part still open.")
	fmt.Println("  A schema with two independent refusals, so the aggregate is real:")
	err := Compile[A8Multi]()
	fmt.Printf("    Compile[A8Multi]()    -> %v\n", errOneLine(err))
	fmt.Printf("    it is a *ferry aggregate     : %v\n", isErrorList(err))
	fmt.Printf("    Elements() can range it      : %d of the 2 refusals above\n", len(Elements(err)))
	fmt.Println("    errors.Join's result is invisible to Elements() and is ordered by")
	fmt.Println("    construction, so ADR-0003's segment-wise rule does not reach it.")

	verdict("PARTLY", "#41 landed ADR-0011's model whole on the RUNTIME path:")
	fmt.Println("           the type, Address(), five class sentinels, Elements(), ErrorAt()")
	fmt.Println("           and DiffErrors() all exist and the runtime calls `join` rather")
	fmt.Println("           than errors.Join. As FOUND none of them existed, because ADR-0011")
	fmt.Println("           was measured on proto/9-errors, which is not an ancestor of this")
	fmt.Println("           tip. What is still open is the COMPILER path: e_schema.go:107 and")
	fmt.Println("           :423 still call errors.Join, so a schema refusal is invisible to")
	fmt.Println("           Elements() and is ordered by construction rather than segment-wise.")
}

func isErrorList(err error) bool {
	var l *errorList
	return errors.As(err, &l)
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

	verdict("IMPLEMENTED", "#41 D9 gave core its own scanner over the raw")
	fmt.Println("           StructField.Tag, and all three failure modes are now named with")
	fmt.Println("           a remedy. Read the Lookup column beside the Compile column: it is")
	fmt.Println("           what a reflect.StructTag.Get-based core would have seen instead.")
	fmt.Println("           As FOUND parseTag called f.Tag.Lookup, so an invalid escape was")
	fmt.Println("           indistinguishable from a field carrying no ferry tag, a bare quote")
	fmt.Println("           truncated silently, and a duplicate key was not seen at all.")
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

	verdict("IMPLEMENTED", "#41 D9's splitter honours a quote wherever a token")
	fmt.Println("           may start one, so ADR-0008's own three-line struct compiles and")
	fmt.Println("           loads its defaults verbatim. As FOUND splitTag treated a quote as")
	fmt.Println("           significant only as the first byte of a token, so a quoted OPTION")
	fmt.Println("           VALUE - which begins with `default=` - was not quoted at all and")
	fmt.Println("           the comma inside it split. Only the quoted NAME case worked.")
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
	fmt.Println("  The refusal is a SCHEMA one, so it reaches Load before any plane is")
	fmt.Println("  opened, which is where ADR-0008 puts it.")

	fmt.Println("\n  and the same refusal on the Dump side, which is what it exists for:")
	nilv, derr := dumpTo(ctx, A11Conf{Port: 8080})
	fmt.Printf("    dump with the embedded pointer nil -> err=%v\n", errOneLine(derr))
	for _, p := range sortedAddrs(nilv) {
		fmt.Printf("      %-8q %s\n", p.String(), nilv[p].GoString())
	}
	fmt.Println("    As FOUND this dumped Null at the EMPTY PATH - the promoted pointer")
	fmt.Println("    has no address of its own - which is the address ADR-0003 says may")
	fmt.Println("    not exist and ADR-0010's root rule refuses everywhere else.")

	verdict("IMPLEMENTED", "#41 D11 added the refusal, in both directions and at")
	fmt.Println("           schema compile. As FOUND there was none: Load materialised the")
	fmt.Println("           pointer from the promoted children's presence bit and Dump wrote")
	fmt.Println("           Null at the empty path, reaching the hole ADR-0010's root-leaf")
	fmt.Println("           rule closes through a door that rule does not cover.")
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

	fmt.Println("\n  and the half the rule is defined against - one child present:")
	one := map[Path]Value{Path{}.Name("auth").Name("user"): String("u")}
	pv1, perr1 := loadFrom(ctx, A12Ptr{}, one)
	// Dereferenced, because a printed pointer is a heap address and a regression
	// diff over this suite has to be stable across runs (ADR-0001, determinism).
	fmt.Printf("  *struct, one child   -> &%+v err=%v\n", *pv1.Auth, errOneLine(perr1))
	vv1, verr1 := loadFrom(ctx, A12Val{}, one)
	fmt.Printf("   struct, one child   -> %+v err=%v\n", vv1, errOneLine(verr1))

	verdict("IMPLEMENTED", "#41 made the composite read n.required, so both the")
	fmt.Println("           pointer and the value form report ADR-0006's own line, and one")
	fmt.Println("           present child satisfies it. As FOUND `required` on a non-pointer")
	fmt.Println("           struct was accepted at compile and enforced by nothing, which is")
	fmt.Println("           the draft defect ADR-0006 records repairing - independently found")
	fmt.Println("           on proto/16-entry-point by the #10/#14/#15 session.")
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

	verdict("IMPLEMENTED", "#41 D17 made the walk enumerate the plane under an")
	fmt.Println("           array address and report an index the array cannot hold, so both")
	fmt.Println("           of ADR-0005's rows now reproduce. As FOUND the walk visited")
	fmt.Println("           exactly n static element addresses and never enumerated, so the")
	fmt.Println("           over-index was not read and not reported.")
	fmt.Println("           One evidence detail: the address prints as /v and ADR-0005")
	fmt.Println("           published /V, because the ADR's prototype invented the Go field")
	fmt.Println("           name and ADR-0008's field rule since made the tag mandatory.")
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

	verdict("IMPLEMENTED", "all three: Commit only on success, Close always, and")
	fmt.Println("           a Close failure joined as an element with no location - which is")
	fmt.Println("           what `2 errors: /a, (close)` on the last row is. As FOUND")
	fmt.Println("           SinkBinding.Dump deferred rel.Close() without capturing its")
	fmt.Println("           result, so the `Close fails` row reported nil and ferry silently")
	fmt.Println("           ignored a failure, which ADR-0001 forbids.")
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

	fmt.Println("\n  and end to end, which is the half that used to hide it:")
	got, lerr := Load[A15Conf](ctx, FYAMLSource{Path: path})
	fmt.Printf("    Load[A15Conf] -> %+v err=%v\n", got, errOneLine(lerr))

	fmt.Println("\n  and the walk no longer deletes a driver error, so a plane that")
	fmt.Println("  really does fail at this address is now distinguishable from Absent:")
	// The message carries a temp path, so this asserts the CLASS rather than
	// printing it: the suite is regression-diffed and a path is not stable.
	_, ferr := Load[A15Conf](ctx, FYAMLSource{Path: filepath.Join(dir, "gone.yaml")})
	fmt.Printf("    Load against a missing file -> non-nil: %v, Is(ErrPlane): %v, Is(ErrMissing): %v\n",
		ferr != nil, errors.Is(ferr, ErrPlane), errors.Is(ferr, ErrMissing))

	verdict("IMPLEMENTED", "both halves. #41 made the YAML driver return Absent")
	fmt.Println("           with a nil error at a container address, which is ADR-0005's own")
	fmt.Println("           row, and B10's fix stopped loadDir's get() from substituting")
	fmt.Println("           Absent for a real driver error. As FOUND the two deviations")
	fmt.Println("           cancelled: the driver returned an error where the ADR measures")
	fmt.Println("           Absent, and the walk deleted it, so neither was visible alone.")
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

	verdict("IMPLEMENTED", "all three diagnostics: edit distance for the near")
	fmt.Println("           miss, the neighbourhood vocabulary table for the foreign word,")
	fmt.Println("           and a separate whitespace sentence. As FOUND there was one")
	fmt.Println("           generic `unknown option` message and none of the three.")
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
