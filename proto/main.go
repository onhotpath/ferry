package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

type Inner struct {
	User string
	Pass string
}

type Conf struct {
	Name    string
	Port    int
	Ratio   float64
	On      bool
	Timeout time.Duration
	When    time.Time
	Secret  []byte
	Auth    Inner
	Opt     *Inner
	Tags    []string
	Limits  map[string]int
}

var runAtEnd = func() {}
var auditHook = func() {}
var containerHook = func() {}
var audit2Hook = func() {}
var audit3Hook = func() {}
var gapsHook = func() {}
var flatHook = func() {}
var edgesHook = func() {}
var timeHook = func() {}
var refusalHook = func() {}
var chainHook = func() {}

func hdr(s string) { fmt.Printf("\n=== %s ===\n", s) }

func dumpAddrs(vals map[Path]Value) {
	for _, p := range sortedAddrs(vals) {
		fmt.Printf("  %-24s %s\n", p, vals[p].GoString())
	}
}

func main() {
	if p := os.Getenv("P19"); p != "" {
		run19(p)
		return
	}
	if p := os.Getenv("P12"); p != "" {
		run12(p)
		return
	}
	ctx := context.Background()

	hdr("P1  the static address set, from the type alone")
	addrs, err := compile(reflect.TypeFor[Conf]())
	fmt.Printf("compile err: %v\n", err)
	for _, p := range sortedPaths(addrs) {
		fmt.Printf("  %s\n", p)
	}

	hdr("P2  nil vs empty slice and map, through the address model")
	for _, c := range []struct {
		label string
		v     Conf
	}{
		{"nil slice, nil map", Conf{Tags: nil, Limits: nil}},
		{"empty slice, empty map", Conf{Tags: []string{}, Limits: map[string]int{}}},
		{"one element each", Conf{Tags: []string{"a"}, Limits: map[string]int{"rps": 1}}},
	} {
		vals, err := dump(reflect.ValueOf(c.v))
		if err != nil {
			fmt.Println("  dump err:", err)
			continue
		}
		var back Conf
		if err := load(vals, reflect.ValueOf(&back).Elem()); err != nil {
			fmt.Println("  load err:", err)
		}
		fmt.Printf("  %-24s tags@%-14s -> nil?%-6v len=%d   limits -> nil?%-6v len=%d\n",
			c.label, vals[Path{}.Name("Tags")].GoString(),
			back.Tags == nil, len(back.Tags), back.Limits == nil, len(back.Limits))
	}

	hdr("P3  is a nil slice's address prefix-free against a populated one?")
	nilv, _ := dump(reflect.ValueOf(Conf{Tags: nil}))
	onev, _ := dump(reflect.ValueOf(Conf{Tags: []string{"a", "b"}}))
	fmt.Println("  nil slice mints:")
	for _, p := range sortedAddrs(nilv) {
		if len(p.String()) >= 5 && p.String()[:5] == "/Tags" {
			fmt.Printf("    %s\n", p)
		}
	}
	fmt.Println("  populated slice mints:")
	for _, p := range sortedAddrs(onev) {
		if len(p.String()) >= 5 && p.String()[:5] == "/Tags" {
			fmt.Printf("    %s\n", p)
		}
	}
	fmt.Println("  -> /Tags is a PREFIX of /Tags#0, so the two shapes are")
	fmt.Println("     not simultaneously representable. One type, two address sets.")

	hdr("P4  round trip through the in-memory value map")
	ny, _ := time.LoadLocation("America/New_York")
	orig := Conf{
		Name: "svc", Port: 8080, Ratio: 3.5, On: true,
		Timeout: 30 * time.Second,
		When:    time.Date(2026, 8, 2, 12, 0, 0, 0, ny),
		Secret:  []byte{0x00, 0xff, 0x41},
		Auth:    Inner{"u", "p"},
		Opt:     nil,
		Tags:    []string{"a", "b,c", ""},
		Limits:  map[string]int{"rps": 10, "burst": 20},
	}
	vals, err := dump(reflect.ValueOf(orig))
	if err != nil {
		fmt.Println("dump err:", err)
	}
	dumpAddrs(vals)
	var back Conf
	if err := load(vals, reflect.ValueOf(&back).Elem()); err != nil {
		fmt.Println("load err:", err)
	}
	fmt.Printf("\n  reflect.DeepEqual(orig, back) : %v\n", reflect.DeepEqual(orig, back))
	fmt.Printf("  When equal by ==              : %v\n", orig.When == back.When)
	fmt.Printf("  When equal by .Equal()        : %v\n", orig.When.Equal(back.When))
	fmt.Printf("  orig.When.Location()          : %v\n", orig.When.Location())
	fmt.Printf("  back.When.Location()          : %q\n", back.When.Location().String())
	fmt.Printf("  every other field DeepEqual   : %v\n", func() bool {
		a, b := orig, back
		a.When, b.When = time.Time{}, time.Time{}
		return reflect.DeepEqual(a, b)
	}())

	hdr("P5  round trip through a REAL YAML plane")
	dir, _ := os.MkdirTemp("", "ferry7")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "c.yaml")
	as := NewAddressSet(sortedAddrs(vals))
	sink := FYAMLSink{Path: path}
	openW, err := sink.Bind(as)
	if err != nil {
		fmt.Println("bind sink:", err)
	}
	if err := fDump(ctx, openW, vals, as); err != nil {
		fmt.Println("dump err:", err)
	}
	b, _ := os.ReadFile(path)
	fmt.Printf("--- %s ---\n%s---\n", path, b)

	src := FYAMLSource{Path: path}
	openR, err := src.Bind(as)
	if err != nil {
		fmt.Println("bind src:", err)
	}
	got, err := fLoad(ctx, openR, as)
	if err != nil {
		fmt.Println("load err:", err)
	}
	fmt.Println("  address-level diff after a real YAML round trip:")
	diffs := 0
	for _, p := range as.All() {
		if vals[p] != got[p] {
			fmt.Printf("    %-22s %s -> %s\n", p, vals[p].GoString(), got[p].GoString())
			diffs++
		}
	}
	fmt.Printf("  %d of %d addresses differ\n", diffs, as.Len())

	var yback Conf
	if err := load(got, reflect.ValueOf(&yback).Elem()); err != nil {
		fmt.Println("  struct load err:", err)
	}
	fmt.Printf("  Secret: %v -> %v\n", orig.Secret, yback.Secret)
	fmt.Printf("  Tags:   %q -> %q\n", orig.Tags, yback.Tags)

	hdr("P6  float specials through the leaf codec and through YAML")
	type F struct{ V float64 }
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.Copysign(0, -1)} {
		v, _ := dump(reflect.ValueOf(F{f}))
		txt := v[Path{}.Name("V")]
		var bk F
		load(v, reflect.ValueOf(&bk).Elem())
		fmt.Printf("  %-6v -> %-14s -> %-6v  ==:%-6v bits==:%v\n",
			f, txt.GoString(), bk.V, bk.V == f, math.Float64bits(bk.V) == math.Float64bits(f))
	}
	// and through YAML
	fv, _ := dump(reflect.ValueOf(F{math.Inf(1)}))
	fas := NewAddressSet(sortedAddrs(fv))
	p2 := filepath.Join(dir, "f.yaml")
	ow, _ := FYAMLSink{Path: p2}.Bind(fas)
	fDump(ctx, ow, fv, fas)
	fb, _ := os.ReadFile(p2)
	fmt.Printf("  +Inf as YAML: %q\n", string(fb))
	or2, _ := FYAMLSource{Path: p2}.Bind(fas)
	fg, ferr := fLoad(ctx, or2, fas)
	fmt.Printf("  read back: %s err=%v\n", fg[Path{}.Name("V")].GoString(), ferr)

	hdr("P7  what compile refuses")
	type Bad1 struct{ C complex128 }
	type Bad2 struct{ F func() }
	type Bad3 struct{ I interface{ Foo() } }
	type Bad4 struct{ Ch chan int }
	type Bad5 struct{ R []rune }
	for _, t := range []reflect.Type{
		reflect.TypeFor[Bad1](), reflect.TypeFor[Bad2](),
		reflect.TypeFor[Bad3](), reflect.TypeFor[Bad4](), reflect.TypeFor[Bad5](),
	} {
		_, err := compile(t)
		fmt.Printf("  %-28s -> %v\n", t, err)
	}
	type Timeout time.Duration
	type T2 struct{ D Timeout }
	a2, e2 := compile(reflect.TypeFor[T2]())
	dv, _ := dump(reflect.ValueOf(T2{Timeout(30 * time.Second)}))
	fmt.Printf("  %-28s -> addrs=%v err=%v value=%s\n", "struct{D Timeout}", a2, e2, dv[Path{}.Name("D")].GoString())
	fmt.Println("     ^ a named type OVER time.Duration falls to kind int64 and dumps nanoseconds.")

	hdr("P8  the round-trip property harness")
	runAtEnd()

	hdr("P9  audit")
	auditHook()

	hdr("P10 what a real plane reports at a container address")
	containerHook()

	hdr("P11 representation, and the property's blind spot")
	audit2Hook()

	hdr("P12 the struct admitted by kind that maps nothing")
	audit3Hook()

	hdr("P13 gap audit against the ticket's literal asks")
	gapsHook()

	hdr("P14 the flattening plane")
	flatHook()

	hdr("P15 what actually happens to the types people reach for")
	edgesHook()

	hdr("P16 what the time.Time losses cost")
	timeHook()

	hdr("P17 every refusal: the limiting factor, and whether a codec lifts it")
	refusalHook()

	hdr("P18 how much the codec-chain ORDER decides (interacts with #12)")
	chainHook()
}

func init() { runAtEnd = runHarness }

func init() { auditHook = runAudit }

func init() { containerHook = runContainer }

func init() { audit2Hook = runAudit2 }

func init() { audit3Hook = runAudit3 }

func init() { gapsHook = runGaps }

func init() { flatHook = runFlat }

func init() { edgesHook = runEdges }

func init() { timeHook = runTimeCost }

func init() { refusalHook = runRefusals }

func init() { chainHook = runChainOrder }
