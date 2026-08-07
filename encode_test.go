package ferry

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"
)

// This file is ADR-0011's Dump policy, measured on the fixture the ADR measured
// it on: eight addresses, four failure shapes, and the two aggregating rows of
// its table.
//
// Everything here goes through Dump. What the sink was asked and what it holds
// afterwards is the whole assertion, which is what makes the criterion about
// the property rather than the mechanism: whether the encode phase buffers its
// values or re-walks to produce them is not observable from out here, and
// nothing below asks.

// storeConf is the eight-address struct. The two timestamps are the two
// addresses that can fail to encode, because time.Time is the one type in
// core's set whose text form does not cover every value the type holds.
type storeConf struct {
	Name     string    `ferry:"name"`
	Region   string    `ferry:"region"`
	Replicas int       `ferry:"replicas"`
	Retries  int       `ferry:"retries"`
	Bucket   string    `ferry:"bucket"`
	Endpoint string    `ferry:"endpoint"`
	Expires  time.Time `ferry:"expires"`
	Started  time.Time `ferry:"started"`
}

var (
	// inRange is an ordinary instant, and outOfRange is one past RFC 3339's
	// year range, which MarshalText refuses.
	inRange    = time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	outOfRange = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
)

func storeFilled() storeConf {
	return storeConf{
		Name:     "svc",
		Region:   "eu-west-1",
		Replicas: 3,
		Retries:  5,
		Bucket:   "artifacts",
		Endpoint: "https://example.invalid",
		Expires:  inRange,
		Started:  inRange,
	}
}

// storeUnrepresentable is the same value with both timestamps outside the
// range, so the plane is asked nothing ferry could not have decided first.
func storeUnrepresentable() storeConf {
	v := storeFilled()
	v.Expires = outOfRange
	v.Started = outOfRange

	return v
}

// storeSink is a sink with the two knobs the four shapes need - a refusal per
// address, and a whole-plane outage - and one more the policy turns on: whether
// it can stage.
//
// Staging is a second Writer type rather than a flag on one, because Committer
// is discovered by assertion and a writer that has the method cannot
// demonstrate what a writer without it does.
type storeSink struct {
	staging bool
	refuse  map[Path]bool
	outage  error
	onClose error

	// attempts is every Set call and accepted is every Set the sink took, which
	// are ADR-0011's "attempts" and "written" columns.
	attempts []Path
	accepted []Path

	// durable is what the plane holds once the run is over. A staging sink
	// stages, so it reaches this only through Commit.
	durable map[Path]Value
	staged  map[Path]Value

	commits int
	closes  int
}

func newStoreSink(staging bool, refused ...Path) *storeSink {
	s := &storeSink{
		staging: staging,
		refuse:  map[Path]bool{},
		durable: map[Path]Value{},
		staged:  map[Path]Value{},
	}

	for _, at := range refused {
		s.refuse[at] = true
	}

	return s
}

func (s *storeSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) {
		if s.staging {
			return stagingWriter{s: s}, nil
		}

		return flatWriter{s: s}, nil
	}, nil
}

func (s *storeSink) set(at Path, v Value) error {
	s.attempts = append(s.attempts, at)

	if s.outage != nil {
		return s.outage
	}

	if s.refuse[at] {
		return errRefused
	}

	s.accepted = append(s.accepted, at)

	if s.staging {
		s.staged[at] = v
	} else {
		s.durable[at] = v
	}

	return nil
}

// ensure is a container-level write, priced as a write like any other: what a
// container says at its own address is one of the things a dump has to get past
// the plane, and a plane refusing it refuses it the same way.
func (s *storeSink) ensure(at Path, p Presence) error {
	if p != PresenceNull {
		return nil
	}

	return s.set(at, Null)
}

func (s *storeSink) close() error {
	s.closes++

	return s.onClose
}

// flatWriter cannot stage, and it holds a resource: it is a Releaser and not a
// Committer, which is what pins the policy to the interface that answers the
// question rather than to "has a lifecycle at all".
type flatWriter struct{ s *storeSink }

func (w flatWriter) Set(_ context.Context, at LeafAddr, v Value) error {
	return w.s.set(at.Path(), v)
}

// Ensure is the container-level write, routed through the same store so that a
// refusal at a container address is priced exactly as a refusal at a leaf.
func (w flatWriter) Ensure(_ context.Context, at Container, p Presence) error {
	return w.s.ensure(at.Path(), p)
}

func (w flatWriter) Close() error { return w.s.close() }

type stagingWriter struct{ s *storeSink }

func (w stagingWriter) Set(_ context.Context, at LeafAddr, v Value) error {
	return w.s.set(at.Path(), v)
}

func (w stagingWriter) Ensure(_ context.Context, at Container, p Presence) error {
	return w.s.ensure(at.Path(), p)
}

func (w stagingWriter) Close() error { return w.s.close() }

func (w stagingWriter) Commit(context.Context) error {
	w.s.commits++
	maps.Copy(w.s.durable, w.s.staged)

	return nil
}

// cell is one measurement: what the sink was asked, what it took, and how many
// elements the caller got.
type cell struct{ attempts, writes, errors int }

// failureShape is one column of ADR-0011's table.
type failureShape struct {
	name        string
	mint        func(staging bool) *storeSink
	value       storeConf
	interleaved cell
	twoPhase    cell
}

// TestDumpMatchesTheTwoAggregatingRows is ADR-0011's table, run rather than
// quoted: four failure shapes over an eight-address struct, against a sink that
// can stage and a sink that cannot.
//
// The two rows differ in one column and it is the one the phase exists for.
// Where two values have no representation, interleaving writes six addresses
// for a failure ferry could have known about without touching the plane, and
// the encode phase writes none. Where the failure is the plane's own, the two
// rows are identical, because an encode phase cannot know a refusal in advance
// and does not pretend to.
//
// Both rows are order-independent by construction: neither policy stops, so
// every number here is a set cardinality rather than the length of a prefix of
// the write order.
func TestDumpMatchesTheTwoAggregatingRows(t *testing.T) {
	t.Parallel()

	shapes := []failureShape{{
		name:        "the whole plane refuses",
		mint:        func(staging bool) *storeSink { return outaged(newStoreSink(staging)) },
		value:       storeFilled(),
		interleaved: cell{attempts: 8, writes: 0, errors: 8},
		twoPhase:    cell{attempts: 8, writes: 0, errors: 8},
	}, {
		name:        "two addresses refuse",
		mint:        func(staging bool) *storeSink { return newStoreSink(staging, At("region"), At("bucket")) },
		value:       storeFilled(),
		interleaved: cell{attempts: 8, writes: 6, errors: 2},
		twoPhase:    cell{attempts: 8, writes: 6, errors: 2},
	}, {
		name:        "two values cannot encode",
		mint:        func(staging bool) *storeSink { return newStoreSink(staging) },
		value:       storeUnrepresentable(),
		interleaved: cell{attempts: 6, writes: 6, errors: 2},
		twoPhase:    cell{attempts: 0, writes: 0, errors: 2},
	}, {
		name:        "both",
		mint:        func(staging bool) *storeSink { return newStoreSink(staging, At("region"), At("bucket")) },
		value:       storeUnrepresentable(),
		interleaved: cell{attempts: 6, writes: 4, errors: 4},
		twoPhase:    cell{attempts: 0, writes: 0, errors: 2},
	}}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			t.Run("a sink that can stage is interleaved", func(t *testing.T) {
				t.Parallel()
				mustMeasure(t, shape, true, shape.interleaved)
			})

			t.Run("a sink that cannot encodes first", func(t *testing.T) {
				t.Parallel()
				mustMeasure(t, shape, false, shape.twoPhase)
			})
		})
	}
}

func outaged(s *storeSink) *storeSink {
	s.outage = errOutage

	return s
}

// mustMeasure runs one cell, on a sink minted for this case alone.
func mustMeasure(t *testing.T, shape failureShape, staging bool, want cell) {
	t.Helper()

	s := shape.mint(staging)
	err := Dump(t.Context(), shape.value, s)

	got := cell{attempts: len(s.attempts), writes: len(s.accepted), errors: len(Elements(err))}
	if got != want {
		t.Errorf("attempts/written/errors is %+v, want %+v\n%+v", got, want, err)
	}

	// Every shape here fails, so no shape here commits: Commit runs only on
	// success, and Close runs whether the walk succeeded or not (ADR-0004).
	if s.commits != 0 {
		t.Errorf("committed %d times after a failed dump, want 0", s.commits)
	}

	if s.closes != 1 {
		t.Errorf("closed %d times, want 1", s.closes)
	}
}

// TestTwoUnencodableValuesLeaveThePlaneUntouched is the property the phase
// exists for, stated on its own: if a Dump fails for a reason ferry could have
// known without touching the plane, the plane is untouched.
func TestTwoUnencodableValuesLeaveThePlaneUntouched(t *testing.T) {
	t.Parallel()

	s := newStoreSink(false)

	err := Dump(t.Context(), storeUnrepresentable(), s)
	if n := len(Elements(err)); n != 2 {
		t.Fatalf("%+v holds %d elements, want both failures", err, n)
	}

	mustBeClass(t, err, ErrValue)

	if want := []Path{At("expires"), At("started")}; !slices.Equal(reportedAddresses(err), want) {
		t.Errorf("the report names %v, want %v", reportedAddresses(err), want)
	}

	if len(s.attempts) != 0 {
		t.Errorf("the sink was asked to write %v, want nothing", s.attempts)
	}

	if len(s.durable) != 0 {
		t.Errorf("the plane holds %v, want it empty", slices.Collect(maps.Keys(s.durable)))
	}
}

// TestAStagingSinkLearnsBothFailureKindsAtOnce is the reason the exemption is
// not merely an optimisation. A Committer gets a better error set, and the
// flat sink beside it is the cost of the untouched plane stated in round trips:
// it learns the refusals only after the timestamps are fixed.
func TestAStagingSinkLearnsBothFailureKindsAtOnce(t *testing.T) {
	t.Parallel()

	staging := newStoreSink(true, At("region"), At("bucket"))

	err := Dump(t.Context(), storeUnrepresentable(), staging)

	want := []Path{At("bucket"), At("expires"), At("region"), At("started")}
	if got := reportedAddresses(err); !slices.Equal(got, want) {
		t.Errorf("the staging sink reported %v, want %v\n%+v", got, want, err)
	}

	// The plane is untouched all the same, because Commit runs only on success.
	if staging.commits != 0 || len(staging.durable) != 0 {
		t.Errorf("committed %d times and the plane holds %d addresses, want 0 and 0",
			staging.commits, len(staging.durable))
	}

	flat := newStoreSink(false, At("region"), At("bucket"))
	if n := len(Elements(Dump(t.Context(), storeUnrepresentable(), flat))); n != 2 {
		t.Errorf("the flat sink reported %d elements, want the 2 it can know without the plane", n)
	}
}

// reportedAddresses is the report as the addresses it names, in the order the
// aggregate holds them, which is sorted at construction.
func reportedAddresses(err error) []Path {
	out := make([]Path, 0, len(Elements(err)))

	for _, e := range Elements(err) {
		if fe, ok := errors.AsType[*Error](e); ok {
			out = append(out, fe.Address())
		}
	}

	return out
}

// TestACloseFailureSortsAfterTheWalkErrors is the sort key's first term seen
// from the case it exists for: a close failure has no location and explains
// nothing, so "location-less sorts first" alone would put it at the head of a
// report it had nothing to do with.
func TestACloseFailureSortsAfterTheWalkErrors(t *testing.T) {
	t.Parallel()

	s := newStoreSink(false, At("region"), At("bucket"))
	s.onClose = errRefused

	err := Dump(t.Context(), storeFilled(), s)

	elements := Elements(err)
	if len(elements) != 3 {
		t.Fatalf("%+v holds %d elements, want the two refusals and the close failure", err, len(elements))
	}

	mustNameAnAddress(t, elements[:2])
	mustBeTheCloseFailure(t, elements[2])
}

func mustNameAnAddress(t *testing.T, elements []error) {
	t.Helper()

	for _, e := range elements {
		if fe, ok := errors.AsType[*Error](e); !ok || fe.Address() == (Path{}) {
			t.Errorf("%v is not a walk error at an address", e)
		}
	}
}

func mustBeTheCloseFailure(t *testing.T, err error) {
	t.Helper()

	last, ok := errors.AsType[*Error](err)
	if !ok || last.Address() != (Path{}) {
		t.Fatalf("the last element is %v, want the close failure, which has no location", err)
	}

	if got := last.Error(); got != "ferry: closing the plane: "+errRefused.Error() {
		t.Errorf("the close element reads %q", got)
	}
}

// TestASuccessfulDumpCommitsOnceAndWritesEachAddressOnce is the other half of
// the lifecycle, and the one assertion that would catch an encode phase
// replaying a write it had already made.
func TestASuccessfulDumpCommitsOnceAndWritesEachAddressOnce(t *testing.T) {
	t.Parallel()

	// Commit runs on success, and only where there is one to run: a sink that
	// cannot stage has no Commit for a successful dump to reach.
	t.Run("a sink that can stage", func(t *testing.T) {
		t.Parallel()
		mustDumpWhole(t, newStoreSink(true), 1)
	})

	t.Run("a sink that cannot", func(t *testing.T) {
		t.Parallel()
		mustDumpWhole(t, newStoreSink(false), 0)
	})
}

func mustDumpWhole(t *testing.T, s *storeSink, wantCommits int) {
	t.Helper()

	if err := Dump(t.Context(), storeFilled(), s); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if len(s.attempts) != 8 || len(s.durable) != 8 {
		t.Errorf("wrote %v, and the plane holds %d addresses, want each of the 8 once", s.attempts, len(s.durable))
	}

	if s.commits != wantCommits || s.closes != 1 {
		t.Errorf("committed %d and closed %d times, want %d and 1", s.commits, s.closes, wantCommits)
	}
}

// labelled is one map field, because a map and a slice are the only two shapes
// whose addresses come from the value rather than from the type.
type labelled struct {
	Name   string            `ferry:"name"`
	Labels map[string]string `ferry:"labels"`
}

// askedSink records what its writer was handed to prepare, and hands out a
// writer that stages or one that does not.
//
// Staging is a second Writer type rather than a flag on one, for [storeSink]'s
// reason: Committer is discovered by assertion, so a writer holding the method
// cannot demonstrate what a writer without it does.
type askedSink struct {
	staging bool
	asked   [][]Path
}

func (s *askedSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) {
		if s.staging {
			return askedStager{askedWriter{s: s}}, nil
		}

		return askedWriter{s: s}, nil
	}, nil
}

type askedWriter struct{ s *askedSink }

func (askedWriter) Set(context.Context, LeafAddr, Value) error { return nil }

// Unset takes the call and does nothing, which is what a fixture about the
// realised set owes the open: a schema holding a mapping is refused there
// against a writer that cannot forget an address, and this one is about what
// Prepare is handed rather than about that rung.
func (askedWriter) Unset(context.Context, CompositeAddr) error { return nil }

func (w askedWriter) Prepare(_ context.Context, addrs []Path) error {
	w.s.asked = append(w.s.asked, addrs)

	return nil
}

type askedStager struct{ askedWriter }

func (askedStager) Commit(context.Context) error { return nil }

// TestWhatAPreparingSinkIsHanded is the capability's contract in three parts:
// who is asked, when, and with what.
//
// The set is the addresses the value determined and none the type did, because
// the type's went to Bind and a driver that already holds them has no use for a
// second copy. It arrives sorted, so a driver refusing two of them reports them
// in one order over repeated runs (ADR-0001). And a sink that stages is not
// asked at all, because the phase that could ask it is the one ADR-0011 does not
// run for a Committer.
func TestWhatAPreparingSinkIsHanded(t *testing.T) {
	t.Parallel()

	v := labelled{Name: "svc", Labels: map[string]string{"b": "2", "a": "1"}}
	realised := []Path{At("labels", "a"), At("labels", "b")}

	for _, tc := range []struct {
		name    string
		staging bool
		want    [][]Path
	}{
		{name: "a sink that cannot stage is handed the realised set", want: [][]Path{realised}},
		{name: "a sink that stages is not asked", staging: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mustAskToPrepare(t, v, tc.staging, tc.want)
		})
	}
}

func mustAskToPrepare(t *testing.T, v labelled, staging bool, want [][]Path) {
	t.Helper()

	sink := &askedSink{staging: staging}
	if err := Dump(t.Context(), v, sink); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if !slices.EqualFunc(sink.asked, want, slices.Equal) {
		t.Errorf("the writer was asked to prepare %v, want %v", sink.asked, want)
	}
}
