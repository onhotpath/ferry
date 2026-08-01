package main

// P9: found by accident in P2(b). Exact-duplicate detection is not enough.
// A leaf at /db and a subtree under /db are two distinct addresses, so the
// injectivity check accepts them - and a tree-shaped plane cannot hold both.
// Is the right core rule "no duplicates" or "no address is a prefix of another"?

import (
	"fmt"
	"slices"
	"strings"
)

func isPrefix(a, b Path) bool {
	return a.canon == b.canon ||
		(strings.HasPrefix(b.canon, a.canon) &&
			(b.canon[len(a.canon)] == sigilName || b.canon[len(a.canon)] == sigilIndex))
}

func checkAntichain(addrs []Path) error {
	sorted := sortedPaths(addrs)
	var clashes []string
	for i := range sorted {
		for j := range sorted {
			if i != j && isPrefix(sorted[i], sorted[j]) && !(i > j && sorted[i] == sorted[j]) {
				if sorted[i] == sorted[j] && i > j {
					continue
				}
				clashes = append(clashes, fmt.Sprintf("%s encloses %s", sorted[i], sorted[j]))
			}
		}
	}
	slices.Sort(clashes)
	clashes = slices.Compact(clashes)
	if len(clashes) > 0 {
		return fmt.Errorf("address set is not prefix-free:\n        %s", strings.Join(clashes, "\n        "))
	}
	return nil
}

func p9PrefixFree() {
	head("P9  a value and a subtree at one address")

	type Inner struct {
		Host string `ferry:"host"`
	}
	type Straddle struct {
		DB     Inner  `ferry:"db"`
		DBSelf string `ferry:"db"`
	}
	s, err := compile[Straddle]()
	fmt.Printf("    duplicate check alone: err=%v\n", err)
	var addrs []Path
	for _, l := range s.leaves {
		addrs = append(addrs, l.Path)
		fmt.Printf("        %-12s %s\n", l.Path, l.Field)
	}
	fmt.Printf("    prefix-free check:     %v\n", checkAntichain(addrs))

	fmt.Println("    what each plane does with the pair, unchecked:")
	pairs := [][2]string{{path("db").String(), "postgres://"}, {path("db", "host").String(), "localhost"}}
	fmt.Printf("        flat env:   %s=%q  %s=%q   (both survive)\n",
		envKey(path("db")), pairs[0][1], envKey(path("db", "host")), pairs[1][1])
	fmt.Println("        tree plane:")
	fmt.Print(indentBlock(buildKinded(pairs).render(0), "            "))
	fmt.Println("        the scalar at db is gone. Reversing the write order loses the other one.")

	// Is prefix-freeness free of false positives on ordinary schemas?
	type Cred struct {
		User string `ferry:"user"`
	}
	type DB struct {
		Host string `ferry:"host"`
		Auth Cred   `ferry:"auth"`
	}
	type Cfg struct {
		DB      DB `ferry:"db"`
		Replica DB `ferry:"replica"`
	}
	ok, _ := compile[Cfg]()
	var good []Path
	for _, l := range ok.leaves {
		good = append(good, l.Path)
	}
	fmt.Printf("    ordinary nested schema is prefix-free: %v\n", checkAntichain(good) == nil)

	// And the relation is exactly jsontext.Pointer.Contains.
	fmt.Printf("    the relation is Contains: /db encloses /db/host = %v, /db vs /dbx = %v\n",
		isPrefix(path("db"), path("db", "host")), isPrefix(path("db"), path("dbx")))
}
