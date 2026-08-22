package winreg

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrWatch reports a watch this driver could not open.
//
// A registry that reports no change of its own, and a registration the registry
// would not place, are both this. A watch that opened successfully and never
// fired is the failure this refusal exists to avoid.
//
// Ferry's own wrapper carries [ferry.ErrPlane], and this stays reachable
// underneath it, so errors.Is answers for both on what [ferry.BindWatched] and
// the stream returned.
var ErrWatch = errors.New("winreg: this watch could not be opened")

// Watched converts this source into one that can be watched.
//
//	src := winreg.NewSource(winreg.CurrentUser, `Software\Example`)
//
//	wb, err := ferry.BindWatched[Config](src.Watched())
//	if err != nil {
//		return err
//	}
//
//	seq, errf := wb.Watch(ctx)
//	for cfg := range seq {
//		publish(cfg)
//	}
//
//	return errf()
//
// It takes no arguments, because this source already knows which key it reads.
//
// It touches nothing and starts nothing. Whether this registry can report its
// changes at all is answered at [ferry.BindWatched], with [ErrWatch] reachable
// under the refusal, and the first registration is placed when a stream opens,
// under that stream's own context.
//
// The whole subtree under the driver's key is watched, so a change to any value
// or any subkey at or under it is a change, and a change elsewhere in the hive
// is not. A dump through [Sink] over the same key is a change like any other, so
// a process that both watches and saves its own configuration hears its own
// writes.
//
// A key that is not there yet is not a failure. The registration goes on the
// nearest key above it that does exist, so a process watching the key its own
// first dump will create is told when that dump creates it, and the watch moves
// down to the key itself from then on. The cost is that until the key exists a
// change to something else under that ancestor wakes it too, which is one
// spurious reload.
//
// The source it converts is unchanged and still loadable, and converting twice
// is two watchable sources over one key rather than a mistake.
func (s *Source) Watched() *WatchedSource { return &WatchedSource{src: s} }

// WatchedSource is a [Source] that can be watched, and it is what
// [Source.Watched] returns.
//
// It loads exactly as the source it was converted from does, so it is a
// [ferry.Source] as well as a [ferry.WatchableSource] and [ferry.Load] takes it
// unchanged.
type WatchedSource struct {
	src *Source
}

var (
	_ ferry.Source          = (*WatchedSource)(nil)
	_ ferry.WatchableSource = (*WatchedSource)(nil)
)

// Bind computes this schema's registry keys and checks them, which is exactly
// what the converted source's own Bind does.
func (w *WatchedSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	return w.src.Bind(addrs)
}

// Watching answers with the mechanism this source's changes arrive through.
//
// It refuses a source whose options did not resolve, and one whose registry
// reports no change of its own: a watch over such a registry could never fire,
// and saying so at the bind is what keeps it from being a process that has
// silently stopped reloading. On Windows the machine's own registry always
// reports changes; a [Store] of your own has to say so as well, by implementing
// [Notifier].
//
// It does no I/O. What the operating system has an opinion about - a hive that
// cannot be opened, a registration it will not place - surfaces when a stream
// places its first registration, and ends that stream rather than the bind.
func (w *WatchedSource) Watching() (ferry.Notifier, error) {
	if err := w.src.cfg.validate(); err != nil {
		return nil, err
	}

	on, ok := w.src.cfg.store.(Notifier)
	if !ok {
		return nil, watchError("this registry reports no change of its own, so a watch over it could never fire")
	}

	return arming{on: on}, nil
}

// arming is the whole of the port: this driver's register-once-wait-once seam is
// what core asks a [ferry.Notifier] for, under the other one's name (ADR-0020).
//
// A [Change] answers Wait and Close with exactly what [ferry.Change] asks for,
// so an armed registration crosses the seam as it stands and this type adapts
// the one call whose name differs.
type arming struct{ on Notifier }

// Notify places one registration, which is [Notifier.Arm].
//
// A registration that could not be placed keeps [ErrWatch] reachable, so the
// stream core ends under [ferry.ErrWatchLost] carries this driver's own reason
// underneath it.
func (a arming) Notify(ctx context.Context) (ferry.Change, error) {
	c, err := a.on.Arm(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: the registry would not report its changes: %w",
			ErrWatch, err)
	}

	return c, nil
}

// watchError states the reason under [ErrWatch] and nothing more. Core stamps
// [ferry.ErrPlane] once at its own seam - the bind refusal and the stream
// ending both wrap it there - so minting it here as well would spell the
// prefix twice (ADR-0020).
func watchError(msg string) error {
	return fmt.Errorf("%w: %s", ErrWatch, msg)
}
