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

// ExampleRequired is the struct the two error examples load, and the plane they
// load from holds nothing at the address it marks required.
type ExampleRequired struct {
	Port    int    `ferry:"port,required"`
	Timeout string `ferry:"timeout"`
}

// exampleLoad is the failing load both error examples assert over.
func exampleLoad() error {
	_, err := ferry.Load[ExampleRequired](context.Background(), ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("timeout"): ferry.String("30s"),
	}))

	return err
}

// ExampleDiffErrors asserts over the exact set of failures a load reported,
// which is what ferry offers in place of matching on message text.
//
// The class here is deliberately the wrong one, so that the report shows both
// halves: the failure that arrived and the expectation that did not match it.
func ExampleDiffErrors() {
	for _, s := range ferrytest.DiffErrors(exampleLoad(),
		ferrytest.Want{Address: ferry.At("port"), Class: ferry.ErrValue},
	) {
		fmt.Println(s)
	}
	// Output:
	// got /port: missing, and nothing wanted it: ferry: /port: required, and the plane holds nothing at this address
	// want /port: invalid value, and nothing reported it
}

// ExampleCheckErrors is the same check reported to a test, which is what a test
// writes. Pass the *testing.T; the stand-in below is only so that the example
// can print what the check said.
func ExampleCheckErrors() {
	var t exampleT

	ferrytest.CheckErrors(&t, exampleLoad(),
		ferrytest.Want{Address: ferry.At("port"), Class: ferry.ErrMissing},
	)

	fmt.Println(len(t), "failures reported")
	// Output: 0 failures reported
}

// exampleT stands in for the *testing.T a real test passes, and it is two
// methods because that is all [ferrytest.T] is.
type exampleT []string

func (t *exampleT) Errorf(format string, args ...any) { *t = append(*t, fmt.Sprintf(format, args...)) }

func (*exampleT) Helper() {}
