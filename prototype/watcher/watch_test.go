package watcher

import (
	"context"
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
)

type Config struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=8080"`
}

func seed(s *MemSource) {
	s.Set(ferry.At("host"), ferry.String("db1"))
	s.Set(ferry.At("port"), ferry.Number("5432"))
}

// A0's whole claim: bind once, reload per signal, fresh value each time -
// zero core changes, zero forks, only the shipped surface.
func TestWatcherBuildsOutsideCore(t *testing.T) {
	src := NewMemSource()
	seed(src)
	b, err := ferry.Bind[Config](src)
	if err != nil {
		t.Fatal(err)
	}
	signal := src.Changes()

	seq, errf := Watch(t.Context(), b, signal)
	var got []Config
	go func() {
		src.Set(ferry.At("host"), ferry.String("db2"))
		src.Set(ferry.At("port"), ferry.Number("6432"))
	}()
	for v := range seq {
		got = append(got, v)
		if len(got) == 2 {
			break
		}
	}
	if err := errf(); err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if last.Host != "db2" || last.Port != 6432 {
		t.Fatalf("reloads did not track the plane: %+v", got)
	}
}

// The error conventions, felt side by side on the same failure: the
// plane loses a required address mid-watch.
func TestErrorConventions(t *testing.T) {
	mk := func() (*MemSource, *ferry.Binding[Config], <-chan struct{}) {
		src := NewMemSource()
		seed(src)
		b, err := ferry.Bind[Config](src)
		if err != nil {
			t.Fatal(err)
		}
		return src, b, src.Changes()
	}

	// jba shape: the stream just ends; errf carries the failure, and the
	// compiler would have warned had we discarded errf.
	src, b, signal := mk()
	seq, errf := Watch(t.Context(), b, signal)
	src.Delete(ferry.At("host"))
	for range seq {
		t.Fatal("no value should arrive from a failing reload")
	}
	if err := errf(); err == nil || !errors.Is(err, ferry.ErrMissing) {
		t.Fatalf("errf lost the failure: %v", err)
	}

	// Seq2 shape: the error is the final element; dropping the second
	// range variable compiles silently - that is the trade.
	src, b, signal = mk()
	src.Delete(ferry.At("host"))
	var sawErr error
	for _, err := range Watch2(t.Context(), b, signal) {
		sawErr = err
	}
	if sawErr == nil || !errors.Is(sawErr, ferry.ErrMissing) {
		t.Fatalf("Seq2 lost the failure: %v", sawErr)
	}
}

// A held value never changes when the plane does: each reload is a fresh
// value (ADR-0006), so publication is replacement, never mutation.
func TestHeldValueIsImmutable(t *testing.T) {
	src := NewMemSource()
	seed(src)
	b, _ := ferry.Bind[Config](src)
	first, err := b.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	src.Set(ferry.At("host"), ferry.String("db9"))
	second, _ := b.Load(context.Background())
	if first.Host != "db1" || second.Host != "db9" {
		t.Fatalf("held value mutated: first=%+v second=%+v", first, second)
	}
}

// The sharp edges a watcher inherits if it reloads via LoadOver(prev)
// instead of Load - pinned as facts for the board, not proposals:
//  1. an address the plane LOST keeps its stale value (the ADR-0006 leak),
//  2. a map composite is REPLACED wholesale, not merged.
func TestLoadOverAsReloadSharpEdges(t *testing.T) {
	type WithMap struct {
		Host string            `ferry:"host"`
		Tags map[string]string `ferry:"tags"`
	}
	src := NewMemSource()
	src.Set(ferry.At("host"), ferry.String("db1"))
	src.Set(ferry.At("tags").At("a"), ferry.String("1"))
	src.Set(ferry.At("tags").At("b"), ferry.String("2"))
	b, err := ferry.Bind[WithMap](src)
	if err != nil {
		t.Fatal(err)
	}
	prev, err := b.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	src.Delete(ferry.At("host"))         // the plane lost /host
	src.Delete(ferry.At("tags").At("b")) // and one tag

	stale, err := b.LoadOver(context.Background(), prev)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Host != "db1" {
		t.Fatalf("expected the leak: lost /host keeps prev value, got %q", stale.Host)
	}
	if len(stale.Tags) != 1 || stale.Tags["a"] != "1" {
		t.Fatalf("expected wholesale composite replace, got %v", stale.Tags)
	}

	fresh, err := b.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Host != "" {
		t.Fatalf("fresh reload must not leak: %q", fresh.Host)
	}
}
