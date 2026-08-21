//go:build protoe

package ferry_test

import (
	"context"
	"errors"

	"github.com/onhotpath/ferry"
)

// This file is the deferred composition helper, written where a caller would
// write it: outside core, over nothing but the published seam.
//
// It is what #361's named trigger is measured against. If callers keep writing
// this, it moves into core; until then core does not carry it.

// WatchAll makes one [ferry.WatchableSource] out of the source a load reads through
// and the watchable sources whose changes it is composed of.
//
//	base  := env.New(env.DotEnv("base.env")).Watched()
//	local := env.New(env.DotEnv("local.env")).Watched()
//	src   := watchAll(layered{base, local}, base, local)
//
// A change on any of them is a change on the whole, and one announcement per
// layer for one deployment is one reload under a driver that coalesces. Every layer is armed
// before any of them is waited on, so a change on the quiet layer while the
// noisy one is being read is not lost.
//
// It composes the watch and never the read. Which layer wins at an address is
// the read source's own business, because ferry has no opinion about layering
// and this is not the place to invent one.
//
// It refuses at [ferry.BindWatched] where any layer refuses, naming that layer's own
// reason, and where it was given no layer at all.
func watchAll(read ferry.Source, of ...ferry.WatchableSource) ferry.WatchableSource {
	return allOf{read: read, of: of}
}

// allOf is [WatchAll]'s value: one reader, many mechanisms.
type allOf struct {
	read ferry.Source
	of   []ferry.WatchableSource
}

func (a allOf) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if a.read == nil {
		return nil, errors.New("watchAll was given no source to read through")
	}

	return a.read.Bind(addrs)
}

// Watching collects every layer's mechanism, refusing on the first layer that
// cannot be watched, so a composite is refused at the bind exactly as a single
// source is.
func (a allOf) Watching() (ferry.Notifier, error) {
	if len(a.of) == 0 {
		return nil, errors.New("watchAll was given no watchable source")
	}

	ns := make([]ferry.Notifier, 0, len(a.of))

	for _, w := range a.of {
		n, err := w.Watching()
		if err != nil {
			return nil, err
		}

		if n == nil {
			return nil, errors.New("a layer handed over no mechanism")
		}

		ns = append(ns, n)
	}

	return fanIn(ns), nil
}

// fanIn is many mechanisms behind one, and it is the whole of what a caller
// composing two watchable planes used to have to write.
type fanIn []ferry.Notifier

// Notify arms every layer before any of them is waited on.
func (f fanIn) Notify(ctx context.Context) (ferry.Change, error) {
	cs := make([]ferry.Change, 0, len(f))

	for _, n := range f {
		c, err := n.Notify(ctx)
		if err != nil {
			closeAll(cs)

			return nil, err
		}

		cs = append(cs, c)
	}

	return &fanInChange{cs: cs}, nil
}

// fanInChange is one registration per layer, waited on together.
type fanInChange struct{ cs []ferry.Change }

// Wait answers with whichever layer speaks first, and stops the rest.
func (f *fanInChange) Wait(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	answers := make(chan waited, len(f.cs))

	for _, c := range f.cs {
		go func() {
			ok, err := c.Wait(ctx)
			answers <- waited{ok: ok, err: err}
		}()
	}

	first := <-answers

	cancel()

	// Every layer's wait is drained before this returns, so no goroutine
	// outlives the call that started it.
	for range len(f.cs) - 1 {
		<-answers
	}

	return first.ok, first.err
}

// waited is one layer's answer.
type waited struct {
	ok  bool
	err error
}

func (f *fanInChange) Close() error {
	closeAll(f.cs)

	return nil
}

// closeAll releases every registration and discards what each reports, which is
// what a watcher can do with it.
func closeAll(cs []ferry.Change) {
	for _, c := range cs {
		_ = c.Close()
	}
}
