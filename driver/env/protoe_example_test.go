//go:build protoe

package env_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
)

// ExampleSource_Watched reloads when a file the source reads changes underneath
// it.
//
// The whole wiring is one expression: the files are named once, the conversion
// to a watchable source is on the value that already holds them, and the stream
// opens with a load. There is nothing to order and nothing to forget.
func ExampleSource_Watched() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("NAME=checkout\nDB_HOST=first\n"), 0o600)

	// Cancelling is what ends the watch and the stream together, and it is the
	// only thing that does.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := env.New(env.Environ(func() []string { return nil }), env.DotEnv(path)).Watched()

	wb, err := ferry.BindWatched[Config](src)
	if err != nil {
		fmt.Println(err)

		return
	}

	seq, errf := wb.Watch(ctx)

	var held, reloaded Config

	for cfg := range seq {
		if held.DB.Host == "" {
			held = cfg

			editTheFile(path)

			continue
		}

		reloaded = cfg

		break // one turn is enough for an example; a server keeps ranging
	}

	if err := errf(); err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println("held:    ", held.DB.Host)
	fmt.Println("reloaded:", reloaded.DB.Host)

	// Output:
	// held:     first
	// reloaded: second
}

// editTheFile is the operator's own edit, landing while the process holds a
// loaded value.
func editTheFile(path string) {
	if err := os.WriteFile(path, []byte("NAME=checkout\nDB_HOST=second\n"), 0o600); err != nil {
		fmt.Println(err)
	}
}
