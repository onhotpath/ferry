//go:build !protoe

package env_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
	"github.com/onhotpath/ferry/watch"
)

// ExampleWatchFiles reloads when the file changes underneath a binding.
//
// The watch starts when the source is built, which is before ferry.Bind has
// handed back the binding to load through, so a change can land while there is
// nothing to load. A watch.Signal records it instead of losing it, and
// watch.Values turns every change into a freshly loaded value.
func ExampleWatchFiles() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("NAME=checkout\nDB_HOST=first\n"), 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := watch.New()

	src := env.New(env.Environ(func() []string { return nil }), env.DotEnv(path),
		env.WatchFiles(ctx, s.Changed))

	b, err := ferry.Bind[Config](src)
	if err != nil {
		fmt.Println(err)

		return
	}

	held, err := b.Load(ctx) // the value this goroutine is holding
	if err != nil {
		fmt.Println(err)

		return
	}

	// The operator's own edit, landing while the process holds a loaded value.
	_ = os.WriteFile(path, []byte("NAME=checkout\nDB_HOST=second\n"), 0o600)

	seq, errf := watch.Values(ctx, s, b)
	for cfg := range seq {
		fmt.Println("held:    ", held.DB.Host)
		fmt.Println("reloaded:", cfg.DB.Host)

		break // one turn is enough for an example; a server keeps ranging
	}

	if err := errf(); err != nil {
		fmt.Println(err)
	}

	// Output:
	// held:     first
	// reloaded: second
}
