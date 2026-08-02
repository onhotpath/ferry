package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func identityPlane(in map[Path]Value) (map[Path]Value, error) { return in, nil }

func yamlPlane(dir string) func(map[Path]Value) (map[Path]Value, error) {
	n := 0
	return func(in map[Path]Value) (map[Path]Value, error) {
		n++
		ctx := context.Background()
		p := filepath.Join(dir, fmt.Sprintf("c%d.yaml", n))
		as := NewAddressSet(sortedAddrs(in))
		ow, err := (FYAMLSink{Path: p}).Bind(as)
		if err != nil {
			return nil, err
		}
		if err := fDump(ctx, ow, in, as); err != nil {
			return nil, err
		}
		or, err := (FYAMLSource{Path: p}).Bind(as)
		if err != nil {
			return nil, err
		}
		return fLoad(ctx, or, as)
	}
}

func runHarness() {
	dir, _ := os.MkdirTemp("", "ferryh")
	defer os.RemoveAll(dir)
	for _, plane := range []struct {
		name string
		fn   func(map[Path]Value) (map[Path]Value, error)
	}{
		{"memory plane (ferrytest)", identityPlane},
		{"yaml driver, real files", yamlPlane(dir)},
	} {
		fmt.Printf("\n--- %s ---\n", plane.name)
		total, bad := 0, 0
		for _, pr := range coreSet() {
			fails := pr.run(plane.fn)
			total++
			if len(fails) > 0 {
				bad++
				fmt.Printf("  FAIL %-14s\n", pr.Name())
				for _, f := range fails {
					fmt.Printf("       %s\n", f)
				}
			} else {
				fmt.Printf("  ok   %s\n", pr.Name())
			}
		}
		fmt.Printf("  %d/%d types round-trip\n", total-bad, total)
	}
}
