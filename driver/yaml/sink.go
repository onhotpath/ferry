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
	_ ferry.Ensurer   = (*writer)(nil)
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
// as the value just written: its line does not change and its value does. A key
// that is itself an alias is written through to the anchor it names, so that
// line does not change either and the value lands where the two share it.
//
// The one refusal that follows is two keys your struct maps that share an
// anchor, saved with different values: the file can hold only one of them, so
// the save fails with [ferry.ErrPlane] and your file is left as it was.
//
// The write is atomic. A temporary file beside yours is renamed into place once
// everything has been written, and a save that fails leaves your file byte for
// byte as it was with no temporary left behind.
//
// It is not durable unless you ask. Pass [Durable] to flush the replacement to
// the disk before the save returns, and read that option before you do: it is
// the most expensive thing a save can be told to do.
//
// It is a separate type from [Source] for the reason recorded there.
type Sink struct {
	path string
	cfg  config
}

// NewSink returns a sink over the YAML file at path.
//
// Pass [Durable] to flush the replacement to the disk before a save returns. The
// replacement is atomic either way.
//
// It touches nothing, and in particular it does not check that the path can be
// written. A sink over an unwritable directory is legal to build, and the save
// refuses when it starts.
func NewSink(path string, opts ...Option) Sink {
	var c config
	for _, o := range opts {
		o.apply(&c)
	}

	return Sink{path: path, cfg: c}
}

// Bind takes the address set and reads nothing out of it, for the reason
// [Source.Bind] records.
//
// It does no I/O, so a file that cannot be written is not refused here. The
// refusal lands when the save starts, which is before anything has been
// written, rather than part way through.
func (s Sink) Bind(_ *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	// Both are read here and not inside the closure, so a Sink reconfigured
	// after Bind cannot change a binding already handed out (ADR-0012).
	path, cfg := s.path, s.cfg

	return func(ctx context.Context) (ferry.Writer, error) { return open(ctx, path, cfg) }, nil
}

// open reads the document that is there and stages the file that will replace
// it.
//
// Both halves are here rather than at the first write, because both are ways
// the plane can refuse and the open is where a refusal costs nothing: a
// document that does not parse would otherwise be silently overwritten, and a
// directory that takes no new file would otherwise be discovered half way
// through a walk.
func open(ctx context.Context, path string, cfg config) (*writer, error) {
	doc, err := readDoc(ctx, path)
	if err != nil {
		return nil, err
	}

	untagMerges(doc)

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".ferry-*")
	if err != nil {
		// ErrReadOnly is the class whatever the reason the file could not be
		// created, and it is subordinate to ErrPlane, so a caller matching
		// either one is answered. What the plane said stays in the chain.
		return nil, fmt.Errorf("%w: no replacement could be staged beside it: %w", ferry.ErrReadOnly, err)
	}

	return &writer{path: path, doc: doc, tmp: tmp, shared: hasAlias(doc), durable: cfg.durable}, nil
}

// writer is one open dump: the document being built up, and the file that will
// become the plane.
//
// The staging state is one bool. The rename sets it, and Close reads it to tell
// a dump that still has a temporary from a dump that no longer does - which is
// the whole protocol, because closed-without-Commit is the abort signal and no
// driver is ever told that it failed (ADR-0004).
//
// It records the rename rather than the commit, because the two came apart when
// the directory sync landed after the rename (#187): a sync that fails is a
// dump that did not commit, and the staged file is gone all the same.
//
// durable is the whole of the Durable option, read once per open and never seen
// by ferry (#188). It gates the two syncs and nothing else: the staging, the
// mode inheritance and the rename are what a save is either way.
//
// claims is what two addresses meeting at one node are caught by, and shared is
// what says whether they can meet at all (#198). Only a document holding an
// alias makes two addresses reach one node, so a document with none records
// nothing and every ordinary dump pays for one bool.
type writer struct {
	path   string
	doc    *yamlv3.Node
	tmp    *os.File
	claims map[*yamlv3.Node]claim

	shared  bool
	durable bool
	swapped bool
}

// claim is one address's write at one node: where it came from, and what it put
// there.
type claim struct {
	addr ferry.Path
	tag  string
	text string
}

// Set writes one value at one address, creating the containers above it.
//
// It is never called with an Absent, and it refuses one rather than writing a
// null for it. That refusal is a regression test rather than a nicety: a
// prototype's sink mapped Absent onto !!null, so an address ferry omitted was
// written as an explicit null and read back as Null - the Absent-versus-Null
// conflation ferry criticises xload for, committed on the write path, where a
// later load cannot tell it from a null the operator wrote.
func (w *writer) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	spelled, err := spell(v)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	return w.put(addr.Path(), spelled, yamlv3.ScalarNode)
}

// Ensure writes what a container has to say at its own address: the plane's null
// where the value is nil or empty, and an empty mapping where the value is a
// section that is there and holds nothing.
//
// The empty mapping is what closes the round trip Go can express and ADR-0006's
// replace rule would otherwise lose: a non-nil pointer whose every field is
// omitted writes no child, and without one section-level write the reload sees
// an absent key and hands back nil (ADR-0016).
func (w *writer) Ensure(_ context.Context, addr ferry.Container, p ferry.Presence) error {
	node, kind, err := container(p)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	return w.put(addr.Path(), node, kind)
}

// container is the node one presence is written as, and the kind an alias at the
// address has to name for the write to go through it.
//
// Absent never reaches here: an address ferry omits gets no call at all rather
// than a call saying nothing, which is the same rule that keeps [ferry.Value]'s
// absent kind off the write side entirely (ADR-0006).
func container(p ferry.Presence) (*yamlv3.Node, yamlv3.Kind, error) {
	switch p {
	case ferry.PresenceNull:
		return leaf(nullTag, nullText), yamlv3.ScalarNode, nil
	case ferry.PresencePresent:
		return &yamlv3.Node{Kind: yamlv3.MappingNode, Tag: mapTag}, yamlv3.MappingNode, nil
	default:
		return nil, 0, fmt.Errorf("%w: an absent container has nothing to write, and an address ferry omits "+
			"gets no call rather than an explicit one", ferry.ErrValue)
	}
}

// put is the write both halves share: find or build the node at the address,
// check that no other address has already claimed it, and replace it while
// keeping what belongs to the operator.
func (w *writer) put(addr ferry.Path, spelled *yamlv3.Node, kind yamlv3.Kind) error {
	at, err := place(w.doc, addr, kind)
	if err != nil {
		return ferry.ErrorAt(addr, err)
	}

	if err := w.claim(addr, at, spelled); err != nil {
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

// claim records that addr wrote this node, and refuses a second address that
// arrives at the same node wanting something else (#198).
//
// Two addresses meet at one node when the walk to one of them crosses an alias
// to the other, which is a document saying the two are one value while the
// destination says they are two. Writing both would leave the file holding
// whichever went last, so a dump that reported success would load back a value
// no field of the caller's held. It is refused before anything is written and
// the plane is left byte for byte as it was.
//
// The node written need not be the anchored one: an anchor on a mapping is
// shared by every leaf under it, and those leaves carry no anchor of their own.
// So what gates the bookkeeping is the document holding an alias at all, and not
// the node in hand.
func (w *writer) claim(addr ferry.Path, at, spelled *yamlv3.Node) error {
	if !w.shared {
		return nil
	}

	held, taken := w.claims[at]
	if taken && (held.tag != spelled.Tag || held.text != spelled.Value) {
		return fmt.Errorf("%w: this address and %s are one value in the plane, which shares it through an alias, "+
			"and the dump gave them different values", ferry.ErrPlane, held.addr)
	}

	if !taken {
		if w.claims == nil {
			w.claims = make(map[*yamlv3.Node]claim, 1)
		}

		w.claims[at] = claim{addr: addr, tag: spelled.Tag, text: spelled.Value}
	}

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

	return w.swap()
}

// swap renames the staged file over the plane and makes the rename itself
// durable.
//
// The emit flushed the staged file's contents, but the rename writes a
// directory entry and that entry sits in the page cache until the directory it
// lives in is synced. Without the second sync a crash just after a Dump that
// returned nil can leave the operator's old document in place with the new
// one's bytes already on the platter, which is the whole of #187: the expensive
// half of durability was being paid for and the cheap half was missing.
//
// The failure is classed exactly as the rename's own, because a rename that
// cannot be made durable and a rename that did not happen are the same answer
// to the caller: the dump did not commit.
//
// The rename is unconditional and the sync after it is not (#188). The rename is
// what keeps a half-written document out of the operator's file and it is cheap;
// the sync is the expensive half and the caller asks for it.
func (w *writer) swap() error {
	if err := os.Rename(w.tmp.Name(), w.path); err != nil {
		return fmt.Errorf("%w: the staged document could not replace the plane: %w", ferry.ErrPlane, err)
	}

	// Set on the rename and not at the end, because from here there is no
	// staged file left for Close to remove whatever the sync answers.
	w.swapped = true

	if !w.durable {
		return nil
	}

	if err := syncDir(filepath.Dir(w.path)); err != nil {
		return fmt.Errorf("%w: the staged document replaced the plane and the replacement could not be made "+
			"durable: %w", ferry.ErrPlane, err)
	}

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

	return w.flushAndClose()
}

// flushAndClose hands the staged file over: the sync where the caller asked for
// one, and then the close either way.
//
// The sync is the staged file's half of durability and it is the half declined
// by default (#188). Declining it costs nothing a reader can see, because the
// close still hands every byte to the kernel and the rename that follows swaps
// in a whole document either way; what is left in the operating system's cache
// is only at risk from a crash. The other half is in swap, since a rename is not
// made durable by syncing the file it moves.
//
// The two are one function so that emit stays inside cognitive-complexity, which
// gating the sync in place put at eight.
func (w *writer) flushAndClose() error {
	if w.durable {
		if err := w.tmp.Sync(); err != nil {
			return fmt.Errorf("%w: the staged document could not be flushed: %w", ferry.ErrPlane, err)
		}
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

// Close removes the staged file where it is still staged, and does nothing once
// the rename has taken it.
//
// It runs whether the walk succeeded or failed, and a walk that failed is a
// Close with no Commit before it, which is the only thing this driver is told
// about the failure and the only thing it needs: the plane is still byte for
// byte what it was, and the temporary that would otherwise be left behind is
// what this removes.
//
// It asks about the rename rather than about the commit, because a directory
// sync that failed after the rename is a dump that did not commit and has no
// temporary left to remove (#187).
func (w *writer) Close() error {
	if w.swapped {
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
