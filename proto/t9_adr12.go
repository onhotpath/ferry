package main

// T9: reconciliation with ADR-0012 (#25), which landed in PR #40 while this
// ticket was running.
//
// The handoff's instruction is to reconcile explicitly rather than silently,
// the way ADR-0010 and ADR-0011 did. ADR-0012 changes two of #14's findings
// and confirms a third, and it does it through a section #14 was not watching:
// the presence observation is a `Source` wrapping a `Source`.

import (
	"context"
	"fmt"
	"slices"
)

// tObserving is ADR-0012's wrapper, written from the ADR's own description:
// "One `Source` wrapping another observes every boundary `Value` the load saw,
// including `Absent`." Core exports no Option, no callback and no report.
type tObserving struct {
	src FSource
	rec *tRecord
}

type tRecord struct {
	vals  map[Path]Value
	order []Path
}

func newRecord() *tRecord { return &tRecord{vals: map[Path]Value{}} }

func (r *tRecord) At(p Path) Value { return r.vals[p] }
func (r *tRecord) Addrs() []Path   { return sortedPaths(r.order) }

func (o tObserving) Bind(a *AddressSet) (FOpenFunc, error) {
	open, err := o.src.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		rd, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return tObservingReader{rd, o.rec}, nil
	}, nil
}

type tObservingReader struct {
	rd  FReader
	rec *tRecord
}

func (r tObservingReader) Get(ctx context.Context, p Path) (Value, error) {
	v, err := r.rd.Get(ctx, p)
	if _, dup := r.rec.vals[p]; !dup {
		r.rec.order = append(r.rec.order, p)
	}
	r.rec.vals[p] = v
	return v, err
}

func (r tObservingReader) Children(ctx context.Context, p Path) ([]Path, error) {
	if en, ok := r.rd.(FEnumerator); ok {
		return en.Children(ctx, p)
	}
	return nil, fmt.Errorf("ferry: %s: this source does not implement Enumerator", p)
}

func runT9() {
	ctx := context.Background()

	fmt.Println("(a) what ADR-0012's observing Source sees that the zero dump does not")
	zrec := newRecorder()
	_ = Dump(ctx, TConf{}, zrec)
	rec := newRecord()
	_, _ = Load[TConf](ctx, tObserving{tEmptySource{}, rec}, WithSched(tAggregating))

	dumped := zrec.addrs()
	observed := rec.Addrs()
	fmt.Printf("    zero Dump into a recording sink : %d addresses\n", len(dumped))
	fmt.Printf("    Load through an observing Source: %d addresses\n", len(observed))
	var onlyObserved []Path
	for _, a := range observed {
		if !slices.Contains(dumped, a) {
			onlyObserved = append(onlyObserved, a)
		}
	}
	fmt.Printf("    seen ONLY by the observation    : %v\n", onlyObserved)

	fmt.Println("\n(b) so T2's first `did not produce` is WRONG, and ADR-0012 is what fixes it")
	fmt.Printf("    /debug, an omitzero field at its zero value:\n")
	fmt.Printf("      in the zero Dump : %v\n", slices.Contains(dumped, path("debug")))
	fmt.Printf("      in the observation: %v, holding %s\n",
		slices.Contains(observed, path("debug")), rec.At(path("debug")).GoString())
	fmt.Println("    `omitzero` is a DUMP-side option (ADR-0008's direction table), so it")
	fmt.Println("    suppresses the Set call and does not suppress the Get. #14 measured the")
	fmt.Println("    dump side and concluded the address was unreachable; it is reachable on")
	fmt.Println("    the load side, and ADR-0012 is what makes that observable from outside")
	fmt.Println("    core rather than only inside the walk.")

	fmt.Println("\n(c) on the ZERO value the dump's set is a strict subset, and only there")
	var onlyDumped []Path
	for _, a := range dumped {
		if !slices.Contains(observed, a) {
			onlyDumped = append(onlyDumped, a)
		}
	}
	fmt.Printf("    seen ONLY by the zero dump: %v\n", onlyDumped)
	fmt.Println("    Neither set contains the other in general: a dump of a POPULATED value")
	fmt.Println("    mints /limits/rps, which an observation of an empty plane never sees,")
	fmt.Println("    and an observation sees an omitzero field, which no zero dump does.")
	fmt.Println("    So a template generator wants both, and it already runs both.")

	fmt.Println("\n(d) what it does NOT fix, which is the finding that survives")
	fmt.Println("    The observation is a map from address to boundary Value. It carries no")
	fmt.Println("    Go type, no declared default TEXT, and no required flag - it reports what")
	fmt.Println("    the PLANE said, and on an empty plane it says `absent` at every address.")
	fmt.Printf("      /db/port through the observation: %s\n", rec.At(path("db", "port")).GoString())
	fmt.Println("    So ADR-0012 improves the ADDRESS SET a template can enumerate and changes")
	fmt.Println("    nothing about the DECLARATIONS it wants to annotate them with.")

	fmt.Println("\n(e) three ADRs have now declined to reopen the read-only schema view")
	fmt.Println("    ADR-0001 left it open, \"reopened only if a concrete need survives the")
	fmt.Println("      dump-into-a-recording-sink pattern\";")
	fmt.Println("    ADR-0010 kept it closed, and its stated reason is the sentence T1")
	fmt.Println("      measured false: \"template generation reaches the defaults through a")
	fmt.Println("      recording sink\";")
	fmt.Println("    ADR-0012 kept it closed, and says so explicitly - \"this ADR is the ticket")
	fmt.Println("      ADR-0010 named as the one that might. It does not: a binding is not a")
	fmt.Println("      schema.\"")
	fmt.Println("    ADR-0012 also refuses #25's option (b) on the same ground: \"option (b) is")
	fmt.Println("    not a second shape on the driver, it is a request to export the compiled")
	fmt.Println("    schema\".")
	fmt.Println("    So #14 is now the only ticket standing where ADR-0001's condition can be")
	fmt.Println("    met, and T1 is the concrete need surviving the pattern ADR-0001 named.")

	fmt.Println("\n(f) does a held Binding change the recipe?")
	fmt.Println("    ADR-0012's Bind[T] hoists the schema lookup and the driver's Bind out of")
	fmt.Println("    the loop. The recipe's loop is 2 Loads, so it hoists 1 of 2 lookups; the")
	fmt.Println("    lookup is 34 ns against a compile in the tens of microseconds (ADR-0010).")
	fmt.Println("    It changes nothing about #14 and the recipe does not need it.")

	fmt.Println("\n(g) the one place ADR-0012 constrains a template GENERATOR")
	fmt.Println("    ADR-0012: a driver whose plane is obtained freshly per load takes it ONLY")
	fmt.Println("    from the context. A template for such a plane is a query string or a")
	fmt.Println("    response buffer, which nobody hands a user to edit, so DOCUMENTING never")
	fmt.Println("    meets that rule. SEEDING can: seeding a per-request sink means writing")
	fmt.Println("    defaults into a plane supplied by a context the generator constructs,")
	fmt.Println("    which is expressible and is nobody's use case today.")
}
