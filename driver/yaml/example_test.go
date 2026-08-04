package yaml_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
