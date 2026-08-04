package ferrytest_test

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// ExampleConfig is the annotated struct both examples below work over.
type ExampleConfig struct {
	Port    int    `ferry:"port"`
	Timeout string `ferry:"timeout"`
}

// ExampleStatic fills a config struct from a literal rather than from a file,
// which is what a test that is not about ferry at all wants.
func ExampleStatic() {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("port"):    ferry.Number("8080"),
		ferry.At("timeout"): ferry.String("30s"),
	})

	cfg, err := ferry.Load[ExampleConfig](context.Background(), src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Port:8080 Timeout:30s}
}

// ExampleRecord answers what a struct maps to, with no plane touched at all.
//
// The addresses are sorted here only so the example has one output; Record
// returns a map.
func ExampleRecord() {
	mapped, err := ferrytest.Record(context.Background(), ExampleConfig{Port: 8080, Timeout: "30s"})
	if err != nil {
		fmt.Println(err)

		return
	}

	for _, addr := range slices.SortedFunc(maps.Keys(mapped), ferry.Path.Compare) {
		fmt.Printf("%s -> %#v\n", addr, mapped[addr])
	}
	// Output:
	// /port -> number("8080")
	// /timeout -> string("30s")
}
