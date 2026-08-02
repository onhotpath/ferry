package main

// P2: absence, four ways.
//
// 5.1 says xload's (string, error) cannot express absence, that its own cached
// provider invented Get() (*string, error) to work around exactly that, and it
// recommends comma-ok. Its trade-off note weighed three spellings.
//
// There is a fourth it did not weigh, and it is the one this prototype ends up
// at: make absence a kind of the value, and keep the ordinary Go (T, error).

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

var errNotFound = errors.New("not found")

type shapePlane map[string]string

// (1) comma-ok, as 5.1 recommends.
func (p shapePlane) commaOK(k string) (Value, bool, error) {
	v, ok := p[k]
	if !ok {
		return Value{}, false, nil
	}
	return String(v), true, nil
}

// (2) pointer, as xload's cached provider actually shipped.
func (p shapePlane) pointer(k string) (*Value, error) {
	v, ok := p[k]
	if !ok {
		return nil, nil
	}
	out := String(v)
	return &out, nil
}

// (3) sentinel error.
func (p shapePlane) sentinel(k string) (Value, error) {
	v, ok := p[k]
	if !ok {
		return Value{}, fmt.Errorf("plane: %q: %w", k, errNotFound)
	}
	return String(v), nil
}

// (4) absence as a kind. Ordinary (T, error).
func (p shapePlane) kinded(k string) (Value, error) {
	v, ok := p[k]
	if !ok {
		return Absent, nil
	}
	return String(v), nil
}

func p2Absence() {
	head("P2  absence: four spellings, and why the tuple is not the answer")

	plane := shapePlane{"SET": "8080", "EMPTY": ""}
	keys := []string{"SET", "EMPTY", "MISSING"}

	// (a) xload's shape, for the baseline. All three states collapse.
	fmt.Println("    (a) xload: Load(ctx, key) (string, error)")
	for _, k := range keys {
		v := plane[k] // exactly what OSLoader and MapLoader do
		fmt.Printf("        %-8s -> %q   required satisfied? %v\n", k, v, v != "")
	}
	fmt.Println("        EMPTY and MISSING are one observation, so FOO= cannot satisfy")
	fmt.Println("        required and a decoder never sees the empty string.")

	// (b) All four candidates, same plane. Every one of them can express the
	//     three states, so correctness does not separate them.
	fmt.Println("\n    (b) all four express three states")
	for _, k := range keys {
		v, ok, _ := plane.commaOK(k)
		p, _ := plane.pointer(k)
		s, serr := plane.sentinel(k)
		kv, _ := plane.kinded(k)
		fmt.Printf("        %-8s comma-ok=(%s,%v) ptr=%-14s sentinel=(%s,%v) kinded=%s\n",
			k, v.GoString(), ok, ptrStr(p), s.GoString(), serr != nil, kv.GoString())
	}

	// (c) What separates them is what a caller can do wrong, silently.
	fmt.Println("\n    (c) the mistake each shape allows")
	fmt.Println("        comma-ok : v, _, err := r.Get(...)")
	fmt.Println("                   compiles, no vet diagnostic, absent now reads as a")
	fmt.Println("                   zero Value. Go's blank identifier makes discarding")
	fmt.Println("                   a second channel a one-character edit.")
	fmt.Println("        pointer  : deref without a nil check panics, inside ferry, on")
	fmt.Println("                   a value a third-party driver produced.")
	fmt.Println("        sentinel : a driver returning a bare errors.New(\"not found\")")
	fmt.Println("                   is indistinguishable from a real failure, and")
	fmt.Println("                   nothing checks that it wrapped correctly.")
	fmt.Println("        kinded   : there is no second channel to discard. The")
	fmt.Println("                   observation is in the value, so it survives being")
	fmt.Println("                   stored, compared, and passed on.")

	// (d) The accessors already refuse it, which is the part that makes the
	//     kinded form safe rather than merely tidy.
	fmt.Println("\n    (d) an absent value cannot be read as anything")
	a, _ := plane.kinded("MISSING")
	_, e1 := a.AsString()
	_, e2 := a.AsInt()
	fmt.Printf("        Absent.AsString() -> %v\n", e1)
	fmt.Printf("        Absent.AsInt()    -> %v\n", e2)
	fmt.Printf("        Absent.Present()  -> %v\n", a.Present())

	// (e) It composes, which the tuple does not.
	fmt.Println("\n    (e) it composes")
	m := map[Path]Value{path("seen-absent"): Absent, path("seen-set"): String("x")}
	miss := m[path("never-looked")]
	fmt.Printf("        recorded absent : %s   map miss : %s   equal? %v\n",
		m[path("seen-absent")].GoString(), miss.GoString(), m[path("seen-absent")] == miss)
	fmt.Println("        A map of observations needs no parallel presence map, because")
	fmt.Println("        the zero Value already means absent. ADR-0001 milestoned plane")
	fmt.Println("        inspection on the grounds that a loaded struct erases absence;")
	fmt.Println("        this is the boundary type not erasing it.")

	// (f) Cost.
	fmt.Println("\n    (f) cost, on the miss path, which is the common one")
	benchShape("comma-ok", func() { _, _, _ = plane.commaOK("MISSING") })
	benchShape("pointer", func() { _, _ = plane.pointer("MISSING") })
	benchShape("sentinel", func() { _, _ = plane.sentinel("MISSING") })
	benchShape("kinded", func() { _, _ = plane.kinded("MISSING") })

	// (g) End to end through a real driver.
	fmt.Println("\n    (g) three states through a real driver")
	src := EnvSource{Lookup: func(k string) (string, bool) {
		switch k {
		case "SET":
			return "8080", true
		case "EMPTY":
			return "", true
		}
		return "", false
	}}
	as := NewAddressSet([]Path{path("set"), path("empty"), path("missing")})
	r, err := bindOpen(context.Background(), src, as)
	if err != nil {
		fmt.Println("        open:", err)
		return
	}
	for _, p := range as.All() {
		v, err := r.Get(context.Background(), p)
		fmt.Printf("        %-10s %-14s present=%-5v err=%v\n", p, v.GoString(), v.Present(), err)
	}
	fmt.Println("        Whether an absent address then takes a default, and whether")
	fmt.Println("        null and absent mean the same thing to a Go field, is #8's.")
}

func ptrStr(p *Value) string {
	if p == nil {
		return "nil"
	}
	return p.GoString()
}

func benchShape(name string, f func()) {
	r := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f()
		}
	})
	fmt.Printf("        %-10s %7.2f ns/op  %3d B/op  %d allocs/op\n",
		name, float64(r.T.Nanoseconds())/float64(r.N),
		r.AllocedBytesPerOp(), r.AllocsPerOp())
}
