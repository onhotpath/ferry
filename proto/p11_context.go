package main

// P11: may a codec take a context.Context?
//
// 5.9 lists "Decode(string) error takes no context even though the whole walk
// is context-carrying" as a defect. Before adopting the implied fix, measure
// what a context in the signature would actually do.

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type ctxCodec struct {
	enc func(context.Context, reflect.Value) (Value, error)
	dec func(context.Context, Value, reflect.Value) error
}

func runContext() {
	fmt.Println("\n--- P11a: not one recognised interface takes a context ---")
	fmt.Println("    TextMarshaler.MarshalText() ([]byte, error)")
	fmt.Println("    TextUnmarshaler.UnmarshalText([]byte) error")
	fmt.Println("    json.Marshaler.MarshalJSON() ([]byte, error)")
	fmt.Println("    xload.Decoder.Decode(string) error")
	fmt.Println("    So a context-carrying ferry interface would be the only one, and")
	fmt.Println("    every adapter would discard the context it was handed.")

	fmt.Println("\n--- P11b: the consequence, which is worse than the asymmetry ---")
	fmt.Println("    If ferry's own codec took a ctx and the recognised arms did not,")
	fmt.Println("    then whether a type's conversion is cancellable would depend on")
	fmt.Println("    which arm claimed it - which is precedence deciding a lifecycle")
	fmt.Println("    property. Measured on one type that carries two arms:")
	for _, ord := range [][]string{{"text"}, {"json"}} {
		c, _, _ := selectPaired(reflect.TypeFor[jsonAndText](), ord)
		fmt.Printf("      order %-8v -> arm %-6s, whose method takes no context\n", ord, c.arm)
	}
	fmt.Println("      Put a ferry-own context-carrying arm first and the same type")
	fmt.Println("      becomes cancellable; move it last and it stops being. A")
	fmt.Println("      lifecycle property decided by list order is not a property.")

	fmt.Println("\n--- P11c: cost is not the argument, so state it and move on ---")
	ctx := context.Background()
	plain := func(v reflect.Value) (Value, error) { return String(v.String()), nil }
	withCtx := func(_ context.Context, v reflect.Value) (Value, error) { return String(v.String()), nil }
	rv := reflect.ValueOf("hello")
	r1 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			sink, _ = plain(rv)
		}
	})
	r2 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			sink, _ = withCtx(ctx, rv)
		}
	})
	fmt.Printf("    no ctx   %6.2f ns/op %3d allocs/op\n", float64(r1.NsPerOp()), r1.AllocsPerOp())
	fmt.Printf("    with ctx %6.2f ns/op %3d allocs/op\n", float64(r2.NsPerOp()), r2.AllocsPerOp())

	fmt.Println("\n--- P11d: what the context would be FOR, and where that belongs ---")
	fmt.Println("    The honest use case is a codec that does I/O: decrypting through a")
	fmt.Println("    KMS, resolving a reference. ADR-0004 already has the seam for that,")
	fmt.Println("    and it is a Source: Get is context-carrying, and a decrypting source")
	fmt.Println("    wraps another source in about ten lines. Putting the I/O in the")
	fmt.Println("    codec instead breaks three things ADR-0005 already relies on:")
	fmt.Println("      - a codec collapses a type to a LEAF, and a leaf is a pure")
	fmt.Println("        conversion the harness can run with no plane in sight")
	fmt.Println("      - the round-trip proof is a table run offline; an I/O codec has")
	fmt.Println("        no proof anybody can write")
	fmt.Println("      - #20 inherits a walk whose leaves may block indefinitely")
	fmt.Println()
	fmt.Println("    ADR-0004 set the precedent for using an ABSENT context as a")
	fmt.Println("    statement: Bind takes none, which is how the type says no I/O")
	fmt.Println("    happens there, and it made the rule assertable in the conformance")
	fmt.Println("    suite. The same instrument applies here.")

	fmt.Println("\n--- P11e: cancellation without a context in the codec ---")
	fmt.Println("    Core checks the context between leaves, so a walk is cancellable at")
	fmt.Println("    leaf granularity and a codec never has to care. Measured on a")
	fmt.Println("    2000-leaf dump cancelled after 1ms:")
	demoCancel()
}

func demoCancel() {
	type big struct{ S [2000]string }
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	var v big
	for i := range v.S {
		v.S[i] = "x"
	}
	go func() { time.Sleep(time.Millisecond); cancel() }()
	// stand-in for the walk: one ctx check per leaf
	for range v.S {
		if ctx.Err() != nil {
			break
		}
		n++
		time.Sleep(time.Microsecond)
	}
	fmt.Printf("      stopped after %d of 2000 leaves, err=%v\n", n, ctx.Err())
}
