package main

// P4: does the candidate canonical form actually have the uniqueness property
// it borrows from jsontext.Pointer, over segment text that is hostile?
// P5: is the canonical string's byte order the same as segment order? If not,
// ADR-0001's determinism invariant needs the sort to be segment-wise.

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

func p4Canon() {
	head("P4  canonical form: round trip and uniqueness")

	cases := [][]string{
		{"db", "host"},
		{"a/b", "c"},
		{"a#b", "c"},
		{"a~b", "c"},
		{"a~1b"},
		{"~"}, {"/"}, {"#"},
		{""},
		{"", ""},
		{"a", "", "b"},
		{"DB_HOST"},
		{"Kéy", "日本"},
		{"a\x00b"},
		{strings.Repeat("~/#", 8)},
	}
	bad := 0
	for _, segs := range cases {
		p := path(segs...)
		got := p.Segments()
		texts := make([]string, len(got))
		for i, s := range got {
			texts[i] = s.Text
		}
		ok := slices.Equal(segs, texts)
		if !ok {
			bad++
		}
		fmt.Printf("    %-26q -> %-28q -> %-26q %s\n", segs, p.String(), texts, mark(ok))
	}

	// Fuzz it, because the interesting failures are the ones not thought of.
	r := rand.New(rand.NewPCG(1, 2))
	alphabet := []string{"a", "~", "/", "#", "0", "", "~0", "~1", "~2", "é"}
	collisions, roundTripFails := 0, 0
	byCanon := map[string][]string{}
	for range 200000 {
		n := 1 + r.IntN(4)
		segs := make([]string, n)
		for i := range segs {
			k := 1 + r.IntN(3)
			var b strings.Builder
			for range k {
				b.WriteString(alphabet[r.IntN(len(alphabet))])
			}
			segs[i] = b.String()
		}
		p := path(segs...)
		got := p.Segments()
		texts := make([]string, len(got))
		for i, s := range got {
			texts[i] = s.Text
		}
		if !slices.Equal(segs, texts) {
			roundTripFails++
			if roundTripFails <= 3 {
				fmt.Printf("    ROUND TRIP FAIL %q -> %q -> %q\n", segs, p.String(), texts)
			}
		}
		key := fmt.Sprintf("%q", segs)
		if prev, ok := byCanon[p.String()]; ok && fmt.Sprintf("%q", prev) != key {
			collisions++
			if collisions <= 3 {
				fmt.Printf("    CANON COLLISION %q and %q both -> %q\n", prev, segs, p.String())
			}
		}
		byCanon[p.String()] = segs
	}
	fmt.Printf("    fuzz: 200000 paths, %d distinct canonical forms, %d round-trip failures, %d collisions\n",
		len(byCanon), roundTripFails, collisions)

	// Comparability: the reason the canonical form is a string.
	set := map[Path]string{}
	set[path("db", "host")] = "x"
	set[path("db", "host")] = "y"
	fmt.Printf("    Path is a usable map key: len=%d after two writes of one address\n", len(set))
	fmt.Printf("    ==  works: %v ; a []string address would not compile as a key\n",
		path("db", "host") == path("db", "host"))

	// Index segments are distinct from name segments that look numeric.
	// This is the ambiguity P1(c) shows jsontext.Pointer cannot express.
	idx := Path{}.Name("servers").Index(0)
	nam := path("servers", "0")
	fmt.Printf("    servers[0]=%q  servers.\"0\"=%q  equal=%v\n", idx.String(), nam.String(), idx == nam)
}

func p5Order() {
	head("P5  ordering: canonical bytes vs segments")

	ps := []Path{
		path("a", "b"),
		path("a-x"),
		path("a"),
		Path{}.Name("s").Index(2),
		Path{}.Name("s").Index(10),
		path("s", "t"),
	}

	byCanon := slices.Clone(ps)
	slices.SortFunc(byCanon, func(x, y Path) int { return strings.Compare(x.String(), y.String()) })
	bySeg := sortedPaths(ps)

	fmt.Println("    sorted by canonical string bytes:")
	for _, p := range byCanon {
		fmt.Printf("        %q\n", p.String())
	}
	fmt.Println("    sorted segment-wise:")
	for _, p := range bySeg {
		fmt.Printf("        %q\n", p.String())
	}
	same := true
	for i := range ps {
		if byCanon[i] != bySeg[i] {
			same = false
		}
	}
	fmt.Printf("    identical orders: %v\n", same)
	fmt.Println("    both are deterministic; only one is the order a human diffing a dump expects")
}

func mark(ok bool) string {
	if ok {
		return "ok"
	}
	return "FAIL"
}
