package main

// T8: the audit. The cases the first seven probes do not contain.
//
// Every session on this map has found its worst defect by asking what its
// fixtures could not express. The handoff names this ticket's version of the
// trap outright: "a template with no secret in it". So the fixture has one,
// and this probe goes looking for the rest.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runT8() {
	ctx := context.Background()

	// ---------------------------------------------------------------- A1
	fmt.Println("A1  does the emitted template contain the secret?  (#10 x #14)")
	p, _ := tPlanFor[TConf](ctx, tAggregating)
	art := tRealYAML(ctx, p)
	fmt.Printf("    the artefact contains the string \"hunter2\": %v\n", strings.Contains(art, "hunter2"))
	fmt.Println("    It arrived as a DECLARED DEFAULT, so it is in the type, it is in every")
	fmt.Println("    compiled schema, and it is in the starter config the tool hands a user.")
	fmt.Println("    Three things follow, and the third is the one that bears on #10:")
	fmt.Println("      - this is not a bug in the recipe. ADR-0006 requires a defaulted field")
	fmt.Println("        to be dumped, and a template is a dump of a defaulted struct, so the")
	fmt.Println("        credential is there BY THE RULE rather than by an oversight.")
	fmt.Println("      - it is not reachable by redaction on the sink either, because at the")
	fmt.Println("        sink there is nothing distinguishing string(\"hunter2\") at /db/password")
	fmt.Println("        from a legitimate default at any other address.")
	fmt.Println("      - so #10's redaction question has a #14 half that is not about")
	fmt.Println("        middleware at all: whether a secret may be a declared default. It")
	fmt.Println("        cannot be answered by wrapping a Sink.")
	seeded, _ := LoadOver(ctx, TConf{Name: "svc", DB: TDB{Host: "h", Password: "s3cr3t"}}, tFixedSource{vals: map[Path]Value{}})
	srec := newRecorder()
	_ = Dump(ctx, seeded, srec)
	fmt.Printf("    and the SEEDED route leaks identically: /db/password = %s\n",
		srec.vals[path("db", "password")].GoString())

	// ---------------------------------------------------------------- A2
	fmt.Println("\nA2  does the generated template LOAD back clean, and should it?")
	dir, _ := os.MkdirTemp("", "t14")
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "app.yaml")
	_ = os.WriteFile(f, []byte(art), 0o644)
	got, err := Load[TConf](ctx, FYAMLSource{Path: f}, WithSched(tAggregating))
	fmt.Printf("    Load of the emitted template: err=%v\n", err)
	fmt.Printf("    Name=%q  DB.Host=%q\n", got.Name, got.DB.Host)
	fmt.Println("    THIS IS THE DEFECT. The template writes `name: \"\"` at a required")
	fmt.Println("    address, ADR-0006 makes `required` a PRESENCE test, and the empty string")
	fmt.Println("    is present. So the starter config satisfies every `required` in the")
	fmt.Println("    schema and the service boots with an empty name and an empty DB host.")
	fmt.Println("    A template that fills in the required addresses defeats the exact")
	fmt.Println("    mechanism it exists to advertise.")

	fmt.Println("\n    the repair, and it is a comment-syntax capability again:")
	fmt.Println("    a required address must be emitted COMMENTED OUT, so it is Absent.")
	fmt.Println("    ADR-0006 measured that a commented-out line removes the key.")
	art2 := tCommentedTemplate(ctx, p)
	fmt.Println(prefixLines(art2, "      "))
	_ = os.WriteFile(f, []byte(art2), 0o644)
	got2, err2 := Load[TConf](ctx, FYAMLSource{Path: f}, WithSched(tAggregating))
	fmt.Printf("    Load of the commented template:\n%s\n", tIndent(errText(err2)))
	fmt.Printf("    value: Name=%q\n", got2.Name)
	fmt.Println("    Which is correct: the user is told exactly what to uncomment.")
	fmt.Println("    And it is unavailable on every plane in T5's `values only` rows, where")
	fmt.Println("    the only way to leave a required address Absent is to omit it, so the")
	fmt.Println("    seeded plane is one a Load refuses until a human edits it - which is")
	fmt.Println("    the right answer and looks like a broken tool.")

	// ---------------------------------------------------------------- A3
	fmt.Println("\nA3  `required` on a leaf inside an OPTIONAL subtree")
	fmt.Printf("    the required set the recipe found: %v\n", sortedPaths(keysOf(p.required)))
	fmt.Println("    /tls/cert is in it, and TLS is a *TTLS whose whole point is to be")
	fmt.Println("    omittable. Measured directly, against a plane holding a complete config")
	fmt.Println("    and no TLS section at all:")
	full := map[Path]Value{
		path("name"): String("svc"), path("db", "host"): String("h"),
	}
	_, err3 := Load[TConf](ctx, tFixedSource{vals: full}, WithSched(tAggregating))
	fmt.Printf("%s\n", tIndent(errText(err3)))
	fmt.Println("    So one `required` field anywhere beneath an optional pointer makes the")
	fmt.Println("    whole optional section MANDATORY, and nothing said so.")
	fmt.Println("    ADR-0006 states the neighbouring rule - \"an optional section stays")
	fmt.Println("    optional and its defaults fill holes in it once it exists\" - and it says")
	fmt.Println("    that about DEFAULTS. It never asks the same question about `required`,")
	fmt.Println("    and the answer the walk gives is the opposite one.")
	fmt.Println("    ADR-0011 has the machinery: it suppresses a required child under a")
	fmt.Println("    required PARENT. The rule it does not have is the one for a child under")
	fmt.Println("    an ABSENT optional parent. This is a finding against ADR-0006 and a")
	fmt.Println("    ticket rather than something #14 may decide.")

	// ---------------------------------------------------------------- A4
	fmt.Println("\nA4  `required` on a leaf under a DYNAMIC shape")
	fmt.Printf("    Compile[TDyn]: %v\n", Compile[TDyn]())
	_, err4 := Load[TDyn](ctx, tFixedSource{vals: map[Path]Value{}}, WithSched(tAggregating))
	fmt.Printf("    Load from an empty plane: %v\n", err4)
	pd, _ := tPlanFor[TDyn](ctx, tAggregating)
	fmt.Printf("    the recipe's required set: %v   addresses: %v\n", sortedPaths(keysOf(pd.required)), pd.addrs)
	fmt.Println("    `required` at /servers/*/host compiles, because ADR-0006 admits it on a")
	fmt.Println("    LEAF and the leaf is under a dynamic shape rather than being one. With")
	fmt.Println("    no members on the plane the leaf is never walked, so it never fires, so")
	fmt.Println("    a template never mentions it. It is a marker that is invisible until")
	fmt.Println("    somebody adds a map entry.")

	// ---------------------------------------------------------------- A5
	fmt.Println("\nA5  a required field the zero dump cannot reach")
	fmt.Printf("    Compile[TReqOmit]: %v\n", Compile[TReqOmit]())
	po, errO := tPlanFor[TReqOmit](ctx, tAggregating)
	fmt.Printf("    the recipe: err=%v addresses=%v required=%v\n",
		errO, po.addrs, sortedPaths(keysOf(po.required)))
	fmt.Println("    `required,omitzero` on one field is admissible (ADR-0008: omitzero is the")
	fmt.Println("    only option admissible at every type) and the two are in different")
	fmt.Println("    directions, so nothing refuses the pair. The zero dump omits the field,")
	fmt.Println("    so step 1 of the recipe never learns its boundary kind, and the recipe")
	fmt.Println("    falls back to String(\"\") - which happens to be right for a string and")
	fmt.Println("    would be a wrong-kind error on an int. Measured on the int case:")
	fmt.Printf("    Compile[TReqOmitInt]: %v\n", Compile[TReqOmitInt]())
	pi, errI := tPlanFor[TReqOmitInt](ctx, tAggregating)
	fmt.Printf("    the recipe: err=%v addresses=%v\n", errI, pi.addrs)

	// ---------------------------------------------------------------- A6
	fmt.Println("\nA6  determinism, which ADR-0001 makes a package-wide invariant")
	seen := map[string]int{}
	for range 100 {
		q, _ := tPlanFor[TConf](ctx, tAggregating)
		seen[tRealYAML(ctx, q)]++
	}
	fmt.Printf("    100 runs of the whole recipe: %d distinct artefacts\n", len(seen))

	// ---------------------------------------------------------------- A7
	fmt.Println("\nA7  the recipe against a schema that does not compile")
	fmt.Printf("    Compile[TBad]: %v\n", Compile[TBad]())
	_, errB := tPlanFor[TBad](ctx, tAggregating)
	fmt.Printf("    tPlanFor:      %v\n", errB)
	fmt.Println("    Correct, and it falls out of ADR-0010's control flow rather than from")
	fmt.Println("    anything the recipe does: schemaFor returns before Bind is called.")
}

// tCommentedTemplate is A2's repair: a required address is emitted as a
// commented-out line, so it is Absent to a Load.
// The address is emitted normally so the EMITTER gets the nesting right, then
// the line is commented out textually. That two-step is itself part of the
// finding: yaml.v3 has no "disabled entry" node, so even a plane with a
// comment syntax has no library-level notion of a key that is present in the
// document and absent to a reader. Every emitter has to do this by hand.
const tReqSentinel = "<<FERRY-REQUIRED-NO-DEFAULT>>"

func tCommentedTemplate(ctx context.Context, p tPlan) string {
	w := newYAMLTemplate()
	for _, a := range p.addrs {
		v := p.vals[a]
		if p.required[a] {
			v = String(tReqSentinel)
		}
		_ = w.Set(ctx, a, v)
		_ = w.Annotate(a, tNote{required: p.required[a]})
	}
	_ = w.Commit(ctx)

	var b strings.Builder
	b.WriteString("# Lines commented out below have no default and ferry refuses to load\n")
	b.WriteString("# until each one is present. Uncomment and set them.\n")
	for _, line := range strings.Split(strings.TrimRight(w.String(), "\n"), "\n") {
		if strings.Contains(line, tReqSentinel) {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			key := strings.SplitN(strings.TrimLeft(line, " "), ":", 2)[0]
			b.WriteString(indent + "# " + key + ":\n")
			continue
		}
		if strings.Contains(line, "# (REQUIRED)") {
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
