package yaml_test

import (
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// watched is what the watching tests load, and the documents below are the
// before and the after.
type watched struct {
	Port int `ferry:"port"`
}

// arrives is how long a test waits for a reload it is owed, and quiet is how
// long it waits for one it is not owed. Neither races the filesystem: the
// change is on disk before the wait starts.
const (
	arrives = 10 * time.Second
	quiet   = 200 * time.Millisecond
)

// TestWatchedReloadsWhenTheFileChanges is the whole of the conversion: an
// operator edits the file, the next value off the stream is the new one, and
// the value held from before the edit is unchanged (ADR-0020).
func TestWatchedReloadsWhenTheFileChanges(t *testing.T) {
	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := watchStream(ctx, t, path)

	before := s.next(t)
	if before.Port != 1 {
		t.Fatalf("the stream opened with %d, want 1: the stream opens with a load", before.Port)
	}

	edit(t, path, "# edited by hand\nport: 2\n")

	after := s.next(t)
	if after.Port != 2 {
		t.Errorf("the reload produced %d, want 2", after.Port)
	}

	if before.Port != 1 {
		t.Errorf("the held value became %d, so a reload wrote into it: a reload is a load, and the value "+
			"held across it does not change", before.Port)
	}
}

// TestWatchedStopsWithItsContext is the lifecycle, which is the context handed
// to Watch and nothing else: there is no Stop, so cancelling has to be one, and
// nothing of the mechanism is left running afterwards (ADR-0020).
func TestWatchedStopsWithItsContext(t *testing.T) {
	path := write(t, "port: 1\n")

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	s := watchStream(ctx, t, path)

	s.next(t)

	// The first change proves the watch was running, which is what makes the
	// silence after the cancellation mean something.
	edit(t, path, "# the first edit\nport: 2\n")
	s.next(t)

	cancel()

	select {
	case <-s.done:
	case <-time.After(arrives):
		t.Fatal("cancelling the context did not end the stream, so there is a second lifetime somewhere")
	}

	if err := s.errf(); !errors.Is(err, context.Canceled) {
		t.Errorf("the stream ended with %v, want the cancellation", err)
	}

	assertNoLeak(t, before)
}

// TestWatchedFileThatDoesNotExistYetIsWatched is the bootstrap case: the
// directory is there and the file is not, and the watch fires when the file
// appears, because what is watched is the directory.
func TestWatchedFileThatDoesNotExistYetIsWatched(t *testing.T) {
	path := filepath.Join(t.TempDir(), planeName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := watchStream(ctx, t, path)

	if first := s.next(t); first.Port != 0 {
		t.Fatalf("the stream opened with %d, want the empty plane's zero", first.Port)
	}

	edit(t, path, "port: 7\n")

	if after := s.next(t); after.Port != 7 {
		t.Errorf("the reload produced %d, want 7: a file that appears is a change", after.Port)
	}
}

// TestWatchedIgnoresAWriteBesideThePlane asserts the exact-name filter, which
// is what keeps this driver's own saves from reloading twice: a save stages its
// replacement beside the plane, and that write is not a change to the plane.
func TestWatchedIgnoresAWriteBesideThePlane(t *testing.T) {
	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := watchStream(ctx, t, path)

	s.next(t)

	staged := filepath.Join(filepath.Dir(path), planeName+".ferry-staged")
	if err := os.WriteFile(staged, []byte("port: 99\n"), 0o600); err != nil {
		t.Fatalf("staging a file beside the plane: %v", err)
	}

	select {
	case v := <-s.values:
		t.Errorf("a write to %s produced a reload holding %d, so the name filter is not exact",
			filepath.Base(staged), v.Port)
	case <-s.done:
		t.Errorf("a write beside the plane ended the stream: %v", s.errf())
	case <-time.After(quiet):
	}
}

// TestWatchedDirectoryThatIsNotThereEndsTheStream is the failure the operating
// system has the opinion about: a path whose directory does not exist is a
// watch that would never fire, and it ends the stream with this driver's reason
// before any value reaches the caller.
func TestWatchedDirectoryThatIsNotThereEndsTheStream(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no-such-directory", planeName)

	wb, err := ferry.BindWatched[watched](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	seq, errf := wb.Watch(t.Context())
	for range seq {
		t.Fatal("a watch over a directory that is not there yielded a value")
	}

	err = errf()
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the stream ended with %v, want a watch that could not be opened", err)
	}

	if !errors.Is(err, yaml.ErrWatch) {
		t.Errorf("the ending is %v, which does not carry this driver's own reason", err)
	}
}

// TestWatchedWithNoPathIsRefusedAtBind is the instance refusal Watching makes
// with no I/O at all: a source over no path cannot be watched, and it is
// refused at the bind rather than by a stream that never fires.
func TestWatchedWithNoPathIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	_, err := ferry.BindWatched[watched](yaml.NewSource("").Watched())
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("binding a source over no path reported %v, want a plane refusal at the bind", err)
	}

	if !errors.Is(err, yaml.ErrWatch) {
		t.Errorf("the refusal is %v, which does not carry this driver's own reason", err)
	}
}

// TestAWatchedSourceStillLoads asserts the conversion changes nothing about
// loading: the source it was converted from is unchanged and so is this one.
func TestAWatchedSourceStillLoads(t *testing.T) {
	t.Parallel()

	path := write(t, "port: 3\n")

	got, err := ferry.Load[watched](t.Context(), yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("loading through a watched source: %v", err)
	}

	if got.Port != 3 {
		t.Errorf("the load holds %d, want 3", got.Port)
	}
}

// TestWatchingIsOptIn asserts the plain source is what it always was: no
// conversion, nothing running, and the file untouched until a load asks for it.
//
// ferry.BindWatched over a plain yaml.Source does not compile, which is the
// other half of this and cannot be written down here.
func TestWatchingIsOptIn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), planeName)

	src := yaml.NewSource(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat of a plane no load has asked for: %v, want it still not to exist", err)
	}

	if _, err := ferry.Load[watched](t.Context(), src); err != nil {
		t.Fatalf("loading through a source over a file that is not there: %v", err)
	}
}

// run is one stream under test: the values it yields, the ending, and the way
// to ask why it ended.
type run struct {
	values chan watched
	done   chan struct{}
	errf   func() error
}

// watchStream binds a watched source over path and ranges it on a goroutine of
// its own, so that a test reads values one at a time.
func watchStream(ctx context.Context, t *testing.T, path string) *run {
	t.Helper()

	seq, errf := watchSeq(ctx, t, path)

	return rangeOn(ctx, seq, errf)
}

// watchSeq is the stream itself, for a test that ranges it on its own
// goroutine: what a test does inside that range runs while the stream is
// waiting on nothing, which is the moment the cancellation races want.
func watchSeq(ctx context.Context, t *testing.T, path string) (iter.Seq[watched], func() error) {
	t.Helper()

	wb, err := ferry.BindWatched[watched](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	return wb.Watch(ctx)
}

// rangeOn ranges the sequence and hands values over one at a time, stopping
// where the context that owns the stream is done.
func rangeOn(ctx context.Context, seq iter.Seq[watched], errf func() error) *run {
	r := &run{values: make(chan watched), done: make(chan struct{}), errf: errf}

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

// next takes the next value off the stream, failing the test rather than
// hanging where none arrives.
func (r *run) next(t *testing.T) watched {
	t.Helper()

	select {
	case v := <-r.values:
		return v
	case <-r.done:
		t.Fatalf("the stream ended before a value arrived: %v", r.errf())
	case <-time.After(arrives):
		t.Fatal("no value arrived and the stream did not end")
	}

	return watched{}
}

// ended waits for the stream to stop and answers with what stopped it, failing
// the test rather than hanging where it does not stop at all.
func (r *run) ended(t *testing.T) error {
	t.Helper()

	for {
		select {
		case <-r.done:
			return r.errf()
		case <-r.values:
		case <-time.After(arrives):
			t.Fatal("the stream did not end")

			return nil
		}
	}
}

// edit rewrites the plane, which is the operator's own change in every test
// here.
func edit(t *testing.T, path, doc string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the plane: %v", err)
	}
}

// assertNoLeak reports a goroutine the watch left running, and it waits for the
// count to come back rather than sleeping a fixed time: a goroutine returning
// is not instantaneous, and a fixed sleep is either flaky or slow.
func assertNoLeak(t *testing.T, before int) {
	t.Helper()

	for range 100 {
		if runtime.NumGoroutine() <= before {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Errorf("goroutines went from %d to %d and stayed there, so the watch left one running",
		before, runtime.NumGoroutine())
}

// TestWatchedEndsWhenTheDirectoryGoesAway is the loss this driver's own name
// filter would otherwise swallow. The watch is on the directory, and where the
// file was never there to be removed first, the directory going away is the
// only event there is: a watch that went quiet instead would leave a process
// holding stale configuration with nothing to tell it so (ADR-0020).
func TestWatchedEndsWhenTheDirectoryGoesAway(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "watched")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("making the directory the watch is on: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := watchStream(ctx, t, filepath.Join(dir, planeName))

	if first := s.next(t); first.Port != 0 {
		t.Fatalf("the stream opened with %d, want the empty plane's zero", first.Port)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing the directory the watch is on: %v", err)
	}

	err := s.ended(t)
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("the directory went away and the stream ended with %v, want a lost watch", err)
	}

	if !errors.Is(err, yaml.ErrWatch) {
		t.Errorf("the ending is %v, which does not carry this driver's own reason", err)
	}
}

// The two cancellation tests below run their race repeatedly, because what
// they assert is that either side of it ends the same way, and which side the
// runtime takes is the runtime's to choose.
//
// pending is how long the edit is given to reach the watcher before the
// cancellation, so that the wait which follows begins with both already true.
// intoTheWindow is where the cancellation lands relative to the start of a
// stream, and it is inside the settle window that opens a few milliseconds in.
const (
	races         = 25
	pending       = 5 * time.Millisecond
	intoTheWindow = 20 * time.Millisecond
)

// TestWatchedCancelledWhileAChangeIsPending is the cancellation that races a
// change already on disk: whichever the runtime picks, a cancelled watch ends
// with the cancellation and never reports one more change first (ADR-0020).
func TestWatchedCancelledWhileAChangeIsPending(t *testing.T) {
	for range races {
		cancelWithAChangePending(t)
	}
}

// cancelWithAChangePending is one turn of that race. The edit and the
// cancellation both happen inside the range body, which is the one moment the
// stream is waiting on nothing, so the wait that follows them begins with the
// event and the cancellation both ready.
func cancelWithAChangePending(t *testing.T) {
	t.Helper()

	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seq, errf := watchSeq(ctx, t, path)

	for range seq {
		edit(t, path, "port: 2\n")
		time.Sleep(pending)
		cancel()
	}

	if err := errf(); !errors.Is(err, context.Canceled) {
		t.Fatalf("a stream cancelled with a change pending ended with %v, want the cancellation", err)
	}
}

// TestWatchedCancelledInsideTheSettleWindow is the cancellation that lands
// while the burst one save produces is still being swallowed: the window is not
// a lifetime of its own, so cancelling ends the wait inside it rather than
// after it.
func TestWatchedCancelledInsideTheSettleWindow(t *testing.T) {
	for range races {
		cancelInsideTheWindow(t)
	}
}

// cancelInsideTheWindow is one turn of that race: the edit opens the window as
// soon as the stream yields, and the cancellation is already scheduled to land
// while it is still open.
func cancelInsideTheWindow(t *testing.T) {
	t.Helper()

	path := write(t, "port: 1\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seq, errf := watchSeq(ctx, t, path)

	timer := time.AfterFunc(intoTheWindow, cancel)
	defer timer.Stop()

	for range seq {
		edit(t, path, "port: 2\n")
	}

	if err := errf(); !errors.Is(err, context.Canceled) {
		t.Fatalf("a stream cancelled inside the settle window ended with %v, want the cancellation", err)
	}
}
