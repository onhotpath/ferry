package main

// P17: Absent and Null are different observations. Are they, all the way
// through, in both directions?
//
// Section 4: "absent is not null is not \"\"". xload conflates all three
// (5.1, 5.8); viper drops null entirely. This probe checks ferry's boundary
// holds the distinction on Load and on Dump, and it does not fully.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func p17Null() {
	head("P17  Absent, Null and the empty string")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	// (a) Four observations a plane can make, read through a real YAML file.
	fmt.Println("    (a) reading: four distinct observations")
	file := filepath.Join(dir, "in.yaml")
	os.WriteFile(file, []byte("nul: null\nempty: \"\"\nvalue: 8080\n"), 0o644)
	set := NewAddressSet([]Path{path("nul"), path("empty"), path("value"), path("gone")})
	open, _ := FYAMLSource{Path: file}.Bind(set)
	got, _ := fLoad(ctx, open, set)
	for _, p := range set.All() {
		v := got[p]
		fmt.Printf("        %-8s %-16s present=%-5v\n", p, v.GoString(), v.Present())
	}
	fmt.Println("        Absent   : the plane has no such address")
	fmt.Println("        Null     : the plane HAS the address and its value is the")
	fmt.Println("                   plane's own null. Only a plane with a null in its")
	fmt.Println("                   type system can produce it.")
	fmt.Println("        String(\"\"): present, and the value is zero-length text.")

	// (b) Which planes can even say Null?
	fmt.Println("\n    (b) which planes can produce Null at all")
	for _, r := range []struct{ plane, can, why string }{
		{"yaml", "yes", "!!null is a resolved tag"},
		{"json", "yes", "null is a token"},
		{"toml", "no", "the grammar has no null"},
		{"env", "no", "FOO= is a zero-length string, not a null"},
		{"query params", "no", "?x= is a zero-length string"},
		{"kv (bytes)", "no", "a zero-length value is Bytes, not Null"},
	} {
		fmt.Printf("        %-14s %-5s %s\n", r.plane, r.can, r.why)
	}
	fmt.Println("        So Null rides the same axis as plane-side type information,")
	fmt.Println("        and on a flat plane the distinction simply never arises.")

	// (c) xload's baseline, for contrast.
	fmt.Println("\n    (c) what xload does with the same document")
	fmt.Println("        FlattenMap -> cast.ToString: nullv => \"\" (5.8, reproduced)")
	fmt.Println("        so null, empty and missing are one observation, and")
	fmt.Println("        viper drops `nul: null` from AllSettings() entirely.")

	// (d) Dump. This is where the prototype does NOT hold the distinction.
	fmt.Println("\n    (d) writing: does the distinction survive a dump?")
	out := filepath.Join(dir, "out.yaml")
	wset := NewAddressSet([]Path{path("nul"), path("empty"), path("gone")})
	ow, _ := FYAMLSink{Path: out}.Bind(wset)
	vals := map[Path]Value{
		path("nul"):   Null(),
		path("empty"): String(""),
		path("gone"):  Absent,
	}
	if err := fDump(ctx, ow, vals, wset); err != nil {
		fmt.Println("        dump:", err)
		return
	}
	b, _ := os.ReadFile(out)
	for _, ln := range splitLines(string(b)) {
		fmt.Println("           ", ln)
	}
	ro, _ := FYAMLSource{Path: out}.Bind(wset)
	back, _ := fLoad(ctx, ro, wset)
	for _, p := range wset.All() {
		fmt.Printf("        %-8s wrote %-14s read back %-14s same=%v\n",
			p, vals[p].GoString(), back[p].GoString(), vals[p] == back[p])
	}

	fmt.Println("\n    (e) the finding")
	fmt.Println("        Null and String(\"\") round trip. Absent does not: this")
	fmt.Println("        prototype's yaml sink maps VAbsent to !!null, so an absent")
	fmt.Println("        address is written as an explicit null and reads back as")
	fmt.Println("        Null. That is the exact conflation this ADR criticises xload")
	fmt.Println("        for, committed by the prototype on the write path.")
	fmt.Println("        It is a prototype shortcut, not a decision. What a sink")
	fmt.Println("        should do with an absent value - write the plane's null, or")
	fmt.Println("        write nothing and leave the address unset - is #8's, and the")
	fmt.Println("        honest answer for the driver contract is that ferry should")
	fmt.Println("        probably never hand a sink an Absent at all.")
}
