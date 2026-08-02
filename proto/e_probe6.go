package main

// E19-E21, opened by #16's review of ADR-0011's Load rule.
//
// E19  the rule has two readings once a seed exists, and every ADR-0011 probe
//      loaded into a fresh zero destination, where they are one observation
// E20  the schema cache memoises the compile behind sync.OnceValues, so it
//      memoises the ERROR: what may a compile-moment error carry?
// E21  does aggregation land only in the scheduler, or does the walk have to
//      know about it too?

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

func init() { e9Hooks = append(e9Hooks, runE19, runE20, runE21) }

// ---------------------------------------------------------------------------
// E19
// ---------------------------------------------------------------------------

type E19Conf struct {
	Host    string
	Port    int
	Retries int
}

// loadOverZero and loadOverSeed are the two readings of "on failure ferry
// yields no value". They are indistinguishable when the seed IS the zero value,
// which is every fixture ADR-0011 built.
func loadOverZero(seed E19Conf, plane map[Path]Value) (E19Conf, error) {
	s := mustSchema(reflect.TypeFor[E19Conf]())
	v := seed
	sink := &errSink{}
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{sink: sink})
	if err := sink.result(); err != nil {
		var zero E19Conf
		return zero, err
	}
	return v, nil
}

func loadOverSeed(seed E19Conf, plane map[Path]Value) (E19Conf, error) {
	s := mustSchema(reflect.TypeFor[E19Conf]())
	v := seed
	sink := &errSink{}
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{sink: sink})
	if err := sink.result(); err != nil {
		return seed, err // ferry yields no value IT BUILT
	}
	return v, nil
}

func runE19() {
	hdr("E19 the Load rule has two readings, and ADR-0011's fixtures hid one")

	good := map[Path]Value{addr("Host"): String("db1"), addr("Port"): Number("5432"), addr("Retries"): Number("3")}
	bad := map[Path]Value{addr("Host"): String("db1"), addr("Port"): String("not-a-port"), addr("Retries"): Number("3")}

	zeroSeed := E19Conf{}
	live, _ := loadOverSeed(zeroSeed, good)
	fmt.Printf("  a good load first: %+v\n\n", live)

	fmt.Printf("  %-24s %-28s %s\n", "reading", "seed = zero (every 0011 probe)", "seed = the live config")
	z1, _ := loadOverZero(zeroSeed, bad)
	s1, _ := loadOverSeed(zeroSeed, bad)
	z2, _ := loadOverZero(live, bad)
	s2, _ := loadOverSeed(live, bad)
	fmt.Printf("  %-24s %-28s %s\n", "return the zero value", fmt.Sprint(z1), fmt.Sprint(z2))
	fmt.Printf("  %-24s %-28s %s\n", "return the seed", fmt.Sprint(s1), fmt.Sprint(s2))

	fmt.Printf("\n  With a zero seed the two are byte-identical: %v\n", z1 == s1)
	fmt.Printf("  With a live seed they are not:                %v\n", z2 == s2)
	fmt.Println("\n  The zero reading DESTROYS a value ferry never touched, which is")
	fmt.Println("  ADR-0011's own worst-outcome argument arriving through the other door,")
	fmt.Println("  and doing more damage, because the caller had a good config.")
	fmt.Println("\n  So the rule is: ferry yields no value IT BUILT. Load[T] returning the")
	fmt.Println("  zero value falls out, because Load is LoadOver with the zero seed.")
}

// ---------------------------------------------------------------------------
// E20. The memoised compile error.
// ---------------------------------------------------------------------------

type E20Bad struct {
	Origins []string `ferry:"origins,required"`
	Ch      chan int `ferry:"ch"`
}

func runE20() {
	hdr("E20 the schema cache memoises the compile, so it memoises the error")

	// #16's cache shape: the compile is behind a per-entry sync.OnceValues, so
	// every later caller receives the FIRST caller's error VALUE, forever.
	compile := sync.OnceValues(func() (*schema, error) {
		return compileT(reflect.TypeFor[E20Bad]())
	})

	_, e1 := compile()
	_, e2 := compile()
	fmt.Printf("  caller 1 and caller 2 receive the same error object: %v\n", errors.Is(e1, e2) && fmt.Sprintf("%p", e1) == fmt.Sprintf("%p", e2))
	fmt.Printf("  it says: %v\n", e1)

	// So the question is what a compile-moment error may carry. Anything
	// per-call would be caller one's, shown to caller two forever.
	fmt.Println("\n  what ADR-0011 puts in a compile-moment error, checked against that:")
	for _, el := range Elements(e1) {
		var fe *Error
		if !errors.As(el, &fe) {
			continue
		}
		fmt.Printf("    location %-12s from reflect.TypeFor[T]() alone      : call-independent\n", fe.loc.String())
		fmt.Printf("    moment   %-12s a constant                           : call-independent\n", fe.mom)
		fmt.Printf("    class    %-12s a package-level sentinel             : call-independent\n", className(fe.class))
		fmt.Printf("    cause    %-12v built from the type                  : call-independent\n", fe.cause)
		break
	}
	fmt.Println("\n  Nothing in the four carried things is per-call, and the tag key, which")
	fmt.Println("  IS per-call, is part of #16's cache key, so two keys are two entries.")
	fmt.Println("  The rule that falls out: a compile-moment error is a property of the")
	fmt.Println("  CACHE KEY, not of the call, and must carry no context, no call site and")
	fmt.Println("  no options snapshot.")

	// And a memoised error is shared across goroutines, so it must be safe to
	// read concurrently. Sorting at construction is what makes that true: there
	// is no lazy state to race on.
	var wg sync.WaitGroup
	seen := make([]string, 64)
	for i := range 64 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := compile()
			seen[i] = fmt.Sprintf("%v|%+v|%d", e, e, len(Elements(e)))
		}(i)
	}
	wg.Wait()
	distinct := map[string]bool{}
	for _, s := range seen {
		distinct[s] = true
	}
	fmt.Printf("\n  64 goroutines formatting the memoised error: %d distinct rendering(s)\n", len(distinct))
	fmt.Println("  Sorting at construction is what makes that safe: nothing is computed")
	fmt.Println("  on first print, so there is no lazy state to race on.")
}

// ---------------------------------------------------------------------------
// E21. Does the walk have to know about aggregation?
// ---------------------------------------------------------------------------

type E21Inner struct {
	User string
	Pass string
}

type E21Conf struct {
	Auth E21Inner `ferry:"auth"`
	Name string
}

func runE21() {
	hdr("E21 does aggregation land in the scheduler, or in the walk too")

	// #16 measures that the same walk under two schedulers gives 1 error and 2,
	// byte-identical in between. ADR-0011 agrees for everything EXCEPT one
	// thing, and this is the probe that finds it.
	//
	// ADR-0011's rule is "report every failure that is not a CONSEQUENCE of
	// another failure it is already reporting". At schema compile that is
	// ADR-0008's tiers. At the walk there is one case, and only the walk knows
	// the subtree relationship.
	s, err := compileD(reflect.TypeFor[E21Conf]())
	if err != nil {
		fmt.Printf("  compile: %v\n", err)
		return
	}
	// Make /auth required, which is admissible on a struct per ADR-0006.
	s.opts[addr("auth")] = fieldOpts{required: true}

	// The plane has a child under /auth, and that child FAILS.
	plane := map[Path]Value{
		addr("auth").Name("User"): Null(), // a Null at a string: refused
		addr("Name"):              String("svc"),
	}
	var v E21Conf
	sink := &errSink{}
	_, _ = loadD(plane, s, reflect.ValueOf(&v).Elem(), loadOpts{sink: sink})
	got := sink.result()
	fmt.Printf("  a required subtree whose only present child fails to decode:\n")
	fmt.Printf("%+v\n", got)
	fmt.Printf("  errors: %d\n", len(Elements(got)))

	if len(Elements(got)) > 1 {
		fmt.Println("\n  TWO errors for ONE cause: the child's refusal, and the parent's")
		fmt.Println("  `required` firing because nothing under it landed. The second is a")
		fmt.Println("  CONSEQUENCE of the first, and only the walk knows that, because only")
		fmt.Println("  the walk holds the subtree relationship.")
	} else {
		fmt.Println("\n  One error. The parent's `required` did not fire, because the walk's")
		fmt.Println("  presence bit is set by a child that was PRESENT even though it failed.")
		fmt.Println("  So the suppression ADR-0011 needs is already implied by ADR-0006's")
		fmt.Println("  presence bit, and it costs #16's walk nothing new.")
	}

	// The same question from the other side: a child that is absent AND
	// required, under a parent that is also required.
	s2, _ := compileD(reflect.TypeFor[E21Conf]())
	s2.opts[addr("auth")] = fieldOpts{required: true}
	s2.opts[addr("auth").Name("User")] = fieldOpts{required: true}
	var v2 E21Conf
	sink2 := &errSink{}
	_, _ = loadD(map[Path]Value{addr("Name"): String("svc")}, s2, reflect.ValueOf(&v2).Elem(), loadOpts{sink: sink2})
	got2 := sink2.result()
	fmt.Printf("\n  a required child that is absent, under a required parent:\n")
	fmt.Printf("%+v\n", got2)
	fmt.Printf("  errors: %d\n", len(Elements(got2)))

	_ = time.Now
}
