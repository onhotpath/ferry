//go:build !protoe

package yaml_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
	"github.com/onhotpath/ferry/watch"
)

// ExampleWatch reloads a config file whenever an operator edits it.
//
// The binding is held across the edit, the callback loads through it, and the
// value loaded before the edit is untouched by the one loaded after. That is the
// whole of reloading: a reload is a load, and publishing one means replacing a
// value rather than writing into a value somebody else is reading.
//
// A server would keep the fresh value in an atomic pointer and swap it on every
// turn of the range.
func ExampleWatch() {
	type config struct {
		Port int `ferry:"port"`
	}

	path := writeExamplePlane("# the port the server listens on\nport: 8080\n")
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	// Cancelling is what stops the watching goroutine, and it is the only thing
	// that does.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// Watching starts when the source is built, which is before Bind has handed
	// back the binding to load through. The signal is what keeps a change that
	// lands in that window: it records one, and the stream opens with it.
	s := watch.New()

	b, err := ferry.Bind[config](yaml.NewSource(path, yaml.Watch(ctx, 10*time.Millisecond, s.Changed)))
	if err != nil {
		fmt.Println(err)

		return
	}

	held, err := b.Load(ctx)
	if err != nil {
		fmt.Println(err)

		return
	}

	// The operator's own edit, landing while the process holds a loaded value.
	if err := os.WriteFile(path, []byte("# the port the server listens on\nport: 443\n"), 0o600); err != nil {
		fmt.Println(err)

		return
	}

	seq, errf := watch.Values(ctx, s, b)
	for cfg := range seq {
		fmt.Printf("held:     %d\n", held.Port)
		fmt.Printf("reloaded: %d\n", cfg.Port)

		break // one turn is enough for an example; a server keeps ranging
	}

	if err := errf(); err != nil {
		fmt.Println(err)
	}

	// Output:
	// held:     8080
	// reloaded: 443
}
