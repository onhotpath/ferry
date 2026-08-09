package watch_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/watch"
)

// Config is the struct the examples and the tests watch.
type Config struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=8080"`
}

// The whole loop: signal first, bind once, hold a value, change the plane,
// receive a fresh one. The held value is untouched by the reload, which is what
// makes publication a replacement rather than a mutation.
func Example() {
	plane := newMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	ctx := context.Background()

	s := watch.New()
	plane.OnChange(s.Changed) // what a driver's watch option takes

	b, err := ferry.Bind[Config](plane)
	if err != nil {
		fmt.Println("bind:", err)

		return
	}

	held, err := b.Load(ctx) // the value this goroutine is holding
	if err != nil {
		fmt.Println("load:", err)

		return
	}

	seq, errf := watch.Values(ctx, s, b)

	// Two writes land before the range reads the signal, so the two changes
	// coalesce into one reload that sees both.
	plane.Set(ferry.At("host"), ferry.String("db2"))
	plane.Set(ferry.At("port"), ferry.Number("5432"))

	for cfg := range seq {
		fmt.Printf("reloaded: %s:%d\n", cfg.Host, cfg.Port)

		break // one turn is enough for an example; a server keeps ranging
	}

	fmt.Printf("held:     %s:%d\n", held.Host, held.Port)
	fmt.Println("stream error:", errf())

	// Output:
	// reloaded: db2:5432
	// held:     db1:8080
	// stream error: <nil>
}

// A failed reload ends the stream, so a process that wants to survive one ranges
// again on the same signal. Nothing is lost in between: the change that fixed
// the plane is pending when the second stream opens.
func Example_failedReload() {
	plane := newMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	ctx := context.Background()

	s := watch.New()
	plane.OnChange(s.Changed)

	b, err := ferry.Bind[Config](plane)
	if err != nil {
		fmt.Println("bind:", err)

		return
	}

	plane.Delete(ferry.At("host")) // the plane loses a required address

	for {
		seq, errf := watch.Values(ctx, s, b)
		for cfg := range seq {
			fmt.Println("reloaded:", cfg.Host)

			break // a server would keep ranging until it was told to stop
		}

		err := errf()
		if err == nil || errors.Is(err, context.Canceled) {
			return // the range ended cleanly, or the process is shutting down
		}

		fmt.Println("reload failed, address missing:", errors.Is(err, ferry.ErrMissing))

		plane.Set(ferry.At("host"), ferry.String("db2")) // somebody fixes it
	}

	// Output:
	// reload failed, address missing: true
	// reloaded: db2
}
