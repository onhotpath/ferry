package main

// P7: the ticket says this decision is gated by template generation, which has
// to emit nested YAML. Build the emitter twice - once from bare string segments,
// once from kinded segments - and see which one can produce the document.
// Also the 5.10 probe: what a composite value costs under each address model.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type node struct {
	leaf     string
	children map[string]*node
	order    []string
	isSeq    bool
}

func newNode() *node { return &node{children: map[string]*node{}} }

func (n *node) child(k string, seq bool) *node {
	if c, ok := n.children[k]; ok {
		return c
	}
	c := newNode()
	n.children[k] = c
	n.order = append(n.order, k)
	n.isSeq = seq
	return c
}

// buildKinded uses Segment.Kind to decide container shape.
func buildKinded(pairs [][2]string) *node {
	root := newNode()
	for _, kv := range pairs {
		cur := root
		segs := Path{canon: kv[0]}.Segments()
		for _, s := range segs {
			cur = cur.child(s.Text, s.Kind == Index)
		}
		cur.leaf = kv[1]
	}
	return root
}

// buildGuessed has only strings, so it must guess: "looks like an integer"
// is the only signal available. That is exactly jsontext.Pointer's admitted
// limitation, reproduced in the emitter that needs the answer.
func buildGuessed(pairs [][2]string) *node {
	root := newNode()
	for _, kv := range pairs {
		cur := root
		for _, s := range strings.Split(strings.TrimPrefix(kv[0], "."), ".") {
			_, numeric := strconv.Atoi(s)
			cur = cur.child(s, numeric == nil)
		}
		cur.leaf = kv[1]
	}
	return root
}

func (n *node) render(indent int) string {
	if n.leaf != "" || len(n.children) == 0 {
		return " " + n.leaf + "\n"
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, k := range n.order {
		b.WriteString(strings.Repeat("  ", indent))
		if n.isSeq {
			b.WriteString("-")
		} else {
			b.WriteString(k + ":")
		}
		b.WriteString(n.children[k].render(indent + 1))
	}
	return b.String()
}

func p7Tree() {
	head("P7  template generation: can the emitter build the tree?")

	// One schema, two things that must not be confused:
	//   servers is a []Elem      -> a YAML sequence
	//   labels  is a map[string] -> a YAML mapping whose keys happen to be digits
	kinded := [][2]string{
		{Path{}.Name("servers").Index(0).Name("host").String(), "a.example"},
		{Path{}.Name("servers").Index(1).Name("host").String(), "b.example"},
		{path("labels", "0").String(), "zero"},
		{path("labels", "1").String(), "one"},
	}
	guessed := [][2]string{
		{"servers.0.host", "a.example"},
		{"servers.1.host", "b.example"},
		{"labels.0", "zero"},
		{"labels.1", "one"},
	}

	fmt.Println("    from kinded segments:")
	fmt.Print(indentBlock(buildKinded(kinded).render(0), "        "))
	fmt.Println("    from bare string segments, guessing by \"looks numeric\":")
	fmt.Print(indentBlock(buildGuessed(guessed).render(0), "        "))
	fmt.Println("    labels must be a mapping. Guessing turns it into a sequence and the")
	fmt.Println("    key text is destroyed, which no later stage can recover.")

	// 5.10: composite values.
	fmt.Println()
	fmt.Println("    5.10, a []string field containing the delimiter:")
	vals := []string{"a", "b,c", ""}
	fmt.Printf("        value                     %q\n", vals)
	fmt.Printf("        flat address, split on ,  %q  -> %q\n",
		strings.Join(vals, ","), strings.Split(strings.Join(vals, ","), ","))
	var addrs []string
	for i, v := range vals {
		addrs = append(addrs, fmt.Sprintf("%s=%q", Path{}.Name("tags").Index(i).String(), v))
	}
	fmt.Printf("        indexed addresses         %s\n", strings.Join(addrs, "  "))
	fmt.Printf("        same on a flat plane      %s\n", strings.Join(func() []string {
		var o []string
		for i, v := range vals {
			o = append(o, fmt.Sprintf("%s=%q", envKey(Path{}.Name("tags").Index(i)), v))
		}
		return o
	}(), "  "))

	// And the ordering the emitter needs is the segment-wise one.
	var ps []Path
	for i := range 12 {
		ps = append(ps, Path{}.Name("tags").Index(i))
	}
	byBytes := slices.Clone(ps)
	slices.SortFunc(byBytes, func(a, b Path) int { return strings.Compare(a.String(), b.String()) })
	fmt.Printf("\n    12 indices sorted by canonical bytes: %s\n", joinIdx(byBytes))
	fmt.Printf("    12 indices sorted segment-wise:       %s\n", joinIdx(sortedPaths(ps)))
}

func joinIdx(ps []Path) string {
	var o []string
	for _, p := range ps {
		segs := p.Segments()
		o = append(o, segs[len(segs)-1].Text)
	}
	return strings.Join(o, " ")
}

func indentBlock(s, pad string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(pad + line + "\n")
	}
	return b.String()
}
