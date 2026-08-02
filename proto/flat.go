package main

// The third plane the harness needs, and the one whose absence hid G2.
//
// ADR-0004: "a typed boundary buys YAML and TOML something real, JSON
// something partial, and Consul, environment variables and query parameters
// nothing at all - and two of the four first-party drivers are in the last
// group." A plane in that group can only ever report String. Every proof so
// far fed dump's own output back into load, so the kinds always matched and
// no probe ever exercised the majority case.
//
// #41 item 7. What was here was a `map[Path]Value -> map[Path]Value` transform
// called `flatten`, which cannot be handed to Load[T] and therefore could not
// be the harness's third column once the harness ran through the engine. What
// is here now is a real Source/Sink pair over ADR-0004's interfaces.
//
// And the transform was doing something the pair is not allowed to do. ADR-0005:
//
//	A driver declares the `Value` kinds its plane can carry. The conformance
//	suite runs the proofs that plane can express, and asserts that the ones it
//	cannot are refused loudly rather than silently mangled.
//
// `flatten` mapped Null onto String(""), with a comment saying "a flat plane has
// no null ... so the container marker cannot survive, and this is where that
// costs". That IS the silent mangling the rule forbids, and it is why the
// published flat-plane numbers move once the declaration is real. See the #41
// report and x2_harness.go.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

type durAlias = time.Duration

// --- the declaration ---------------------------------------------------------

// unsupportedKind is what a driver returns when the plane cannot carry the
// `Value` kind offered. It declares ErrPlane, which is ADR-0011's extension
// rule: "core supplies the default class for that moment unless the driver's
// error already carries a ferry class sentinel, in which case core keeps it".
// Here core would have said ErrPlane anyway, and saying it explicitly is what
// makes the declaration checkable from the error rather than from a string.
type unsupportedKind struct{ kind VKind }

func (e unsupportedKind) Error() string {
	return "this plane cannot carry " + e.kind.String()
}
func (e unsupportedKind) Is(t error) bool { return t == ErrPlane }

// refusedKind reports the kind a driver refused, if that is what happened.
// This is how the conformance suite tells "refused loudly" from "went wrong".
func refusedKind(err error) (VKind, bool) {
	var u unsupportedKind
	if errors.As(err, &u) {
		return u.kind, true
	}
	return VAbsent, false
}

// --- the flat store ----------------------------------------------------------

// FlatStore is the plane: text at addresses, and nothing else. It holds
// map[Path]Value rather than map[Path]string only so that mapReader can be
// reused as its Reader; every Value in it is a String by construction, which
// is the invariant flatSink.Set maintains and flatKinds declares.
type FlatStore struct {
	mu sync.Mutex
	m  map[Path]Value
}

func NewFlatStore() *FlatStore { return &FlatStore{m: map[Path]Value{}} }

func (s *FlatStore) put(p Path, v Value) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[p] = v
}

func (s *FlatStore) snapshot() map[Path]Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[Path]Value, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

// flatKinds is the declaration. Everything except Null: ADR-0004's own table
// says YAML and JSON can produce Null and that TOML, env, query params and
// opaque KV cannot, and ADR-0005 names env, query and Consul as three of the
// four first-party drivers.
var flatKinds = []VKind{VAbsent, VBool, VNumber, VString, VBytes}

// --- sink --------------------------------------------------------------------

type FlatSink struct{ Store *FlatStore }

func (s FlatSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return flatWriter{s.Store}, nil }, nil
}

type flatWriter struct{ st *FlatStore }

func (w flatWriter) Set(_ context.Context, p Path, v Value) error {
	switch v.Kind() {
	case VAbsent:
		return nil // Absent does not write, which is ADR-0006's rule
	case VNull:
		// LOUDLY. The old transform wrote String("") here, and the cost was
		// that a nil pointer came back as a zero value with a nil error, which
		// is the outcome ADR-0005 says the declaration exists to prevent.
		return unsupportedKind{VNull}
	}
	// Everything else flattens to its source text, which is the whole of what
	// a flat plane is: it has nowhere to put the kind.
	w.st.put(p, String(v.Text()))
	return nil
}

// --- source ------------------------------------------------------------------

type FlatSource struct{ Store *FlatStore }

func (s FlatSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) {
		// A snapshot, so a reader opened during a walk cannot see a later
		// write. ADR-0004 puts the read consistency question at the open.
		return mapReader{s.Store.snapshot()}, nil
	}, nil
}

// --- the probes this file already owned --------------------------------------

func runFlat() {
	defer runCast()
	fmt.Println("\n--- the core set through a FLATTENING plane (env/query/kv shaped) ---")
	fmt.Println("    ADR-0005: \"A flattening plane, which reports String for everything and")
	fmt.Println("    has no null. This is the plane whose absence hid the donor rule for two")
	fmt.Println("    drafts, and it is not an exotic case: it is env, query and Consul.\"")
	pl := flatPlane()
	for _, set := range []struct {
		name   string
		proofs []Proof
	}{{"core", coreSet()}, {"composites", auditSet()}} {
		r := RoundTrip(pl, set.proofs...)
		for _, l := range r.Lines {
			fmt.Println(l)
		}
		fmt.Printf("  %s: %s\n", set.name, r.summary())
	}
	fmt.Println("    ADR-0005 published 11/11 core and 8/10 composites here, with the two")
	fmt.Println("    failures named as *int and *Cred at nil. Those numbers were taken")
	fmt.Println("    against a map transform that mapped Null onto String(\"\"), which is the")
	fmt.Println("    silent mangling the same ADR's declaration rule exists to prevent. See")
	fmt.Println("    the #41 report: this is an ADR-0005 EVIDENCE problem, not a fix.")

	fmt.Println("\n--- and the coercion that must NOT happen ---")
	type S struct{ V string }
	var s S
	e := load(map[Path]Value{Path{}.Name("V"): Number("8080")}, reflect.ValueOf(&s).Elem())
	fmt.Printf("  Number(\"8080\") into a Go string field -> %q err=%v\n", s.V, e)
	var b struct{ V bool }
	e2 := load(map[Path]Value{Path{}.Name("V"): String("yes")}, reflect.ValueOf(&b).Elem())
	fmt.Printf("  String(\"yes\")  into a Go bool   field -> %v err=%v\n", b.V, e2)
	var i struct{ V int }
	e3 := load(map[Path]Value{Path{}.Name("V"): String("010")}, reflect.ValueOf(&i).Elem())
	fmt.Printf("  String(\"010\")  into a Go int    field -> %v err=%v  (cast gives 8)\n", i.V, e3)
}

// The survey names cast's zero-padded-port defect by number. Check ferry's
// answer against it directly.
func runCast() {
	fmt.Println("\n--- against the defects the survey measured in spf13/cast ---")
	for _, c := range []struct{ in, castSays string }{
		{"0080", "0   (invalid octal, error swallowed)"},
		{"010", "8   (base 0: octal)"},
		{"0x10", "16  (base 0: hex)"},
		{"1.9", "1   (truncated)"},
		{"", "0   (indistinguishable from a real 0)"},
	} {
		var i struct{ V int }
		e := load(map[Path]Value{Path{}.Name("V"): String(c.in)}, reflect.ValueOf(&i).Elem())
		got := fmt.Sprintf("%d", i.V)
		if e != nil {
			got = "refused"
		}
		fmt.Printf("  %-6q ferry=%-9s cast=%s\n", c.in, got, c.castSays)
	}
	var d struct{ D int64 }
	_ = d
	fmt.Println("  cast also turns \"30\" into 30ns for a Duration; ferry's Duration")
	fmt.Println("  parses with time.ParseDuration, so \"30\" is refused and \"30s\" is 30s.")
	var dd struct{ D durAlias }
	e := load(map[Path]Value{Path{}.Name("D"): String("30")}, reflect.ValueOf(&dd).Elem())
	fmt.Printf("  String(\"30\") into time.Duration -> err=%v\n", e)
}
