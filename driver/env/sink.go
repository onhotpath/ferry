package env

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// The write half implements both lifecycle interfaces: it stages a replacement,
// so it has something to commit, and it holds an open file, so it has something
// to release. It implements no [ferry.Preparer], because there is nothing to
// check across the whole address set that the open has not checked already, and
// no [ferry.Concurrent], because a dump walk is single-threaded.
var (
	_ ferry.Sink       = (*DotEnvSink)(nil)
	_ ferry.Writer     = (*dotEnvWriter)(nil)
	_ ferry.Ensurer    = (*dotEnvWriter)(nil)
	_ ferry.Unsetter   = (*dotEnvWriter)(nil)
	_ ferry.Committer  = (*dotEnvWriter)(nil)
	_ ferry.Releaser   = (*dotEnvWriter)(nil)
	_ ferry.PlaneNamer = (*dotEnvWriter)(nil)
)

// DotEnvSink writes a struct's fields into a .env file.
//
//	err := ferry.Dump(ctx, cfg, env.NewDotEnvSink(".env"))
//
// A save is a merge into whatever file is already at the path: the variables
// your struct maps are replaced where they stand, and everything else - the
// comments, the order, the "export " prefixes, the spacing and every variable no
// field of yours maps - is left byte for byte as it was. That is what makes a
// hand-maintained .env file survive being loaded and written back.
//
// A slice or a map your struct maps is the one place the merge stops, because it
// is replaced whole: a three-element slice saved over with one element leaves one
// variable in the file, and the other two are removed along with the comment
// block directly above each of them.
//
// A value is written back in the quoting the line already used where the new
// value permits it, and otherwise in the narrowest quoting that holds it: bare
// where nothing needs quoting, single quotes for text, and double quotes with
// escapes for anything else.
//
// A value this driver writes is one a shell reads back identically when the file
// is sourced, with two exceptions, both in the double-quoted case. A shell does
// not read "\n", "\r" or "\t" as the byte this driver means by them, so a value
// holding a newline, a carriage return or a tab is one thing to ferry and
// another to a shell that sources the file. And a value holding a single quote
// together with a "$" or a backtick has to be double-quoted and is then expanded
// by the shell. Both round trip through ferry exactly.
//
// The write is atomic. A temporary file beside yours is renamed into place once
// everything has been written, and a save that fails leaves your file byte for
// byte as it was with no temporary left behind.
//
// A path that is a symlink is written through rather than replaced: the file the
// link names is the one a save reads, stages beside and renames over, and the
// link itself is left exactly as it is. The link is followed once per save, so
// re-pointing it between two saves sends the second one to the new file.
//
// A save that started before somebody else edited the file refuses rather than
// overwriting them, reports [ferry.ErrPlane] and leaves your file as it was:
// load again, apply your changes to what the file holds now, and save again. The
// check is the file's length and modification time, so the one edit it cannot
// see is a rewrite in the same modification-time tick that leaves the length
// alone.
//
// It writes the file and nothing else unless you pass [Setenv]. Read that
// option: without it, a save that changes DB_HOST leaves the running process
// exporting the old value, and the next load through [Source] answers with the
// old one.
//
// It is not durable unless you ask. Pass [Durable] to flush the replacement to
// the disk before the save returns, and read that option before you do: it is
// the most expensive thing a save can be told to do.
type DotEnvSink struct {
	path string
	cfg  sinkConfig
}

// NewDotEnvSink returns a sink over the .env file at path.
//
//	sink := env.NewDotEnvSink(".env", env.Separator("__"))
//
// One path, because a save has one destination. [DotEnv] takes several because a
// load has several layers, and the sink writes the one the caller names.
//
// Give it the same [Naming] settings the source has. Nothing checks that the two
// agree, and a sink writing TAGS_0 with a source reading TAGS__0 is a round trip
// that loses the slice.
//
// It touches nothing, and in particular it does not check that the path can be
// written. A sink over an unwritable directory is legal to build, and the save
// refuses when it starts.
func NewDotEnvSink(path string, opts ...SinkOption) *DotEnvSink {
	c := sinkDefaults()
	for _, o := range opts {
		o.applySink(&c)
	}

	return &DotEnvSink{path: path, cfg: c}
}

// Bind computes this schema's variable names and checks them, and it is where a
// schema this plane cannot write is refused.
//
// Two things are checked, before any file is opened: that every address has a
// variable name at all, and that no two fold to one name. A third check comes
// from core and matters more here than it does on the read side: a name that
// lies inside the space a slice or a map is enumerated out of is refused, which
// is what stops a save of the slice from sweeping that name away.
//
// It does no I/O, so a file that cannot be written is not refused here. That
// refusal lands when the save starts, which is before anything has been written
// rather than part way through.
func (s *DotEnvSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if err := s.cfg.validateNaming(); err != nil {
		return nil, err
	}

	keys, err := ferry.NewKeys(addrs, driverName, s.cfg.key)
	if err != nil {
		return nil, err
	}

	// Both are read here and not inside the closure, so a sink reconfigured
	// after Bind cannot change a binding already handed out (ADR-0012).
	path, cfg := s.path, s.cfg

	return func(ctx context.Context) (ferry.Writer, error) { return openWriter(ctx, path, &cfg, keys) }, nil
}

// linkLimit bounds how far a chain of symlinks is followed. A path this driver
// did not create cannot make a save spin, and a cycle of links is the shape that
// would.
const linkLimit = 32

// target is the file a save replaces: the path with every symlink on its last
// component followed, one at a time.
//
// The rename is what makes this necessary. Renaming the staged file over a path
// that is a symlink replaces the link with a regular file, so the link is
// destroyed, the file it named keeps the contents the save was replacing, and
// every reader of that file goes on reading it. Following the link first puts
// the whole save - the staging, the stat the commit compares, the mode and the
// rename - on the file the operator's path actually names.
//
// A link that names nothing is followed all the same, and the path at the end of
// it is what comes back: a save through a dangling link creates that file, which
// is what an ordinary write through one does.
//
// A path that is not a link, one whose link cannot be read, and a chain longer
// than the bound all answer with what they were handed.
//
// It is driver/yaml's target copied rather than shared, because ADR-0002 forbids
// the internal module that would carry it.
func target(path string) string {
	for range linkLimit {
		dest, err := os.Readlink(path)
		if err != nil {
			return path
		}

		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(path), dest)
		}

		path = dest
	}

	return path
}

// openWriter reads the file that is there and stages the file that will replace
// it.
//
// Both halves are here rather than at the first write, because both are ways the
// plane can refuse and the open is where a refusal costs nothing: a file that
// does not parse would otherwise be silently overwritten, and a directory that
// takes no new file would otherwise be discovered half way through a walk.
func openWriter(ctx context.Context, path string, cfg *sinkConfig, keys *ferry.Keys) (*dotEnvWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Every path this save uses afterwards is the file itself and never a link
	// to it: the staging directory, the stat the commit compares, the mode the
	// replacement inherits, and the rename.
	path = target(path)

	f, err := readDotEnv(path)
	if err != nil {
		return nil, err
	}

	if f == nil {
		f = &file{index: make(map[string]int)}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".ferry-*")
	if err != nil {
		// ErrReadOnly is the class whatever the reason the file could not be
		// created, and it is subordinate to ErrPlane, so a caller matching either
		// one is answered. What the plane said stays in the chain.
		return nil, fmt.Errorf("%w: no replacement could be staged beside it: %w", ferry.ErrReadOnly, err)
	}

	return &dotEnvWriter{
		path: path, f: f, tmp: tmp, cfg: *cfg,
		names: keys, key: keys.Open(), wrote: make(map[string]bool), found: planeStamp(path),
	}, nil
}

// dotEnvWriter is one open dump: the file model being edited, and the staged file
// that will become the plane.
//
// The staging state is one bool. The rename sets it, and Close reads it to tell a
// dump that still has a temporary from a dump that no longer does - which is the
// whole protocol, because closed-without-Commit is the abort signal and no driver
// is ever told that it failed (ADR-0004).
//
// forget is the prefixes this dump replaces and wrote is every name it wrote. The
// two are subtracted at the commit rather than at the moment core asks, for the
// reason [dotEnvWriter.Unset] gives, and both stay empty for a dump over a value
// with no slice and no map in it.
//
// found is the file as the open read it, and it is what [dotEnvWriter.settle]
// compares against before the swap: a save merges into the file it read, so a
// file somebody else edited in between is a save that would drop their edit
// (ADR-0020).
type dotEnvWriter struct {
	path string
	f    *file
	tmp  *os.File
	cfg  sinkConfig

	// names is the binding's checked name table, held for the reports rather
	// than for the writes: it answers what this plane calls an address without
	// minting anything (ADR-0011, #159).
	names *ferry.Keys

	// key is this open's key function, and everything it mints belongs to this
	// open (ADR-0012).
	key ferry.KeyFunc

	wrote  map[string]bool
	order  []string
	forget []string
	found  stamp

	swapped bool
}

// PlaneName is the environment variable name an address is written to, which is
// what a report opens with in place of the address: /db/host prints as DB_HOST.
//
// It goes through the table and never through this open's key function, so it
// records nothing and cannot refuse (ADR-0011, #159).
func (w *dotEnvWriter) PlaneName(addr ferry.Path) (string, bool) { return w.names.PlaneName(addr) }

// Set writes one value at one variable, rewriting the line that is already there
// and appending one beside its siblings where there is none.
//
// Nothing reaches the file here. The edit is in memory until the commit, which is
// what makes a failed walk leave the file byte-identical.
func (w *dotEnvWriter) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	key, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	text, err := w.cfg.carried(v)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	if err := spellable(text); err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	if w.wrote[key] {
		return ferry.ErrorAt(addr.Path(), fmt.Errorf("%w: this dump has already written this variable, and a "+
			"file holds one value per name, so one of the two writes would be lost", ferry.ErrPlane))
	}

	w.wrote[key] = true
	w.order = append(w.order, key)
	w.f.put(key, text)

	return nil
}

// Ensure takes a container that is there and holds nothing, and writes nothing
// for it, because a container has no variable of its own on this plane: its
// presence is the presence of the variables under its prefix, which is what the
// read half's own probe already says.
//
// A null is refused instead, and that is the same refusal a leaf handed a null
// gets, one address up. This plane has no null: nothing distinguishes a variable
// that is not there from a container that is there and empty, so writing nothing
// for a null would make a nil map and an absent one the same file, and writing
// something for it would need a name no container here has.
//
// The refusal is what a caller sees for a nil or empty slice or map, and it is
// the honest end of the road rather than a gap: this plane cannot spell a
// container at all, which is a fact about every address here rather than about
// this value.
//
// The default arm is a live refusal rather than dead code. An absent container
// gets no call at all, so reaching it means core is asking something this method
// has no answer for, and a method that always returns nil is one nothing would
// catch changing.
func (*dotEnvWriter) Ensure(_ context.Context, addr ferry.Container, p ferry.Presence) error {
	switch p {
	case ferry.PresencePresent:
		return nil
	case ferry.PresenceNull:
		return ferry.ErrorAt(addr.Path(), errNoNull)
	default:
		return ferry.ErrorAt(addr.Path(), fmt.Errorf("%w: a container has no variable of its own here, so there "+
			"is nothing to write for a %s container", ferry.ErrValue, p))
	}
}

// Unset records that this dump replaces every variable the file holds under one
// slice or one map, which is what stops a save of a shorter slice from leaving
// the previous save's later positions behind.
//
// Nothing is removed here. What this dump writes arrives afterwards and has to
// survive (ADR-0004), and a line removed now and written again later would be a
// fresh line: the comment above it, the "export " on it and its position in the
// file are exactly what a save must not lose. So the sweep runs at the commit,
// once the writes are in.
func (w *dotEnvWriter) Unset(_ context.Context, addr ferry.CompositeAddr) error {
	key, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	w.forget = append(w.forget, key+w.cfg.sep)

	return nil
}

// Commit sweeps what this dump replaced, writes the file into the staged one,
// renames it over the plane, and then brings the process into agreement with it.
//
// The rename is what makes the replacement atomic: a reader sees the old file or
// the new one and never a half-written one, and a failure anywhere before the
// rename leaves the file byte-identical.
//
// The process half is last, so a save that could not write the file has not
// already changed the environment.
//
// It runs only where the walk succeeded, which is why nothing here has to ask
// whether it did.
func (w *dotEnvWriter) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	swept := w.sweep()

	if err := w.emit(); err != nil {
		return err
	}

	if err := w.settle(); err != nil {
		return err
	}

	if err := w.swap(); err != nil {
		return err
	}

	return w.apply(swept)
}

// sweep removes every variable under a replaced prefix that this dump did not
// write, and answers with the names it removed so the process half can forget
// them too.
func (w *dotEnvWriter) sweep() []string {
	if len(w.forget) == 0 {
		return nil
	}

	var gone []string

	for i := range w.f.lines {
		if ln := &w.f.lines[i]; ln.kind == kindAssign && !ln.gone && w.superseded(ln.a.name) {
			w.f.drop(i)

			gone = append(gone, ln.a.name)
		}
	}

	return gone
}

// superseded reports whether one variable is under a prefix this dump replaced
// and was not written by it.
func (w *dotEnvWriter) superseded(name string) bool {
	if w.wrote[name] {
		return false
	}

	for _, prefix := range w.forget {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// emit writes the file into the staged one and closes it.
func (w *dotEnvWriter) emit() error {
	if _, err := w.tmp.Write(w.f.render()); err != nil {
		return fmt.Errorf("%w: the replacement could not be written: %w", ferry.ErrPlane, err)
	}

	return w.flushAndClose()
}

// flushAndClose hands the staged file over: the sync where the caller asked for
// one, and then the close either way.
//
// Declining the sync costs nothing a reader can see, because the close still
// hands every byte to the kernel and the rename that follows swaps in a whole
// file either way; what is left in the operating system's cache is only at risk
// from a crash. The other half is in swap, since a rename is not made durable by
// syncing the file it moves.
func (w *dotEnvWriter) flushAndClose() error {
	if w.cfg.durable {
		if err := w.tmp.Sync(); err != nil {
			return fmt.Errorf("%w: the replacement could not be flushed: %w", ferry.ErrPlane, err)
		}
	}

	return w.tmp.Close()
}

// settle reads the file one last time and does the two things that answer to that
// read: refuse a file that changed since the open, and give the replacement the
// mode the file already had.
//
// It is one stat, and it is the whole cost of the refusal (ADR-0020). The mode
// inheritance needed the same call already, so the two share it rather than stat
// the same file twice on the commit path.
func (w *dotEnvWriter) settle() error {
	fi, err := os.Stat(w.path)
	if err != nil {
		return w.unchanged(stamp{})
	}

	if err := w.unchanged(stampOf(fi)); err != nil {
		return err
	}

	return w.inheritMode(fi)
}

// unchanged refuses a save whose file is no longer the one it merged into.
//
// This is optimistic concurrency and it is deliberately not a lock: a save reads,
// stages and swaps, and the window in between is where somebody else's edit
// lands. Swapping anyway would report success for a save that silently discarded
// it, so the loser of the race is told it lost and the file is left byte for byte
// as it was (ADR-0020).
func (w *dotEnvWriter) unchanged(now stamp) error {
	if w.found == now {
		return nil
	}

	return fmt.Errorf("%w: the file changed after this save read it, and saving now would discard that change: "+
		"load the file again, apply the same edits to what it holds now, and save again", ferry.ErrPlane)
}

// inheritMode gives the staged file the mode the file already had.
//
// Without it a dump would silently tighten or loosen the permissions of a file
// somebody else set up, because a staged file is created 0600 and a rename
// carries its own mode over. Where there is no file yet, that 0600 stands: a
// .env ferry creates may hold whatever the struct held, and the narrow mode is
// the one to be wrong in the safe direction with.
func (w *dotEnvWriter) inheritMode(fi os.FileInfo) error {
	if err := os.Chmod(w.tmp.Name(), fi.Mode().Perm()); err != nil {
		return fmt.Errorf("%w: the replacement could not take the file's mode: %w", ferry.ErrPlane, err)
	}

	return nil
}

// swap renames the staged file over the plane and makes the rename itself
// durable.
//
// The emit flushed the staged file's contents, but the rename writes a directory
// entry and that entry sits in the page cache until the directory it lives in is
// synced. Without the second sync a crash just after a Dump that returned nil can
// leave the old file in place with the new one's bytes already on the platter.
func (w *dotEnvWriter) swap() error {
	if err := os.Rename(w.tmp.Name(), w.path); err != nil {
		return fmt.Errorf("%w: the replacement could not replace the file: %w", ferry.ErrPlane, err)
	}

	// Set on the rename and not at the end, because from here there is no staged
	// file left for Close to remove whatever the sync answers.
	w.swapped = true

	if !w.cfg.durable {
		return nil
	}

	if err := syncDir(filepath.Dir(w.path)); err != nil {
		return fmt.Errorf("%w: the replacement replaced the file and could not be made durable: %w",
			ferry.ErrPlane, err)
	}

	return nil
}

// apply is the dump's optional second half: the process environment brought into
// agreement with the file that was just written.
//
// It does nothing at all unless [Setenv] named somewhere to write, which is what
// keeps process-global mutation something the caller asked for.
func (w *dotEnvWriter) apply(swept []string) error {
	if w.cfg.proc == nil {
		return nil
	}

	errs := append(w.exported(), w.retracted(swept)...)

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: the file was replaced and the process environment could not be brought into "+
			"agreement with it: %w", ferry.ErrPlane, err)
	}

	return nil
}

// exported sets every variable this dump wrote, in the order the walk produced
// them, so two identical dumps make identical sequences of calls.
func (w *dotEnvWriter) exported() []error {
	errs := make([]error, 0, len(w.order))

	for _, name := range w.order {
		if err := w.cfg.proc.Setenv(name, w.f.valueAt(name)); err != nil {
			errs = append(errs, fmt.Errorf("setting %s: %w", name, err))
		}
	}

	return errs
}

// retracted unsets every variable the sweep removed, which is the half that makes
// a shortened slice actually shorter on the next load.
func (w *dotEnvWriter) retracted(swept []string) []error {
	errs := make([]error, 0, len(swept))

	for _, name := range swept {
		if err := w.cfg.proc.Unsetenv(name); err != nil {
			errs = append(errs, fmt.Errorf("unsetting %s: %w", name, err))
		}
	}

	return errs
}

// Close removes the staged file where it is still staged, and does nothing once
// the rename has taken it.
//
// It runs whether the walk succeeded or failed, and a walk that failed is a Close
// with no Commit before it, which is the only thing this driver is told about the
// failure and the only thing it needs: the file is still byte for byte what it
// was, and the temporary that would otherwise be left behind is what this
// removes.
func (w *dotEnvWriter) Close() error {
	if w.swapped {
		return nil
	}

	// The staged file may already be closed, where the emit got that far before
	// something after it failed. Closing twice reports an error about the second
	// close and nothing about the dump, so it is the removal that is reported.
	_ = w.tmp.Close()

	if err := os.Remove(w.tmp.Name()); err != nil {
		return fmt.Errorf("%w: the staged replacement could not be removed: %w", ferry.ErrPlane, err)
	}

	return nil
}

// The edits a save makes to the line model. Each keeps the operator's own bytes
// wherever the new value does not force them to change.

// put writes one value, rewriting the line that holds the name and inserting one
// where the file has none.
func (f *file) put(name, text string) {
	i, held := f.index[name]
	if !held {
		f.insert(name, text)

		return
	}

	ln := &f.lines[i]
	ln.gone = false
	ln.a.src = spellAs(text, styleOf(ln.a.src))
	ln.a.value = text
}

// valueAt is what the file now holds at one name, and the empty string where it
// holds nothing.
func (f *file) valueAt(name string) string {
	if i, held := f.index[name]; held {
		return f.lines[i].a.value
	}

	return ""
}

// insert adds a line for a name the file does not hold, beside the lines it is
// most like.
func (f *file) insert(name, text string) {
	at := f.beside(name)

	if at > 0 && f.lines[at-1].term == "" {
		// The file ended without a terminator, and a line appended after that one
		// would otherwise run into it.
		f.lines[at-1].term = f.terminator()
	}

	f.lines = slices.Insert(f.lines, at, line{
		kind: kindAssign,
		term: f.terminator(),
		a:    assign{name: name, src: narrowest(text), value: text},
	})

	for held, i := range f.index {
		if i >= at {
			f.index[held] = i + 1
		}
	}

	f.index[name] = at
}

// beside is where a new name goes: after the last line whose own name shares the
// longest prefix with it, and at the end of the file where none shares any.
//
// It is what puts a new slice element next to its siblings rather than at the
// bottom, so a dump that grows TAGS_0 and TAGS_1 into three leaves TAGS_2 under
// them.
func (f *file) beside(name string) int {
	best, at := 0, len(f.lines)

	for i := range f.lines {
		if n := f.likeness(i, name); n > 0 && n >= best {
			best, at = n, i+1
		}
	}

	return at
}

// likeness is how much one line's own name has in common with another, and zero
// for a line that holds no name at all.
func (f *file) likeness(i int, name string) int {
	ln := &f.lines[i]
	if ln.kind != kindAssign || ln.gone {
		return 0
	}

	return shared(ln.a.name, name)
}

// shared is how many leading bytes two names have in common.
func shared(a, b string) int {
	n := min(len(a), len(b))

	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}

	return n
}

// terminator is what an appended line ends with: whatever the file already used,
// and a newline for a file that used none.
func (f *file) terminator() string { return cmp.Or(f.eol, "\n") }

// drop removes one assignment and the comment block written directly above it.
//
// The whole logical span goes, so a value that crossed physical lines does not
// leave its tail behind as a file that no longer parses, and so does the trailing
// comment on the line itself, which is part of that span.
//
// The comment block above it goes because a comment left behind attaches itself
// to whatever line is now beneath it and reads as documentation of an unrelated
// variable, which is worse than losing it. A blank line between the comment and
// the assignment breaks that attachment: the comment is about the section rather
// than about the variable, and it stays.
//
// Nothing is reflowed. Two blank lines left adjacent stay adjacent, because a
// save that tidies whitespace nobody asked it to tidy is a diff nobody asked for.
func (f *file) drop(i int) {
	f.lines[i].gone = true

	for j := i - 1; j >= 0 && f.lines[j].kind == kindComment; j-- {
		f.lines[j].gone = true
	}
}
