package main

// E5: Validate[T](), and the thing nobody has looked at - what it does to
// ADR-0009's freeze.
//
// ADR-0008 named Validate[T]() as a public entry point and required that it
// and Load share ONE compiler, or it validates a schema no Load will compile.
// It also left one question open by name: "whether they also see the same
// codec registry is #19's, and it is the other thing that could make them
// disagree."
//
// ADR-0009 answered the registry half and created a question it did not know
// it was creating: a registry freezes at its FIRST USE, which is the first
// schema compiled against it. Validate compiles a schema. So Validate freezes,
// and ADR-0009's soundness argument runs through where "first use" falls.

import (
	"fmt"
	"reflect"
)

// E5Opaque is a struct with no exported field and no text pair, so ADR-0005's
// maps-no-address backstop refuses it and only a registration lifts it. It is
// declared here rather than borrowed (netip.Addr is registered by one of #19's
// package init()s, which is exactly the fixture contamination this probe is
// about).
type E5Opaque struct{ v string }

func (o E5Opaque) text() string { return o.v }

type E5Conf struct {
	Listen E5Opaque `ferry:"listen"`
	Name   string   `ferry:"name"`
}

func e5Codec() Reg {
	return StringCodec(
		E5Opaque.text,
		func(s string) (E5Opaque, error) { return E5Opaque{s}, nil })
}

func runE5() {
	fmt.Println("--- E5a: Validate and Load are the same call, so they cannot disagree ---")
	fmt.Println("  Validate[T](opts...) IS schemaFor(reflect.TypeFor[T](), opts), with the")
	fmt.Println("  schema discarded. Not a second compiler with the same rules - the same")
	fmt.Println("  function. Two entry points that could disagree about whether a type is")
	fmt.Println("  legal would be viper's two-engines defect at ferry's own front door.")

	fmt.Println("\n--- E5b: it takes the same Options, and ADR-0008 already forced that ---")
	type E5Lib struct {
		Host string `mylib:"host"`
	}
	fmt.Printf("  Validate[E5Lib]()                  -> %v\n", trunc(Validate[E5Lib]()))
	fmt.Printf("  Validate[E5Lib](TagKey(\"mylib\"))   -> %v\n", Validate[E5Lib](TagKey("mylib")))

	fmt.Println("\n  And the half ADR-0008 left to #19 and #16 jointly: the registry.")
	fmt.Println("  A type whose compilability depends on a registration, which ADR-0007")
	fmt.Println("  makes possible by having a codec collapse a type to a leaf:")
	reg := NewRegistry()
	mustReg(reg, e5Codec())
	fmt.Printf("  Validate[E5Conf]()                     -> %v\n", trunc(Validate[E5Conf]()))
	fmt.Printf("  Validate[E5Conf](WithRegistry(reg))    -> %v\n", Validate[E5Conf](WithRegistry(reg)))
	fmt.Println("  So a Validate that could not see the registry Option would answer a")
	fmt.Println("  question about a codec table no Load will ever use. It takes every")
	fmt.Println("  compile-affecting Option, and it takes them because there are exactly")
	fmt.Println("  two and both change what compiles.")

	fmt.Println("\n--- E5c: Validate is a USE, so it freezes the registry ---")
	fresh := NewRegistry()
	fmt.Printf("  a new registry, frozen=%v\n", fresh.frozen.Load())
	_ = Validate[E5Conf](WithRegistry(fresh))
	fmt.Printf("  after Validate[E5Conf](WithRegistry(fresh)), frozen=%v\n", fresh.frozen.Load())
	err := fresh.Register(e5Codec())
	fmt.Printf("  a registration after that -> %v\n", err)

	fmt.Println("\n  This is not a choice #16 gets to make freely. The alternative - Validate")
	fmt.Println("  compiles without caching and without marking a use - is measured:")
	fresh2 := NewRegistry()
	o := defaultOpts()
	o.reg = fresh2
	done := fresh2.install()
	_, e1 := compileSchema2(reflect.TypeFor[E5Conf](), o)
	done()
	fmt.Printf("    Validate before the registration -> %v\n", trunc(e1))
	mustReg(fresh2, e5Codec())
	done = fresh2.install()
	_, e2 := compileSchema2(reflect.TypeFor[E5Conf](), o)
	done()
	fmt.Printf("    the Load that follows            -> %v\n", e2)
	fmt.Println("  A Validate that does not freeze reports a failure that never happens,")
	fmt.Println("  about a registry state no Load ever sees. Both readings are loud, and")
	fmt.Println("  only one of them keeps ADR-0008's \"one compiler\" true.")

	fmt.Println("\n--- E5d: so where may Validate be called, and where may it not ---")
	fmt.Println("  ADR-0009 priced exactly one broken shape: a LOAD during init(), where")
	fmt.Println("  whether a later package's init may still register depends on the import")
	fmt.Println("  graph. Validate is a second way into that shape, and a more likely one,")
	fmt.Println("  because it is pitched as the thing you call to check a schema early.")
	fmt.Println()
	fmt.Println("    func init()     { ferry.Register(...) }        every init completes")
	fmt.Println("    func TestX(t)   { ferry.Validate[Config]() }   before main/tests run,")
	fmt.Println("                                                   so this is always safe")
	fmt.Println()
	fmt.Println("    var _ = must(ferry.Validate[Config]())         a package-level var, so")
	fmt.Println("                                                   it runs DURING init and")
	fmt.Println("                                                   freezes the default")
	fmt.Println("                                                   registry mid-graph")
	fmt.Println()
	fmt.Println("  Go's own initialisation order is what makes the first safe: every")
	fmt.Println("  package-level variable and every init in the program runs to completion")
	fmt.Println("  before main.main, and a test function is not an init. So Validate from a")
	fmt.Println("  test is always after every Register, which is where ADR-0008 pitched it.")
	fmt.Println("  Validate from a package-level var is ADR-0009's broken shape with a")
	fmt.Println("  different verb on it, and #16 says so rather than leaving it to be found.")
}

func trunc(err error) string {
	if err == nil {
		return "<nil>"
	}
	s := err.Error()
	if i := indexByte(s, '\n'); i >= 0 {
		s = s[:i] + " ..."
	}
	if len(s) > 96 {
		s = s[:96] + "..."
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := range len(s) {
		if s[i] == b {
			return i
		}
	}
	return -1
}
