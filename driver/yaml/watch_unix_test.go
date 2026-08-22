//go:build unix

package yaml_test

import (
	"errors"
	"syscall"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// TestWatchedWhenTheSystemWillGiveNoWatcher is the other half of the refusal a
// stream makes when it places its first registration: not a directory that is
// missing, but an operating system that will hand out no watcher at all
// (ADR-0020).
//
// The process is held to no descriptors for the length of the range, which is
// what the machine looks like when something else has exhausted them, and the
// limit is put back before anything else in this package needs one. It is
// UNIX-only for the same reason: the descriptor limit is what this reaches
// through, and Windows has no such knob.
func TestWatchedWhenTheSystemWillGiveNoWatcher(t *testing.T) {
	path := write(t, "port: 1\n")

	wb, err := ferry.BindWatched[watched](yaml.NewSource(path).Watched())
	if err != nil {
		t.Fatalf("BindWatched: %v", err)
	}

	restore := noDescriptors(t)

	seq, errf := wb.Watch(t.Context())
	for range seq {
		restore()
		t.Fatal("a stream that could open no watcher yielded a value")
	}

	restore()

	err = errf()
	if !errors.Is(err, ferry.ErrWatchLost) {
		t.Fatalf("a watch the system would not give ended the stream with %v, want a lost watch", err)
	}

	if !errors.Is(err, yaml.ErrWatch) {
		t.Errorf("the ending is %v, which does not carry this driver's own reason", err)
	}
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

	var once bool

	return func() {
		if once {
			return
		}

		once = true

		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &was); err != nil {
			panic("the descriptor limit could not be put back: " + err.Error())
		}
	}
}
