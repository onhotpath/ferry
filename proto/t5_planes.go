package main

// T5: which planes can be templated at all, and what one that cannot reports.
//
// This is #14's fourth ask and it is the one that splits the feature in two.

import (
	"context"
	"fmt"
	"strings"
)

func runT5() {
	ctx := context.Background()
	p, _ := tPlanFor[TConf](ctx, tAggregating)

	fmt.Println("(a) the same plan through three sinks")

	fmt.Println("\n  yaml, a text plane with a comment syntax:")
	w := newYAMLTemplate()
	for _, a := range p.addrs {
		_ = w.Set(ctx, a, p.vals[a])
		_ = w.Annotate(a, tNote{required: p.required[a]})
	}
	_ = w.Commit(ctx)
	fmt.Println(prefixLines(w.String(), "    "))

	fmt.Println("  kv, opaque bytes, no format:")
	kv := &tKVSink{vals: map[Path]Value{}}
	kw, _ := mustOpen(ctx, kv)
	for _, a := range p.addrs {
		_ = kw.Set(ctx, a, p.vals[a])
	}
	for _, a := range sortedPaths(keysOf(kv.vals)) {
		fmt.Printf("    %-16s %s\n", a, kv.vals[a].GoString())
	}
	if _, ok := kw.(tAnnotator); !ok {
		fmt.Println("    the writer implements no annotation channel, so every marker is lost.")
	}

	fmt.Println("\n  a JSON-shaped plane, which has a format and NO comment syntax:")
	fmt.Println("    the two things a JSON emitter can do with `# (REQUIRED)`:")
	fmt.Println("      1. drop it        -> the artefact is the kv row above with braces")
	fmt.Println("      2. invent a key   -> \"//db/host\": \"REQUIRED\", which is a new address")
	fmt.Println("         in a key space ADR-0003 says is the schema's, and which the")
	fmt.Println("         matching Load then reads back as an unmapped key.")
	fmt.Println("    Neither is a template. So \"has a serialization format\" is not the")
	fmt.Println("    predicate; \"has a comment syntax\" is, and it is a strictly smaller set.")

	fmt.Println("\n(b) the predicate, over ADR-0004's own first-party list and the charter's planes")
	rows := []struct {
		plane, dump, comment, verdict string
	}{
		{"yaml", "yes", "yes  #", "full template"},
		{"toml", "yes", "yes  #", "full template"},
		{"json", "yes", "NO", "values only"},
		{"env (the process)", "NO (ADR-0002)", "n/a", "not a sink at all"},
		{".env file", "yes", "yes  #", "full template, but it is a different plane"},
		{"kv / Consul", "yes", "NO", "values only"},
		{"Vault", "yes", "NO", "values only"},
		{"query params", "possible", "NO", "values only"},
		{"Windows Registry", "yes", "NO", "values only  (see #15)"},
	}
	fmt.Printf("    %-18s %-14s %-8s %s\n", "plane", "has a Dump", "comment", "what a template can be")
	for _, r := range rows {
		fmt.Printf("    %-18s %-14s %-8s %s\n", r.plane, r.dump, r.comment, r.verdict)
	}

	fmt.Println("\n(c) so the feature is two features, and only one of them is new")
	fmt.Println("    SEEDING    : write the defaulted values to the plane.")
	fmt.Println("                 Works on every writable plane. Is exactly Dump of the value")
	fmt.Println("                 T2's recipe produces. Needs nothing that does not exist.")
	fmt.Println("    DOCUMENTING: emit an artefact a human edits, carrying the markers.")
	fmt.Println("                 Needs a comment syntax, so it needs a text plane, and it")
	fmt.Println("                 needs facts ADR-0004's Writer cannot carry and ADR-0001")
	fmt.Println("                 keeps unexported.")
	fmt.Println("    ADR-0001 calls template generation \"dump a defaulted struct to a starter")
	fmt.Println("    config a user can edit\", which is the first sentence describing SEEDING")
	fmt.Println("    and the second describing DOCUMENTING.")

	fmt.Println("\n(d) what a plane that cannot do it should report")
	fmt.Println("    ADR-0001 rules out silently ignoring anything, so the third option -")
	fmt.Println("    write the values and drop the markers with no signal - is not available.")
	fmt.Println("    Two that are:")
	fmt.Println("      refuse at the generator, before any I/O, because whether the sink")
	fmt.Println("      annotates is knowable by assertion at Bind time, which is the same")
	fmt.Println("      before-any-I/O property ADR-0004 buys with a context-free Bind;")
	fmt.Println("      or degrade to SEEDING and say so in the return value.")
	fmt.Println("    Measured, the assertion is available exactly where ADR-0004 puts every")
	fmt.Println("    other one:")
	for _, s := range []struct {
		name string
		sink FSink
	}{{"yaml template", newYAMLTemplate()}, {"kv", &tKVSink{vals: map[Path]Value{}}}} {
		ww, _ := mustOpen(ctx, s.sink)
		_, ok := ww.(tAnnotator)
		fmt.Printf("      %-14s annotates=%v\n", s.name, ok)
	}
}

func prefixLines(s, pre string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = pre + lines[i]
	}
	return strings.Join(lines, "\n")
}
