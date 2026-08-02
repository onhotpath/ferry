package main

// The Null-at-a-scalar question, reopened. The ADR's deciding argument was
// that plane-to-plane transfer would silently turn a YAML null into a zero.
// Transfer is Enabled in ADR-0001, so it ships OUTSIDE core, which means it
// may not be core's to protect. These probes test the argument rather than
// restate it.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
)

type NConf struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=8080"`
}

// NCont carries the container case separately, because a "/Tags/*" shape
// address is not a plane address and querying it directly is a prototype
// artefact rather than a finding.
type NCont struct {
	Host string `ferry:"host"`
}

// ---------------------------------------------------------------------------
// N1  Does an address-to-address transfer exist, and does it preserve Null?
// ---------------------------------------------------------------------------

func n1() {
	dhdr("N1  the two shapes of plane-to-plane transfer")
	ctx := context.Background()
	s := mustSchema(reflect.TypeFor[NConf]())
	dir, _ := os.MkdirTemp("", "ferryN")
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "in.yaml")
	_ = os.WriteFile(src, []byte("host: h\nport:\n"), 0o644)

	vals, err := yamlVals(ctx, src, s.addrs)
	if err != nil {
		fmt.Println("  read err:", err)
		return
	}
	fmt.Println("  what the source plane reports:")
	for _, p := range sortedAddrs(vals) {
		fmt.Printf("     %-8s %s\n", p, vals[p].GoString())
	}

	// (a) address-to-address: Get from A, Set into B. No Go value anywhere.
	// This is ~10 lines over ADR-0004's contract and needs no struct.
	out := map[Path]Value{}
	for _, p := range s.addrs {
		out[p] = vals[p] // what a Writer.Set would be handed
	}
	fmt.Println("\n  (a) ADDRESS-TO-ADDRESS, which never builds a Go value:")
	for _, p := range sortedAddrs(out) {
		fmt.Printf("     %-8s %s\n", p, out[p].GoString())
	}

	// (b) struct-mediated: Load into T, Dump out.
	fmt.Println("\n  (b) STRUCT-MEDIATED, under each candidate rule:")
	for _, r := range []struct {
		label string
		o     loadOpts
	}{
		{"refuse        ", loadOpts{}},
		{"null-means-zero", loadOpts{nullMeansZero: true}},
	} {
		var v NConf
		_, e := loadD(vals, s, reflect.ValueOf(&v).Elem(), r.o)
		if e != nil {
			fmt.Printf("     %s  REFUSED: %v\n", r.label, e)
			continue
		}
		calls, _ := dumpD(reflect.ValueOf(v), s)
		fmt.Printf("     %s  %+v -> %s\n", r.label, v, callStr(calls))
	}
	fmt.Println("\n  and the container case, which ADR-0005 already settled:")
	sc := mustSchema(reflect.TypeFor[D2Conf]())
	for _, doc := range []string{"Tags: []\n", "Tags: null\n"} {
		f := filepath.Join(dir, "t.yaml")
		_ = os.WriteFile(f, []byte(doc), 0o644)
		tv, e := yamlVals(ctx, f, []Path{addr("Tags"), addr("Limits")})
		if e != nil {
			fmt.Println("     err:", e)
			continue
		}
		var t D2Conf
		_, _ = loadD(tv, sc, reflect.ValueOf(&t).Elem(), loadOpts{})
		calls, _ := dumpD(reflect.ValueOf(t), sc)
		fmt.Printf("     source %-12q -> struct Tags nil=%-5v -> dumps %s\n",
			doc[:len(doc)-1], t.Tags == nil, callStr(calls))
	}
	fmt.Println("\n  So a STRUCT-MEDIATED transfer already rewrites `Tags: []` to `null`,")
	fmt.Println("  and ADR-0005 accepted that. The transfer that preserves the plane")
	fmt.Println("  exactly is (a), and (a) never runs #8's rules at all.")
}

// ---------------------------------------------------------------------------
// N2  Is either policy recoverable by the mechanism ferry already ships?
// ---------------------------------------------------------------------------

type LenientPort int

func init() {
	// ADR-0005: a codec collapses a type to a leaf, and a registered codec is
	// consulted before kind. So a codec can decide what Null means for ITS type.
	byIdentity[reflect.TypeFor[LenientPort]()] = leafCodec{
		name: "LenientPort",
		enc: func(v reflect.Value) (Value, error) {
			return Number(strconv.FormatInt(v.Int(), 10)), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			if val.Kind() == VNull {
				dst.SetInt(0) // this type says a plane null means zero
				return nil
			}
			s, err := val.AsNumber()
			if err != nil {
				if s2, e2 := val.AsString(); e2 == nil {
					s = s2
				} else {
					return err
				}
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return err
			}
			dst.SetInt(n)
			return nil
		},
	}
}

type NRecover struct {
	Strict  int         `ferry:"strict"`
	Lenient LenientPort `ferry:"lenient"`
}

func n2() {
	dhdr("N2  can a user recover the other policy without a knob?")
	s := mustSchema(reflect.TypeFor[NRecover]())
	plane := map[Path]Value{addr("strict"): Null(), addr("lenient"): Null()}

	fmt.Println("  under REFUSE as the core rule:")
	for _, f := range []string{"strict", "lenient"} {
		var v NRecover
		_, err := loadD(map[Path]Value{addr(f): Null()}, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("     /%-8s Null -> %v  err=%v\n", f, structField(v, f), err)
	}
	fmt.Println("     A registered codec recovers leniency for its own type, which is")
	fmt.Println("     ADR-0005's stated escape hatch used for exactly what it is for.")

	fmt.Println("\n  under ZERO as the core rule:")
	for _, f := range []string{"strict", "lenient"} {
		var v NRecover
		_, err := loadD(map[Path]Value{addr(f): Null()}, s, reflect.ValueOf(&v).Elem(), loadOpts{nullMeansZero: true})
		fmt.Printf("     /%-8s Null -> %v  err=%v\n", f, structField(v, f), err)
	}
	fmt.Println("     The zeroing happens in the walk BEFORE any codec is consulted, so")
	fmt.Println("     nothing recovers strictness for a plain int. Recovering it would")
	fmt.Println("     need the walk to hand Null to the chain, which is #12's call.")
	_ = plane
}

func structField(v NRecover, f string) any {
	if f == "strict" {
		return v.Strict
	}
	return v.Lenient
}

// ---------------------------------------------------------------------------
// N3  What a hand-written YAML actually contains, and what each spelling does.
// ---------------------------------------------------------------------------

func n3() {
	dhdr("N3  the four ways a human writes 'no value here', through the real driver")
	ctx := context.Background()
	s := mustSchema(reflect.TypeFor[NConf]())
	dir, _ := os.MkdirTemp("", "ferryN3")
	defer os.RemoveAll(dir)

	docs := []struct{ label, body string }{
		{"key deleted     ", "host: h\n"},
		{"key blank       ", "host: h\nport:\n"},
		{"explicit null   ", "host: h\nport: null\n"},
		{"commented out   ", "host: h\n# port: 9090\n"},
		{"empty string    ", "host: h\nport: \"\"\n"},
	}
	fmt.Printf("  %-17s %-14s %-24s %-14s %s\n", "document", "reports", "refuse", "null-means-zero", "null-means-absent")
	for _, d := range docs {
		f := filepath.Join(dir, "c.yaml")
		_ = os.WriteFile(f, []byte(d.body), 0o644)
		vals, err := yamlVals(ctx, f, s.addrs)
		if err != nil {
			fmt.Printf("  %-17s read error: %v\n", d.label, err)
			continue
		}
		row := fmt.Sprintf("  %-17s %-14s ", d.label, vals[addr("port")].GoString())
		for i, o := range []loadOpts{{}, {nullMeansZero: true}, {nullMeansAbsent: true}} {
			var v NConf
			_, e := loadD(vals, s, reflect.ValueOf(&v).Elem(), o)
			out := fmt.Sprintf("Port=%d", v.Port)
			if e != nil {
				out = "REFUSED"
			}
			w := 24
			if i > 0 {
				w = 14
			}
			row += fmt.Sprintf("%-*s ", w, out)
		}
		fmt.Println(row)
	}
	fmt.Println("  The field declares default=8080, so 'Port=8080' is the default applying")
	fmt.Println("  and 'Port=0' is a value ferry chose. Four of five spellings mean the")
	fmt.Println("  same thing to a human and three of them are one observation to ferry.")
}
