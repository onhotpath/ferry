package watcher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/examples/watcher"
)

// Cancellation is the other ending, and it cannot be an example because the
// error text of a cancelled context is not what an example should pin.
func TestWatchEndsOnContextCancellation(t *testing.T) {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	b, err := ferry.Bind[Config](plane)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	seq, errf := watcher.Watch(ctx, b, plane.Changes())
	cancel()

	for range seq {
		t.Fatal("a cancelled watch yielded a value")
	}
	if !errors.Is(errf(), context.Canceled) {
		t.Fatalf("errf lost the cancellation: %v", errf())
	}
}

// Closing the signal is a clean ending: the stream stops and errf reports nil,
// which is how a driver says it has stopped watching.
func TestWatchEndsCleanlyWhenTheSignalCloses(t *testing.T) {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	b, err := ferry.Bind[Config](plane)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	signal := make(chan struct{})
	close(signal)

	seq, errf := watcher.Watch(t.Context(), b, signal)
	for range seq {
		t.Fatal("a closed signal yielded a value")
	}
	if errf() != nil {
		t.Fatalf("a closed signal is not a failure: %v", errf())
	}
}
