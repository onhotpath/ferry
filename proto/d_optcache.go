package main

// If a refusal is lifted later by an Option, what does that Option cost?
// It depends on whether it changes what COMPILES or only what a load does.

import (
	"fmt"
	"reflect"
)

type LoadOption func(*loadOpts)

func rlift() {
	dhdr("OC1 two kinds of Option, and only one is cheap")

	// A load-time Option: the null policy. It changes decode, not the schema.
	s := mustSchema(reflect.TypeFor[struct {
		V int `ferry:"v,default=8080"`
	}]())
	fmt.Printf("  a LOAD-TIME Option (null policy) leaves the schema identical:\n")
	fmt.Printf("     addresses=%v  declarations at /v: default=%q\n", s.addrs, s.at(addr("v")).defText)
	for _, o := range []loadOpts{{}, {nullMeansZero: true}} {
		v := struct {
			V int `ferry:"v,default=8080"`
		}{}
		_, e := loadD(map[Path]Value{addr("v"): Null()}, s, reflect.ValueOf(&v).Elem(), o)
		fmt.Printf("     Null at /v -> V=%d err=%v\n", v.V, errOrBlank(e))
	}
	fmt.Println("     One compiled schema serves both, so nothing touches the cache key.")

	// A compile-time Option: lifting a refusal. It changes what compiles.
	fmt.Println("\n  a COMPILE-TIME Option (lift required on a collection) does not:")
	t := reflect.TypeFor[struct {
		V []string `ferry:"v,required"`
	}]()
	for _, allow := range []bool{false, true} {
		allowRequiredOnComposite = allow
		_, err := compileD(t)
		fmt.Printf("     lifted=%-5v -> %s\n", allow, errOrOK(err))
	}
	allowRequiredOnComposite = false
	fmt.Println("     One reflect.Type, two different compiled schemas. So the Option")
	fmt.Println("     becomes part of whatever keys the schema cache, which is #16's.")

	// And why that key is awkward, which ADR-0004 measured for drivers.
	fmt.Println("\n  and why that key is awkward, reproducing ADR-0004's finding here:")
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("     using a functional-option value as a map key panics: %v\n", r)
			fmt.Println("     Same shape as ADR-0004's `hash of unhashable type main.EnvSource`:")
			fmt.Println("     a func field makes the value unhashable, and an option list is funcs.")
		}
	}()
	m := map[any]int{}
	var opt LoadOption = func(*loadOpts) {}
	m[opt] = 1
	fmt.Println("     (no panic, which would be a surprise)")
}
