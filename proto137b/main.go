// Command proto137b is the evidence behind PROTO-137B.md.
//
// Every block in that document marked "output" is what this prints. Run one
// section at a time:
//
//	go run ./proto137b surface <dir>   core's exported names, over any tree
//	go run ./proto137b reach           which cases the new suite reaches, per type
//	go run ./proto137b 143             the false positive, old case 1 against new
//	go run ./proto137b defects         the eight defect classes through every gate
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
	"github.com/onhotpath/ferry/internal/valuewalk"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: proto137b surface|reach|143|defects|contrast [arg]")

		return
	}

	switch os.Args[1] {
	case "surface":
		surface(os.Args[2])
	case "reach":
		reach()
	case "143":
		falsePositive()
	case "defects":
		defects()
	case "contrast":
		contrast()
	default:
		fmt.Println("unknown section", os.Args[1])
	}
}

// rec is a ferrytest.T that keeps what a suite reported.
type rec struct{ lines []string }

func (r *rec) Errorf(f string, a ...any) { r.lines = append(r.lines, fmt.Sprintf(f, a...)) }
func (*rec) Helper()                     {}

func (r *rec) print(label string) {
	if len(r.lines) == 0 {
		fmt.Printf("  %-42s NO REPORTS\n", label)

		return
	}

	fmt.Printf("  %-42s %d report(s)\n", label, len(r.lines))

	for _, l := range r.lines {
		fmt.Println("    -", l)
	}
}

func codecOver(reg *ferry.Registry) *rec {
	r := &rec{}
	ferrytest.Codec(r, reg)

	return r
}

// ---------------------------------------------------------------- section 1

// surface lists every exported name declared in the package at dir, so that the
// same list can be taken over the base commit and over this tree.
func surface(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("read:", err)

		return
	}

	var out []string

	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}

		out = append(out, namesIn(filepath.Join(dir, n))...)
	}

	slices.Sort(out)
	fmt.Printf("%d exported names in %s\n", len(out), dir)
	fmt.Println(strings.Join(out, " "))
}

func namesIn(path string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}

	var out []string
	for _, d := range f.Decls {
		out = append(out, declNames(d)...)
	}

	return out
}

func declNames(decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil && d.Name.IsExported() {
			return []string{d.Name.Name}
		}

		return nil
	case *ast.GenDecl:
		return specNames(d)
	default:
		return nil
	}
}

func specNames(d *ast.GenDecl) []string {
	var out []string
	for _, s := range d.Specs {
		out = append(out, oneSpec(s)...)
	}

	return out
}

func oneSpec(spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return exported(s.Name)
	case *ast.ValueSpec:
		return exported(s.Names...)
	default:
		return nil
	}
}

func exported(ids ...*ast.Ident) []string {
	var out []string

	for _, id := range ids {
		if id.IsExported() {
			out = append(out, id.Name)
		}
	}

	return out
}

// ---------------------------------------------------------------- section 2

// Meters is the lossy codec's type: it formats to two decimals, so its zero
// value round trips exactly and every value needing a third decimal does not.
type Meters float64

func lossyMeters() ferry.Reg {
	return ferry.StringCodec(
		func(m Meters) string { return fmt.Sprintf("%.2f", float64(m)) },
		func(s string) (Meters, error) { f, err := strconv.ParseFloat(s, 64); return Meters(f), err },
	)
}

// Drift declares String and emits Number at every value but its zero.
type Drift string

func driftingCodec() ferry.Reg {
	return ferry.ValueCodec[Drift](ferry.KindString,
		func(d Drift) (ferry.Value, error) {
			if d == "" {
				return ferry.String(""), nil
			}

			return ferry.Number("42"), nil
		},
		func(v ferry.Value) (Drift, error) { s, err := v.AsString(); return Drift(s), err },
	)
}

// Folding is a key type whose codec folds case, so two keys distinct under ==
// render to one plane address.
type Folding string

func foldingCodec() ferry.Reg {
	return ferry.StringCodec(
		func(f Folding) string { return strings.ToLower(string(f)) },
		func(s string) (Folding, error) { return Folding(s), nil },
	)
}

// Digits declares String and its text is always a run of digits, consistently.
type Digits string

func digitsCodec() ferry.Reg {
	return ferry.StringCodec(
		func(d Digits) string {
			if d == "" {
				return "0"
			}

			return string(d)
		},
		func(s string) (Digits, error) { return Digits(s), nil },
	)
}

// Wandering is the class the reach adds: total over the zero value, and the
// zero's own encoding is not a fixed point of the pair.
type Wandering string

func wanderingCodec() ferry.Reg {
	return ferry.StringCodec(
		func(w Wandering) string {
			if w == "" {
				return "zero"
			}

			return "x:" + string(w)
		},
		func(s string) (Wandering, error) { return Wandering(s), nil },
	)
}

// reach reports, per registered type, which of the six cases the suite now
// exercises against that registrant's own codec.
func reach() {
	rows := []struct {
		name string
		reg  ferry.Reg
	}{
		{"Meters (lossy)", lossyMeters()},
		{"Drift (declares String, emits Number away from zero)", driftingCodec()},
		{"Folding (key codec, not injective)", foldingCodec().AsMapKey()},
		{"Wandering (zero is not a fixed point)", wanderingCodec()},
	}

	fmt.Println("-- what the suite reaches for each registrant's own type, per case")

	for _, row := range rows {
		reg := ferry.NewRegistry()
		if err := reg.Register(row.reg); err != nil {
			fmt.Printf("\n  %s\n    refused at Register: %v\n", row.name, err)

			continue
		}

		fmt.Printf("\n  %s\n", row.name)
		reachOne(reg)
		codecOver(reg).print("ferrytest.Codec(t, reg)")
	}
}

// reachOne prints what the walk sees for the one registered type in reg, which
// is exactly what the suite's per-registrant half asks.
func reachOne(reg *ferry.Registry) {
	t := reg.Types()[0]
	root := reflect.New(wrapperOf(t)).Elem()

	got, err := recordValue(root, reg)
	if err != nil {
		fmt.Printf("    case 2, the zero encodes    FAILED: %v\n", err)

		return
	}

	wrote := got[ferry.At("value")]
	fmt.Printf("    case 2, the zero encodes    reached, wrote %#v\n", wrote)

	dst := reflect.New(wrapperOf(t)).Elem()
	if err := loadValue(dst, wrote, reg); err != nil {
		fmt.Printf("    case 3, and loads back      FAILED: %v\n", err)
	} else {
		again, _ := recordValue(dst, reg)
		fmt.Printf("    case 3, and loads back      reached, re-encodes %#v\n", again[ferry.At("value")])
	}

	fmt.Printf("    case 5, String donated      reached, declared kind is %s\n", wrote.Kind())
	fmt.Printf("    case 4, drift               NOT REACHABLE: needs a value of %s away from its zero\n", t)
	fmt.Printf("    case 6, two keys            NOT REACHABLE: needs two distinct values of %s\n", t)
}

// ---------------------------------------------------------------- section 3

// Tag is #143's fixture: two text spellings that disagree, on a type whose
// registration means ferry calls neither.
type Tag struct{ s string }

// errRefused is what the fixture's halves answer for the one value they
// refuse, so that the error return is reachable rather than decorative.
var errRefused = errors.New("proto137b: the fixture refuses this value")

func (t Tag) MarshalText() ([]byte, error) {
	if t.s == "!" {
		return nil, errRefused
	}

	return []byte("FROM-MARSHAL"), nil
}

func (t Tag) AppendText(b []byte) ([]byte, error) {
	if t.s == "!" {
		return nil, errRefused
	}

	return append(b, "FROM-APPEND"...), nil
}

func (t *Tag) UnmarshalText(b []byte) error {
	if string(b) == "!" {
		return errRefused
	}

	t.s = string(b)

	return nil
}

// oldCase1 is the case exactly as it stood before this branch: pure reflection
// over the caller's types, with no way to ask which arm a registration used.
func oldCase1(reg *ferry.Registry) []string {
	var out []string

	for _, t := range reg.Types() {
		zero := reflect.New(t).Interface()

		appender, isA := zero.(interface{ AppendText([]byte) ([]byte, error) })
		marshaler, isM := zero.(interface{ MarshalText() ([]byte, error) })

		if !isA || !isM {
			continue
		}

		appended, _ := appender.AppendText(nil)
		marshalled, _ := marshaler.MarshalText()

		if !bytes.Equal(appended, marshalled) {
			out = append(out, fmt.Sprintf("codec case 1: %s: AppendText wrote %q and MarshalText wrote %q: "+
				"ferry prefers the appender, so the plane holds the first of these", t, appended, marshalled))
		}
	}

	return out
}

func falsePositive() {
	stringReg := ferry.NewRegistry()
	must(stringReg.Register(ferry.StringCodec(
		func(v Tag) string { return v.s },
		func(s string) (Tag, error) { return Tag{s: s}, nil },
	)))

	textReg := ferry.NewRegistry()
	must(textReg.Register(ferry.TextCodec[Tag](ferry.KindString)))

	fmt.Println("-- #143's fixture, registered through StringCodec, so ferry calls neither text half")
	fmt.Printf("  what ferry actually writes for the zero      %#v\n", writes(stringReg))
	printLines("case 1 as it stood", oldCase1(stringReg))
	codecOver(stringReg).print("ferrytest.Codec(t, reg), this branch")

	fmt.Println("\n-- the same type registered through TextCodec, so ferry does prefer the appender")
	fmt.Printf("  what ferry actually writes for the zero      %#v\n", writes(textReg))
	printLines("case 1 as it stood", oldCase1(textReg))
	codecOver(textReg).print("ferrytest.Codec(t, reg), this branch")
}

func printLines(label string, lines []string) {
	if len(lines) == 0 {
		fmt.Printf("  %-42s NO REPORTS\n", label)

		return
	}

	fmt.Printf("  %-42s %d report(s)\n", label, len(lines))

	for _, l := range lines {
		fmt.Println("    -", l)
	}
}

// writes is what ferry encodes for the zero value of the one type in reg.
func writes(reg *ferry.Registry) ferry.Value {
	root := reflect.New(wrapperOf(reg.Types()[0])).Elem()

	got, err := recordValue(root, reg)
	if err != nil {
		return ferry.Value{}
	}

	return got[ferry.At("value")]
}

// ---------------------------------------------------------------- section 4

// Addr is D7's interface type, and nilAddr is the codec that dereferences a nil
// one.
type Addr interface{ Network() string }

type udp struct{}

func (udp) Network() string { return "udp" }

func nilHostileCodec() ferry.Reg {
	return ferry.ValueCodec[Addr](ferry.KindString,
		func(a Addr) (ferry.Value, error) { return ferry.String(a.Network()), nil },
		func(ferry.Value) (Addr, error) { return udp{}, nil },
	)
}

// Constant is D3: the codec always writes the same text.
type Constant float64

func constantCodec() ferry.Reg {
	return ferry.StringCodec(
		func(Constant) string { return "0.00" },
		func(s string) (Constant, error) { f, err := strconv.ParseFloat(s, 64); return Constant(f), err },
	)
}

// holder is the struct a leaf travels in for the real-dump column.
type holder[T any] struct {
	Value T `ferry:"value"`
}

// keyed is the struct a map travels in for D6.
type keyed[K comparable] struct {
	Map map[K]string `ferry:"m"`
}

type defect struct {
	name string
	reg  func() ferry.Reg
	dump func(reg *ferry.Registry) (map[ferry.Path]ferry.Value, error)
	trip func() ferrytest.Proof
}

// registrantLines keeps only the Complete lines about a registered codec, since
// Complete also names every core type with no proof and that is not what this
// table is about.
func registrantLines(all []string) string {
	var out []string

	for _, l := range all {
		if strings.Contains(l, "has a registered codec") {
			out = append(out, l)
		}
	}

	return strings.Join(out, "; ")
}

// showingProof is D2's proof: values that show the loss.
func showingProof() ferrytest.Proof {
	return ferrytest.Type("Meters", ferrytest.Eq[Meters],
		ferrytest.Case[Meters]{Value: 0, Want: ferry.String("0.00")},
		ferrytest.Case[Meters]{Value: Meters(1.0 / 3.0), Want: ferry.String("0.3333333333333333")},
	)
}

// hidingProof is D8's proof: the same codec, and values that happen to be
// lossless under it.
func hidingProof() ferrytest.Proof {
	return ferrytest.Type("Meters", ferrytest.Eq[Meters],
		ferrytest.Case[Meters]{Value: 0, Want: ferry.String("0.00")},
		ferrytest.Case[Meters]{Value: 2.5, Want: ferry.String("2.50")},
	)
}

func defects() {
	rows := []defect{
		{"D1 not total over the zero value", notTotalCodec, dumpOf[Meters], nil},
		{"D2 lossy", lossyMeters, dumpNonZero, showingProof},
		{"D3 constant", constantCodec, dumpOf[Constant], nil},
		{"D4 drifting kind", driftingCodec, dumpDrift, nil},
		{"D5 consistently wrong kind", digitsCodec, dumpDigits, nil},
		{"D6 non-injective key", func() ferry.Reg { return foldingCodec().AsMapKey() }, dumpBothKeys, nil},
		{"D7 nil-hostile interface codec", nilHostileCodec, dumpOf[Addr], nil},
		{"D8 lossy, proof values lossless", lossyMeters, dumpNonZero, hidingProof},
		{"D9 zero is not a fixed point", wanderingCodec, dumpOf[Wandering], nil},
	}

	for _, row := range rows {
		fmt.Printf("\n%s\n", row.name)
		runDefect(row)
	}
}

func runDefect(d defect) {
	reg := ferry.NewRegistry()

	err := guarded(func() error { return reg.Register(d.reg()) })
	if err != nil {
		fmt.Printf("  %-24s CAUGHT: %v\n", "Register", err)

		return
	}

	fmt.Printf("  %-24s passes\n", "Register")
	codecOver(reg).print("ferrytest.Codec")

	// Only the registered type's own line: Complete also lists every core type
	// with no proof, which is true and not what this table is about.
	if own := registrantLines(ferrytest.Complete(reg)); own != "" {
		fmt.Printf("  %-24s CAUGHT: %s\n", "Complete", own)
	} else {
		fmt.Printf("  %-24s silent\n", "Complete")
	}

	if d.trip != nil {
		r := &rec{}
		ferrytest.RoundTrip(r, ferrytest.MemPlane(), []ferrytest.Proof{d.trip()}, ferry.WithRegistry(reg))
		r.print("RoundTrip + proof")
	}

	got, err := d.dump(reg)
	if err != nil {
		fmt.Printf("  %-24s CAUGHT: %v\n", "a real Dump", err)

		return
	}

	fmt.Printf("  %-24s silent, wrote %v\n", "a real Dump", render(got))
}

func notTotalCodec() ferry.Reg {
	return ferry.StringCodec(
		func(Meters) string { return "not a number" },
		func(s string) (Meters, error) { f, err := strconv.ParseFloat(s, 64); return Meters(f), err },
	)
}

func dumpOf[T any](reg *ferry.Registry) (map[ferry.Path]ferry.Value, error) {
	var zero T

	return ferrytest.Record(context.Background(), holder[T]{Value: zero}, ferry.WithRegistry(reg))
}

func dumpNonZero(reg *ferry.Registry) (map[ferry.Path]ferry.Value, error) {
	return ferrytest.Record(context.Background(), holder[Meters]{Value: Meters(1.0 / 3.0)},
		ferry.WithRegistry(reg))
}

func dumpDigits(reg *ferry.Registry) (map[ferry.Path]ferry.Value, error) {
	return ferrytest.Record(context.Background(), holder[Digits]{Value: "42"}, ferry.WithRegistry(reg))
}

func dumpDrift(reg *ferry.Registry) (map[ferry.Path]ferry.Value, error) {
	return ferrytest.Record(context.Background(), holder[Drift]{Value: "x"}, ferry.WithRegistry(reg))
}

func dumpBothKeys(reg *ferry.Registry) (map[ferry.Path]ferry.Value, error) {
	both := map[Folding]string{"Ab": "", "AB": ""}

	return ferrytest.Record(context.Background(), keyed[Folding]{Map: both}, ferry.WithRegistry(reg))
}

func render(got map[ferry.Path]ferry.Value) string {
	keys := make([]string, 0, len(got))
	for p, v := range got {
		keys = append(keys, fmt.Sprintf("%s=%#v", p, v))
	}

	slices.Sort(keys)

	return "{" + strings.Join(keys, ", ") + "}"
}

func guarded(body func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("PANICKED: %v", r)
		}
	}()

	return body()
}

// ---------------------------------------------------------------- section 5

// contrast is the previous investigation's decisive probe, re-run: is the
// registry still inert, and is it still unfrozen when Codec returns?
func contrast() {
	fmt.Println("-- the same suite over three registries")

	codecOver(nil).print("Codec(t, nil)")

	wrong := ferry.NewRegistry()
	must(wrong.Register(lossyMeters(), driftingCodec(), foldingCodec().AsMapKey()))
	codecOver(wrong).print("Codec(t, three wrong codecs)")

	wandering := ferry.NewRegistry()
	must(wandering.Register(wanderingCodec()))
	codecOver(wandering).print("Codec(t, one wandering codec)")

	fmt.Println("\n-- was reg handed to a verb? a registry freezes at its first retained compile")

	fresh := ferry.NewRegistry()
	must(fresh.Register(lossyMeters()))
	fmt.Printf("  %-42s %v\n", "Register(Constant) before Codec", fresh.Register(constantCodec()))
	codecOver(fresh).print("Codec(t, reg)")
	fmt.Printf("  %-42s %v\n", "Register(Digits) after Codec", fresh.Register(digitsCodec()))

	empty := ferry.NewRegistry()
	codecOver(empty).print("Codec(t, an empty registry)")
	fmt.Printf("  %-42s %v\n", "Register(Meters) after Codec", empty.Register(lossyMeters()))
}

// ---------------------------------------------------------------- shared

// wrapperOf is what the suite builds internally, repeated here so that this
// probe can show what the walk sees without reaching into ferrytest.
func wrapperOf(t reflect.Type) reflect.Type {
	return reflect.StructOf([]reflect.StructField{{
		Name: "Value",
		Type: t,
		Tag:  `ferry:"value"`,
	}})
}

// valueWalker is the seam, declared here exactly as ferrytest declares it: an
// interface over ferry's own types, recovered from internal/valuewalk with one
// assertion. Nothing about it is exported from ferry.
type valueWalker interface {
	DumpValue(ctx context.Context, v reflect.Value, sink ferry.Sink, opts []ferry.Option) error
	LoadValue(ctx context.Context, dst reflect.Value, src ferry.Source, opts []ferry.Option) error
}

var coreWalk, coreWalkOK = valuewalk.Seam.(valueWalker)

// recordValue and loadValue are what the suite does internally, repeated here
// so this probe can show the same reach without reaching into ferrytest.
func recordValue(root reflect.Value, reg *ferry.Registry) (map[ferry.Path]ferry.Value, error) {
	if !coreWalkOK {
		return nil, errors.New("the seam is not installed")
	}

	sink := &recorder{seen: map[ferry.Path]ferry.Value{}}
	opts := []ferry.Option{ferry.WithRegistry(reg)}

	if err := coreWalk.DumpValue(context.Background(), root, sink, opts); err != nil {
		return nil, err
	}

	return sink.seen, nil
}

func loadValue(dst reflect.Value, v ferry.Value, reg *ferry.Registry) error {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{ferry.At("value"): v})

	return coreWalk.LoadValue(context.Background(), dst, src, []ferry.Option{ferry.WithRegistry(reg)})
}

// recorder is a sink that keeps what ferry encoded and writes it nowhere.
type recorder struct{ seen map[ferry.Path]ferry.Value }

func (r *recorder) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return r, nil }, nil
}

func (r *recorder) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	r.seen[addr] = v

	return nil
}

func must(err error) {
	if err != nil {
		panic(errors.New("proto137b: " + err.Error()))
	}
}
