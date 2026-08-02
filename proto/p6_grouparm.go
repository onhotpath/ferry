package main

// P6: does the value model need a group arm?
//
// Section 4 says "a group arm is required, not optional", because xload's
// flattening is exactly where the YAML list is lost (5.8). That was written
// against a flat key space. ADR-0003 gives a composite one address per
// element, so the reason may have evaporated. Go looking for a case that
// still forces it, rather than carrying the recommendation over.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func p6GroupArm() {
	head("P6  is a group arm still required once composites get one address each?")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	// (a) 5.8's own case: servers: [a, b] read through the boundary.
	file := filepath.Join(dir, "in.yaml")
	os.WriteFile(file, []byte("servers:\n  - a\n  - b\nnullv: null\nport: 8080\n"), 0o644)

	indexed := NewAddressSet([]Path{
		Path{}.Name("servers").Index(0),
		Path{}.Name("servers").Index(1),
		path("nullv"), path("port"),
	})
	r, err := bindOpen(ctx, YAMLSource{Path: file}, indexed)
	if err != nil {
		fmt.Println("    open:", err)
		return
	}
	fmt.Println("    (a) per-element addresses, no group arm in sight")
	for _, p := range indexed.All() {
		v, err := r.Get(ctx, p)
		fmt.Printf("        %-14s present=%-5v %-16s err=%v\n", p, v.Present(), v.GoString(), err)
	}
	fmt.Println("        5.8's list-becomes-empty-string cannot happen: nothing ever")
	fmt.Println("        asks the plane for the value *at* /servers.")

	// (b) The case that could force it: a leaf whose address IS the composite.
	fmt.Println("\n    (b) asking for the composite itself")
	whole := NewAddressSet([]Path{path("servers")})
	r2, _ := bindOpen(ctx, YAMLSource{Path: file}, whole)
	v, err := r2.Get(ctx, path("servers"))
	fmt.Printf("        /servers      present=%-5v %-16s err=%v\n", v.Present(), v.GoString(), err)
	fmt.Println("        The driver refuses loudly rather than casting to \"\". That is")
	fmt.Println("        an error, not a missing arm: a schema only asks for /servers")
	fmt.Println("        if a codec claimed the whole field, and then the plane is")
	fmt.Println("        being asked for a scalar it does not have.")

	// (c) The flat-plane version of the same thing: TAGS=a,b,c. Here the plane
	//     genuinely does hold the whole list in one value, and it is a string.
	fmt.Println("\n    (c) the flat plane, where a composite is one scalar")
	env := EnvSource{Lookup: func(k string) (string, bool) {
		if k == "TAGS" {
			return "a,b,c", true
		}
		return "", false
	}}
	r3, _ := bindOpen(ctx, env, whole2())
	v, _ = r3.Get(ctx, path("tags"))
	fmt.Printf("        /tags         present=%-5v %-16s\n", v.Present(), v.GoString())
	fmt.Println("        Still a scalar. Splitting it is the codec's job (#12), and")
	fmt.Println("        the split happens on ferry's side of the boundary where the")
	fmt.Println("        target Go type is known - not in the value model.")

	// (d) So what would a group arm actually be for?
	fmt.Println("\n    (d) verdict")
	fmt.Println("        Every address in a compiled schema is a leaf, because")
	fmt.Println("        ADR-0003 mints one address per element and a codec that")
	fmt.Println("        claims a whole composite makes it a scalar leaf. A group arm")
	fmt.Println("        would be an arm no address can be at. Section 4's")
	fmt.Println("        recommendation was correct for a flat key space and is")
	fmt.Println("        obsoleted by ADR-0003, which landed after it.")
	fmt.Println("        Cost of being wrong: adding a kind later is an API change,")
	fmt.Println("        and slog's KindAny is the precedent for shipping the escape")
	fmt.Println("        arm on day one. Weighed in the ADR rather than here.")
}

func whole2() *AddressSet { return NewAddressSet([]Path{path("tags")}) }
