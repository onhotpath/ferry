package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// The write half implements both lifecycle interfaces, which no other
// first-party plane does: it stages a replacement, so it has something to
// commit, and it holds an open file, so it has something to release.
var (
	_ ferry.Sink      = Sink{}
	_ ferry.Writer    = (*writer)(nil)
	_ ferry.Committer = (*writer)(nil)
	_ ferry.Releaser  = (*writer)(nil)
)

// indent is how wide a level is on the way out. YAML's emitter has to be told a
// width and cannot be told to keep the one it read, so two spaces is written
// here rather than discovered - it is the convention nearly every hand-written
// config file already uses, and a document that used four is re-indented rather
// than mangled.
const indent = 2

// Sink writes a struct's fields into a YAML file.
//
// A save is a merge into whatever document is already at the path: the keys
// your struct maps are replaced, and everything else - comments, key order, and
// every key no field of yours maps - is left as it was. That is what makes a
// hand-maintained config file survive being loaded and written back.
//
// An anchor is the exception, and it is deliberate. A value ferry replaces keeps
// the anchor you wrote on it, so a key no field maps that aliases it reads back
// as the value just written: its line does not change and its value does.
//
// The write is atomic. A temporary file beside yours is renamed into place once
// everything has been written, and a save that fails leaves your file byte for
// byte as it was with no temporary left behind.
//
// It is a separate type from [Source] for the reason recorded there.
type Sink struct {
	path string
}

// NewSink returns a sink over the YAML file at path.
//
// It touches nothing, and in particular it does not check that the path can be
// written. A sink over an unwritable directory is legal to build, and the save
// refuses when it starts.
func NewSink(path string) Sink { return Sink{path: path} }

// Bind takes the address set and reads nothing out of it, for the reason
// [Source.Bind] records.
//
// It does no I/O, so a file that cannot be written is not refused here. The
// refusal lands when the save starts, which is before anything has been
// written, rather than part way through.
func (s Sink) Bind(_ *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	path := s.path

	return func(ctx context.Context) (ferry.Writer, error) { return open(ctx, path) }, nil
}

// open reads the document that is there and stages the file that will replace
// it.
//
// Both halves are here rather than at the first write, because both are ways
// the plane can refuse and the open is where a refusal costs nothing: a
// document that does not parse would otherwise be silently overwritten, and a
// directory that takes no new file would otherwise be discovered half way
// through a walk.
func open(ctx context.Context, path string) (*writer, error) {
	doc, err := readDoc(ctx, path)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".ferry-*")
	if err != nil {
		// ErrReadOnly is the class whatever the reason the file could not be
		// created, and it is subordinate to ErrPlane, so a caller matching
		// either one is answered. What the plane said stays in the chain.
		return nil, fmt.Errorf("%w: no replacement could be staged beside it: %w", ferry.ErrReadOnly, err)
	}

	return &writer{path: path, doc: doc, tmp: tmp}, nil
}

// writer is one open dump: the document being built up, and the file that will
// become the plane.
//
// The staging state is one bool. Commit sets it after the rename has happened,
// and Close reads it to tell a dump that finished from a dump that was
// abandoned - which is the whole protocol, because closed-without-Commit is the
// abort signal and no driver is ever told that it failed (ADR-0004).
type writer struct {
	path string
	doc  *yamlv3.Node
	tmp  *os.File

	committed bool
}

// Set writes one value at one address, creating the containers above it.
//
// It is never called with an Absent, and it refuses one rather than writing a
// null for it. That refusal is a regression test rather than a nicety: a
// prototype's sink mapped Absent onto !!null, so an address ferry omitted was
// written as an explicit null and read back as Null - the Absent-versus-Null
// conflation ferry criticises xload for, committed on the write path, where a
// later load cannot tell it from a null the operator wrote.
func (w *writer) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	spelled, err := spell(v)
	if err != nil {
		return ferry.ErrorAt(addr, err)
	}

	at, err := place(w.doc, addr)
	if err != nil {
		return ferry.ErrorAt(addr, err)
	}

	// The comments around the value are the operator's and survive the value
	// being replaced. The value, the tag and the style are ferry's.
	spelled.HeadComment, spelled.LineComment, spelled.FootComment = at.HeadComment, at.LineComment, at.FootComment

	// So is the anchor, for the same reason and with a sharper consequence
	// (#196): dropping it leaves every alias to this node dangling, so the save
	// reports success and writes a document no reader can parse. Keeping it
	// means an alias at an address no field maps now reads back as the value
	// just written here, which is what the operator asked for by writing one.
	spelled.Anchor = at.Anchor

	*at = *spelled

	return nil
}

// Commit emits the document into the staged file and renames it over the plane.
//
// The rename is what makes the replacement atomic: a reader of the plane sees
// the old document or the new one and never a half-written file, and a failure
// anywhere before the rename leaves the plane byte-identical.
//
// It runs only where the walk succeeded, which is why nothing here has to ask
// whether it did.
func (w *writer) Commit(_ context.Context) error {
	if err := w.emit(); err != nil {
		return err
	}

	if err := w.inheritMode(); err != nil {
		return err
	}

	if err := os.Rename(w.tmp.Name(), w.path); err != nil {
		return fmt.Errorf("%w: the staged document could not replace the plane: %w", ferry.ErrPlane, err)
	}

	w.committed = true

	return nil
}

// emit writes the document into the staged file and closes it.
//
// A document with no content is an empty file rather than an error, which is
// what a dump that wrote nothing at all should leave behind.
//
// The failure class is [ferry.ErrValue], because what fails here is a value
// this plane cannot spell: the emitter refuses invalid UTF-8, and it refuses
// raw bytes under a !!binary tag by name, which is the check that caught a
// prototype's unencoded bytes (ADR-0005).
func (w *writer) emit() error {
	if len(w.doc.Content) > 0 {
		enc := yamlv3.NewEncoder(w.tmp)
		enc.SetIndent(indent)

		if err := enc.Encode(w.doc); err != nil {
			return fmt.Errorf("%w: the document could not be written as YAML: %w", ferry.ErrValue, err)
		}

		if err := enc.Close(); err != nil {
			return fmt.Errorf("%w: the document could not be written as YAML: %w", ferry.ErrValue, err)
		}
	}

	if err := w.tmp.Sync(); err != nil {
		return fmt.Errorf("%w: the staged document could not be flushed: %w", ferry.ErrPlane, err)
	}

	return w.tmp.Close()
}

// inheritMode gives the staged file the mode the plane already had.
//
// Without it a dump would silently tighten or loosen the permissions of a file
// somebody else set up, because a staged file is created 0600 and a rename
// carries its own mode over. Where there is no plane yet, that 0600 stands: a
// file ferry creates may hold whatever the struct held, and the narrow mode is
// the one to be wrong in the safe direction with.
func (w *writer) inheritMode() error {
	// A plane that is not there yet has no mode to inherit, and the stat
	// failing for any other reason is a question the rename is about to ask
	// again and answer better.
	if fi, err := os.Stat(w.path); err == nil {
		if err := os.Chmod(w.tmp.Name(), fi.Mode().Perm()); err != nil {
			return fmt.Errorf("%w: the staged document could not take the plane's mode: %w", ferry.ErrPlane, err)
		}
	}

	return nil
}

// Close removes the staged file where the dump did not commit, and does nothing
// where it did.
//
// It runs whether the walk succeeded or failed, and a walk that failed is a
// Close with no Commit before it, which is the only thing this driver is told
// about the failure and the only thing it needs: the plane is still byte for
// byte what it was, and the temporary that would otherwise be left behind is
// what this removes.
func (w *writer) Close() error {
	if w.committed {
		return nil
	}

	// The staged file may already be closed, where the emit got that far before
	// something after it failed. Closing twice reports an error about the second
	// close and nothing about the dump, so it is the removal that is reported.
	_ = w.tmp.Close()

	if err := os.Remove(w.tmp.Name()); err != nil {
		return fmt.Errorf("%w: the staged document could not be removed: %w", ferry.ErrPlane, err)
	}

	return nil
}
