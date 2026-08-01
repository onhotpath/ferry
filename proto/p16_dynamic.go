package main

// P16: auditing my own claim. The ADR says the collision rules are "checked at
// schema compile time, from the type alone, with no value in hand". Is that
// true for every address a schema can produce?
//
// Struct fields come from the type. Map keys and slice indices come from the
// VALUE. So the address set is not fully knowable from the type, and the claim
// needs scoping or the rule needs a second tier.

import (
	"fmt"
	"reflect"
	"strings"
)

type Limits struct {
	HTTPPort int               `ferry:"http_port"`
	Extra    map[string]string `ferry:"extra"`
}

type DynConf struct {
	Limits Limits            `ferry:"limits"`
	Labels map[string]string `ferry:"labels"`
}

func p16Dynamic() {
	head("P16  addresses that only exist once there is a value")

	// (a) What is knowable from the type alone?
	fmt.Println("    (a) walking the TYPE: which addresses are static?")
	staticWalk(reflect.TypeFor[DynConf](), Path{}, 0)

	// (b) The same schema with two different values produces two different
	//     address sets. Neither is a property of the type.
	fmt.Println("\n    (b) walking two VALUES of that same type:")
	vals := []DynConf{
		{Limits: Limits{HTTPPort: 80, Extra: map[string]string{"burst": "5"}},
			Labels: map[string]string{"env": "prod"}},
		{Limits: Limits{HTTPPort: 80, Extra: map[string]string{"http.port": "9", "http_port": "10"}},
			Labels: map[string]string{}},
	}
	xform := envDriverTransforming()
	for i, v := range vals {
		var pairs []pair
		dumpAddrs(reflect.ValueOf(v), Path{}, &pairs)
		var addrs []Path
		var shown []string
		for _, p := range pairs {
			addrs = append(addrs, p.Addr)
			shown = append(shown, p.Addr.String())
		}
		fmt.Printf("        value %d addresses: %s\n", i+1, strings.Join(shown, "  "))
		fmt.Printf("        value %d core prefix-free: %-6v  driver: %s\n",
			i+1, checkAntichain(addrs) == nil, verdict(xform.accept(addrs)))
	}

	fmt.Println("\n    the second value is refused, and nothing about the TYPE differs.")
	var pairs []pair
	dumpAddrs(reflect.ValueOf(vals[1]), Path{}, &pairs)
	var addrs []Path
	for _, p := range pairs {
		addrs = append(addrs, p.Addr)
	}
	fmt.Printf("        %v\n", xform.accept(addrs))

	// (c) So where can the check actually run? Not at schema compile for these.
	fmt.Println("\n    (c) when each tier is checkable:")
	fmt.Println("        static addresses  (struct fields)  -> schema compile, no value, no plane")
	fmt.Println("        dynamic addresses (map keys)       -> Dump: as minted, before the write")
	fmt.Println("                                           -> Load: only if the source enumerates (#5)")

	// (d) Is the dynamic check still cheap? It is an insert into the set the
	//     static pass already built, not a re-check of everything.
	seen := map[string]Path{}
	for _, a := range sortedPaths(addrs) {
		k := xform.key(a)
		if prev, ok := seen[k]; ok {
			fmt.Printf("\n    (d) incremental: minting %s collides with %s at %q, caught on write\n", a, prev, k)
			break
		}
		seen[k] = a
	}
}

func staticWalk(t reflect.Type, at Path, depth int) {
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("ferry"), ",")
		if name == "" {
			name = f.Name
		}
		p := at.Name(name)
		switch f.Type.Kind() {
		case reflect.Struct:
			staticWalk(f.Type, p, depth+1)
		case reflect.Map:
			fmt.Printf("        %-22s DYNAMIC: keys come from the value, type says only %s\n", p, f.Type)
		case reflect.Slice:
			fmt.Printf("        %-22s DYNAMIC: length comes from the value\n", p)
		default:
			fmt.Printf("        %-22s static\n", p)
		}
	}
}
