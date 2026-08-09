package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/onhotpath/ferry"
)

// The write half implements both lifecycle interfaces, which no other
// first-party plane does: it stages a replacement, so it has something to
// commit, and it holds an open file, so it has something to release.
var (
	_ ferry.Sink       = Sink{}
	_ ferry.Writer     = (*writer)(nil)
	_ ferry.Ensurer    = (*writer)(nil)
	_ ferry.Unsetter   = (*writer)(nil)
	_ ferry.Committer  = (*writer)(nil)
	_ ferry.Releaser   = (*writer)(nil)
	_ ferry.PlaneNamer = (*writer)(nil)
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
// A list or a map your struct maps is the one place the merge stops, because it
// is replaced whole: a three-element list saved over with one element is one
// element afterwards, and a mapping saved over with a map that lost a key no
// longer holds that key. Anything a save like that keeps is still the
// operator's - its comments, its anchor and the node tag it was written under
// all survive - and a struct's fields are untouched by the rule, since a field
// your value leaves out is left exactly where it is rather than removed.
//
// A value that is a leaf rather than a struct - an int, a string, a []byte -
// maps one address, the document itself, and saving one replaces the whole file:
// a file holding "keep: me" and "other: 2" is "8080" and nothing else after
// saving 8080 to it. That is the same rule as the one above and not an exception
// to it. There is no replace-or-patch switch to set here; a save always replaces
// what it writes, and the root is simply the one address with nothing above it
// left over to keep.
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
// A path that is a symlink is written through rather than replaced: the file the
// link names is the one a save reads, stages beside and renames over, and the
// link itself is left exactly as it is. A link that names a file which does not
// exist yet is followed all the same, and the save writes the file at the end of
// it. The link is followed once per save, so re-pointing it between two saves
// sends the second one to the new file.
//
// A save that started before somebody else edited the file refuses rather than
// overwriting them. Because a save is a merge into the document it read, an edit
// that lands between the read and the rename would be silently dropped, so the
// save reports [ferry.ErrPlane] and leaves your file as it was: load again,
// apply your changes to what the file holds now, and save again. The check is
// the file's length and modification time, so the one edit it cannot see is a
// rewrite in the same modification-time tick that leaves the length alone.
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

// Bind builds no flat key from the address set, for the reason [Source.Bind]
// records.
//
// What it does take from the set is the node tag declared at each address,
// where the registry this save resolves against was given [Extension]. A
// declaration this driver cannot honour is refused here, which is before the
// operator's file has been opened.
//
// It does no I/O, so a file that cannot be written is not refused here. That
// refusal lands when the save starts, which is before anything has been
// written, rather than part way through.
func (s Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	nodes, err := nodeTags(addrs)
	if err != nil {
		return nil, err
	}

	// All three are read here and not inside the closure, so a Sink reconfigured
	// after Bind cannot change a binding already handed out (ADR-0012). The
	// table is the schema's rather than the call's, which is why reading it once
	// here is the whole idiom (ADR-0021).
	path, cfg := s.path, s.cfg

	return func(ctx context.Context) (ferry.Writer, error) { return open(ctx, path, cfg, nodes) }, nil
}

// open reads the document that is there and stages the file that will replace
// it.
//
// Both halves are here rather than at the first write, because both are ways
// the plane can refuse and the open is where a refusal costs nothing: a
// document that does not parse would otherwise be silently overwritten, and a
// directory that takes no new file would otherwise be discovered half way
// through a walk.
func open(ctx context.Context, path string, cfg config, nodes map[ferry.Path]string) (*writer, error) {
	// Every path this save uses afterwards is the file itself and never a link
	// to it: the staging directory, the stat the commit compares, the mode the
	// replacement inherits, and the rename (#256).
	path = target(path)

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

	return &writer{
		path:    path,
		doc:     doc,
		tmp:     tmp,
		nodes:   nodes,
		found:   planeStamp(path),
		shared:  hasAlias(doc),
		durable: cfg.durable,
	}, nil
}

// linkLimit bounds how far a chain of symlinks is followed. It is here for the
// reason [aliasLimit] is: a path this driver did not create cannot make a save
// spin, and a cycle of links is the shape that would (#256).
const linkLimit = 32

// target is the file a save replaces: the path with every symlink on its last
// component followed, one at a time.
//
// The rename is what makes this necessary (#256). Renaming the staged file over
// a path that is a symlink replaces the link with a regular file, so the link is
// destroyed, the file it named keeps the document the save was replacing, and
// every reader of that file goes on reading it. Following the link first puts
// the whole save - the staging, the stat the commit compares, the mode and the
// rename - on the file the operator's path actually names.
//
// A link that names nothing is followed all the same, and the path at the end of
// it is what comes back: a save through a dangling link creates that file, which
// is what an ordinary write through one does.
//
// A path that is not a link, and one whose link cannot be read, both answer with
// what they were handed, which is where an ordinary save comes out. So does a
// chain longer than the bound, and there the read that follows is what reports
// the cycle.
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
//
// nodes is the schema's own answer for what tag an address is written under,
// built at Bind and nil for every save under a registry that declares none
// (ADR-0021, #156).
//
// forget is the composites this dump replaces, in the order core named them,
// and wrote is every address it wrote with every address above it, each under
// its [trail] rather than under its ferry.Path, for [mark]'s reason. The two are
// subtracted at the commit rather than at the moment core asks, for the reason
// [writer.Unset] gives, and both stay empty for a dump over a value with no
// slice and no map in it (ADR-0004, #220).
//
// found is the plane as the open read it, and it is what [writer.settle]
// compares the plane against before the swap: a save merges into the document
// it read, so a file somebody else edited in between is a save that would drop
// their edit (ADR-0020).
type writer struct {
	path   string
	doc    *yamlv3.Node
	tmp    *os.File
	claims map[*yamlv3.Node]claim
	nodes  map[ferry.Path]string
	forget []ferry.Path
	wrote  map[string]bool
	found  stamp

	shared  bool
	durable bool
	swapped bool
}

// PlaneName is the document's own name for an address, and it is the same
// rendering the read half answers with: one address has one spelling in a report
// whichever direction failed (ADR-0011, #159).
func (*writer) PlaneName(addr ferry.Path) (string, bool) { return nameInDocument(addr) }

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

// Unset records that this dump replaces everything the document holds under one
// list or one mapping, which is what stops a save of a shorter list from leaving
// the previous document's later positions behind.
//
// Nothing is removed here. What this dump writes arrives afterwards and has to
// survive (ADR-0004), and a node removed now and written again later would be a
// fresh node: the comments, the anchor and the tag on the member that stays are
// exactly what a save must not lose, and they live on the node that is already
// there. So the members are subtracted at the commit, once the writes are in.
func (w *writer) Unset(_ context.Context, addr ferry.CompositeAddr) error {
	w.forget = append(w.forget, addr.Path())

	return nil
}

// replace drops from every replaced composite what this dump did not write,
// which is the whole of dump-is-replace for this plane (ADR-0004).
//
// A mapping keeps the members the dump wrote, in the order the document had
// them, and a sequence keeps its positions up to the last one written. The
// difference is that removing a position renumbers the ones after it, so a
// sequence is truncated rather than picked through: core writes a slice's
// positions from zero and a position it wrote nothing at is one whose members
// were all omitted, which is a value left alone rather than removed (ADR-0006).
//
// A composite whose node is neither - the empty arm writes a null at the
// address, and the unset precedes it - has nothing under it to drop.
func (w *writer) replace() {
	for _, at := range w.forget {
		if n := deref(lookup(w.doc, at)); n != nil {
			w.prune(at, n)
		}
	}
}

// prune is one replaced composite's own subtraction, by the kind of node the
// walk left at its address.
func (w *writer) prune(at ferry.Path, n *yamlv3.Node) {
	held := trail(at)

	switch n.Kind {
	case yamlv3.SequenceNode:
		n.Content = n.Content[:w.written(held, len(n.Content))]
	case yamlv3.MappingNode:
		n.Content = w.members(held, n.Content)
	default:
		// A scalar, an alias resolving to one, or a node the document never
		// had: nothing holds members here, so nothing is subtracted.
	}
}

// written is how long a replaced sequence stays: one past the last position this
// dump wrote at, and zero where it wrote at none.
func (w *writer) written(at string, held int) int {
	kept := 0

	for i := range held {
		if w.wrote[at+mark(ferry.IndexSegment(uint(i)))] {
			kept = i + 1
		}
	}

	return kept
}

// members is a replaced mapping's content with every pair this dump did not
// write taken out.
//
// One pass and no bookkeeping, because one key is one pair: a mapping spelling
// one key twice is refused at the open (#257), so there is no second occurrence
// here for a keep to leave behind.
func (w *writer) members(at string, held []*yamlv3.Node) []*yamlv3.Node {
	out := held[:0]

	for i := 0; i+1 < len(held); i += 2 {
		k := held[i]
		if k.Kind != yamlv3.ScalarNode || !w.wrote[at+mark(ferry.NameSegment(k.Value))] {
			continue
		}

		out = append(out, k, held[i+1])
	}

	return out
}

// record marks an address this dump wrote, and every address above it, as one
// [writer.replace] must keep.
//
// The addresses above it are what makes a member of a replaced composite count
// as written when what the dump actually wrote was a leaf somewhere below it.
//
// It records nothing until a composite has been replaced, which loses nothing:
// core names a composite before it writes anything beneath it, so an address
// under one is always recorded (ADR-0004). An address written before the first
// unset is under none of them.
func (w *writer) record(addr ferry.Path) {
	if len(w.forget) == 0 {
		return
	}

	if w.wrote == nil {
		w.wrote = make(map[string]bool)
	}

	var b []byte

	for seg := range addr.Segments() {
		b = append(b, mark(seg)...)
		w.wrote[string(b)] = true
	}
}

// mark is one segment's contribution to a trail: its kind, then its text under a
// length that keeps one segment from spelling two.
//
// It is what an address is recorded and looked up under, rather than the
// [ferry.Path] itself, because a prefix of an address has to be recorded and a
// Path cannot be extended by a [ferry.Segment]: [ferry.Path.Elem] takes a
// position, and recovering one from the segment's text means a parse that only
// an address no walk can mint could fail (ADR-0016).
func mark(seg ferry.Segment) string {
	return string(byte(seg.Kind())) + strconv.Itoa(len(seg.Text())) + ":" + seg.Text()
}

// trail is a whole address spelled in [mark]s, which is what a replaced
// composite's children are looked up beneath.
func trail(p ferry.Path) string {
	var b []byte

	for seg := range p.Segments() {
		b = append(b, mark(seg)...)
	}

	return string(b)
}

// put is the write both halves share: find or build the node at the address,
// check that no other address has already claimed it, and replace it while
// keeping what belongs to the operator.
func (w *writer) put(addr ferry.Path, spelled *yamlv3.Node, kind yamlv3.Kind) error {
	w.record(addr)

	at, err := place(w.doc, addr, kind)
	if err != nil {
		return ferry.ErrorAt(addr, err)
	}

	if err := w.claim(addr, at, spelled); err != nil {
		return ferry.ErrorAt(addr, err)
	}

	// The comments around the value are the operator's and survive the value
	// being replaced. The value and the style are ferry's.
	spelled.HeadComment, spelled.LineComment, spelled.FootComment = at.HeadComment, at.LineComment, at.FootComment

	// The tag is the schema's where a field declared one and the operator's
	// otherwise, which is what [retag] settles (#155, #156).
	if err := w.retag(addr, at, spelled); err != nil {
		return ferry.ErrorAt(addr, err)
	}

	// So is the spelling of a number, on the rule the tag and the anchor are
	// kept on: what the operator wrote survives until the value says otherwise
	// (#259, ADR-0016, ADR-0018).
	carrySpelling(at, spelled)

	// So is the anchor, for the same reason and with a sharper consequence
	// (#196): dropping it leaves every alias to this node dangling, so the save
	// reports success and writes a document no reader can parse. Keeping it
	// means an alias at an address no field maps now reads back as the value
	// just written here, which is what the operator asked for by writing one.
	spelled.Anchor = at.Anchor

	*at = *spelled

	return nil
}

// retag settles which tag the node being written carries.
//
// Where the schema declared one at this address it wins, and it wins over the
// operator's for the reason the declaration exists (#156): the tag in the file
// is what a save had no way of knowing, and the tag on the field is the answer
// to that, so a document that never carried one still comes out with it. Where
// nothing declared one, [carryTag] keeps whatever the operator wrote, which is
// what a save that was asked to change nothing must do (#155).
//
// A container write reaches here too, and never with a declaration on it:
// [nodeTags] refuses one at a section's or a composite's own address, so the
// kind check below is about the value and not about the shape.
func (w *writer) retag(addr ferry.Path, at, spelled *yamlv3.Node) error {
	tag, declared := w.nodes[addr]
	if !declared || spelled.Kind != yamlv3.ScalarNode {
		carryTag(at, spelled)

		return nil
	}

	return writeUnder(tag, spelled)
}

// writeUnder puts a declared tag on the node, or refuses the value that cannot
// come back from under it.
//
// The kind is the whole of the check, and it is [carryTag]'s guard read in the
// other direction: this plane reads a tag it has no arm for as a String
// (see [kindOf]), so a String is the value a declared tag returns unchanged and
// anything else would come back as text. Refusing is louder than dropping the
// tag, and it is the honest answer to a field that asked for a node type its
// own value cannot survive.
//
// A Null is written plainly instead, and that is not an exception smuggled in:
// there is no value at a null for a node type to describe, the address reads
// back null under either spelling, and refusing would have made an optional
// field that happens to be unset fail a save.
func writeUnder(tag string, spelled *yamlv3.Node) error {
	switch kindOf(spelled.Tag) {
	case ferry.KindNull:
		return nil
	case ferry.KindString:
		spelled.Tag, spelled.Style = tag, spelled.Style|yamlv3.TaggedStyle

		return nil
	default:
		return fmt.Errorf("%w: this key declares the node tag %s and holds a value written as %s: "+
			"a scalar under a tag this driver does not read comes back as a string, so the value would not "+
			"survive the trip", ferry.ErrValue, tag, spelled.Tag)
	}
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

	node := anchored(at)

	held, taken := w.claims[node]
	if taken && (held.tag != spelled.Tag || held.text != spelled.Value) {
		return fmt.Errorf("%w: this key and %s are one value in the document, which shares it through an alias, "+
			"and the dump gave them different values", ferry.ErrPlane, held.addr)
	}

	if !taken {
		if w.claims == nil {
			w.claims = make(map[*yamlv3.Node]claim, 1)
		}

		w.claims[node] = claim{addr: addr, tag: spelled.Tag, text: spelled.Value}
	}

	return nil
}

// anchored is the node a write is recorded against: the node an alias names, and
// the node in hand where it is not one.
//
// Recording the node written would make the refusal depend on the order the walk
// went in. [through] follows an alias only where the node it names is already
// the kind the address needs, and a container write is the first thing that can
// change that kind, so whichever of two addresses arrived first decided whether
// the second one followed - and where it did not follow, the two never met and
// the collision went unseen. Resolving the alias here makes both arrive at one
// node whichever order they were written in (#198).
//
// A chain too long to resolve is recorded against the node in hand, which is
// where the write is going anyway.
func anchored(n *yamlv3.Node) *yamlv3.Node {
	if named := deref(n); named != nil {
		return named
	}

	return n
}

// Commit emits the document into the staged file and renames it over the plane.
//
// The rename is what makes the replacement atomic: a reader of the plane sees
// the old document or the new one and never a half-written file, and a failure
// anywhere before the rename leaves the plane byte-identical.
//
// It runs only where the walk succeeded, which is why nothing here has to ask
// whether it did.
//
// The replaced composites are settled first, and the document that reaches the
// staged file is the one a load has to see (ADR-0004).
func (w *writer) Commit(_ context.Context) error {
	w.replace()

	if err := w.emit(); err != nil {
		return err
	}

	if err := w.settle(); err != nil {
		return err
	}

	return w.swap()
}

// settle reads the plane one last time and does the two things that answer to
// that read: refuse a plane that changed since the open, and give the
// replacement the mode the plane already had.
//
// It is one stat, and it is the whole cost of the refusal (ADR-0020). The mode
// inheritance needed the same call already, so the two share it rather than
// stat the same file twice on the commit path.
//
// A stat that fails is read as a plane that is not there, which is what
// [planeStamp] answered at the open for the same failure: the two compare equal
// when nothing moved, and a plane that went away between the open and here is a
// change like any other.
func (w *writer) settle() error {
	fi, err := os.Stat(w.path)
	if err != nil {
		return w.unchanged(stamp{})
	}

	if err := w.unchanged(stampOf(fi)); err != nil {
		return err
	}

	return w.inheritMode(fi)
}

// unchanged refuses a save whose plane is no longer the one it merged into.
//
// This is optimistic concurrency and it is deliberately not a lock: a save
// reads, stages and swaps, and the window in between is where a watcher's whole
// point - somebody else edits this file - lands. Swapping anyway would report
// success for a save that silently discarded the operator's edit, so the loser
// of the race is told it lost and the plane is left byte for byte as it was
// (ADR-0020).
func (w *writer) unchanged(now stamp) error {
	if w.found == now {
		return nil
	}

	return fmt.Errorf("%w: the file changed after this save read it, and saving now would discard that change: "+
		"load the file again, apply the same edits to what it holds now, and save again", ferry.ErrPlane)
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
		return fmt.Errorf("%w: the staged document could not replace the file: %w", ferry.ErrPlane, err)
	}

	// Set on the rename and not at the end, because from here there is no
	// staged file left for Close to remove whatever the sync answers.
	w.swapped = true

	if !w.durable {
		return nil
	}

	if err := syncDir(filepath.Dir(w.path)); err != nil {
		return fmt.Errorf("%w: the staged document replaced the file and the replacement could not be made "+
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
//
// It is handed the plane's own stat rather than taking one, because [settle]
// took it: a plane that is not there reaches neither.
func (w *writer) inheritMode(fi os.FileInfo) error {
	if err := os.Chmod(w.tmp.Name(), fi.Mode().Perm()); err != nil {
		return fmt.Errorf("%w: the staged document could not take the file's mode: %w", ferry.ErrPlane, err)
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
