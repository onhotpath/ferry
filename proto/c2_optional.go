package main

// C2: what a wrapper silently drops.
//
// ADR-0004 has three optional interfaces "discovered by assertion and never
// required", and lists the consequence it was worried about: "Three optional
// interfaces mean three prose rules the compiler cannot enforce... Each
// failure mode is caught by a conformance case that has to exist anyway, and
// that is the entire argument for the trade."
//
// It did not consider the case where the thing failing the assertion is not a
// driver but a WRAPPER over a driver that satisfies it. That case is #10's
// whole mechanism, and it is the sharpest result in this ticket.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func runC2() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "c10b")
	defer os.RemoveAll(dir)

	cfg := CConf{Name: "svc", DB: CDB{Host: "db1", Password: "hunter2"}}

	fmt.Println("(a) THE FINDING: a naive wrapper over the real YAML sink writes NOTHING")
	for i, c := range []struct {
		label string
		mk    func(FSink) FSink
	}{
		{"no wrapper at all", func(s FSink) FSink { return s }},
		{"the naive wrapper", func(s FSink) FSink { var l []string; return cNaiveSink{s, &l} }},
		{"the forwarding wrapper", func(s FSink) FSink { return cFwdSink{inner: s} }},
	} {
		p := filepath.Join(dir, fmt.Sprintf("out-%d.yaml", i))
		err := Dump(ctx, cfg, c.mk(FYAMLSink{Path: p}))
		st, statErr := os.Stat(p)
		size := int64(-1)
		if statErr == nil {
			size = st.Size()
		}
		fmt.Printf("    %-24s dump err=%-6v  file exists=%-6v  bytes=%d\n",
			c.label, errShortW(err), statErr == nil, size)
	}
	fmt.Println("    The naive wrapper returns a nil error and the plane is never written.")
	fmt.Println("    ADR-0004 designed exactly this failure and assigned it to a driver:")
	fmt.Println("      \"A sink that needs Commit and omits it writes nothing at all,")
	fmt.Println("       silently, which is exactly what ADR-0001 rules out. Measured: it")
	fmt.Println("       fails the first case in the driver-fidelity suite.\"")
	fmt.Println("    A wrapper is not a driver, so it runs no conformance suite, and the")
	fmt.Println("    mitigation ADR-0004 relies on does not reach it.")

	fmt.Println("\n(b) each of the three optional interfaces fails differently, and none")
	fmt.Println("    of the three failures is loud")
	fmt.Printf("    %-12s %-46s %s\n", "dropped", "what happens", "loud?")
	fmt.Printf("    %-12s %-46s %s\n", "Committer", "the staging sink never commits, nothing written", "NO, nil error")
	fmt.Printf("    %-12s %-46s %s\n", "Releaser", "the temp file leaks, one per dump", "NO")
	fmt.Printf("    %-12s %-46s %s\n", "Enumerator", "a map or slice field is a loud refusal", "yes")

	fmt.Println("\n    the Enumerator case, measured, because it is the one that IS loud:")
	p := filepath.Join(dir, "src.yaml")
	_ = os.WriteFile(p, []byte("name: svc\nlimits:\n  rps: 1\n"), 0o644)
	for _, c := range []struct {
		label string
		src   FSource
	}{
		{"unwrapped", FYAMLSource{Path: p}},
		{"naive wrapper", cNaiveSource{FYAMLSource{Path: p}, new([]string)}},
	} {
		got, err := Load[CMapConf](ctx, c.src, WithSched(tAggregating))
		fmt.Printf("      %-16s -> %v  err=%v\n", c.label, got.Limits, errShortW(err))
	}
	fmt.Println("      So adding a wrapper turns a working schema into a refusal, and the")
	fmt.Println("      message names the SOURCE as not implementing Enumerator when the")
	fmt.Println("      source does. ADR-0012's own Observing wrapper has to forward this")
	fmt.Println("      and its ADR does not say so.")

	fmt.Println("\n    the Releaser case, measured, because it is the one nothing reports:")
	tmpDir := filepath.Join(dir, "leak")
	_ = os.MkdirAll(tmpDir, 0o755)
	for range 3 {
		var l []string
		_ = Dump(ctx, cfg, cNaiveSink{FYAMLSink{Path: filepath.Join(tmpDir, "x.yaml")}, &l})
	}
	ents, _ := os.ReadDir(tmpDir)
	leaked := 0
	for _, e := range ents {
		if len(e.Name()) > 6 && e.Name()[:6] == ".ferry" {
			leaked++
		}
	}
	fmt.Printf("      three dumps through the naive wrapper: %d leaked temp files\n", leaked)

	fmt.Println("\n(c) the cost of the correct wrapper, counted")
	fmt.Println("    cFwdWriter's Set is 6 lines. Its Commit and Close are 10 lines that do")
	fmt.Println("    nothing but conditionally forward, and they exist ONLY because the two")
	fmt.Println("    interfaces are optional. If they were required, a wrapper would embed")
	fmt.Println("    the inner Writer and inherit both for free, at the cost ADR-0004")
	fmt.Println("    priced: \"a required Close would be `return nil` boilerplate in four of")
	fmt.Println("    six sinks... indistinguishable in the source from a driver that should")
	fmt.Println("    have rolled back and did not\".")
	fmt.Println("    So the trade ADR-0004 made is between boilerplate in every DRIVER and")
	fmt.Println("    a silent failure in every WRAPPER, and it weighed only the first.")

	fmt.Println("\n(d) Go embedding does not rescue it, and this is worth measuring rather")
	fmt.Println("    than assuming")
	var w FWriter = cEmbedWriter{}
	_, isCommitter := w.(FCommitter)
	fmt.Printf("    a wrapper EMBEDDING FWriter: implements FCommitter = %v\n", isCommitter)
	fmt.Println("    Embedding the INTERFACE promotes only the interface's own method set,")
	fmt.Println("    so Set is inherited and Commit is not. Embedding the CONCRETE type")
	fmt.Println("    would work and is unavailable: a wrapper takes an FSink and does not")
	fmt.Println("    know what it wrapped.")
	fmt.Println("    That is the structural reason this cannot be fixed on the wrapper's")
	fmt.Println("    side, and it is why the answer has to be a core-supplied helper or a")
	fmt.Println("    conformance case rather than advice.")
}

type cEmbedWriter struct{ FWriter }

// CMapConf is C2's Enumerator fixture.
type CMapConf struct {
	Name   string         `ferry:"name"`
	Limits map[string]int `ferry:"limits"`
}
