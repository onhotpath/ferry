package watcher_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/examples/watcher"
)

// Config is the struct every example below reloads.
type Config struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=8080"`
}

// The whole wiring: a channel the operator kicks, a source that cannot watch
// itself, and one binding that loads and streams.
//
// The range opens with a load, so there is no separate first load to write. The
// value held from before the kick is untouched by the one after it, which is
// what makes publishing a replacement rather than a mutation.
func Example() {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	// Buffered, so a kick that lands mid-reload waits rather than being
	// dropped. A real process fills this channel from SIGHUP alone.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	defer signal.Stop(hup)

	// Cancelling ends the watch and the stream together, and it is the only
	// ending a caller asks for.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[Config](watcher.Kick{Source: plane, On: hup})
	if err != nil {
		fmt.Println("bind:", err)

		return
	}

	seq, errf := wb.Watch(ctx)

	var held, reloaded Config

	for cfg := range seq {
		if held.Host == "" {
			held = cfg // the value the range opened with

			// Two writes and one kick: a change carries no payload, and the
			// reload is what reads both of them.
			plane.Set(ferry.At("host"), ferry.String("db2"))
			plane.Set(ferry.At("port"), ferry.Number("5432"))
			hup <- syscall.SIGHUP

			continue
		}

		reloaded = cfg

		break // one turn is enough for an example; a server keeps ranging
	}

	if err := errf(); err != nil {
		fmt.Println("stream:", err)

		return
	}

	fmt.Printf("held:     %s:%d\n", held.Host, held.Port)
	fmt.Printf("reloaded: %s:%d\n", reloaded.Host, reloaded.Port)

	// Output:
	// held:     db1:8080
	// reloaded: db2:5432
}

// A reload that fails ends the stream with the load's own error, and recovery
// is a second Watch on the same binding: nothing has to be rebuilt.
func ExampleKick_failedReload() {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	hup := make(chan os.Signal, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[Config](watcher.Kick{Source: plane, On: hup})
	if err != nil {
		fmt.Println("bind:", err)

		return
	}

	seq, errf := wb.Watch(ctx)
	for cfg := range seq {
		fmt.Println("loaded:", cfg.Host)

		plane.Delete(ferry.At("host")) // the plane loses a required address
		hup <- syscall.SIGHUP
	}

	fmt.Println("required address missing:", errors.Is(errf(), ferry.ErrMissing))

	plane.Set(ferry.At("host"), ferry.String("db2")) // somebody fixes it

	seq, errf = wb.Watch(ctx)
	for cfg := range seq {
		fmt.Println("loaded:", cfg.Host)

		break
	}

	fmt.Println("stream error:", errf())

	// Output:
	// loaded: db1
	// required address missing: true
	// loaded: db2
	// stream error: <nil>
}

// A Kick with no channel to reload on is refused where every watch refusal
// lands: at BindWatched, before any load, under ferry.ErrPlane.
func ExampleKick_noChannel() {
	_, err := ferry.BindWatched[Config](watcher.Kick{Source: watcher.NewMemPlane()})

	fmt.Println("refused at bind:", errors.Is(err, ferry.ErrPlane))

	// Output:
	// refused at bind: true
}

// A Kick naming no source is refused at the same place, because a wrapper with
// nothing to wrap has no plane to reload from.
func ExampleKick_noSource() {
	hup := make(chan os.Signal, 1)

	_, err := ferry.BindWatched[Config](watcher.Kick{On: hup})

	fmt.Println("refused at bind:", errors.Is(err, ferry.ErrPlane))

	// Output:
	// refused at bind: true
}

// Cancelling the context ends the watch and the stream together, which is the
// ending a process reaches for when it is shutting down.
func ExampleKick_cancelled() {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	hup := make(chan os.Signal, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[Config](watcher.Kick{Source: plane, On: hup})
	if err != nil {
		fmt.Println("bind:", err)

		return
	}

	seq, errf := wb.Watch(ctx)
	for cfg := range seq {
		fmt.Println("loaded:", cfg.Host)

		cancel() // the process is going down
	}

	fmt.Println("cancelled:", errors.Is(errf(), context.Canceled))

	// Output:
	// loaded: db1
	// cancelled: true
}

// Closing the channel takes the mechanism away, and a mechanism that has gone
// away ends the stream under ferry.ErrWatchLost rather than in silence.
func ExampleKick_closed() {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	hup := make(chan os.Signal, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wb, err := ferry.BindWatched[Config](watcher.Kick{Source: plane, On: hup})
	if err != nil {
		fmt.Println("bind:", err)

		return
	}

	seq, errf := wb.Watch(ctx)
	for cfg := range seq {
		fmt.Println("loaded:", cfg.Host)

		close(hup) // nobody will kick this process again
	}

	fmt.Println("watch lost:", errors.Is(errf(), ferry.ErrWatchLost))

	// Output:
	// loaded: db1
	// watch lost: true
}
