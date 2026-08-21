//go:build protoe

package winreg

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// This file is variant E of the typed watch prototype, ported to this driver's
// change notification.
//
// [NewSource] is untouched and keeps returning a [Source]. Watching is a
// conversion on the source that already holds the hive, the subkey and the
// registry, so there is nothing to wire and nothing new to name.
//
// Almost nothing is implemented here. This driver's own [Notifier] already
// registers once and waits once, which is the shape core asks for, so the
// conversion is an adapter over the seam that was already in registry.go: Arm
// is Notify, and a [Change] is a [ferry.Change] as it stands.

// ErrWatch reports a watch this driver could not open.
//
// A registry that reports no change of its own, and a registration the registry
// would not place, are both this. A watch that succeeded silently and never
// fired is the failure this refusal exists to avoid.
//
// It wraps [ferry.ErrNotWatchable], and it stays reachable under ferry's
// wrapper.
var ErrWatch = errors.New("winreg: this watch could not be opened")

// Watched converts this source into one that can be watched.
//
//	src := winreg.NewSource(winreg.CurrentUser, `Software\Example`)
//	wb, err := ferry.BindWatched[Config](src.Watched())
//
// It takes no arguments, because this source already knows which key it reads
// and the mechanism has no interval to name.
//
// It touches nothing and starts nothing. Whether this registry can report its
// changes at all is answered at [ferry.BindWatched], and the first registration
// is placed when a stream opens, under that stream's own context.
//
// The whole subtree under the driver's own key is watched, so a change to any
// value at or under it is a change. A key that does not exist yet is not a
// failure: the registration goes on the nearest existing key above it, so a
// process watching the key its own first dump will create is told when the dump
// creates it.
//
// The source it converts is unchanged and still loadable, and converting twice
// is two watchable sources over one key rather than a mistake.
func (s *Source) Watched() *WatchedSource { return &WatchedSource{src: s} }

// WatchedSource is a [Source] that can be watched, and it is what [Watched]
// returns. It loads exactly as the source it was converted from does.
type WatchedSource struct {
	src *Source
}

var _ ferry.WatchableSource = (*WatchedSource)(nil)

// Bind is the source's own Bind, so a WatchedSource loads through [ferry.Bind]
// and [ferry.Load] like any other.
func (w *WatchedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return w.src.Bind(addrs)
}

// Watching answers with the mechanism this source's changes arrive through.
//
// It refuses a source whose options did not resolve, and one whose registry
// reports no change of its own: a watch over such a registry could never fire,
// and saying so at the bind is what keeps it from being a process that has
// silently stopped reloading.
//
// It does no I/O. The first registration is placed when a stream opens.
func (w *WatchedSource) Watching() (ferry.Notifier, error) {
	if err := w.src.cfg.err; err != nil {
		return nil, err
	}

	on, ok := w.src.cfg.store.(Notifier)
	if !ok {
		return nil, notWatchable("this registry reports no change of its own, so a watch over it could never fire")
	}

	return arming{on: on}, nil
}

// arming is the adapter, and it is the whole of the port: this driver's
// register-once-wait-once seam is what core asks a [ferry.Notifier] for, under
// the other one's name.
type arming struct{ on Notifier }

// Notify places one registration, which is [Notifier.Arm].
//
// A [Change] answers Wait and Close with exactly what [ferry.Change] asks for,
// so the registration crosses the seam as it stands.
func (a arming) Notify(ctx context.Context) (ferry.Change, error) {
	c, err := a.on.Arm(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: the registry would not report its changes: %w", ErrWatch, err)
	}

	return c, nil
}

// notWatchable states the class this driver has an opinion about and keeps
// [ErrWatch] reachable underneath it.
func notWatchable(msg string) error {
	return fmt.Errorf("%w: %s", ErrWatch, msg)
}
