package main

// X4: D11, a promoted embedded pointer is a schema compile error.

import (
	"context"
	"fmt"
	"reflect"
)

type X1Common struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

type X1PromotedPtr struct {
	*X1Common
	Port int `ferry:"port"`
}

type X1NestedPtr struct {
	*X1Common `ferry:"common"`
	Port      int `ferry:"port"`
}

type X1PromotedVal struct {
	X1Common
	Port int `ferry:"port"`
}

func runX1_4() {
	quoteADR(
		"ADR-0008's field rule: embedded **pointer**, with no ferry tag:",
		"**schema compile error**.",
		"",
		"\"Promotion walks the pointed-to struct at the parent address, so the",
		"pointer has no address subtree of its own, and ADR-0006's presence bit",
		"has nothing to materialise it from.\"")

	for _, tc := range []struct {
		label string
		t     reflect.Type
	}{
		{"struct{ *X1Common; Port int }", reflect.TypeFor[X1PromotedPtr]()},
		{"struct{ *X1Common `ferry:\"common\"`; ... }", reflect.TypeFor[X1NestedPtr]()},
		{"struct{ X1Common; Port int }", reflect.TypeFor[X1PromotedVal]()},
	} {
		s, err := compileSchema2(tc.t, defaultOpts())
		if err != nil {
			fmt.Printf("    %-42s REFUSED\n", tc.label)
			for _, l := range splitLines(err.Error()) {
				fmt.Printf("        %s\n", l)
			}
			continue
		}
		fmt.Printf("    %-42s compiles, addrs %v\n", tc.label, s.addrs)
	}

	fmt.Println("\n  the consequence #41 found that ADR-0008 did not have:")
	fmt.Println("  a promoted embedded pointer's own address IS the empty path, so a nil")
	fmt.Println("  one dumps Null at the address ADR-0003 says may not exist and ADR-0010's")
	fmt.Println("  root rule refuses at every other door. Measured on the tip by A41=11:")
	fmt.Println("      dump with the embedded pointer nil, err=<nil>")
	fmt.Println("        \"\"       null")
	fmt.Println("        \"/port\"  number(\"8080\")")
	fmt.Println("  With the refusal in place that dump cannot be reached, because the")
	fmt.Println("  schema does not compile. The empty path is now unmintable through this")
	fmt.Println("  door as well as through the root.")

	fmt.Println("\n  and nesting still works, which is the remedy the message names:")
	v := X1NestedPtr{X1Common: &X1Common{Name: "n", Env: "e"}, Port: 8080}
	vals, err := dumpTo(context.Background(), v)
	fmt.Printf("    dump -> err=%v\n", x1ErrText(err))
	dumpAddrs(vals)
	back, lerr := loadFrom(context.Background(), X1NestedPtr{}, vals)
	fmt.Printf("    load -> err=%v  Name=%q Env=%q Port=%d\n",
		x1ErrText(lerr), back.X1Common.Name, back.X1Common.Env, back.Port)
}
