package main

// P8: "how does prefixing on nested structs express itself in that model?"
// xload's prefix is raw text concatenation onto a flat key. Under a structured
// address a prefix can only be a segment. What does that gain and what does it
// take away?

import "fmt"

func p8Prefix() {
	head("P8  prefixing on nested structs")

	// Text concatenation, xload style: prefix "DB_" onto key "HOST".
	concat := func(prefix, key string) string { return prefix + key }
	fmt.Println("    text concatenation (flat model):")
	for _, c := range [][2]string{{"DB_", "HOST"}, {"DB", "HOST"}, {"DB_", "_HOST"}, {"", "HOST"}} {
		fmt.Printf("        prefix=%-6q key=%-8q -> %q\n", c[0], c[1], concat(c[0], c[1]))
	}
	fmt.Println("    all four are legal and three of them are typos nothing can detect,")
	fmt.Println("    because the separator is not part of the model.")

	fmt.Println("    segment prepending (structured model):")
	for _, c := range [][2]string{{"DB", "HOST"}, {"DB_", "HOST"}} {
		p := path(c[0], c[1])
		fmt.Printf("        prefix=%-6q key=%-8q -> %-20q env=%-12q dotted=%q\n",
			c[0], c[1], p.String(), envKey(p), dottedKey(p))
	}
	fmt.Println("    \"DBHOST\" with no separator is now unexpressible, which is the point:")
	fmt.Println("    it is the spelling that manufactures the DB/HOST vs DB_HOST collision.")

	// Nesting depth composes without the walk knowing the separator.
	type Cred struct {
		User string `ferry:"user"`
		Pass string `ferry:"pass"`
	}
	type DB struct {
		Host string `ferry:"host"`
		Auth Cred   `ferry:"auth"`
	}
	type Cfg struct {
		DB      DB   `ferry:"db"`
		Replica DB   `ferry:"replica"`
		Inline  Cred `ferry:",squash"`
	}
	s, err := compile[Cfg]()
	fmt.Printf("\n    compile[Cfg] err=%v\n", err)
	for _, l := range s.leaves {
		fmt.Printf("        %-28s env=%-22s dotted=%s\n", l.Path, envKey(l.Path), dottedKey(l.Path))
	}
	var addrs []Path
	for _, l := range s.leaves {
		addrs = append(addrs, l.Path)
	}
	fmt.Printf("    env key function injective over this schema: %v\n", checkInjective(addrs, envKey) == nil)
	fmt.Printf("    dotted key function injective over this schema: %v\n", checkInjective(addrs, dottedKey) == nil)
}
