package main

// P15: Close(ctx, cause) conflates two different things.
//
// "Free the resource" and "decide whether the staged write persists" both
// happen at the end of a dump, which is why they ended up in one method. They
// are not the same concern:
//
//   release  - unconditional, both directions, says nothing about success
//   commit   - conditional, write side only, and only for a sink that stages
//
// The read side has no commit, so `cause` was always dead weight there. This
// probe splits them and re-runs every property.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// The split. Note what the first one is: it is io.Closer, exactly.
type Releaser interface{ Close() error }
type Committer interface {
	Commit(ctx context.Context) error
}

// runDump is what ferry's engine does. The protocol is the whole design:
// Commit runs only on success; Close always runs. "Closed without Commit"
// is the abort signal, which is sql.Tx's shape and bufio's.
func runDump(ctx context.Context, w SlimWriter2, sets func() error) (err error) {
	if r, ok := w.(Releaser); ok {
		defer func() { err = errors.Join(err, r.Close()) }()
	}
	if err := sets(); err != nil {
		return err
	}
	if c, ok := w.(Committer); ok {
		return c.Commit(ctx)
	}
	return nil
}

type SlimWriter2 interface {
	Set(context.Context, Path, Value) error
}

func p15Release() {
	head("P15  release is not commit, and only one of them is conditional")

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)

	// (a) The two concerns, per sink.
	fmt.Println("    (a) which sink needs which")
	fmt.Printf("        %-24s %-10s %s\n", "sink", "release", "commit")
	for _, r := range []struct{ s, rel, com string }{
		{"yaml file (staging)", "yes", "yes"},
		{"kv, transactional", "no", "yes"},
		{"kv, write-per-Set", "no", "no"},
		{"recorder (ferrytest)", "no", "no"},
		{"http PUT per key", "no", "no"},
		{"lazy kv source (read)", "yes", "n/a"},
	} {
		fmt.Printf("        %-24s %-10s %s\n", r.s, r.rel, r.com)
	}
	fmt.Println("        They do not co-occur. Two sinks want one and not the other,")
	fmt.Println("        and the read side wants release with no commit at all -")
	fmt.Println("        which is why `cause` was meaningless there.")

	// (b) Releaser IS io.Closer, so it is free for anything already closeable.
	fmt.Println("\n    (b) Releaser is io.Closer")
	var _ Releaser = (*os.File)(nil)
	var _ io.Closer = Releaser(nil)
	fmt.Println("        var _ Releaser = (*os.File)(nil)   compiles")
	fmt.Println("        A driver wrapping a file, a conn or a client satisfies it")
	fmt.Println("        for free, and every Go author already knows the method.")

	// (c) A staging sink, both halves, success path.
	fmt.Println("\n    (c) staging yaml sink, success")
	out := filepath.Join(dir, "out.yaml")
	os.WriteFile(out, []byte("keep: me\n"), 0o644)
	w := newStagedYAML(out)
	err := runDump(ctx, w, func() error { return w.Set(ctx, path("a"), String("1")) })
	b, _ := os.ReadFile(out)
	tmps, _ := filepath.Glob(filepath.Join(dir, ".ferry-*"))
	fmt.Printf("        err=%v plane=%q temps=%d\n", err, string(b), len(tmps))

	// (d) Same sink, failure path. Nothing said "abort" - Commit just did not run.
	fmt.Println("\n    (d) same sink, the walk fails partway")
	os.WriteFile(out, []byte("keep: me\n"), 0o644)
	w2 := newStagedYAML(out)
	err = runDump(ctx, w2, func() error {
		_ = w2.Set(ctx, path("a"), String("1"))
		return errors.New("field /b: invalid")
	})
	b, _ = os.ReadFile(out)
	tmps, _ = filepath.Glob(filepath.Join(dir, ".ferry-*"))
	fmt.Printf("        err=%v\n", err)
	fmt.Printf("        plane=%q temps=%d\n", string(b), len(tmps))
	check("plane byte-identical after a failed dump", string(b) == "keep: me\n")
	check("no temp file leaked", len(tmps) == 0)
	fmt.Println("        The sink never needed to be told it failed. It knows,")
	fmt.Println("        because Close ran and Commit did not - one bool.")

	// (e) A sink that needs neither writes no lifecycle code at all.
	fmt.Println("\n    (e) a sink that needs neither")
	rec := map[Path]Value{}
	err = runDump(ctx, recWriter2{rec}, func() error { return recWriter2{rec}.Set(ctx, path("a"), String("1")) })
	_, isRel := any(recWriter2{}).(Releaser)
	_, isCom := any(recWriter2{}).(Committer)
	fmt.Printf("        recorder: Releaser=%v Committer=%v captured=%d err=%v\n", isRel, isCom, len(rec), err)
	fmt.Println("        One method total. No `return nil` standing in for a decision")
	fmt.Println("        it never had to make.")

	// (f) The read side, which now has an honest signature.
	fmt.Println("\n    (f) the read side")
	lazy := &lazyConnReader{}
	rel, _ := any(lazy).(Releaser)
	_ = rel
	if r, ok := any(lazy).(Releaser); ok {
		_ = r.Close()
	}
	fmt.Printf("        lazy reader holding a connection: released=%v\n", lazy.closed)
	fmt.Println("        Close() error, no ctx and no cause, because there is")
	fmt.Println("        nothing to roll back and nothing to decide. It is exactly")
	fmt.Println("        io.Closer, and the phase it belongs to is 'this load is over'.")

	// (g) Surface.
	fmt.Println("\n    (g) surface")
	fmt.Printf("        %-22s %s\n", "Reader", "Get                    required")
	fmt.Printf("        %-22s %s\n", "Writer", "Set                    required")
	fmt.Printf("        %-22s %s\n", "Releaser", "Close() error          optional, both directions")
	fmt.Printf("        %-22s %s\n", "Committer", "Commit(ctx) error      optional, write side")
	fmt.Printf("        %-22s %s\n", "Enumerator", "Children(ctx, Path)    optional, read side")
	fmt.Println("        Releaser is io.Closer, so it is not a new name ferry invents.")
}

// --- a staging sink that needs both ----------------------------------------

type stagedYAML struct {
	path      string
	tmp       *os.File
	root      *yaml.Node
	committed bool
}

func newStagedYAML(p string) *stagedYAML {
	tmp, _ := os.CreateTemp(filepath.Dir(p), ".ferry-*")
	return &stagedYAML{path: p, tmp: tmp, root: &yaml.Node{Kind: yaml.MappingNode}}
}

func (w *stagedYAML) Set(ctx context.Context, p Path, v Value) error {
	return (&yamlWriter{w.path, w.tmp, w.root}).Set(ctx, p, v)
}

func (w *stagedYAML) Commit(context.Context) error {
	enc := yaml.NewEncoder(w.tmp)
	enc.SetIndent(2)
	if err := errors.Join(enc.Encode(w.root), enc.Close()); err != nil {
		return err
	}
	if err := w.tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(w.tmp.Name(), w.path); err != nil {
		return err
	}
	w.committed = true
	return nil
}

// Close is unconditional cleanup. It does not need to be told what happened.
func (w *stagedYAML) Close() error {
	if w.committed {
		return nil
	}
	w.tmp.Close()
	return os.Remove(w.tmp.Name())
}

// --- a sink that needs neither ---------------------------------------------

type recWriter2 struct{ m map[Path]Value }

func (w recWriter2) Set(_ context.Context, p Path, v Value) error { w.m[p] = v; return nil }

// --- a reader that needs release only ---------------------------------------

type lazyConnReader struct{ closed bool }

func (r *lazyConnReader) Get(context.Context, Path) (Value, error) { return Absent, nil }
func (r *lazyConnReader) Close() error                             { r.closed = true; return nil }
