//go:build unix

package env

import (
	"errors"
	"syscall"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestAStreamTheSystemWillGiveNoWatcherEndsWithTheDriversReason is the other
// half of what a stream refuses when it places its first registration: not a
// directory that is missing, but an operating system that will hand out no
// watcher at all.
//
// It is the case that says [Watching] doing no I/O costs the caller nothing:
// what the machine has an opinion about still reaches them, on the stream,
// before any value, carrying this driver's own reason (ADR-0020).
//
// The process is held to no descriptors for the length of the range, which is
// what the machine looks like when something else has exhausted them, and the
// limit is put back before anything else here needs one. It is UNIX-only
// because the descriptor limit is what it reaches through, and it runs alone:
// the limit is the whole process, so nothing in this file is parallel.
func TestAStreamTheSystemWillGiveNoWatcherEndsWithTheDriversReason(t *testing.T) {
	path := staged(t, "HOST=old\n")

	wb, err := ferry.BindWatched[host](New(Environ(noEnviron), DotEnv(path)).Watched())
	if err != nil {
		t.Fatalf("bind watched: %+v", err)
	}

	restore := noDescriptors(t)

	seq, errf := wb.Watch(t.Context())
	for range seq {
		restore()
		t.Fatal("a stream that could open no watcher handed over a value")
	}

	restore()

	err = errf()
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("a watch the system would not give ended the stream with %+v, want a lost watch", err)
	}

	answers(t, err, ferry.ErrPlane, ErrWatch)
}

// noDescriptors holds the process to no new file descriptors and answers with
// the way to put the limit back, which every path out of the caller has to
// take: a test binary that cannot open a file reports nothing.
//
// It is called on the test's own goroutine and nothing here runs in parallel,
// so the window is this one range and nothing else is inside it.
func noDescriptors(t *testing.T) (restore func()) {
	t.Helper()

	var was syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &was); err != nil {
		t.Skipf("the descriptor limit cannot be read on this system: %v", err)
	}

	none := syscall.Rlimit{Cur: 0, Max: was.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &none); err != nil {
		t.Skipf("the descriptor limit cannot be lowered on this system: %v", err)
	}

	var done bool

	return func() {
		if done {
			return
		}

		done = true

		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &was); err != nil {
			panic("the descriptor limit could not be put back: " + err.Error())
		}
	}
}
