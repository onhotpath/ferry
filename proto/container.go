package main

// Can a real plane tell "present and empty" from "absent" at a CONTAINER
// address? If it can, nil-vs-empty is recoverable at every position and not
// only at static ones. If it cannot, the carve-out is structural.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func runContainer() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryc")
	defer os.RemoveAll(dir)

	docs := map[string]string{
		"absent      ":  "other: 1\n",
		"empty seq   ":  "tags: []\n",
		"empty map   ":  "tags: {}\n",
		"explicit null": "tags: null\n",
		"populated   ":  "tags: [a]\n",
	}
	probe := Path{}.Name("tags")
	as := NewAddressSet([]Path{probe, Path{}.Name("other")})

	fmt.Printf("  %-14s %-14s %-22s %s\n", "document", "Get(/tags)", "Children(/tags)", "verdict")
	for _, label := range []string{"absent      ", "empty seq   ", "empty map   ", "explicit null", "populated   "} {
		body := docs[label]
		p := filepath.Join(dir, label[:5]+".yaml")
		os.WriteFile(p, []byte(body), 0o644)
		open, err := (FYAMLSource{Path: p}).Bind(as)
		if err != nil {
			fmt.Println("bind:", err)
			continue
		}
		r, err := open(ctx)
		if err != nil {
			fmt.Println("open:", err)
			continue
		}
		v, _ := r.Get(ctx, probe)
		var kids []Path
		if e, ok := r.(FEnumerator); ok {
			kids, _ = e.Children(ctx, probe)
		}
		verdict := ""
		switch {
		case v.Kind() == VAbsent && len(kids) == 0:
			verdict = "indistinguishable from absent"
		case v.Kind() == VNull:
			verdict = "carries a null"
		case len(kids) > 0:
			verdict = "has children"
		default:
			verdict = "scalar " + v.GoString()
		}
		fmt.Printf("  %-14s %-14s %-22v %s\n", label, v.GoString(), kids, verdict)
	}
}
