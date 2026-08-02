package main

// X6 and X8: the Close element, Dump's two phases, and the Committer branch.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// --- the sinks the ADR's table needs -----------------------------------------
//
// ADR-0011's first draft "used a sink that could only refuse a write, so it
// measured Set failures and reported the number as the cost of aggregating in
// general". Reproducing its corrected table therefore needs a sink that can
// refuse selectively, a struct that can fail to ENCODE, and both at once.

// x2Plane records what the sink actually did, which is the only thing the
// untouched-plane property can be checked against.
type x2Plane struct {
	attempts int
	written  []Path
	staged   []Path
	calls    []string
	closeErr error
}

func (p *x2Plane) reset() { *p = x2Plane{} }

func (p *x2Plane) writtenStr() string {
	if len(p.written) == 0 {
		return "(empty)"
	}
	s := make([]string, len(p.written))
	for i, a := range p.written {
		s[i] = a.String()
	}
	slices.Sort(s)
	return strings.Join(s, " ")
}

// x2RefusingSink is the non-staging sink: ADR-0004's "http PUT per key" row,
// and the one with something to lose.
type x2RefusingSink struct {
	store     *BStore
	plane     *x2Plane
	refuseAll bool
	refuse    map[string]bool
}

func (s x2RefusingSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return x2Writer{s.plane, s.refuseAll, s.refuse}, nil }, nil
}

type x2Writer struct {
	pl        *x2Plane
	refuseAll bool
	refuse    map[string]bool
}

func (w x2Writer) Set(_ context.Context, p Path, _ Value) error {
	if w.pl == nil {
		return nil
	}
	w.pl.attempts++
	if w.refuseAll || w.refuse[p.String()] {
		return errorsNew("kv: 403 no write ACL")
	}
	w.pl.written = append(w.pl.written, p)
	return nil
}

// x2StagingSink is ADR-0004's Committer: writes are staged and the plane only
// changes at Commit, which runs ONLY on success.
type x2StagingSink struct {
	plane  *x2Plane
	refuse map[string]bool
}

func (s x2StagingSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return &x2Stager{s.plane, s.refuse}, nil }, nil
}

type x2Stager struct {
	pl     *x2Plane
	refuse map[string]bool
}

func (w *x2Stager) Set(_ context.Context, p Path, _ Value) error {
	w.pl.attempts++
	if w.refuse[p.String()] {
		return errorsNew("kv: 403 no write ACL")
	}
	w.pl.staged = append(w.pl.staged, p)
	return nil
}

func (w *x2Stager) Commit(context.Context) error {
	w.pl.written = append(w.pl.written, w.pl.staged...)
	return nil
}

// x2ClosingSink is D14's fixture: Commit and Close both observable.
type x2ClosingSink struct {
	plane    *x2Plane
	failSet  bool
	failShut bool
}

func (s x2ClosingSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) {
		return &x2Closer{s.plane, s.failSet, s.failShut}, nil
	}, nil
}

type x2Closer struct {
	pl       *x2Plane
	failSet  bool
	failShut bool
}

func (w *x2Closer) Set(_ context.Context, p Path, _ Value) error {
	w.pl.calls = append(w.pl.calls, "set "+p.String())
	if w.failSet {
		return errorsNew("kv: no write ACL")
	}
	return nil
}

func (w *x2Closer) Commit(context.Context) error {
	w.pl.calls = append(w.pl.calls, "commit")
	return nil
}

func (w *x2Closer) Close() error {
	w.pl.calls = append(w.pl.calls, "close")
	if w.failShut {
		return errorsNew("kv: flush failed")
	}
	return nil
}

// --- X6 -----------------------------------------------------------------------

type X2One struct {
	A string `ferry:"a"`
}

func runX2f() {
	ctx := context.Background()
	saysX2("ADR-0004 / ADR-0011", `"Commit runs only when the walk succeeded, Close always runs."
	and
	"a Close failure has no location and explains nothing... Discarding the
	latter is silently ignoring something, which ADR-0001 forbids, so it is an
	element."`)

	for _, tc := range []struct {
		label            string
		failSet, failShut bool
	}{
		{"success", false, false},
		{"Set fails", true, false},
		{"Close fails", false, true},
		{"both fail", true, true},
	} {
		pl := &x2Plane{}
		err := Dump(ctx, X2One{A: "v"}, x2ClosingSink{pl, tc.failSet, tc.failShut})
		fmt.Printf("  %-12s calls=%v\n", tc.label, pl.calls)
		fmt.Printf("  %-12s err=%v\n", "", err)
		fmt.Printf("  %-12s elements=%d\n\n", "", len(Elements(err)))
	}
	fmt.Println("  BEFORE (#41 D14): `Close fails` reported err=<nil> - the failure vanished,")
	fmt.Println("  because SinkBinding.Dump wrote `defer rel.Close()` and dropped the result;")
	fmt.Println("  and `both fail` reported only the Set error, because final.go's joinErr")
	fmt.Println("  keeps the first and discards the second.")
	fmt.Println("\n  The moment being FIRST in the sort key is what puts it last in a mixed")
	fmt.Println("  report, which is the whole reason the key has three parts:")
	pl := &x2Plane{}
	_ = Dump(ctx, X2Five{}, x2ClosingSink{pl, true, true})
	mixed := join(
		errAt(mWalk, ErrValue, Path{}.Name("db").Name("host"), "is not a valid int"),
		fromDriver(mClose, Path{}, false, errorsNew("kv: flush failed")),
		fromDriver(mOpen, Path{}, false, errorsNew("kv: dial tcp: connection refused")))
	fmt.Printf("%+v\n", mixed)
}

// --- X8 -----------------------------------------------------------------------

// X2Eight is the ADR's eight-address struct. Two of the addresses are
// time.Time, which is the type that can fail to ENCODE while the plane is
// perfectly healthy: MarshalText refuses a year outside [0,9999].
//
// The FIELD ORDER is reconstructed from the ADR's own worked output rather than
// guessed: it prints `fail-fast plane: /Name /Region /Replicas` for the
// encode-failure column, so Started is the fourth field visited and Expires is
// later. Field order is what a fail-fast interleaved walk's numbers turn on,
// so getting it wrong makes the table unreproducible for a reason that has
// nothing to do with the policy.
type X2Eight struct {
	Name     string    `ferry:"Name"`
	Region   string    `ferry:"Region"`
	Replicas int       `ferry:"Replicas"`
	Started  time.Time `ferry:"Started"`
	Retries  int       `ferry:"Retries"`
	Endpoint string    `ferry:"Endpoint"`
	Bucket   string    `ferry:"Bucket"`
	Expires  time.Time `ferry:"Expires"`
}

func x2EightOK() X2Eight {
	return X2Eight{
		Name: "n", Region: "r", Bucket: "b", Endpoint: "e", Replicas: 3, Retries: 2,
		Started: time.Unix(0, 0).UTC(), Expires: time.Unix(1, 0).UTC(),
	}
}

// x2EightBadTimes puts two timestamps outside RFC 3339's year range.
func x2EightBadTimes() X2Eight {
	v := x2EightOK()
	v.Started = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	v.Expires = time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)
	return v
}

func runX2h() {
	ctx := context.Background()
	saysX2("ADR-0011", `"Dump encodes every address before it writes any of them. If anything
	fails to encode, every such failure is reported and NOTHING is written."
	and
	"Dump asks the sink whether it can stage. A Committer gets interleaved
	aggregation, because Commit runs only on success, so the plane is already
	untouched on failure. Everything else gets the encode phase."
	and the property: "If a Dump fails for a reason ferry could have known
	without touching the plane, the plane is untouched."`)

	twoAddrs := map[string]bool{"/Bucket": true, "/Region": true}

	fmt.Println("  ADR-0011's four-column table, attempts / written / errors, on a")
	fmt.Println("  non-staging sink. `two-phase, then aggregate` is the decided policy;")
	fmt.Println("  the other two rows are run through the same engine by selecting the")
	fmt.Println("  scheduler and, for the interleaved row, the staging code path.")
	fmt.Println()
	cols := []struct {
		name   string
		v      X2Eight
		refuse map[string]bool
		all    bool
	}{
		{"whole plane refuses", x2EightOK(), nil, true},
		{"two addresses refuse", x2EightOK(), twoAddrs, false},
		{"two cannot encode", x2EightBadTimes(), nil, false},
		{"both", x2EightBadTimes(), twoAddrs, false},
	}
	fmt.Printf("  %-26s", "policy")
	for _, c := range cols {
		fmt.Printf(" %-22s", c.name)
	}
	fmt.Println()

	for _, pol := range []struct {
		name string
		run  func(c int) (int, string, int)
	}{
		// The first two rows are the two policies the ADR weighed and did not
		// take, so both run through the INTERLEAVED code path - the staging
		// sink - and differ only in the scheduler. Reading `written` off the
		// stage is what shows the plane an interleaved NON-staging sink would
		// have been left holding, which is the number the table is about.
		{"fail-fast", func(i int) (int, string, int) {
			c := cols[i]
			pl := &x2Plane{}
			err := Dump(ctx, c.v, x2StagingSink{plane: pl, refuse: x2Merge(c.refuse, c.all)}, WithSched(serial))
			pl.written = pl.staged
			return pl.attempts, pl.writtenStr(), len(Elements(err))
		}},
		{"aggregate, interleaved", func(i int) (int, string, int) {
			c := cols[i]
			pl := &x2Plane{}
			err := Dump(ctx, c.v, x2StagingSink{plane: pl, refuse: x2Merge(c.refuse, c.all)})
			pl.written = pl.staged
			return pl.attempts, pl.writtenStr(), len(Elements(err))
		}},
		{"two-phase, then aggregate", func(i int) (int, string, int) {
			c := cols[i]
			pl := &x2Plane{}
			err := Dump(ctx, c.v, x2RefusingSink{plane: pl, refuseAll: c.all, refuse: c.refuse})
			return pl.attempts, pl.writtenStr(), len(Elements(err))
		}},
	} {
		fmt.Printf("  %-26s", pol.name)
		for i := range cols {
			a, w, e := pol.run(i)
			n := 0
			if w != "(empty)" {
				n = len(strings.Fields(w))
			}
			fmt.Printf(" %-22s", fmt.Sprintf("%d / %d / %d", a, n, e))
		}
		fmt.Println()
	}

	fmt.Println("\n  The third column shown as what the plane actually holds, which is")
	fmt.Println("  the case the ADR's first probe never built:")
	fmt.Println("    two time.Time fields outside RFC 3339's year range, plane healthy")
	for _, r := range []struct {
		name string
		opts []Option
	}{{"fail-fast", []Option{WithSched(serial)}}, {"aggregate", nil}} {
		pl := &x2Plane{}
		err := Dump(ctx, x2EightBadTimes(), x2StagingSink{plane: pl}, r.opts...)
		pl.written = pl.staged
		fmt.Printf("    %-12s plane: %-52s %d error(s)\n", r.name, pl.writtenStr(), len(Elements(err)))
	}
	pl := &x2Plane{}
	err := Dump(ctx, x2EightBadTimes(), x2RefusingSink{plane: pl})
	fmt.Printf("    %-12s plane: %-52s %d error(s)\n", "two-phase", pl.writtenStr(), len(Elements(err)))
	fmt.Println("    ^ two-phase gets both diagnostics and writes nothing, where")
	fmt.Println("      interleaved aggregation writes six addresses for a failure ferry")
	fmt.Println("      could have known about before touching the plane.")

	fmt.Println("\n  And the Committer's better ERROR SET, which is why the branch is not")
	fmt.Println("  merely an optimisation: a plane that both refuses two addresses AND")
	fmt.Println("  holds two unencodable values.")
	nonPl := &x2Plane{}
	nonErr := Dump(ctx, x2EightBadTimes(), x2RefusingSink{plane: nonPl, refuse: twoAddrs})
	fmt.Printf("    non-staging sink, Committer=false   two-phase    plane %-9s %d errors\n",
		nonPl.writtenStr(), len(Elements(nonErr)))
	stPl := &x2Plane{}
	stErr := Dump(ctx, x2EightBadTimes(), x2StagingSink{plane: stPl, refuse: twoAddrs})
	fmt.Printf("    staging sink,     Committer=true    interleaved  plane %-9s %d errors\n",
		stPl.writtenStr(), len(Elements(stErr)))
	for _, e := range Elements(stErr) {
		fmt.Printf("      %s\n", e)
	}
	fmt.Println("\n    So a flat sink pays for the untouched plane in ROUND TRIPS - the")
	fmt.Println("    ACL refusal only appears on the second run, after the timestamps are")
	fmt.Println("    fixed - and a Committer pays nothing for either property.")
	fixPl := &x2Plane{}
	fixErr := Dump(ctx, x2EightOK(), x2RefusingSink{plane: fixPl, refuse: twoAddrs})
	fmt.Printf("    second run, timestamps fixed        two-phase    plane %-9s %d errors\n",
		fixPl.writtenStr(), len(Elements(fixErr)))
}

func x2Merge(m map[string]bool, all bool) map[string]bool {
	if !all {
		return m
	}
	out := map[string]bool{}
	for _, k := range []string{"/Name", "/Region", "/Bucket", "/Endpoint", "/Replicas", "/Retries", "/Started", "/Expires"} {
		out[k] = true
	}
	return out
}
