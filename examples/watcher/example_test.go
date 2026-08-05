package watcher_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/examples/watcher"
)

// Config is the struct both examples watch.
type Config struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=8080"`
}

// The whole loop: bind once, hold a value, change the plane, receive a fresh
// one. The held value is untouched by the reload, which is what makes
// publication a replacement rather than a mutation.
func Example() {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	ctx := context.Background()

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

	seq, errf := watcher.Watch(ctx, b, plane.Changes())

	// Two writes land before the loop reads its signal, so the two signals
	// coalesce into one reload that sees both. A signal carries no payload;
	// the reload is what reads the truth.
	plane.Set(ferry.At("host"), ferry.String("db2"))
	plane.Set(ferry.At("port"), ferry.Number("5432"))

	for cfg := range seq {
		fmt.Printf("reloaded: %s:%d\n", cfg.Host, cfg.Port)
		break // one turn is enough for an example; a server would keep ranging
	}

	fmt.Printf("held:     %s:%d\n", held.Host, held.Port)
	fmt.Println("stream error:", errf())

	// Output:
	// reloaded: db2:5432
	// held:     db1:8080
	// stream error: <nil>
}

// A reload that fails ends the stream with no value yielded, and errf carries
// the failure out of the range.
func ExampleWatch_failedReload() {
	plane := watcher.NewMemPlane()
	plane.Set(ferry.At("host"), ferry.String("db1"))

	b, err := ferry.Bind[Config](plane)
	if err != nil {
		fmt.Println("bind:", err)
		return
	}

	seq, errf := watcher.Watch(context.Background(), b, plane.Changes())

	plane.Delete(ferry.At("host")) // the plane loses a required address

	for range seq {
		fmt.Println("unreachable: a failed reload yields no value")
	}

	fmt.Println("required address missing:", errors.Is(errf(), ferry.ErrMissing))

	// Output:
	// required address missing: true
}
