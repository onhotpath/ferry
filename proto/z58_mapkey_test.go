package main

// #58: a registered key codec's text must be what Dump writes.
//
// These run through the CALLER-FACING entry points - Dump and LoadOver - and
// not through dumpTo/loadFrom. That distinction is the whole finding: the probe
// helpers install the registry around their walk, so a measurement taken
// through them is honest about the codec and silent about the defect. Every
// assertion below would have passed on 0d86c00 if it had been written against
// dumpTo.
//
// What each case is for:
//
//	1  a registered key codec's text IS the address segment, and the map
//	   round-trips (the fix)
//	2  a key type WITH String() whose text differs from its codec - ADR-0005's
//	   "fmt.Stringer is never consulted, in either direction"
//	3  a key type WITHOUT String() - the same door, the %v struct dump
//	4  a registered LEAF codec, unchanged - the control that keeps the
//	   diagnosis honest, since ADR-0009 already resolves that one into the node
//	5  core key types, unaffected, because byIdentity needs no install
//	6  ferr_mapkeys.go's collapse check FIRES through Dump - the second-order
//	   effect, and the reason this is more than a wrong string

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// z58Host is the key type. Its codec's text drops the Port, which is what makes
// case 6 possible: two distinct Go keys, one address.
type z58Host struct {
	Name string
	Port int
}

// String is deliberately NOT the codec's text. If it ever appears in an
// address, fmt.Stringer was consulted.
func (h z58Host) String() string { return h.Name + ":" + fmt.Sprint(h.Port) }

// z58Plain is z58Host with no String() method, so %v renders it as the struct
// dump `{api 80}`. Same defect, the other door.
type z58Plain struct {
	Name string
	Port int
}

// z58Leaf is the control: a registered codec at a LEAF position, which
// ADR-0009 already resolves into the compiled node.
type z58Leaf struct{ S string }

func z58Reg(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	err := r.Register(
		StringCodec(
			func(h z58Host) string { return h.Name },
			func(s string) (z58Host, error) { return z58Host{Name: s}, nil },
		).AsMapKey(),
		StringCodec(
			func(p z58Plain) string { return p.Name },
			func(s string) (z58Plain, error) { return z58Plain{Name: s}, nil },
		).AsMapKey(),
		StringCodec(
			func(l z58Leaf) string { return "CODEC:" + l.S },
			func(s string) (z58Leaf, error) { return z58Leaf{S: strings.TrimPrefix(s, "CODEC:")}, nil },
		),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return r
}

// z58Dump runs the entry point and returns the addresses the sink was actually
// told to write.
func z58Dump[T any](t *testing.T, v T, reg *Registry) (map[Path]Value, error) {
	t.Helper()
	rec := newRecorder()
	b, err := BindSink[T](rec, WithRegistry(reg))
	if err != nil {
		return nil, err
	}
	return rec.vals, b.Dump(context.Background(), v)
}

// z58Load is the other entry point, over the addresses a Dump produced.
func z58Load[T any](t *testing.T, vals map[Path]Value, reg *Registry) (T, error) {
	t.Helper()
	var zero T
	b, err := Bind[T](tFixedSource{vals: vals}, WithRegistry(reg))
	if err != nil {
		return zero, err
	}
	return b.Load(context.Background())
}

func z58Addrs(vals map[Path]Value) []string {
	out := make([]string, 0, len(vals))
	for _, p := range sortedAddrs(vals) {
		out = append(out, p.String()+"="+vals[p].GoString())
	}
	return out
}

// --- 1: the registered key codec's text is what Dump writes -----------------

type z58HostMap struct {
	M map[z58Host]int `ferry:"m"`
}

func TestDumpUsesRegisteredKeyCodec(t *testing.T) {
	reg := z58Reg(t)
	in := z58HostMap{M: map[z58Host]int{{"api", 80}: 1}}

	vals, err := z58Dump(t, in, reg)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got := z58Addrs(vals)
	want := []string{`/m/api=number("1")`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dump wrote %v, want %v", got, want)
	}

	// And the round trip closes: the codec's parse half reads its own text
	// back. On 0d86c00 this failed with "is not a valid main.z58Host map key",
	// which is a dump that succeeds and never loads back.
	back, err := z58Load[z58HostMap](t, vals, reg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := (map[z58Host]int{{Name: "api"}: 1}); !reflect.DeepEqual(back.M, want) {
		t.Errorf("round trip gave %v, want %v", back.M, want)
	}
}

// --- 2: fmt.Stringer is never consulted -------------------------------------

func TestDumpDoesNotConsultStringer(t *testing.T) {
	reg := z58Reg(t)
	vals, err := z58Dump(t, z58HostMap{M: map[z58Host]int{{"api", 80}: 1}}, reg)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	for p := range vals {
		if strings.Contains(p.String(), "api:80") {
			t.Errorf("address %s carries z58Host.String(); ADR-0005: "+
				"\"fmt.Stringer is never consulted, in either direction\"", p)
		}
	}
}

// --- 3: the %v struct dump, for a key type with no String() -----------------

type z58PlainMap struct {
	M map[z58Plain]int `ferry:"m"`
}

func TestDumpDoesNotStructDumpKey(t *testing.T) {
	reg := z58Reg(t)
	vals, err := z58Dump(t, z58PlainMap{M: map[z58Plain]int{{"api", 80}: 1}}, reg)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got := z58Addrs(vals)
	want := []string{`/m/api=number("1")`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dump wrote %v, want %v", got, want)
	}
}

// --- 4: the leaf control -----------------------------------------------------

type z58LeafHolder struct {
	L z58Leaf `ferry:"l"`
}

func TestDumpLeafCodecUnchanged(t *testing.T) {
	reg := z58Reg(t)
	vals, err := z58Dump(t, z58LeafHolder{L: z58Leaf{S: "api"}}, reg)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got := z58Addrs(vals)
	want := []string{`/l=string("CODEC:api")`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dump wrote %v, want %v", got, want)
	}
}

// --- 5: core key types, unaffected ------------------------------------------

type z58CoreMaps struct {
	S map[string]int `ferry:"s"`
	I map[int]string `ferry:"i"`
}

func TestDumpCoreKeyTypesUnchanged(t *testing.T) {
	reg := z58Reg(t)
	in := z58CoreMaps{
		S: map[string]int{"a": 1, "b": 2},
		I: map[int]string{7: "x", -3: "y"},
	}
	vals, err := z58Dump(t, in, reg)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	got := z58Addrs(vals)
	want := []string{
		`/i/-3=string("y")`,
		`/i/7=string("x")`,
		`/s/a=number("1")`,
		`/s/b=number("2")`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dump wrote %v, want %v", got, want)
	}
	back, err := z58Load[z58CoreMaps](t, vals, reg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Errorf("round trip gave %+v, want %+v", back, in)
	}
}

// --- 6: the collapse check fires through Dump -------------------------------

func TestDumpCollapseCheckFiresThroughEntryPoint(t *testing.T) {
	reg := z58Reg(t)
	// Two distinct Go keys the codec renders alike. The %v fallback rendered
	// them "api:80" and "api:443", which is accidentally injective where the
	// codec is not, so on 0d86c00 the check never fired and Dump wrote two
	// addresses with no error.
	in := z58HostMap{M: map[z58Host]int{{"api", 80}: 1, {"api", 443}: 2}}

	vals, err := z58Dump(t, in, reg)
	if err == nil {
		t.Fatalf("Dump succeeded and wrote %v; want the collapse refusal", z58Addrs(vals))
	}
	if !strings.Contains(err.Error(), "would be lost") {
		t.Errorf("Dump failed with %v; want ferr_mapkeys.go's collapse diagnostic", err)
	}
	if len(vals) != 0 {
		t.Errorf("the sink was written to despite the refusal: %v", z58Addrs(vals))
	}
}
