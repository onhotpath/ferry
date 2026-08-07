package yaml_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// Example loads a hand-maintained config file, changes two fields and writes it
// back through the same path.
//
// The output is the whole point: a dump is a merge into the document that is
// already there, so the comment, the key order, the quoting ferry did not touch
// and the key no field maps are all still in the file afterwards.
//
// It is quoted in this package's README, which is why it is written to be read
// rather than to cover a branch.
func Example() {
	type config struct {
		Port  int      `ferry:"port"`
		Label string   `ferry:"label"`
		Debug bool     `ferry:"debug"`
		Tags  []string `ferry:"tags"`
	}

	const plane = `# the port the server listens on
port: 8080
label: "8080" # quoted, so it stays a string
debug: false
tags:
  - a
owner: platform-team
`

	path := writeExamplePlane(plane)
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	ctx := context.Background()

	cfg, err := ferry.Load[config](ctx, yaml.NewSource(path))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("port=%d label=%q debug=%v tags=%v\n\n", cfg.Port, cfg.Label, cfg.Debug, cfg.Tags)

	cfg.Debug = true
	cfg.Tags = append(cfg.Tags, "b")

	if err := ferry.Dump(ctx, cfg, yaml.NewSink(path)); err != nil {
		fmt.Println(err)

		return
	}

	back, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Print(string(back))

	// Output:
	// port=8080 label="8080" debug=false tags=[a]
	//
	// # the port the server listens on
	// port: 8080
	// label: "8080" # quoted, so it stays a string
	// debug: true
	// tags:
	//   - a
	//   - b
	// owner: platform-team
}

// ExampleExtension declares this driver's own struct tag key, so a field can
// say what node type its value is written as.
//
// The document going in carries no tag at either address, and the one coming
// out carries the one the field declared. That is the half a save could not do
// before: the boundary hands a driver a value and not a Go type, so nothing in
// a plain `wait: 30s` said this address wanted !mycompany:duration.
//
// It is quoted in this package's README.
func ExampleExtension() {
	type config struct {
		Wait string `ferry:"wait" yamlext:"node=!mycompany:duration"`
		Port int    `ferry:"port"`
	}

	registry := ferry.MustRegistry(ferry.WithTagKeys(yaml.Extension()))

	path := writeExamplePlane("wait: 30s\nport: 8080\n")
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	ctx := context.Background()

	cfg, err := ferry.Load[config](ctx, yaml.NewSource(path), ferry.WithRegistry(registry))
	if err != nil {
		fmt.Println(err)

		return
	}

	if err := ferry.Dump(ctx, cfg, yaml.NewSink(path), ferry.WithRegistry(registry)); err != nil {
		fmt.Println(err)

		return
	}

	back, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Print(string(back))

	// Output:
	// wait: !mycompany:duration 30s
	// port: 8080
}

// ExampleWatch reloads a config file whenever an operator edits it.
//
// The binding is held across the edit, the callback loads through it, and the
// value loaded before the edit is untouched by the one loaded after. That is the
// whole of reloading: a reload is a load, and publishing one means replacing a
// value rather than writing into a value somebody else is reading.
//
// A server would keep the fresh value in an atomic pointer and swap it here. The
// channel is what an example can print.
func ExampleWatch() {
	type config struct {
		Port int `ferry:"port"`
	}

	type reload struct {
		cfg config
		err error
	}

	path := writeExamplePlane("# the port the server listens on\nport: 8080\n")
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	// Cancelling is what stops the watching goroutine, and it is the only thing
	// that does.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	reloaded := make(chan reload, 1)

	// Watching starts when the source is built, which is before Bind has handed
	// back the binding the callback loads through. Closing ready is what orders
	// the two, and a server that keeps its binding in an atomic pointer gets the
	// same ordering from the pointer.
	var b *ferry.Binding[config]

	ready := make(chan struct{})

	onChange := func(ctx context.Context) {
		<-ready

		cfg, err := b.Load(ctx)

		select {
		case reloaded <- reload{cfg: cfg, err: err}:
		default: // this example wants one; a server would take every one
		}
	}

	b, err := ferry.Bind[config](yaml.NewSource(path, yaml.Watch(ctx, 10*time.Millisecond, onChange)))
	if err != nil {
		fmt.Println(err)

		return
	}

	close(ready)

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

	got := <-reloaded
	if got.err != nil {
		fmt.Println(got.err)

		return
	}

	fmt.Printf("held:     %d\n", held.Port)
	fmt.Printf("reloaded: %d\n", got.cfg.Port)

	// Output:
	// held:     8080
	// reloaded: 443
}

// writeExamplePlane puts the example's starting document in a directory of its
// own, because the sink stages its replacement beside the plane.
func writeExamplePlane(doc string) string {
	dir, err := os.MkdirTemp("", "ferry-yaml-example")
	if err != nil {
		panic(err)
	}

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		panic(err)
	}

	return path
}
