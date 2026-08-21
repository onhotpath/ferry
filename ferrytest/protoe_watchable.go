//go:build protoe

package ferrytest

import (
	"context"
	"errors"
	"iter"
	"time"

	"github.com/onhotpath/ferry"
)

// This file is the conformance sketch for a watchable driver, and it is a
// proposal rather than shipped surface.
//
// Everything below is exported so that the shape can be read as a driver author
// would read it, and it is behind the protoe tag so that nothing here is part of
// ferrytest until the shape is signed off.

// WatchPlane is what [Watchable] is handed: a watchable source to bind, and the
// two things only the driver's own test knows how to do to its plane.
//
// It is a struct rather than a list of arguments because a suite that grows a
// case must not change its own signature, which is the rule [Driver] already
// follows.
type WatchPlane struct {
	// Name is the driver, and it opens every failure this suite reports.
	Name string

	// Open mints a fresh watchable source over a fresh plane. It is called once
	// per case, so no case can see what another one left behind.
	//
	// The plane it opens over holds one address, /host, carrying the text this
	// suite writes through Change. A driver whose plane cannot spell /host says
	// so through Address.
	Open func() ferry.WatchableSource

	// Change makes one change to the plane the last Open opened, setting /host
	// to the given text. It is the only way this suite has of moving a plane it
	// knows nothing else about.
	Change func(to string)

	// Lose takes the watch away the way the mechanism loses it: the directory
	// removed, the connection dropped, the registration refused. It is optional,
	// and a driver that cannot lose a watch leaves it nil and skips that case.
	//
	// A driver that can lose a watch and does not declare it here is the one
	// this suite cannot check, and it is the failure the whole seam exists to
	// prevent.
	Lose func()

	// Unwatchable mints a source of this driver that is watchable by type and
	// cannot be watched by configuration: an env source naming no file, a yaml
	// source over no path, a registry that reports nothing.
	//
	// It is optional. A driver with no such state - a poll needs nothing - leaves
	// it nil, and this suite records that rather than inventing one.
	Unwatchable func() ferry.WatchableSource

	// Address is the address the plane spells /host at, and it is empty where
	// /host is what the plane spells.
	Address string

	// Settle is how long this driver may take to notice a change. It is the one
	// number a driver supplies, because it is the one thing a suite cannot know:
	// an inotify queue answers in milliseconds and a poll answers in its
	// interval. Zero takes five seconds.
	Settle time.Duration
}

// watchTarget is what every case in this suite loads. The field is optional, so
// a plane that holds nothing yet is a value rather than a failure, which is what
// lets the first case assert the stream opens with a load.
type watchTarget struct {
	Host string `ferry:"host"`
}

// Watchable is the conformance suite for a driver that can be watched: six cases
// over one plane, and the whole of what the author of a watchable driver writes.
//
//	func TestWatchConformance(t *testing.T) {
//	    ferrytest.Watchable(t, ferrytest.WatchPlane{
//	        Name:   "yaml",
//	        Open:   func() ferry.WatchableSource { return yaml.NewSource(path(t)).Watched() },
//	        Change: func(to string) { write(t, path, "host: "+to+"\n") },
//	        Lose:   func() { os.RemoveAll(dir) },
//	        Unwatchable: func() ferry.WatchableSource { return yaml.NewSource("").Watched() },
//	    })
//	}
//
// There is one call and no menu, for the reason [Driver] gives: a suite a driver
// author can partially adopt measures nothing.
//
// It asserts the six properties a watchable driver owes its caller, and it
// asserts them through [ferry.BindWatched] and the stream, which is the only
// seam there is. Two of them are optional and are skipped, loudly, where the
// plane says the driver cannot reach that state.
func Watchable(t T, p WatchPlane) {
	t.Helper()

	if p.Open == nil || p.Change == nil {
		t.Errorf("plane %s: Open and Change are what this suite needs, and one of them is nil", p.Name)

		return
	}

	watchCases(t, &p)
}

// watchCases is the run itself, one case per property.
func watchCases(t T, p *WatchPlane) {
	t.Helper()

	watchOpensWithALoad(t, p)
	watchReloadsOnAChange(t, p)
	watchCoalescesABurst(t, p)
	watchHoldsValuesStill(t, p)
	watchEndsOnCancel(t, p)
	watchEndsOnLoss(t, p)
	watchRefusesTheUnwatchable(t, p)
}

// defaultSettle is how long a case waits where the plane named no window. It is
// generous on purpose: a suite that fails because a machine was busy teaches a
// driver author nothing.
const defaultSettle = 5 * time.Second

// settleOf is how long a case waits for this driver to notice something.
func settleOf(p *WatchPlane) time.Duration {
	if p.Settle <= 0 {
		return defaultSettle
	}

	return p.Settle
}

// watchOpensWithALoad is the first property: a stream begins with a value and
// not with a wait, so a caller writes no separate first load.
func watchOpensWithALoad(t T, p *WatchPlane) {
	t.Helper()

	p.Change("first")

	s := open(t, p)
	if s == nil {
		return
	}

	defer s.stop()

	if _, ok := s.next(settleOf(p)); !ok {
		t.Errorf("plane %s: the stream yielded no value before the first change, so a caller has to load "+
			"once themselves and there are two idioms for the first value", p.Name)
	}
}

// watchReloadsOnAChange is the second: a change the plane makes reaches the
// caller as a freshly loaded value.
func watchReloadsOnAChange(t T, p *WatchPlane) {
	t.Helper()

	s := open(t, p)
	if s == nil {
		return
	}

	defer s.stop()

	if _, ok := s.next(settleOf(p)); !ok {
		return
	}

	p.Change("second")

	v, ok := s.next(settleOf(p))
	if !ok {
		t.Errorf("plane %s: a change to the plane produced no reload", p.Name)

		return
	}

	if v.Host != "second" {
		t.Errorf("plane %s: the reload holds %q, want second: a reload is a load, and it reads what the "+
			"plane holds now", p.Name, v.Host)
	}
}

// burst is how many changes one save stands for in the coalescing case.
const burst = 5

// watchCoalescesABurst is the third: a burst is one reload, because a mechanism
// that reports its own bursts makes every consumer write the same settle window.
func watchCoalescesABurst(t T, p *WatchPlane) {
	t.Helper()

	s := open(t, p)
	if s == nil {
		return
	}

	defer s.stop()

	if _, ok := s.next(settleOf(p)); !ok {
		return
	}

	for range burst {
		p.Change("burst")
	}

	if _, ok := s.next(settleOf(p)); !ok {
		t.Errorf("plane %s: a burst of five changes produced no reload", p.Name)

		return
	}

	// One more reload is legal and two are not: a burst may split, and a
	// mechanism that reports every event of one save has not coalesced at all.
	if _, ok := s.next(settleOf(p)); ok {
		if _, again := s.next(settleOf(p)); again {
			t.Errorf("plane %s: a burst of five changes produced at least three reloads, so nothing "+
				"coalesces it and every consumer has to", p.Name)
		}
	}
}

// watchHoldsValuesStill is the fourth: a value handed out earlier never changes
// underneath the goroutine holding it.
func watchHoldsValuesStill(t T, p *WatchPlane) {
	t.Helper()

	p.Change("held")

	s := open(t, p)
	if s == nil {
		return
	}

	defer s.stop()

	held, ok := s.next(settleOf(p))
	if !ok {
		return
	}

	p.Change("moved")

	if _, ok := s.next(settleOf(p)); !ok {
		return
	}

	if held.Host != "held" {
		t.Errorf("plane %s: the value held from the first load now says %q, so a reload wrote into a value "+
			"somebody else was reading", p.Name, held.Host)
	}
}

// watchEndsOnCancel is the fifth: one context is the whole lifetime, and
// cancelling it ends the stream cleanly.
func watchEndsOnCancel(t T, p *WatchPlane) {
	t.Helper()

	s := open(t, p)
	if s == nil {
		return
	}

	if _, ok := s.next(settleOf(p)); !ok {
		return
	}

	s.cancel()

	err, ended := s.ended(settleOf(p))
	if !ended {
		t.Errorf("plane %s: cancelling the context did not end the stream, so there is a second lifetime "+
			"somewhere", p.Name)

		return
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("plane %s: a cancelled stream ended with %v, want the cancellation", p.Name, err)
	}
}

// watchEndsOnLoss is the sixth, and it is the one the whole seam exists for: a
// watch the mechanism cannot keep ends the stream with a reason, rather than
// leaving a process holding stale configuration with nothing to tell it so.
func watchEndsOnLoss(t T, p *WatchPlane) {
	t.Helper()

	if p.Lose == nil {
		return
	}

	s := open(t, p)
	if s == nil {
		return
	}

	defer s.stop()

	if _, ok := s.next(settleOf(p)); !ok {
		return
	}

	p.Lose()

	err, ended := s.ended(settleOf(p))
	if !ended {
		t.Errorf("plane %s: the watch was lost and the stream did not end, so a process holding stale "+
			"configuration has nothing to tell it so", p.Name)

		return
	}

	if !errors.Is(err, ferry.ErrWatchLost) && !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("plane %s: a lost watch ended the stream with %v, want ferry.ErrWatchLost or a plane "+
			"failure the next load reported", p.Name, err)
	}
}

// watchRefusesTheUnwatchable is the bind-seam half: a source of this driver that
// cannot be watched is refused before any load.
//
// What it matches is the class and not a sentinel, because the sentinel is the
// driver's own and this suite does not know it. A driver's own test is where
// that match belongs, and it is one line beside this call.
func watchRefusesTheUnwatchable(t T, p *WatchPlane) {
	t.Helper()

	if p.Unwatchable == nil {
		return
	}

	_, err := ferry.BindWatched[watchTarget](p.Unwatchable())
	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("plane %s: binding a source that cannot be watched gave %v, want a plane refusal before "+
			"any load, carrying this driver's own reason", p.Name, err)
	}
}

// watchRun is one stream under test: the values, the ending, and the one context
// that owns both.
type watchRun struct {
	values chan watchTarget
	done   chan struct{}
	errf   func() error
	cancel context.CancelFunc
}

// open binds a fresh source and ranges it on a goroutine of its own.
func open(t T, p *WatchPlane) *watchRun {
	t.Helper()

	wb, err := ferry.BindWatched[watchTarget](p.Open())
	if err != nil {
		t.Errorf("plane %s: BindWatched refused a source this plane says is watchable: %v", p.Name, err)

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	seq, errf := wb.Watch(ctx)

	return start(ctx, cancel, seq, errf)
}

// start ranges the sequence and hands values over one at a time.
func start(ctx context.Context, cancel context.CancelFunc,
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
func (r *watchRun) ended(within time.Duration) (error, bool) { //nolint:revive // the bool is the wait, not a flag.
	timer := time.NewTimer(within)
	defer timer.Stop()

	for {
		select {
		case <-r.done:
			return r.errf(), true
		case <-r.values:
		case <-timer.C:
			return nil, false
		}
	}
}

// stop ends the stream and waits for the goroutine ranging it, so no case leaves
// one behind for the next.
func (r *watchRun) stop() {
	r.cancel()
	<-r.done
}
