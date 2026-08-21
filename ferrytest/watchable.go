package ferrytest

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/onhotpath/ferry"
)

// WatchPlane describes a watchable plane to [Watchable]: a source to bind, and
// the things only the driver's own test knows how to do to the plane behind it.
//
// It is a struct rather than a parameter list because a suite that grows a case
// must not change its own signature, which is the rule [Plane] already follows.
type WatchPlane struct {
	// Name is the driver, and it opens every failure this suite reports.
	Name string

	// Open mints a fresh watchable source over the one plane this description
	// is about. It is called once per case, so no case inherits a source, a
	// watch or a handle another one left behind.
	//
	// The plane itself persists across every Open, and what it holds is what
	// the last Change wrote. That is what lets a case put a value on the plane
	// before it opens anything and expect the stream to open with it.
	//
	// The plane holds one address, host, carrying the text this suite writes
	// through Change.
	Open func() ferry.WatchableSource

	// Change makes one change to the plane, setting host to the given text. It
	// is the only way this suite has of moving a plane it knows nothing else
	// about.
	//
	// It changes the plane and not a source: a case may change the plane before
	// it opens anything, so that the stream has a value to open with, and a
	// plane that must exist before it can be watched is what the first Change
	// creates.
	Change func(to string)

	// Lose takes the watch away the way the mechanism loses it: the directory
	// removed, the connection dropped, the registration refused.
	//
	// It is optional, and a driver that cannot lose a watch leaves it nil and
	// the case is skipped out loud. A driver that can lose a watch and does not
	// declare it here is the one this suite cannot check, and it is the failure
	// the whole seam exists to prevent.
	Lose func()

	// Unwatchable mints a source of this driver that is watchable by type and
	// cannot be watched by configuration: a source naming no file, a handle
	// that reports no changes.
	//
	// It is optional. A driver with no such state leaves it nil and the case is
	// skipped out loud, rather than an unwatchable instance being invented for
	// it.
	Unwatchable func() ferry.WatchableSource

	// Settle is how long this driver may take to notice a change. It is the one
	// number a driver supplies, because it is the one thing a suite cannot
	// know: a notification queue answers in milliseconds and a poll answers in
	// its interval. Zero takes five seconds.
	Settle time.Duration
}

// watchTarget is what every case in this suite loads.
//
// The field is optional, so a plane holding nothing yet is a value rather than
// a failure, which is what lets the first case assert that the stream opens
// with a load (ADR-0020).
type watchTarget struct {
	Host string `ferry:"host"`
}

// Watchable is the conformance suite for a driver that can be watched: seven
// cases over one plane, and the whole of what the author of a watchable driver
// writes.
//
//	func TestWatchConformance(t *testing.T) {
//	    ferrytest.Watchable(t, ferrytest.WatchPlane{
//	        Name:        "yaml",
//	        Open:        func() ferry.WatchableSource { ... },
//	        Change:      func(to string) { ... },
//	        Lose:        func() { ... },
//	        Unwatchable: func() ferry.WatchableSource { ... },
//	        Settle:      3 * time.Second,
//	    })
//	}
//
// There is one call and no menu, for the reason [Driver] gives: a suite a
// driver author can partially adopt measures nothing.
//
// It asserts the seven properties a watchable driver owes its caller: the
// stream opens with a load, a change reloads, a burst is one reload, a held
// value never moves, cancelling ends it cleanly, a lost watch ends it with a
// reason, and a source that cannot be watched is refused at the bind. Two of
// them need a state some drivers cannot reach, and they are skipped, out loud,
// where [WatchPlane.Lose] or [WatchPlane.Unwatchable] is nil.
//
// It asserts through [ferry.BindWatched] and the stream and through nothing
// else, and it matches every error with [errors.Is] rather than on message
// text.
//
// It takes no [context.Context]. Each case owns the one context its own stream
// runs under, because ending that context is one of the properties above.
//
// # A new case does not break a driver
//
// This suite may gain cases in a minor release of ferry, exactly as [Driver]
// may, so a driver that passed yesterday can fail today with nothing in the Go
// toolchain having warned you first. A new case does not break a driver, it
// reports that the driver was already broken.
func Watchable(t T, p WatchPlane) {
	t.Helper()

	if p.Open == nil || p.Change == nil {
		t.Errorf("plane %s: Open and Change are what this suite needs, and one of them is nil", p.Name)

		return
	}

	w := &watchSuite{rep: t, plane: &p}
	w.run()
}

// watchSuite is one Watchable call, carried down to the cases so that each of
// them is a method with no parameter list of its own.
type watchSuite struct {
	rep   reporter
	plane *WatchPlane
}

// run is the seven cases, in the order ADR-0020 lists them.
func (w *watchSuite) run() {
	w.rep.Helper()

	w.caseOpensWithALoad()
	w.caseReloadsOnAChange()
	w.caseCoalescesABurst()
	w.caseHoldsValuesStill()
	w.caseEndsOnCancel()
	w.caseEndsOnLoss()
	w.caseRefusesTheUnwatchable()
}

// The seven cases, numbered in the order [Watchable] runs them, so a driver
// author reading their own CI output knows which of the seven went red
// (ADR-0014).
const (
	watchCaseOpensNo = iota + 1
	watchCaseReloadNo
	watchCaseBurstNo
	watchCaseHoldsNo
	watchCaseCancelNo
	watchCaseLossNo
	watchCaseUnwatchableNo
)

// watchDefaultSettle is how long a case waits where the plane named no window.
// It is
// generous on purpose: a suite that fails because a machine was busy teaches a
// driver author nothing.
const watchDefaultSettle = 5 * time.Second

// settle is how long a case waits for this driver to notice something.
func (w *watchSuite) settle() time.Duration {
	if w.plane.Settle <= 0 {
		return watchDefaultSettle
	}

	return w.plane.Settle
}

// failf is what every case reports through, and it names the plane and the
// case for the reason [driverRun.fail] does.
//
// The caller's text is formatted before it reaches the reporter, so a driver
// whose name holds a percent sign is a name and not a verb.
func (w *watchSuite) failf(n int, format string, args ...any) {
	w.rep.Helper()

	w.rep.Errorf("plane %s: case %d: %s", w.plane.Name, n, fmt.Sprintf(format, args...))
}

// skip says out loud that a case did not run, through [logTo].
func (w *watchSuite) skip(n int, why string) {
	w.rep.Helper()

	logTo(w.rep, "plane %s: case %d skipped: %s", w.plane.Name, n, why)
}

// caseOpensWithALoad is the first property: a stream begins with a value and
// not with a wait, so a caller writes no separate first load.
//
// It is the one case that opens its own stream, because every other case needs
// that first value and this one is where its absence is reported.
func (w *watchSuite) caseOpensWithALoad() {
	w.rep.Helper()

	w.plane.Change("first")

	s := w.open()
	if s == nil {
		return
	}

	defer s.stop(w.settle())

	if _, ok := s.next(w.settle()); !ok {
		w.failf(watchCaseOpensNo, "the stream yielded no value before the first change, so a caller has "+
			"to load once themselves and there are two idioms for the first value")
	}
}

// onOpenStream opens a stream, takes the value it opens with, and runs body
// over both, releasing the stream afterwards.
//
// A stream that will not bind, or that opens with no value, is reported by the
// bind and by case one respectively, so body does not run and this reports
// nothing: a driver that fails the first property would otherwise fail all six
// of the ones built on it, and six reports for one defect name the wrong one
// five times.
func (w *watchSuite) onOpenStream(body func(s *watchRun, first watchTarget)) {
	w.rep.Helper()

	s := w.open()
	if s == nil {
		return
	}

	defer s.stop(w.settle())

	first, ok := s.next(w.settle())
	if !ok {
		return
	}

	body(s, first)
}

// caseReloadsOnAChange is the second: a change the plane makes reaches the
// caller as a freshly loaded value.
func (w *watchSuite) caseReloadsOnAChange() {
	w.rep.Helper()

	w.onOpenStream(func(s *watchRun, _ watchTarget) {
		w.plane.Change("second")

		v, ok := s.next(w.settle())
		if !ok {
			w.failf(watchCaseReloadNo, "a change to the plane produced no reload")

			return
		}

		if v.Host != "second" {
			w.failf(watchCaseReloadNo, "the reload holds %q, want second: a reload is a load, and it "+
				"reads what the plane holds now", v.Host)
		}
	})
}

// watchBurst is how many changes one save stands for in the coalescing case.
const watchBurst = 5

// caseCoalescesABurst is the third: a burst is one reload, because a mechanism
// that reports its own bursts makes every consumer write the same settle
// window.
func (w *watchSuite) caseCoalescesABurst() {
	w.rep.Helper()

	w.onOpenStream(func(s *watchRun, _ watchTarget) {
		for range watchBurst {
			w.plane.Change("burst")
		}

		if _, ok := s.next(w.settle()); !ok {
			w.failf(watchCaseBurstNo, "a burst of %d changes produced no reload", watchBurst)

			return
		}

		w.noThirdReload(s)
	})
}

// noThirdReload is the other half of the coalescing case: one further reload is
// legal, because a burst may split across the window, and two are not, because
// a mechanism reporting every event of one save has not coalesced at all.
func (w *watchSuite) noThirdReload(s *watchRun) {
	w.rep.Helper()

	if _, ok := s.next(w.settle()); !ok {
		return
	}

	if _, again := s.next(w.settle()); again {
		w.failf(watchCaseBurstNo, "a burst of %d changes produced at least three reloads, so nothing "+
			"coalesces it and every consumer has to", watchBurst)
	}
}

// caseHoldsValuesStill is the fourth: a value handed out earlier never changes
// underneath the goroutine holding it.
//
// It is the weakest of the seven and it is kept, because it is the one that
// would catch a driver that hands the engine a buffer it goes on writing to.
// What it can see is bounded by what the target holds: a struct of one string
// is copied whole, so this passes for a plane that cannot reach the failure at
// all (ADR-0020).
func (w *watchSuite) caseHoldsValuesStill() {
	w.rep.Helper()

	w.plane.Change("held")

	w.onOpenStream(func(s *watchRun, held watchTarget) {
		w.plane.Change("moved")

		if _, ok := s.next(w.settle()); !ok {
			return
		}

		if held.Host != "held" {
			w.failf(watchCaseHoldsNo, "the value held from the first load now says %q, so a reload "+
				"wrote into a value somebody else was reading", held.Host)
		}
	})
}

// caseEndsOnCancel is the fifth: one context is the whole lifetime, and
// cancelling it ends the stream cleanly.
func (w *watchSuite) caseEndsOnCancel() {
	w.rep.Helper()

	w.onOpenStream(func(s *watchRun, _ watchTarget) {
		s.cancel()

		ended, err := s.ended(w.settle())
		if !ended {
			w.failf(watchCaseCancelNo, "cancelling the context did not end the stream, so there is a "+
				"second lifetime somewhere")

			return
		}

		if !errors.Is(err, context.Canceled) {
			w.failf(watchCaseCancelNo, "a cancelled stream ended with %v, want the cancellation", err)
		}
	})
}

// caseEndsOnLoss is the sixth, and it is the one the whole seam exists for: a
// watch the mechanism cannot keep ends the stream with a reason, rather than
// leaving a process holding stale configuration with nothing to tell it so.
func (w *watchSuite) caseEndsOnLoss() {
	w.rep.Helper()

	if w.plane.Lose == nil {
		w.skip(watchCaseLossNo, "the plane declares no way to lose this driver's watch, so the ending a "+
			"lost watch reports is not asserted here")

		return
	}

	w.onOpenStream(func(s *watchRun, _ watchTarget) {
		w.plane.Lose()
		w.endsWithAReason(s)
	})
}

// endsWithAReason is what a lost watch owes the caller: an ending, and one that
// [errors.Is] can tell from a clean one.
//
// The plane's next load failing is the other legal answer, because a mechanism
// that dies with the plane it watched may report the death as the load's error
// rather than the registration's (ADR-0020).
func (w *watchSuite) endsWithAReason(s *watchRun) {
	w.rep.Helper()

	ended, err := s.ended(w.settle())
	if !ended {
		w.failf(watchCaseLossNo, "the watch was lost and the stream did not end, so a process holding "+
			"stale configuration has nothing to tell it so")

		return
	}

	if !errors.Is(err, ferry.ErrWatchLost) && !errors.Is(err, ferry.ErrPlane) {
		w.failf(watchCaseLossNo, "a lost watch ended the stream with %v, want ferry.ErrWatchLost or a "+
			"plane failure the next load reported", err)
	}
}

// caseRefusesTheUnwatchable is the seventh, and the bind-seam half: a source of
// this driver that cannot be watched is refused before any load.
//
// What it matches is the class and not a sentinel, because the sentinel is the
// driver's own and this suite does not know it. A driver's own test is where
// that match belongs, and it is one line beside this call (ADR-0020).
func (w *watchSuite) caseRefusesTheUnwatchable() {
	w.rep.Helper()

	if w.plane.Unwatchable == nil {
		w.skip(watchCaseUnwatchableNo, "the plane mints no source that is watchable by type and "+
			"unwatchable by configuration, so the refusal at the bind is not asserted here")

		return
	}

	_, err := ferry.BindWatched[watchTarget](w.plane.Unwatchable())
	if !errors.Is(err, ferry.ErrPlane) {
		w.failf(watchCaseUnwatchableNo, "binding a source that cannot be watched gave %v, want a plane "+
			"refusal before any load, carrying this driver's own reason", err)
	}
}

// watchRun is one stream under test: the values, the ending, and the one
// context that owns both.
type watchRun struct {
	values chan watchTarget
	done   chan struct{}
	errf   func() error
	cancel context.CancelFunc
}

// open binds a fresh source and ranges it on a goroutine of its own.
//
// A refusal here is reported against the case that asked for the stream in
// every case but the first, and the number is the first's because a source
// that will not bind fails every one of them for the same reason.
func (w *watchSuite) open() *watchRun {
	w.rep.Helper()

	wb, err := ferry.BindWatched[watchTarget](w.plane.Open())
	if err != nil {
		w.failf(watchCaseOpensNo, "BindWatched refused a source this plane says is watchable: %v", err)

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	seq, errf := wb.Watch(ctx)

	return startWatchRun(ctx, cancel, seq, errf)
}

// startWatchRun ranges the sequence and hands values over one at a time.
func startWatchRun(ctx context.Context, cancel context.CancelFunc,
	seq iter.Seq[watchTarget], errf func() error,
) *watchRun {
	r := &watchRun{values: make(chan watchTarget), done: make(chan struct{}), errf: errf, cancel: cancel}

	go func() {
		defer close(r.done)

		for v := range seq {
			select {
			case r.values <- v:
			case <-ctx.Done():
				return
			}
		}
	}()

	return r
}

// next takes one value, reporting whether one arrived inside the window.
func (r *watchRun) next(within time.Duration) (watchTarget, bool) {
	timer := time.NewTimer(within)
	defer timer.Stop()

	select {
	case v := <-r.values:
		return v, true
	case <-r.done:
		return watchTarget{}, false
	case <-timer.C:
		return watchTarget{}, false
	}
}

// ended waits for the range to exit, reporting what ended it.
//
// It drains values while it waits, because a stream that yields one more value
// before it ends is not a stream that failed to end.
func (r *watchRun) ended(within time.Duration) (ended bool, cause error) {
	timer := time.NewTimer(within)
	defer timer.Stop()

	for {
		select {
		case <-r.done:
			return true, r.errf()
		case <-r.values:
		case <-timer.C:
			return false, nil
		}
	}
}

// stop ends the stream and waits for the goroutine ranging it, so no case
// leaves one behind for the next.
//
// The wait is bounded by the same window every case uses, because a driver
// whose stream ignores its context is a failure this suite has already
// reported and must not hang on top of.
func (r *watchRun) stop(within time.Duration) {
	r.cancel()
	_, _ = r.ended(within)
}
