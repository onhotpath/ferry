package main

// X1, X2, X3: the scheduler default and the error model.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// X2Five is the ADR's own five-field fixture, five leaves that can all fail.
type X2Five struct {
	A int `ferry:"a"`
	B int `ferry:"b"`
	C int `ferry:"c"`
	D int `ferry:"d"`
	E int `ferry:"e"`
}

func x2BadFive() map[Path]Value {
	m := map[Path]Value{}
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		m[Path{}.Name(k)] = String("x")
	}
	return m
}

// --- X1 ----------------------------------------------------------------------

func runX2a() {
	ctx := context.Background()
	saysX2("ADR-0011", `"ferry reports every failure that is not a consequence of another failure
	it is already reporting."
	and
	"No StopOnFirstError: it is a public knob whose only job is to make ferry
	report less. The survey recommends it for callers who want the old
	behaviour, and ferry has no old behaviour."`)

	fmt.Println("  five leaves, every one unparseable, through Load[T] with NO options:")
	_, err := Load[X2Five](ctx, x2FixedSource{x2BadFive()})
	fmt.Printf("    errors reported: %d\n", len(Elements(err)))
	fmt.Printf("    %%v  : %v\n", err)
	fmt.Printf("    %%+v :\n")
	for _, l := range strings.Split(fmt.Sprintf("%+v", err), "\n") {
		if strings.TrimSpace(l) != "" {
			fmt.Printf("      %s\n", l)
		}
	}

	fmt.Println("\n  and the same load asking for the scheduler the tip used to default to:")
	_, err2 := Load[X2Five](ctx, x2FixedSource{x2BadFive()}, WithSched(serial))
	fmt.Printf("    WithSched(serial) -> errors reported: %d   %v\n", len(Elements(err2)), err2)

	fmt.Println("\n  BEFORE (the tip, #41 D6): 1 error. AFTER: 5, without asking.")
	fmt.Println("  The seam ADR-0010 argued for is untouched - WithSched still selects -")
	fmt.Println("  and what moved is which end of it you get by default.")

	fmt.Println("\n  Dump takes the same default, which is 5.4's second half:")
	fmt.Println("    \"xload's serial path fails fast and its concurrent path aggregates,")
	fmt.Println("     so Concurrency(4) changes how many errors exist. ferry has one")
	fmt.Println("     collection rule at every moment and in both directions.\"")
	pl := &x2Plane{}
	e3 := Dump(ctx, X2Five{}, x2RefusingSink{plane: pl, refuseAll: true})
	fmt.Printf("    Dump onto a sink that refuses every write -> %d errors from %d attempts\n",
		len(Elements(e3)), pl.attempts)
	pl2 := &x2Plane{}
	e3b := Dump(ctx, X2Five{}, x2RefusingSink{plane: pl2, refuseAll: true}, WithSched(serial))
	fmt.Printf("    the same, WithSched(serial)              -> %d errors from %d attempts\n",
		len(Elements(e3b)), pl2.attempts)

	fmt.Println("\n  What item 1 changed about dropMissingUnder:")
	fmt.Println("    It deletes ErrMissing elements under an ABSENT optional *T, gated on")
	fmt.Println("    the presence bit, which is accumulated ACROSS SIBLINGS. Under `serial`")
	fmt.Println("    the first missing child aborts the sibling list, so the bit it reads is")
	fmt.Println("    a partial sum. Aggregation makes it a total one.")
	type X2Sect struct {
		First  string `ferry:"first,required"`
		Second string `ferry:"second"`
	}
	type X2Opt struct {
		Sect *X2Sect `ferry:"sect"`
	}
	// The plane supplies the SECOND field only. The first is required and
	// absent, so the section DOES exist and the failure is real.
	live := map[Path]Value{Path{}.Name("sect").Name("second"): String("s")}
	got, err4 := Load[X2Opt](ctx, x2FixedSource{live})
	fmt.Printf("    section present via its second field only:\n")
	fmt.Printf("      aggregating (default) -> %v   err=%v\n", x2Sect(got), err4)
	got2, err5 := Load[X2Opt](ctx, x2FixedSource{live}, WithSched(serial))
	fmt.Printf("      serial                -> %v   err=%v\n", x2Sect(got2), err5)
	fmt.Println()
	fmt.Println("    THIS IS THE MEASUREMENT. The section EXISTS - the plane supplied")
	fmt.Println("    /sect/second - so /sect/first being required and absent is a real")
	fmt.Println("    failure and not a consequence of an absent section.")
	fmt.Println("    Under `serial` the walk visits `first`, fails, and abandons `second`,")
	fmt.Println("    so the presence bit dropMissingUnder reads is a PARTIAL SUM reading")
	fmt.Println("    false, the ErrMissing is deleted, and the load returns a nil error")
	fmt.Println("    with Sect=nil. That is ADR-0001's \"silently ignoring\" reached through")
	fmt.Println("    a fix for the opposite defect.")
	fmt.Println("    Under aggregation every sibling runs, the bit is a total sum, and the")
	fmt.Println("    failure survives. So item 1 did not change dropMissingUnder's code at")
	fmt.Println("    all - it made the precondition the code already documented true by")
	fmt.Println("    default instead of true only if the caller passed WithSched.")
}

func x2Sect(v any) string {
	rv := reflect.ValueOf(v).Field(0)
	if rv.IsNil() {
		return "Sect=nil"
	}
	return fmt.Sprintf("Sect=%+v", rv.Elem().Interface())
}

// --- X2 ----------------------------------------------------------------------

// X2Secret is ADR-0011's own five-leaf fixture, on a plane where every value is
// a secret.
type X2Secret struct {
	MaxConns int           `ferry:"max_conns"`
	Timeout  time.Duration `ferry:"timeout"`
	Ratio    float64       `ferry:"ratio"`
	Enabled  bool          `ferry:"enabled"`
	Budget   int8          `ferry:"budget"`
}

const x2Leak = "AKIAIOSFODNN7EXAMPLE"

func runX2b() {
	ctx := context.Background()
	saysX2("ADR-0011", `"ferry's own message text never contains a value the plane supplied.
	The cause stays in the chain and is not printed."
	Measured there at "4 of 5 naive messages contain the plane's own text", on a
	plane class where every value is a secret.`)

	vals := map[Path]Value{
		Path{}.Name("max_conns"): String(x2Leak),
		Path{}.Name("timeout"):   String(x2Leak),
		Path{}.Name("ratio"):     String(x2Leak),
		Path{}.Name("enabled"):   String(x2Leak),
		Path{}.Name("budget"):    Number("99999"),
	}
	_, err := Load[X2Secret](ctx, x2FixedSource{vals})
	els := Elements(err)
	leaks := 0
	fmt.Printf("  the plane holds a secret at five addresses; ferry reports %d errors:\n", len(els))
	for _, e := range els {
		if strings.Contains(e.Error(), x2Leak) {
			leaks++
		}
		fmt.Printf("    %s\n", e)
	}
	fmt.Printf("\n  elements whose message contains the secret: %d of %d\n", leaks, len(els))

	fmt.Println("\n  and the naive form the tip shipped, for the same five:")
	nleaks := 0
	for _, k := range []string{"max_conns", "timeout", "ratio", "enabled", "budget"} {
		at := Path{}.Name(k)
		n := x2NodeFor(k)
		probe := reflect.New(n).Elem()
		e := decLeaf(vals[at], probe)
		naive := fmt.Errorf("ferry: %s: %w", at, e)
		if strings.Contains(naive.Error(), x2Leak) {
			nleaks++
		}
		fmt.Printf("    %s\n", naive)
	}
	fmt.Printf("\n  %d of 5 naive messages contain the plane's own text, which is\n", nleaks)
	fmt.Println("  ADR-0011's published number reproduced. The fifth carries the plane's")
	fmt.Println("  text too - `parsing \"99999\"` is a value the plane supplied - so the")
	fmt.Println("  rule is if anything stronger than the measurement that motivated it.")

	fmt.Println("\n  The cause is REACHABLE and never printed, which is what stops")
	fmt.Println("  redaction being a loss:")
	for _, e := range els {
		fmt.Printf("    %-42s ErrSyntax=%-5v ErrRange=%-5v ErrValue=%v\n",
			shortenX2(e.Error(), 42),
			errors.Is(e, strconv.ErrSyntax), errors.Is(e, strconv.ErrRange), errors.Is(e, ErrValue))
	}

	fmt.Println("\n  The two-entry hint table, which is the measured cost of the rule:")
	for _, tc := range []struct {
		typ reflect.Type
		val Value
	}{
		{reflect.TypeFor[time.Duration](), String("30")},
		{reflect.TypeFor[time.Time](), String("2026-08-02")},
	} {
		probe := reflect.New(tc.typ).Elem()
		e := decLeaf(tc.val, probe)
		fmt.Printf("    stdlib   %v\n", e)
		fmt.Printf("    ferry    ferry: /addr: %s\n\n", safeDecodeMsg(tc.val, tc.typ, e))
	}

	fmt.Println("  The carve-out ADR-0011 states rather than hides: a DYNAMIC ADDRESS")
	fmt.Println("  SEGMENT is plane-supplied and IS printed, because ferry cannot name the")
	fmt.Println("  address without it.")
	type X2Creds struct {
		Creds map[string]int `ferry:"creds"`
	}
	_, cerr := Load[X2Creds](ctx, x2FixedSource{map[Path]Value{
		Path{}.Name("creds").Name(x2Leak): String("nope"),
	}})
	fmt.Printf("    %v\n", cerr)
}

func x2NodeFor(k string) reflect.Type {
	switch k {
	case "max_conns":
		return reflect.TypeFor[int]()
	case "timeout":
		return reflect.TypeFor[time.Duration]()
	case "ratio":
		return reflect.TypeFor[float64]()
	case "enabled":
		return reflect.TypeFor[bool]()
	}
	return reflect.TypeFor[int8]()
}

func shortenX2(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// --- X3 ----------------------------------------------------------------------

func runX2c() {
	ctx := context.Background()
	saysX2("ADR-0011", `"type Error struct{ /* no exported fields */ }" with Error(), Unwrap(),
	Format(fmt.State, rune) and Address() Path, all on the POINTER receiver.
	"The classification is matched with errors.Is and nothing else."
	"ferry never calls errors.Join."
	"Sorted at construction, not in Format."`)

	fmt.Println("  the type, and 5.14 closed by construction:")
	var ev Error
	var ep *Error
	_, okV := any(ev).(error)
	_, okP := any(ep).(error)
	fmt.Printf("    Error  (value)   implements error : %v\n", okV)
	fmt.Printf("    *Error (pointer) implements error : %v\n", okP)
	fmt.Printf("    exported fields on Error          : %d\n", x2ExportedFields(reflect.TypeFor[Error]()))

	fmt.Println("\n  errors.AsType through both wrapping forms at once:")
	_, lerr := Load[X2Five](ctx, x2FixedSource{x2BadFive()})
	wrapped := fmt.Errorf("loading config: %w", lerr)
	e, ok := errors.AsType[*Error](wrapped)
	addr := Path{}
	if ok {
		addr = e.Address()
	}
	fmt.Printf("    errors.AsType[*Error](wrapped)  ok=%v  addr=%v\n", ok, addr)

	fmt.Println("\n  the vocabulary, matched by errors.Is alone:")
	for _, c := range []struct {
		name string
		err  error
	}{
		{"ErrSchema", ErrSchema}, {"ErrMissing", ErrMissing},
		{"ErrValue", ErrValue}, {"ErrPlane", ErrPlane}, {"ErrDriver", ErrDriver},
		{"ErrReadOnly (ADR-0004's, now a member)", ErrReadOnly},
	} {
		fmt.Printf("    %-40s %q\n", c.name, c.err.Error())
	}

	fmt.Println("\n  ErrorAt: it attaches and never classifies.")
	at := ErrorAt(Path{}.Name("somewhere"), errorsNew("driver said no"))
	_, isFerry := errors.AsType[*Error](at)
	fmt.Printf("    ErrorAt alone is a *ferry.Error : %v\n", isFerry)
	anyClass := false
	for _, c := range []error{ErrSchema, ErrMissing, ErrValue, ErrPlane} {
		anyClass = anyClass || errors.Is(at, c)
	}
	fmt.Printf("    ErrorAt alone matches any class : %v\n", anyClass)
	core := fromDriver(mWalk, Path{}.Name("db").Name("host"), true,
		ErrorAt(Path{}.Name("somewhere").Name("else"), errorsNew("boom")))
	fmt.Printf("    driver names /somewhere/else inside a Get at /db/host -> %v\n", core.Address())

	fmt.Println("\n  ferry never calls errors.Join, and this is why it is a rule:")
	joined := errors.Join(
		errAt(mWalk, ErrValue, Path{}.Name("a"), "one"),
		errAt(mWalk, ErrValue, Path{}.Name("b"), "two"))
	fmt.Printf("    errors.Join of two    -> Elements reports %d, and %d errors print\n",
		len(Elements(joined)), len(strings.Split(strings.TrimSpace(joined.Error()), "\n")))
	ours := join(
		errAt(mWalk, ErrValue, Path{}.Name("a"), "one"),
		errAt(mWalk, ErrValue, Path{}.Name("b"), "two"))
	fmt.Printf("    ferry's one constructor -> Elements reports %d\n", len(Elements(ours)))
	fmt.Println("    That was the SILENT failure the ADR records: Elements reporting one")
	fmt.Println("    element while two errors printed.")

	fmt.Println("\n  Sorted AT CONSTRUCTION, not in Format. 300 runs of an identical")
	fmt.Println("  three-error set whose collection order is not deterministic:")
	sortedPicks, sortedPrints := map[string]int{}, map[string]bool{}
	unsortedPicks, unsortedPrints := map[string]int{}, map[string]bool{}
	for range 300 {
		els := x2Shuffled()
		s := join(els...)
		var se *Error
		errors.As(s, &se)
		sortedPicks[se.Address().String()]++
		sortedPrints[s.Error()] = true

		u := &errorList{errs: slices.Clone(els)} // no sort: the Format-only reading
		var ue *Error
		errors.As(u, &ue)
		unsortedPicks[ue.Address().String()]++
		unsortedPrints[x2SortedPrint(u)] = true
	}
	fmt.Printf("    %-26s %-22s %s\n", "", "what AsType picks", "what it prints")
	fmt.Printf("    %-26s %-22s %d distinct\n", "sorted at construction",
		fmt.Sprintf("%d distinct %v", len(sortedPicks), x2Counts(sortedPicks)), len(sortedPrints))
	fmt.Printf("    %-26s %-22s %d distinct\n", "sorted in Format only",
		fmt.Sprintf("%d distinct %v", len(unsortedPicks), x2Counts(unsortedPicks)), len(unsortedPrints))
	fmt.Println("    The second row is the finding: the printed form is stable and the")
	fmt.Println("    programmatic reader is not, so 5.5 would look fixed and would not be.")

	fmt.Println("\n  The three-part key, with the moment first because of Close:")
	rep := join(
		fromDriver(mOpen, Path{}, false, errorsNew("kv: dial tcp: connection refused")),
		errAt(mWalk, ErrValue, Path{}.Name("db").Name("host"), "is not a valid int"),
		errAt(mWalk, ErrValue, Path{}.Name("db").Name("port"), "is not a valid int"),
		fromDriver(mClose, Path{}, false, errorsNew("kv: flush failed")))
	fmt.Printf("%+v\n", rep)

	fmt.Printf("  Error() is one line naming the addresses; %%+v is the report:\n")
	fmt.Printf("    %%v -> %v\n", rep)
	many := make([]error, 40)
	for i := range many {
		many[i] = errAt(mWalk, ErrMissing, Path{}.Name("svc").Name(fmt.Sprintf("f%02d", i)), "required")
	}
	fmt.Printf("    at forty: loading config: %v\n", join(many...))

	fmt.Println("\n  DiffErrors, the exact-set assertion over (address, class):")
	fmt.Printf("    exact match      -> %v\n", DiffErrors(lerr,
		Want{Path{}.Name("a"), ErrValue}, Want{Path{}.Name("b"), ErrValue},
		Want{Path{}.Name("c"), ErrValue}, Want{Path{}.Name("d"), ErrValue},
		Want{Path{}.Name("e"), ErrValue}))
	fmt.Printf("    one want missing -> %v\n", DiffErrors(lerr,
		Want{Path{}.Name("a"), ErrValue}, Want{Path{}.Name("b"), ErrValue}))
}

func x2ExportedFields(t reflect.Type) int {
	n := 0
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			n++
		}
	}
	return n
}

func x2Shuffled() []error {
	els := []error{
		errAt(mWalk, ErrValue, Path{}.Name("alpha"), "is not a valid int"),
		errAt(mWalk, ErrValue, Path{}.Name("beta"), "is not a valid int"),
		errAt(mWalk, ErrValue, Path{}.Name("gamma"), "is not a valid int"),
	}
	rand.Shuffle(len(els), func(i, j int) { els[i], els[j] = els[j], els[i] })
	return els
}

// x2SortedPrint is what a Format-only implementation would render: the sort
// applied at print time, over a list that was never ordered.
func x2SortedPrint(l *errorList) string {
	cp := slices.Clone(l.errs)
	slices.SortStableFunc(cp, compareErrs)
	return (&errorList{errs: cp}).Error()
}

func x2Counts(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, fmt.Sprintf("%s:%d", k, m[k]))
	}
	return out
}

// --- shared apparatus ---------------------------------------------------------

// x2FixedSource is the memory plane as a Source, which is what lets every probe
// above run through the entry point rather than through a helper.
type x2FixedSource struct{ vals map[Path]Value }

func (s x2FixedSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return mapReader{s.vals}, nil }, nil
}
