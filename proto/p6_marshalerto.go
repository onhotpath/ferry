package main

// P6: what MarshalerTo / UnmarshalerFrom buy at ferry's boundary.
//
// ADR-0002 bars the import from core, so the easy answer is "the ADR says no".
// That is a bad answer if the interface is worth an amendment, so measure the
// thing the interface exists for instead.
//
// v2's own rationale: MarshalJSON forces an allocation and a re-parse, and
// that is API-bound rather than implementation-bound. MarshalerTo fixes it by
// letting the value write straight into the enclosing stream.
//
// ferry has no enclosing stream. ADR-0004 fixed the boundary value as
// {kind, text}, so every leaf is materialised on its own regardless.

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
)

type bothMarshalers struct{ S string }

func (v bothMarshalers) MarshalJSON() ([]byte, error) { return []byte(`"` + v.S + `"`), nil }
func (v bothMarshalers) MarshalJSONTo(e *jsontext.Encoder) error {
	return e.WriteToken(jsontext.String(v.S))
}
func (v *bothMarshalers) UnmarshalJSON(b []byte) error { v.S = string(b); return nil }
func (v *bothMarshalers) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	t, err := d.ReadToken()
	if err != nil {
		return err
	}
	v.S = t.String()
	return nil
}

func runMarshalerTo() {
	v := bothMarshalers{"192.0.2.1"}

	fmt.Println("\n--- P6a: MarshalerTo wins inside v2, confirmed on go1.27rc2 ---")
	b, err := jsonv2.Marshal(v)
	fmt.Printf("    jsonv2.Marshal(type implementing both) -> %s err=%v\n", b, err)

	fmt.Println("\n--- P6b: both routes, each terminating in a ferry Value ---")
	fmt.Println("    ferry's boundary is {kind, text}, so the bytes must exist as a Go")
	fmt.Println("    string either way. Count what each route costs to get there.")

	viaMarshaler := func() Value {
		b, _ := v.MarshalJSON()
		return String(string(b))
	}
	// Give MarshalerTo its best case: one encoder and one buffer, reused,
	// which is what a streaming caller does. A fresh encoder per leaf costs
	// 400 B and 6 allocs and would be an unfair comparison.
	var buf []byte
	bw := &byteWriter{&buf}
	enc := jsontext.NewEncoder(bw)
	viaMarshalerTo := func() Value {
		buf = buf[:0]
		enc.Reset(bw)
		_ = v.MarshalJSONTo(enc)
		return String(string(buf))
	}
	fmt.Printf("    Marshaler   -> %s\n", viaMarshaler().GoString())
	fmt.Printf("    MarshalerTo -> %s\n", viaMarshalerTo().GoString())
	fmt.Println("    (the trailing newline is the encoder's, and is itself a sign that")
	fmt.Println("     the interface is written for a document rather than for a value)")

	r1 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			sink = viaMarshaler()
		}
	})
	r2 := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			sink = viaMarshalerTo()
		}
	})
	fmt.Printf("\n    %-14s %8s ns/op %6s B/op %4s allocs/op\n", "route", "", "", "")
	fmt.Printf("    %-14s %8.1f      %6d      %4d\n", "Marshaler",
		float64(r1.NsPerOp()), r1.AllocedBytesPerOp(), r1.AllocsPerOp())
	fmt.Printf("    %-14s %8.1f      %6d      %4d\n", "MarshalerTo",
		float64(r2.NsPerOp()), r2.AllocedBytesPerOp(), r2.AllocsPerOp())
	fmt.Println("\n    ^ the allocation MarshalerTo exists to remove is the one ferry's")
	fmt.Println("      own boundary reinstates. There is no enclosing stream to write")
	fmt.Println("      into, because ADR-0004 made a Value a standalone {kind, text}.")

	fmt.Println("\n--- P6c: and the output is JSON syntax, which is P5's problem again ---")
	fmt.Printf("    a MarshalerTo for a string-shaped type writes %s, quotes included.\n",
		viaMarshalerTo().GoString())
	fmt.Println("    So recognising it would cost core a jsontext import, a go 1.27")
	fmt.Println("    floor, and a build break under GOEXPERIMENT=nojsonv2, in exchange")
	fmt.Println("    for an allocation ferry cannot avoid and a representation ferry")
	fmt.Println("    does not want.")
}

var sink Value

type byteWriter struct{ b *[]byte }

func (w *byteWriter) Write(p []byte) (int, error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}
