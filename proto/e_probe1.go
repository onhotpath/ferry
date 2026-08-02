package main

// E2-E4: the shape, and the two determinism questions.
//
// E2  does errors.AsType reach a type with no exported fields, through both
//     %w and Unwrap() []error, and does the pointer receiver close 5.14
// E3  (P3) sorting AT CONSTRUCTION against sorting in Format, measured against
//     what errors.AsType actually picks
// E4  (P6) is the three-part key a TOTAL order, including two errors at one
//     address, and does it survive an insertion order that is not deterministic

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

func init() { e9Hooks = append(e9Hooks, runE2, runE3, runE4) }

// ---------------------------------------------------------------------------
// E2
// ---------------------------------------------------------------------------

func runE2() {
	hdr("E2  the shape: AsType, an unexported field set, and 5.14")

	leaf := errAt(mWalk, ErrValue, path("db", "port"), "value did not parse as int")
	agg := join(
		errAt(mWalk, ErrMissing, path("tls", "cert"), "required, and the plane supplied nothing"),
		leaf,
	)
	wrapped := fmt.Errorf("loading config: %w", agg)

	e, ok := errors.AsType[*Error](wrapped)
	fmt.Printf("  AsType[*Error] through Unwrap()[]error + %%w : ok=%v addr=%s\n", ok, e.Address())
	fmt.Printf("  errors.Is(wrapped, ErrValue)                 : %v\n", errors.Is(wrapped, ErrValue))
	fmt.Printf("  errors.Is(wrapped, ErrMissing)               : %v\n", errors.Is(wrapped, ErrMissing))
	fmt.Printf("  errors.Is(wrapped, ErrSchema)                : %v\n", errors.Is(wrapped, ErrSchema))

	// The aggregate keeps errors.Join's meaning: Is answers "at least one
	// element is of this class", and counting is the caller's range loop.
	// Nothing departs from the stdlib here, so nothing has to be taught.
	n := 0
	for _, el := range Elements(agg) {
		if errors.Is(el, ErrValue) {
			n++
		}
	}
	fmt.Printf("  ...and how many elements are ErrValue        : %d of %d, by ranging\n", n, len(Elements(agg)))

	// 5.14: xload declares Error() on the VALUE and returns the POINTER, so
	// both forms satisfy `error` and the natural value form silently fails.
	var asAny any = Error{}
	_, valueImplements := asAny.(error)
	var asAny2 any = &Error{}
	_, ptrImplements := asAny2.(error)
	fmt.Printf("\n  5.14, reproduced against ferry's own type:\n")
	fmt.Printf("    Error  (value)   implements error : %v\n", valueImplements)
	fmt.Printf("    *Error (pointer) implements error : %v\n", ptrImplements)

	// And the same check against the one type already in this prototype that
	// carries a value receiver, which is how the defect gets in by accident.
	var u any = unsupportedTypeError{}
	_, uValue := u.(error)
	fmt.Printf("    the prototype's own unsupportedTypeError, value form: %v", uValue)
	if uValue {
		fmt.Printf("   <- the shape 5.14 warns about\n")
	} else {
		fmt.Println()
	}

	// Elements is uniform: one failure returns the leaf bare, and Elements
	// still hands the caller a one-element slice so the loop does not branch.
	one := join(leaf)
	fmt.Printf("\n  join(one error)      -> %T, Elements len=%d\n", one, len(Elements(one)))
	fmt.Printf("  join(two errors)     -> %T, Elements len=%d\n", agg, len(Elements(agg)))
	fmt.Printf("  join(nil, nil)       -> %v\n", join(nil, nil))
	fmt.Printf("  Elements(nil)        -> %v\n", Elements(nil))
	fmt.Printf("  ErrorAt(p, nil)      -> %v  (so `return ErrorAt(a, f())` is safe)\n", ErrorAt(path("x"), nil))
}

// ---------------------------------------------------------------------------
// E3 (P3). The claim under test: sorting only in Format leaves the
// PROGRAMMATIC reader nondeterministic while the printed form looks stable.
// ---------------------------------------------------------------------------

// formatSorted is the rejected design: hold insertion order, sort when printing.
type formatSorted struct{ errs []error }

func (l *formatSorted) Error() string {
	s := slices.Clone(l.errs)
	slices.SortStableFunc(s, compareErrs)
	var b strings.Builder
	for i, e := range s {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(locLabel(e))
	}
	return b.String()
}
func (l *formatSorted) Unwrap() []error { return l.errs }

// collected models a walk whose element order is not deterministic - today
// because a map is ranged, tomorrow because #20 made the walk concurrent.
func collected() []error {
	src := map[string]moment{"/z/last": mWalk, "/a/first": mWalk, "/m/mid": mWalk}
	var out []error
	for k, m := range src {
		segs := strings.Split(strings.TrimPrefix(k, "/"), "/")
		out = append(out, errAt(m, ErrValue, path(segs...), "did not parse"))
	}
	return out
}

func runE3() {
	hdr("E3  (P3) sort at construction, against sort in Format")

	const runs = 300
	pickedCons := map[string]int{}
	pickedFmt := map[string]int{}
	printedCons := map[string]int{}
	printedFmt := map[string]int{}

	for range runs {
		c := join(collected()...)
		e, _ := errors.AsType[*Error](c)
		pickedCons[e.Address().String()]++
		printedCons[c.Error()]++

		f := &formatSorted{errs: collected()}
		e2, _ := errors.AsType[*Error](f)
		pickedFmt[e2.Address().String()]++
		printedFmt[f.Error()]++
	}

	fmt.Printf("  %d runs of an identical three-error walk\n\n", runs)
	fmt.Printf("  sorted at construction   AsType picks %d distinct: %v\n", len(pickedCons), countsOf(pickedCons))
	fmt.Printf("                           prints       %d distinct\n", len(printedCons))
	fmt.Printf("  sorted in Format only    AsType picks %d distinct: %v\n", len(pickedFmt), countsOf(pickedFmt))
	fmt.Printf("                           prints       %d distinct\n", len(printedFmt))
	fmt.Println("\n  The second row is the finding: the printed form is stable and the")
	fmt.Println("  programmatic reader is not, so 5.5 would look fixed and would not be.")
}

func countsOf(m map[string]int) string {
	keys := slices.Sorted(maps_Keys(m))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// E4 (P6). Is the key total?
// ---------------------------------------------------------------------------

func runE4() {
	hdr("E4  (P6) the three-part key: is it a total order")

	// ADR-0006 measured ONE FIELD producing TWO errors at ONE address. Address
	// alone is therefore not a key, and insertion order is only a tiebreak for
	// as long as the walk stays serial - which #20 may change.
	twoAtOne := func() []error {
		return []error{
			errAt(mCompile, ErrSchema, path("origins"), "required is not available on []string"),
			errAt(mCompile, ErrSchema, path("origins"), "default is not available on []string"),
		}
	}
	seen := map[string]int{}
	for range 300 {
		a := twoAtOne()
		if len(a)%2 == 0 { // shuffle deterministically-but-differently by reversing half the time
			slices.Reverse(a)
		}
		seen[fmt.Sprintf("%+v", join(a...))]++
	}
	fmt.Printf("  two errors at one address, insertion order reversed: %d distinct report(s)\n", len(seen))

	// Under a concurrent walk the collection order is genuinely racy. The key
	// has to survive that without a lock, or 5.5 comes back.
	concurrent := func() error {
		var mu sync.Mutex
		var got []error
		var wg sync.WaitGroup
		for i, p := range []Path{path("z"), path("a"), path("m"), path("a", "b")} {
			wg.Add(1)
			go func(i int, p Path) {
				defer wg.Done()
				mu.Lock()
				got = append(got, errAt(mWalk, ErrValue, p, "did not parse"))
				mu.Unlock()
			}(i, p)
		}
		wg.Wait()
		return join(got...)
	}
	conc := map[string]int{}
	for range 300 {
		conc[fmt.Sprintf("%+v", concurrent())]++
	}
	fmt.Printf("  four errors collected by four goroutines:            %d distinct report(s)\n", len(conc))

	// The moment is the FIRST term, and this is the case that forced it: a
	// Close failure has no location and explains nothing, so a rule of
	// "location-less sorts first" alone would put it at the head of a report
	// it had nothing to do with.
	fmt.Println("\n  a failed dump that also failed to Close:")
	dumpish := join(
		errAt(mWalk, ErrValue, path("db", "port"), "value did not parse as int"),
		errAt(mWalk, ErrValue, path("db", "host"), "value did not parse as int"),
		fromDriver(mClose, Path{}, false, errors.New("kv: flush failed")),
		fromDriver(mOpen, Path{}, false, errors.New("kv: dial tcp: connection refused")),
	)
	fmt.Printf("%+v\n", dumpish)
	fmt.Println("  open precedes the walk errors it caused; close follows them.")
	fmt.Println("  Ordering on location alone put close at the top; the moment is why it is not.")
}
