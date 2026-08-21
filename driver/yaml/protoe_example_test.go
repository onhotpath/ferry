//go:build protoe

package yaml_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// ExampleSource_Watched reloads a config file whenever an operator edits it.
//
// The whole wiring is two calls: convert the source, bind it, range what comes
// back. The stream opens with a load, so the value the process starts from
// arrives without a separate load, and there is nothing to order and nothing to
// forget.
//
// The value held from before the edit is untouched by the one after it. That is
// the whole of reloading: a reload is a load, and publishing one means replacing
// a value rather than writing into a value somebody else is reading.
//
// A server would keep the fresh value in an atomic pointer and swap it on every
// turn of the range.
func ExampleSource_Watched() {
	type config struct {
		Port int `ferry:"port"`
	}

	path := writeExamplePlane("# the port the server listens on\nport: 8080\n")
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	// Cancelling is what ends the watch and the stream together, and it is the
	// only thing that does.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	wb, err := ferry.BindWatched[config](yaml.NewSource(path).Watched())
	if err != nil {
		fmt.Println(err)

		return
	}

	seq, errf := wb.Watch(ctx)

	// The first value off the stream is the load the range opens with, and the
	// second is the reload the operator's edit produced.
	var held, reloaded config

	for cfg := range seq {
		if held.Port == 0 {
			held = cfg

			editThePlane(path)

			continue
		}

		reloaded = cfg

		break // one turn is enough for an example; a server keeps ranging
	}

	if err := errf(); err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("held:     %d\n", held.Port)
	fmt.Printf("reloaded: %d\n", reloaded.Port)

	// Output:
	// held:     8080
	// reloaded: 443
}

// editThePlane is the operator's own edit, landing while the process holds a
// loaded value.
func editThePlane(path string) {
	if err := os.WriteFile(path, []byte("# the port\nport: 443\n"), 0o600); err != nil {
		fmt.Println(err)
	}
}
