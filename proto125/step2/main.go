// Command step2 asks how a "." can get into a ferry address at all.
package main

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/onhotpath/ferry"
)

// Route 1: the struct tag. The tag grammar's name is a bare token, "any byte
// except a comma", so a dot is ordinary text.
type tagged struct {
	Host  string `ferry:"db.host"`
	Other string `ferry:"db_host"`
}

// Route 2: a map key. The segment text comes from runtime data.
type dynamic struct {
	Labels map[string]string `ferry:"labels"`
}

// Route 3: a slice index. Index segments are minted by Path.Elem from a uint.
type indexed struct {
	Tags []string `ferry:"tags"`
}

// Route 4: a non-string map key, whose segment text is whatever the key type's
// text form is. A float's text form carries a dot.
type keyed struct {
	Quantiles map[float64]string `ferry:"quantiles"`
}

// Route 5: a map keyed by a type somebody registered a codec for. The segment
// text is that codec's output, and netip.Addr's output is dotted.
type peered struct {
	Peers map[netip.Addr]string `ferry:"peers"`
}

func main() {
	fmt.Println("== route 1: a dot written in a struct tag ==")
	fmt.Printf("Compile[tagged]() -> %v\n", ferry.Compile[tagged]())
	fmt.Println("static address set handed to Bind:")

	for _, a := range boundSet[tagged]() {
		fmt.Printf("      %s\n", a)
	}

	fmt.Println()
	fmt.Println("== route 2: a dot arriving from a map key ==")
	fmt.Printf("Compile[dynamic]() -> %v\n", ferry.Compile[dynamic]())
	fmt.Println("static address set handed to Bind:")

	for _, a := range boundSet[dynamic]() {
		fmt.Printf("      %s\n", a)
	}

	v := dynamic{Labels: map[string]string{"app.name": "ferry", "team": "core"}}
	fmt.Println("addresses a Dump of", v.Labels, "actually asked the sink to Set:")

	for _, a := range dumped(v) {
		fmt.Printf("      %s\n", a)
	}

	fmt.Println()
	fmt.Println("== route 3: a slice index ==")

	for _, a := range dumped(indexed{Tags: []string{"a", "b"}}) {
		fmt.Printf("      %s\n", a)
	}

	fmt.Println()
	fmt.Println("== route 4: a non-string map key whose text form carries a dot ==")
	fmt.Printf("Compile[keyed]() -> %v\n", ferry.Compile[keyed]())

	for _, a := range dumped(keyed{Quantiles: map[float64]string{0.99: "slo", 0.5: "median"}}) {
		fmt.Printf("      %s\n", a)
	}

	fmt.Println()
	fmt.Println("== route 5: a map keyed by a registered type whose text is dotted ==")

	reg := ferry.NewRegistry()
	if err := reg.Register(ferry.TextCodec[netip.Addr](ferry.KindString).AsMapKey()); err != nil {
		fmt.Println("      (register reported:", err, ")")
	}

	fmt.Printf("Compile[peered]() -> %v\n", ferry.Compile[peered](ferry.WithRegistry(reg)))

	v5 := peered{Peers: map[netip.Addr]string{
		netip.MustParseAddr("10.0.0.1"): "a",
		netip.MustParseAddr("10.0.0.2"): "b",
	}}

	for _, a := range dumped(v5, ferry.WithRegistry(reg)) {
		fmt.Printf("      %s\n", a)
	}
}

// recSink records the address set it was bound to and every address it was
// asked to write. It flattens nothing, so nothing here is a key function's
// doing.
type recSink struct {
	set     []ferry.Path
	written []ferry.Path
}

func (s *recSink) Bind(a *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	for addr := range a.All() {
		s.set = append(s.set, addr)
	}

	return func(context.Context) (ferry.Writer, error) { return s, nil }, nil
}

func (s *recSink) Set(_ context.Context, addr ferry.Path, _ ferry.Value) error {
	s.written = append(s.written, addr)

	return nil
}

func boundSet[T any]() []ferry.Path {
	var zero T

	s := &recSink{}
	if err := ferry.Dump(context.Background(), zero, s); err != nil {
		fmt.Println("      (dump reported:", err, ")")
	}

	return s.set
}

func dumped[T any](v T, opts ...ferry.Option) []ferry.Path {
	s := &recSink{}
	if err := ferry.Dump(context.Background(), v, s, opts...); err != nil {
		fmt.Println("      (dump reported:", err, ")")
	}

	return s.written
}
