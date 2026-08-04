// Command step6 runs a realistic config struct against an env-shaped plane
// under each of the three key functions, in both directions.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/proto125/kf"
)

// Config is the shape a service actually writes: some static fields, and a map
// whose keys are runtime data.
type Config struct {
	DB     DB                `ferry:"db"`
	Labels map[string]string `ferry:"labels"`
}

// DB is the static part.
type DB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}

// Tagged is the static route: a dot written into a tag, beside the nested
// struct it collides with under a transforming key function.
type Tagged struct {
	Legacy string `ferry:"db.host"`
	DB     DB     `ferry:"db"`
}

func main() {
	fmt.Println("== 6a: can the operating system even hold these names? ==")
	osNames()

	fmt.Println()
	fmt.Println("== 6b: Dump, dot from a map key ==")

	cfg := Config{
		DB:     DB{Host: "db1", Port: 5432},
		Labels: map[string]string{"app.name": "ferry", "team": "core"},
	}

	for _, f := range kf.EnvThree() {
		dump(f, cfg)
	}

	fmt.Println()
	fmt.Println("== 6c: Dump, two map keys that fold together ==")

	both := Config{
		DB:     DB{Host: "db1", Port: 5432},
		Labels: map[string]string{"app.name": "dotted", "app_name": "scored"},
	}

	for _, f := range kf.EnvThree() {
		dump(f, both)
	}

	fmt.Println()
	fmt.Println("== 6d: Dump, dot from a struct tag (static, so Bind sees it) ==")

	for _, f := range kf.EnvThree() {
		dumpTagged(f)
	}

	fmt.Println()
	fmt.Println("== 6e: round trip, dump then load through the same key function ==")

	for _, f := range kf.EnvThree() {
		roundTrip(f, cfg)
	}

	fmt.Println()
	fmt.Println("== 6f: the same Dump through a driver that hand-rolls its table ==")
	handRolled(both)
}

// osNames measures whether a name a key function produces can be carried by the
// process environment and by a POSIX shell.
func osNames() {
	for _, name := range []string{"DB_HOST", "LABELS_APP.NAME", "FEATURE-FLAGS"} {
		goErr := os.Setenv(name, "x")
		got := os.Getenv(name)

		out, shErr := exec.Command("/bin/sh", "-c", name+"=x").CombinedOutput()

		fmt.Printf("  %-16s os.Setenv err=%v os.Getenv=%q | sh %s=x: %s\n",
			name, goErr, got, name, shResult(out, shErr))
	}

	envOut, _ := exec.Command("/usr/bin/env", "LABELS_APP.NAME=x", "sh", "-c", "echo ran").CombinedOutput()
	fmt.Printf("  env(1) can still place LABELS_APP.NAME in a child: %q\n", trim(envOut))
}

func shResult(out []byte, err error) string {
	if err != nil {
		return fmt.Sprintf("FAILED (%v) %q", err, trim(out))
	}

	return fmt.Sprintf("ok %q", trim(out))
}

func trim(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}

	return string(b)
}

func dump(f kf.Named, cfg Config) {
	sink := kf.NewFlatSink("env", f.F)

	err := ferry.Dump(context.Background(), cfg, sink)

	fmt.Printf("  %-38s ", f.Label)

	if err != nil {
		fmt.Println("FAILED")

		for _, e := range ferry.Elements(err) {
			fmt.Printf("      %s\n", e)
		}

		fmt.Printf("      plane after the failure: %v\n", sink.Plane)

		return
	}

	fmt.Printf("wrote %v\n", sink.Plane)
}

func dumpTagged(f kf.Named) {
	sink := kf.NewFlatSink("env", f.F)

	err := ferry.Dump(context.Background(), Tagged{Legacy: "old", DB: DB{Host: "db1", Port: 1}}, sink)

	fmt.Printf("  %-38s ", f.Label)

	if err != nil {
		fmt.Println("FAILED at Bind, before any write")

		for _, e := range ferry.Elements(err) {
			fmt.Printf("      %s\n", e)
		}

		return
	}

	fmt.Printf("wrote %v\n", sink.Plane)
}

// roundTrip dumps the config through one key function and loads it straight
// back through the same one, which is the honest test of what a flat plane can
// carry.
func roundTrip(f kf.Named, cfg Config) {
	fmt.Printf("  %-38s ", f.Label)

	sink := kf.NewFlatSink("env", f.F)
	if err := ferry.Dump(context.Background(), cfg, sink); err != nil {
		fmt.Printf("dump FAILED: %v\n", err)

		return
	}

	src := &kf.FlatSource{Name: "env", F: f.F, Plane: sink.Plane}

	got, err := ferry.Load[Config](context.Background(), src)
	if err != nil {
		fmt.Printf("load FAILED: %v\n", err)

		return
	}

	fmt.Printf("plane %v\n", sink.Keys())
	fmt.Printf("  %-38s back as %+v\n", "", got)
}

// sloppySink is the driver ADR-0003 warns about: it computes a plane key per
// write and routes through no injectivity check at all.
type sloppySink struct{ plane map[string]string }

func (s *sloppySink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return s, nil }, nil
}

func (s *sloppySink) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	key, err := kf.EnvScrub(addr)
	if err != nil {
		return err
	}

	s.plane[key] = kf.Text(v)

	return nil
}

func handRolled(cfg Config) {
	s := &sloppySink{plane: map[string]string{}}

	err := ferry.Dump(context.Background(), cfg, s)

	fmt.Printf("  hand-rolled transforming sink: err=%v\n", err)
	fmt.Printf("  plane: %v\n", s.plane)
}
