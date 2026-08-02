package main

// P4: what a driver costs to write.
//
// ADR-0001's consequences name the bar: koanf gets twenty providers off a
// two-method interface at 31 to 246 lines each, median around 120. If ferry's
// contract makes a Consul driver meaningfully longer than that, the contract
// has overreached and the ADR has to say so.
//
// The count is of the four drivers in this prototype, which are written as
// real drivers rather than as sketches.

import (
	"fmt"
	"os"
	"strings"
)

func p4Cost() {
	head("P4  driver authoring cost, against koanf's 31-246 line bar")

	files := []struct{ name, file, dirs string }{
		{"env", "drv_env.go", "source"},
		{"query params", "drv_query.go", "source"},
		{"kv (Consul-shaped)", "drv_kv.go", "source+sink"},
		{"yaml", "drv_yaml.go", "source+sink"},
	}
	fmt.Printf("    %-20s %-12s %5s %5s\n", "driver", "directions", "code", "all")
	for _, f := range files {
		code, all := countLines(f.file)
		fmt.Printf("    %-20s %-12s %5d %5d\n", f.name, f.dirs, code, all)
	}

	fmt.Println("\n    (a) required methods")
	fmt.Println("        Source:  Bind          Binding:      Open   Reader: Get")
	fmt.Println("        Sink:    Bind          WriteBinding: Open   Writer: Set, Commit, Abort")
	fmt.Println("        A read-only driver implements 3 methods, one above koanf's bar.")
	fmt.Println("        What the third buys is measured in P9: without it the")
	fmt.Println("        precomputed key table has nowhere to live that is not a cache,")
	fmt.Println("        and both ways of keying that cache are defective.")
	fmt.Println("        Two of the four drivers here satisfy Binding with a method that")
	fmt.Println("        returns the receiver, because their plane needs no snapshot.")

	fmt.Println("\n    (b) what the address set costs a driver, per plane")
	fmt.Println("        flat plane  : one KeyTable call, and core runs legality and")
	fmt.Println("                      injectivity over the result. env is 12 lines")
	fmt.Println("                      of key function.")
	fmt.Println("        tree plane  : nothing at all. yaml never builds a plane key,")
	fmt.Println("                      it walks the segments, so it has no injectivity")
	fmt.Println("                      obligation and makes no KeyTable call.")
	fmt.Println("        That asymmetry is real and worth stating: ADR-0003's driver")
	fmt.Println("        rule binds flattening drivers only.")

	fmt.Println("\n    (c) the escape ferry does not offer")
	fmt.Println("        There is no optional Batcher/Snapshotter interface to")
	fmt.Println("        upgrade to, because Bind already handed over the whole")
	fmt.Println("        address set and Open is free to use it. Batch versus lazy")
	fmt.Println("        is a branch inside one driver (drv_kv.go, one bool), not a")
	fmt.Println("        second contract for ferry to define and version.")
}

func countLines(name string) (code, all int) {
	b, err := os.ReadFile(name)
	if err != nil {
		return 0, 0
	}
	for _, ln := range strings.Split(string(b), "\n") {
		all++
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		code++
	}
	return code, all - 1
}
