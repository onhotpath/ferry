package main

// T7: the API surface, LAST, because #14's body says to prototype the artefact
// first and the artefact is what rules three of these out.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// --- candidate (c), written as a real function ------------------------------
//
// This is the shape a `ferry/template` sub-module would export. It is here
// rather than in a design section because writing it is the measurement: every
// argument it needs that ferry cannot supply is a line in this signature.

type tEmitOpts struct {
	Prose map[Path]string // the caller's side table, because ferry has nowhere for prose
	Notes map[Path]tNote  // the SECOND WALK's output, because the schema is unexported
}

func tEmit[T any](ctx context.Context, w io.Writer, o tEmitOpts) error {
	p, err := tPlanFor[T](ctx, tAggregating)
	if err != nil {
		return err
	}
	tw := newYAMLTemplate()
	for _, a := range p.addrs {
		v := p.vals[a]
		if p.required[a] {
			v = String(tReqSentinel)
		}
		if err := tw.Set(ctx, a, v); err != nil {
			return err
		}
		n := o.Notes[a]
		n.required = p.required[a]
		n.prose = o.Prose[a]
		if err := tw.Annotate(a, n); err != nil {
			return err
		}
	}
	if err := tw.Commit(ctx); err != nil {
		return err
	}
	_, err = io.WriteString(w, tCommentOut(tw.String()))
	return err
}

// tCommentOut is T8's repair applied to an already-annotated document, so the
// prose and the type hints survive the pass. It is textual for the reason T8
// records: yaml.v3 has no disabled-entry node.
func tCommentOut(doc string) string {
	var b strings.Builder
	b.WriteString("# Lines commented out below have no default and ferry refuses to load\n")
	b.WriteString("# until each one is present. Uncomment and set them.\n")
	for _, line := range strings.Split(strings.TrimRight(doc, "\n"), "\n") {
		if strings.Contains(line, tReqSentinel) {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			key := strings.SplitN(strings.TrimLeft(line, " "), ":", 2)[0]
			b.WriteString(indent + "# " + key + ":\n")
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func runT7() {
	ctx := context.Background()

	fmt.Println("(a) `Dump` of a zero-valued struct with an Option, which the ticket names")
	fmt.Println("    Refuted by T1 and T2, on three counts, none of which an Option fixes:")
	fmt.Println("      the defaults are not applied on Dump (ADR-0006: a Load-side rule);")
	fmt.Println("      `required` is not knowable from a Dump at all;")
	fmt.Println("      an omitzero field and an empty composite are not in the output.")
	fmt.Println("    An Option on Dump could only change what Dump does with a value it has,")
	fmt.Println("    and the missing facts are not in the value.")

	fmt.Println("\n(b) a distinct entry point in CORE: ferry.Template[T](ctx, sink, opts...)")
	fmt.Println("    Fails ADR-0001's bucket rule rather than its veto. The veto passes -")
	fmt.Println("    an annotated starter artefact is not a configuration-only idea, an i18n")
	fmt.Println("    bundle wants one too. But nothing in it is un-buildable from outside")
	fmt.Println("    core once core exports the two facts T4(c) named, so Enabled stands and")
	fmt.Println("    the entry point does not belong in core.")

	fmt.Println("\n(c) a sub-module, ferry/template, at run time. Written above. The call site:")
	fmt.Println()
	fmt.Println("      err := template.Emit[Config](ctx, os.Stdout, template.Opts{")
	fmt.Println("          Prose: map[ferry.Path]string{ ... },")
	fmt.Println("          Notes: ???,")
	fmt.Println("      })")
	fmt.Println()
	fmt.Println("    `Notes` is the signature admitting the defect. It is the Go type and the")
	fmt.Println("    declared default text per address, and there is no ferry call that")
	fmt.Println("    returns it, so either the caller passes it or the sub-module walks the")
	fmt.Println("    type a second time. Run, with the second walk:")
	fmt.Println()
	if err := tEmit[TConf](ctx, os.Stdout, tEmitOpts{
		Prose: tProseSideTable(),
		Notes: tSecondWalkNotes(),
	}); err != nil {
		fmt.Println("    err:", err)
	}
	fmt.Println()
	fmt.Println("    It works. It is also two authorities for one field rule in one process,")
	fmt.Println("    which is the defect ADR-0010 wrote a whole axis about and ADR-0008 found")
	fmt.Println("    live in a real ferry prototype.")

	fmt.Println("\n(d) a command, cmd/ferry-template, reading the Go source")
	fmt.Println("    T6(b) measured that the prose only exists in the source, and the same")
	fmt.Println("    parse yields the tags and the types, so `Notes` stops being a parameter.")
	fmt.Println("    It is one authority per PROCESS rather than one per program: the")
	fmt.Println("    generator's reading of the tags can still drift from core's, but it")
	fmt.Println("    drifts at build time against a checked-in artefact rather than silently")
	fmt.Println("    at run time. And ADR-0002 already reserved `cmd/` naming this ticket.")
	fmt.Println("    What it costs: it cannot see a registered codec (ADR-0009's table is a")
	fmt.Println("    run-time value), so a type whose representation a Register decides is")
	fmt.Println("    one the generator must either refuse or guess. Measured:")
	fmt.Printf("      Compile[TOpaqueConf]() against a bare registry : %v\n",
		Compile[TOpaqueConf](WithRegistry(NewRegistry())))
	reg := NewRegistry()
	if err := reg.Register(StringCodec(
		func(o TOpaque) string { return strconv.Itoa(o.v) },
		func(s string) (TOpaque, error) { n, err := strconv.Atoi(s); return TOpaque{n}, err },
	)); err != nil {
		fmt.Println("      register:", err)
	}
	fmt.Printf("      Compile[TOpaqueConf]() with the codec          : %v\n", Compile[TOpaqueConf](WithRegistry(reg)))
	fmt.Println("      Whether that type is templatable at all is decided by a line of Go")
	fmt.Println("      in some init() the generator never runs. So a source-reading")
	fmt.Println("      generator has to refuse the type or guess its representation, and")
	fmt.Println("      guessing is ADR-0005's category 3 reintroduced at the tool.")

	fmt.Println("\n(e) so the answer to \"is it a distinct entry point or Dump with an Option\"")
	fmt.Println("    is neither, and the shape of the question is what needs revising:")
	fmt.Println("    SEEDING is Dump of the value T2's recipe produces and needs no entry")
	fmt.Println("    point at all; DOCUMENTING is a generator, and what decides whether it")
	fmt.Println("    lives in a sub-module or a command is the prose question, not the API.")
}
