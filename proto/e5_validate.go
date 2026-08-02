package main

// E5: Compile[T](), and the thing nobody has looked at - what it does to
// does to ADR-0009's freeze.
//
// ADR-0008 named Compile[T]() as a public entry point and required that it
// and Load share ONE compiler, or it validates a schema no Load will compile.
// It also left one question open by name: "whether they also see the same
// codec registry is #19's, and it is the other thing that could make them
// disagree."
//
// ADR-0009 answered the registry half and created a question it did not know
// it was creating: a registry freezes at its FIRST USE, which is the first
// schema compiled against it. This entry point compiles a schema, so it
// freezes,
// and ADR-0009's soundness argument runs through where "first use" falls.

import (
	"context"
	"fmt"
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

type E5Other struct{ v string }

func runE5() {
	fmt.Println("--- E5a: Compile and Load are the same call, so they cannot disagree ---")
	fmt.Println("  Compile[T](opts...) IS schemaFor(reflect.TypeFor[T](), opts), with the")
	fmt.Println("  schema discarded. Not a second compiler with the same rules - the same")
	fmt.Println("  function. Two entry points that could disagree about whether a type is")
	fmt.Println("  legal would be viper's two-engines defect at ferry's own front door.")

	fmt.Println("\n--- E5b: it takes the same Options, and ADR-0008 already forced that ---")
	type E5Lib struct {
		Host string `mylib:"host"`
	}
	fmt.Printf("  Compile[E5Lib]()                  -> %v\n", trunc(Compile[E5Lib]()))
	fmt.Printf("  Compile[E5Lib](TagKey(\"mylib\"))   -> %v\n", Compile[E5Lib](TagKey("mylib")))

	fmt.Println("\n  And the half ADR-0008 left to #19 and #16 jointly: the registry.")
	fmt.Println("  A type whose compilability depends on a registration, which ADR-0007")
	fmt.Println("  makes possible by having a codec collapse a type to a leaf:")
	reg := NewRegistry()
	mustReg(reg, e5Codec())
	fmt.Printf("  Compile[E5Conf]()                     -> %v\n", trunc(Compile[E5Conf]()))
	fmt.Printf("  Compile[E5Conf](WithRegistry(reg))    -> %v\n", Compile[E5Conf](WithRegistry(reg)))
	fmt.Println("  So a Compile that could not see the registry Option would answer a")
	fmt.Println("  question about a codec table no Load will ever use. It takes every")
	fmt.Println("  compile-affecting Option, and it takes them because there are exactly")
	fmt.Println("  two and both change what compiles.")

	fmt.Println("\n--- E5c: Compile does NOT freeze, and that is E10's result ---")
	fresh := NewRegistry()
	fmt.Printf("  a new registry, frozen=%v\n", fresh.frozen.Load())
	_ = Compile[E5Conf](WithRegistry(fresh))
	fmt.Printf("  after Compile[E5Conf](WithRegistry(fresh)), frozen=%v\n", fresh.frozen.Load())
	err := fresh.Register(e5Codec())
	fmt.Printf("  a registration after that -> %v\n", trunc(err))

	fmt.Println("\n  A Load DOES freeze, because it retains the compiled schema in the cache:")
	fresh2 := NewRegistry()
	mustReg(fresh2, e5Codec())
	_, _ = loadFrom(context.Background(), E5Conf{}, map[Path]Value{}, WithRegistry(fresh2))
	fmt.Printf("  after a Load, frozen=%v\n", fresh2.frozen.Load())
	fmt.Printf("  a registration after that -> %v\n", trunc(fresh2.Register(StringCodec(
		func(t E5Other) string { return t.v },
		func(s string) (E5Other, error) { return E5Other{s}, nil }))))

	fmt.Println("\n  So the rule is about RETENTION rather than about the verb:")
	fmt.Println("    ADR-0009's obligation is that a registry's answer for a type must")
	fmt.Println("    never change once that type has been RESOLVED against it. A compile")
	fmt.Println("    whose result is discarded resolves nothing that outlives the call.")
	fmt.Println("    Caching and freezing are therefore one decision, not two.")

	fmt.Println("\n--- E5d: so where may Compile be called ---")
	fmt.Println("  Anywhere. That is the point of E10's correction: Compile records no use")
	fmt.Println("  of the registry, so a Compile from a package-level var during init() is")
	fmt.Println("  no longer able to poison a later package's Register.")
	fmt.Println()
	fmt.Println("    func init()   { ferry.Register(...) }        every init completes")
	fmt.Println("    func TestX(t) { ferry.Compile[Config]() }    before main/tests run")
	fmt.Println("    var _ = must(ferry.Compile[Config]())        legal, and answers about")
	fmt.Println("                                                 the registry AS IT STANDS")
	fmt.Println()
	fmt.Println("  The residual cost, stated: a Compile that runs during init() answers")
	fmt.Println("  about a registry a later init may still add to, so it can report a")
	fmt.Println("  failure a later registration would have fixed. That is loud, it is at")
	fmt.Println("  the Compile call, and it costs the user a moved line. The freezing")
	fmt.Println("  reading's failure is a plane holding a representation nobody chose.")
	fmt.Println()
	fmt.Println("  ADR-0009's one broken shape - a LOAD during init() - is unchanged and")
	fmt.Println("  still broken, because a Load retains its schema.")
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
