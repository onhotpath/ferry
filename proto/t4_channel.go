package main

// T4: the annotation channel, and the half of it that is actually hard.
//
// L0 of T3 needed nothing. L1, L2 and L3 all needed TWO separate things: a way
// for the emitter to LEARN the annotation, and a way to WRITE it. They are not
// the same problem and the second is the easy one, which is the finding this
// probe exists to make explicit, because a design that only fixes the writing
// half looks complete and is not.

import (
	"context"
	"fmt"
	"reflect"
	"unsafe"
)

func runT4() {
	ctx := context.Background()

	fmt.Println("(a) can an annotation cross ADR-0004's Writer?")
	fmt.Println("    Set(ctx context.Context, addr Path, v Value) error")
	fmt.Printf("    Value is {kind VKind; text string}, %d bytes, comparable=%v\n",
		unsafe.Sizeof(Value{}), tComparable[Value]())
	fmt.Println("    There is no third parameter and no field to put one in. Adding one to")
	fmt.Println("    Value costs comparability, which ADR-0004 spent a section buying and")
	fmt.Println("    which the round-trip harness and the recording sink both rely on.")

	fmt.Println("\n(b) the optional-interface shape, discovered by assertion")
	var sink FSink = newYAMLTemplate()
	w, _ := mustOpen(ctx, sink)
	_, canAnnotate := w.(tAnnotator)
	kvw, _ := mustOpen(ctx, &tKVSink{vals: map[Path]Value{}})
	_, kvCanAnnotate := kvw.(tAnnotator)
	fmt.Printf("    yaml template writer implements Annotator: %v\n", canAnnotate)
	fmt.Printf("    kv writer implements Annotator:             %v\n", kvCanAnnotate)
	fmt.Println("    That is exactly ADR-0004's Committer/Releaser/Enumerator pattern and it")
	fmt.Println("    works. It costs a fourth optional interface and an amendment to ADR-0004.")

	fmt.Println("\n(c) but the emitter still has to LEARN the note, and that is the hard half")
	p, _ := tPlanFor[TConf](ctx, tAggregating)
	notes := tSecondWalkNotes()
	fmt.Println("    of the four things L2 prints, where each one comes from:")
	fmt.Printf("      the VALUE at /db/port         %-12s from the recording sink\n", p.vals[path("db", "port")].GoString())
	fmt.Printf("      REQUIRED at /db/host          %-12v from ADR-0011's error set\n", p.required[path("db", "host")])
	fmt.Printf("      the Go TYPE at /db/port       %-12s from NOWHERE in ferry's surface\n", notes[path("db", "port")].gotype)
	fmt.Printf("      the DECLARED default text     %-12s from NOWHERE in ferry's surface\n", notes[path("db", "port")].def)
	fmt.Println("    So an Annotator interface would give an emitter somewhere to put two")
	fmt.Println("    facts it cannot obtain. Fixing the writing half alone is not a fix.")

	fmt.Println("\n(d) the three ways to obtain the last two, priced")
	fmt.Println("    1. a second reflect walk in the emitter")
	fmt.Println("       works today, needs no ferry change, and is ADR-0010's walk-duplication")
	fmt.Println("       axis 1: two implementations of the field rule that nothing keeps in step.")
	fmt.Println("       Measured cost of the divergence, on this fixture:")
	tShowDivergence(ctx, p, notes)
	fmt.Println("    2. a read-only schema view exported by core")
	fmt.Println("       ADR-0001 left this open and said to reopen it \"only if a concrete need")
	fmt.Println("       survives the dump-into-a-recording-sink pattern\". T1 and T2 are that")
	fmt.Println("       need surviving. ADR-0010 kept it closed on a sentence T1 measured false.")
	fmt.Println("    3. a code generator that reads the Go source")
	fmt.Println("       has the tags AND the doc comments, and runs at build time rather than")
	fmt.Println("       at run time, so it is not a second authority inside one process. T6.")
}

// tShowDivergence reproduces the axis-1 defect against a REAL divergence
// rather than describing it: the compiler and the second walk disagree about
// an embedded field, because the second walk has to reimplement promotion and
// this one does it the obvious wrong way.
func tShowDivergence(ctx context.Context, p tPlan, notes map[Path]tNote) {
	plan, err := tPlanFor[TWithEmbed](ctx, tAggregating)
	fmt.Printf("       the compiler's address set  : %v  (err=%v)\n", plan.addrs, err)
	second := tWalkTags(reflect.TypeFor[TWithEmbed](), Path{}, map[Path]tNote{})
	fmt.Printf("       the second walk's           : %v\n", sortedPaths(keysOf(second)))
	fmt.Println("       The second walk skips the embedded field, because Lookup returns")
	fmt.Println("       ok=false for it and the obvious `continue` is wrong: ADR-0008 promotes")
	fmt.Println("       an untagged embedded field. The emitter then annotates /port and")
	fmt.Println("       silently leaves /env bare, with no error from anything.")

	// keep the unused params honest
	_ = p
	_ = notes
}

func mustOpen(ctx context.Context, s FSink) (FWriter, error) {
	open, err := s.Bind(NewAddressSet(nil))
	if err != nil {
		return nil, err
	}
	return open(ctx)
}

// --- a KV-shaped sink, for T4 and T5 ----------------------------------------
//
// Opaque bytes, no format, no comment syntax. It is ADR-0004's `kv` axis.

type tKVSink struct{ vals map[Path]Value }

func (s *tKVSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return s, nil }, nil
}
func (s *tKVSink) Set(_ context.Context, p Path, v Value) error { s.vals[p] = v; return nil }

func tComparable[T any]() bool { return reflect.TypeFor[T]().Comparable() }
