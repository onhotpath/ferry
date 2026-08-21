package ferrytest_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// watchSettle is the window every case in this file runs under. It is short
// because this plane is a map and a channel, and a suite that takes five
// seconds per case is a suite nobody runs.
const watchSettle = 200 * time.Millisecond

// planeSettle is the plane's own coalescing window. Coalescing is the driver's
// job - core does not debounce - so a plane that emits a burst swallows its
// own.
const planeSettle = 20 * time.Millisecond

// ignoreCancelFor is how long the plane that ignores its context ignores it
// for. It is longer than the suite's window and short enough that the run ends.
const ignoreCancelFor = 2 * time.Second

// errNoWatch is what an unwatchable instance of this fake driver refuses with,
// standing in for a real driver's own watch sentinel.
var errNoWatch = errors.New("this source watches nothing")

// errLost is what a lost watch reports, standing in for a directory removed
// under a file watcher.
var errLost = errors.New("the mechanism died")

// TestWatchableAllSeven is the acceptance the suite exists for: a plane that
// does everything right, with every optional hook supplied, passes all seven
// cases and reports nothing.
func TestWatchableAllSeven(t *testing.T) {
	p := newWatchFake()
	c := &capture{}

	ferrytest.Watchable(c, watchPlaneOf(p))

	if len(c.lines) != 0 {
		t.Errorf("a conforming plane was reported %d times:\n\t%s", len(c.lines), strings.Join(c.lines, "\n\t"))
	}

	if len(c.logs) != 0 {
		t.Errorf("a plane declaring both optional hooks skipped a case: %v", c.logs)
	}

	if c.helpers == 0 {
		t.Error("Watchable never called Helper, so every failure would name a line inside ferrytest")
	}
}

// TestWatchableSkipsTheOptionalHooks is the other half of the acceptance: a
// driver that cannot reach either optional state passes, and says out loud
// which cases did not run.
func TestWatchableSkipsTheOptionalHooks(t *testing.T) {
	p := newWatchFake()
	wp := watchPlaneOf(p)
	wp.Lose = nil
	wp.Unwatchable = nil

	c := &capture{}
	ferrytest.Watchable(c, wp)

	if len(c.lines) != 0 {
		t.Errorf("a plane with neither optional hook was reported %d times:\n\t%s",
			len(c.lines), strings.Join(c.lines, "\n\t"))
	}

	if len(c.logs) != 2 {
		t.Errorf("two optional cases were skipped and %d were logged: %v", len(c.logs), c.logs)
	}
}

// TestWatchableSkipsQuietly holds the skip to [ferrytest.T] being two methods:
// a reporter that cannot log runs the suite and is told nothing.
func TestWatchableSkipsQuietly(t *testing.T) {
	p := newWatchFake()
	wp := watchPlaneOf(p)
	wp.Lose = nil
	wp.Unwatchable = nil

	q := &quiet{}
	ferrytest.Watchable(q, wp)

	if len(q.lines) != 0 {
		t.Errorf("the suite reported %q through a reporter that cannot log, want nothing", q.lines)
	}
}

// TestWatchableNeedsOpenAndChange is the one refusal the suite makes about its
// own description rather than about a driver.
func TestWatchableNeedsOpenAndChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    ferrytest.WatchPlane
	}{
		{"neither", ferrytest.WatchPlane{Name: "fake"}},
		{"no open", ferrytest.WatchPlane{Name: "fake", Change: func(string) {}}},
		{"no change", ferrytest.WatchPlane{
			Name: "fake",
			Open: func() ferry.WatchableSource { return newWatchFake() },
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &capture{}
			ferrytest.Watchable(c, tc.p)

			if len(c.lines) != 1 {
				t.Errorf("a plane missing a required hook was reported %d times, want 1: %v", len(c.lines), c.lines)
			}
		})
	}
}

// The failing halves. Each one breaks exactly one property, and the assertion
// is that the case owning it reports, which is what makes the suite a check
// rather than seven functions that never fail.

func TestWatchableReportsAStreamThatNeverOpens(t *testing.T) {
	reportsFor(t, func(_ *ferrytest.WatchPlane, p *watchFake) { p.silent = true })
}

func TestWatchableReportsAWatchThatNeverFires(t *testing.T) {
	reportsFor(t, func(_ *ferrytest.WatchPlane, p *watchFake) { p.deaf = true })
}

func TestWatchableReportsAStreamThatIgnoresCancellation(t *testing.T) {
	reportsFor(t, func(_ *ferrytest.WatchPlane, p *watchFake) { p.ignoreCancel = true })
}

func TestWatchableReportsALossThatIsNotReported(t *testing.T) {
	reportsFor(t, func(wp *ferrytest.WatchPlane, _ *watchFake) {
		wp.Lose = func() {}
	})
}

func TestWatchableReportsAnUnwatchableThatBinds(t *testing.T) {
	reportsFor(t, func(wp *ferrytest.WatchPlane, _ *watchFake) {
		wp.Unwatchable = func() ferry.WatchableSource { return newWatchFake() }
	})
}

func TestWatchableReportsAPlaneThatCannotBeBound(t *testing.T) {
	reportsFor(t, func(wp *ferrytest.WatchPlane, _ *watchFake) {
		wp.Open = func() ferry.WatchableSource {
			f := newWatchFake()
			f.bindFail = errNoWatch

			return f
		}
	})
}

// reportsFor runs the suite over a plane one property has been broken in, and
// asserts that something was reported.
func reportsFor(t *testing.T, breakIt func(*ferrytest.WatchPlane, *watchFake)) {
	t.Helper()

	p := newWatchFake()
	wp := watchPlaneOf(p)
	breakIt(&wp, p)

	c := &capture{}
	ferrytest.Watchable(c, wp)

	if len(c.lines) == 0 {
		t.Error("a plane with a broken property passed the suite in silence")
	}
}

// TestWatchableTakesADefaultWindow holds the plane that names no settle
// duration: five seconds is the window, and a plane that fails every load
// reaches every case's report without waiting one out.
func TestWatchableTakesADefaultWindow(t *testing.T) {
	p := newWatchFake()
	p.silent = true

	wp := watchPlaneOf(p)
	wp.Settle = 0

	c := &capture{}
	ferrytest.Watchable(c, wp)

	if len(c.lines) == 0 {
		t.Error("a plane whose every load fails passed the suite in silence")
	}
}

// TestWatchableReportsAReloadThatReadsTheOldValue is the reload case's own
// half: a stream that fires but hands back what the plane held before.
func TestWatchableReportsAReloadThatReadsTheOldValue(t *testing.T) {
	p := newWatchFake()
	p.pinned = true

	c := &capture{}
	ferrytest.Watchable(c, watchPlaneOf(p))

	if len(c.lines) == 0 {
		t.Error("a plane whose reload reads a stale value passed the suite in silence")
	}
}

// TestWatchableReportsACancellationSpeltAsAFailure is the cancellation case's
// other half: a stream that ends when the context does, and ends with the
// plane's own complaint rather than with the cancellation.
func TestWatchableReportsACancellationSpeltAsAFailure(t *testing.T) {
	p := newWatchFake()
	p.stall = true

	c := &capture{}
	ferrytest.Watchable(c, watchPlaneOf(p))

	if len(c.lines) == 0 {
		t.Error("a stream that ended a cancellation with the plane's own error passed the suite in silence")
	}
}

// TestWatchableReportsALossSpeltAsSomethingElse is the loss case's other half:
// the watch goes away and the stream ends with an error that is neither a lost
// watch nor a plane failure, which is an ending no caller's restart policy can
// classify.
func TestWatchableReportsALossSpeltAsSomethingElse(t *testing.T) {
	p := newWatchFake()

	wp := watchPlaneOf(p)
	wp.Lose = func() {
		p.poison()
		p.announce()
	}

	c := &capture{}
	ferrytest.Watchable(c, wp)

	if len(c.lines) == 0 {
		t.Error("a stream that ended a lost watch with an unclassifiable error passed the suite in silence")
	}
}

// TestWatchableTakesALastValueBeforeTheEnd is the drain the ending waits
// through: a plane that announces one more change and then loses the watch
// hands over a value while the suite is already waiting for the ending, and
// that value is not the stream failing to end.
func TestWatchableTakesALastValueBeforeTheEnd(t *testing.T) {
	p := newWatchFake()

	wp := watchPlaneOf(p)
	wp.Lose = func() {
		p.set("last")
		time.Sleep(3 * planeSettle)
		p.lose(errLost)
	}

	c := &capture{}
	ferrytest.Watchable(c, wp)

	if len(c.lines) != 0 {
		t.Errorf("a plane that yielded one more value before ending was reported %d times:\n\t%s",
			len(c.lines), strings.Join(c.lines, "\n\t"))
	}
}

// TestWatchableCoalescingIsAsserted is the burst case's own half, and it needs
// a plane that reports a change every time it is asked rather than coalescing
// anything. A burst of five is not enough to drive that on its own: five
// changes made back to back land while the stream is still reloading the first,
// so even a plane that coalesces nothing yields two reloads and not five.
func TestWatchableCoalescingIsAsserted(t *testing.T) {
	p := newWatchFake()
	p.spin = true

	c := &capture{}
	ferrytest.Watchable(c, watchPlaneOf(p))

	if len(c.lines) == 0 {
		t.Error("a plane that reports every event of a burst passed the suite in silence")
	}
}

// watchPlaneOf is the description a driver author writes, over the fake.
func watchPlaneOf(p *watchFake) ferrytest.WatchPlane {
	return ferrytest.WatchPlane{
		Name:   "fake",
		Open:   func() ferry.WatchableSource { return p.reopen() },
		Change: p.set,
		Lose:   func() { p.lose(errLost) },
		Unwatchable: func() ferry.WatchableSource {
			f := newWatchFake()
			f.bindFail = errNoWatch

			return f
		},
		Settle: watchSettle,
	}
}

// watchFake is an in-memory watchable plane: one address, an armed-once
// registration, and switches for each property a driver can get wrong.
//
// It is the miniature of a notification handle rather than of an event queue,
// because armed-once is the weaker mechanism and a suite that is right against
// it is right against a queue.
type watchFake struct {
	mu   sync.Mutex
	host string

	// opens counts this case's loads, reset by every Open, which is what lets a
	// stalling plane serve each case's first load and hold every one after it.
	opens int

	// gen counts announcements, and a registration records the generation it
	// was armed at, so a change between Notify and Wait is reported by Wait.
	gen uint64

	// bell is closed and replaced on every announcement, which wakes every
	// waiter without a channel per waiter.
	bell chan struct{}

	// lost is the reason the watch cannot be kept.
	lost error

	// coalesce is this plane's own settle window. Zero reports every event.
	coalesce time.Duration

	// bindFail is what Watching refuses with: a source watchable by type and
	// unwatchable by configuration.
	bindFail error

	// silent makes every load fail, so no value ever reaches the stream.
	silent bool

	// pinned makes every load read the empty value the plane opened with, so a
	// change fires and the reload is stale.
	pinned bool

	// poisoned makes every load read a value the target cannot hold, so the
	// stream ends on the reload's own error rather than on the watch.
	poisoned bool

	// spin makes every wait answer at once, whether or not anything changed,
	// which is a mechanism that coalesces nothing.
	spin bool

	// stall makes every load after the first block until the context goes and
	// then fail with the plane's own error, and it makes every wait answer at
	// once, so a cancellation lands inside a load.
	stall bool

	// deaf drops every announcement, so a change produces no reload.
	deaf bool

	// ignoreCancel makes the wait outlive the context it was handed.
	ignoreCancel bool
}

func newWatchFake() *watchFake {
	return &watchFake{bell: make(chan struct{}), coalesce: planeSettle}
}

// reopen mints the fresh source each case asks for. The contents and the
// mechanism are shared, because this plane is the plane and Open is a second
// handle on it.
func (p *watchFake) reopen() *watchFake {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.opens = 0

	return p
}

func (p *watchFake) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(ctx context.Context) (ferry.Reader, error) {
		p.mu.Lock()
		p.opens++
		stall := p.stall && p.opens > 1
		p.mu.Unlock()

		if stall {
			<-ctx.Done()

			return nil, errLost
		}

		return p.snapshot()
	}, nil
}

// snapshot is one load's view of the contents, and the three ways this plane
// can make a load answer with something other than what it holds.
func (p *watchFake) snapshot() (ferry.Reader, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.silent {
		return nil, ferry.ErrPlane
	}

	if p.poisoned {
		return badSnapshot{}, nil
	}

	if p.pinned {
		return watchSnapshot(""), nil
	}

	return watchSnapshot(p.host), nil
}

// poison makes every load from here on read a value the target cannot hold.
func (p *watchFake) poison() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.poisoned = true
}

func (p *watchFake) Watching() (ferry.Notifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bindFail != nil {
		return nil, p.bindFail
	}

	return p, nil
}

func (p *watchFake) Notify(context.Context) (ferry.Change, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return &watchReg{p: p, at: p.gen}, nil
}

// set writes the one address this plane holds and announces the change.
func (p *watchFake) set(to string) {
	p.mu.Lock()
	p.host = to
	deaf := p.deaf
	p.mu.Unlock()

	if deaf {
		return
	}

	p.announce()
}

// lose ends the watch the way a plane that cannot keep it does: every armed
// registration answers with the reason, and no announcement follows.
func (p *watchFake) lose(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lost = err
	close(p.bell)
	p.bell = make(chan struct{})
}

func (p *watchFake) announce() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.gen++
	close(p.bell)
	p.bell = make(chan struct{})
}

func (p *watchFake) generation() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.gen
}

// watchReg is one registration: the generation it was armed at, and the wait
// that reports anything after it.
type watchReg struct {
	p  *watchFake
	at uint64
}

// Wait answers at once for the two planes that coalesce nothing, and otherwise
// waits for an announcement after the generation it was armed at.
func (c *watchReg) Wait(ctx context.Context) (bool, error) {
	if c.p.stall || c.p.spin {
		return true, nil
	}

	return c.awaitAnnouncement(ctx)
}

// awaitAnnouncement is the loop: look, and park on the bell until something
// this registration has not seen yet has happened.
func (c *watchReg) awaitAnnouncement(ctx context.Context) (bool, error) {
	for {
		bell, changed, err := c.look()
		if err != nil {
			return false, err
		}

		if changed {
			c.settle(ctx)

			return true, nil
		}

		if c.park(ctx, bell) {
			return false, nil
		}
	}
}

// park waits for the next announcement, and reports whether the wait ends here
// rather than looping round to look again.
func (c *watchReg) park(ctx context.Context, bell <-chan struct{}) bool {
	if c.p.ignoreCancel {
		// Outlive the context, then give up, so a suite that has already
		// reported the failure is not waited on by a goroutine that never ends.
		select {
		case <-bell:
			return false
		case <-time.After(ignoreCancelFor):
			return true
		}
	}

	select {
	case <-ctx.Done():
		return true
	case <-bell:
		return false
	}
}

// settle waits for the plane to go quiet, so a burst is one change and one
// reload.
func (c *watchReg) settle(ctx context.Context) {
	if c.p.coalesce <= 0 {
		return
	}

	for {
		at := c.p.generation()

		select {
		case <-ctx.Done():
			return
		case <-time.After(c.p.coalesce):
		}

		if c.p.generation() == at {
			return
		}
	}
}

// look reports what the plane holds now: the bell to wait on, whether a change
// has already landed, and the loss if there is one.
func (c *watchReg) look() (bell <-chan struct{}, changed bool, err error) {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()

	if c.p.lost != nil {
		return nil, false, c.p.lost
	}

	if c.p.gen > c.at {
		return nil, true, nil
	}

	return c.p.bell, false, nil
}

func (*watchReg) Close() error { return nil }

// badSnapshot answers every address with a value the target cannot hold, which
// is a reload failing on its own terms rather than on the watch's.
type badSnapshot struct{}

func (badSnapshot) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.Bool(true), nil
}

// watchSnapshot serves one load from an immutable copy of the contents.
type watchSnapshot string

func (s watchSnapshot) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.String(string(s)), nil
}
