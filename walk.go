package ferry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
)

// sched runs a container's members and reports what they collectively said and
// failed at. It is the seam a concurrent mode goes behind, and it is why the
// walk is written once: a concurrent mode is a second scheduler and never a
// second walk (ADR-0010).
//
// It takes a count and one body rather than a slice of closures, which is
// ADR-0010 amended under #176: a closure per member captures the walker, the
// context and the member and therefore escapes, so a container cost one heap
// closure per member for a seam whose whole purpose is to keep aggregation out
// of the walk. A count and one body cost one closure per container however many
// members it has, and every property the seam was built for survives, because a
// first-error scheduler still swaps in against this one.
//
// It is unexported and no exported name takes or returns one, which is what
// keeps [MaxConcurrency] the only door onto the choice: core ships two
// implementations, [serial] and one fanout per walk, and which of them a walk
// runs under is decided by the caller's budget and the instance's declared
// tolerance together (ADR-0019).
type sched func(n int, run func(i int) (outcome, error)) (outcome, error)

// serial is core's default scheduler: every member, in order, and every
// failure. It is what a walk runs under unless the caller set a budget and the
// open instance declared it tolerates overlap (ADR-0019).
//
// Aggregation lives here rather than in the walk, which is ADR-0011's rule and
// ADR-0010's measurement of it: the same walk function under a first-error
// scheduler and under this one reports one error and two over a plane with two
// bad leaves, byte-identical in between. So the walk never decides whether to
// continue, and the error model costs it nothing.
//
// Combining the members' outcomes lives here for the same reason and is the
// same work: a scheduler combines what its members returned, in the order the
// members were listed and never the order they finished (ADR-0019).
func serial(n int, run func(i int) (outcome, error)) (outcome, error) {
	var b batch

	for i := range n {
		b.add(run(i))
	}

	return b.done()
}

// batch is what a container's members have said so far: their outcomes combined
// in member order, and every failure among them.
//
// Only a failure is collected, which is the same aggregate join would have
// built out of a slice of nils and costs nothing per container where there is
// nothing to report.
type batch struct {
	out  outcome
	errs []error
}

// add folds one member's answer in. The combination can fail on its own, at an
// address two members of one walk both determined, so it is collected beside
// the member's own failure rather than instead of it.
func (b *batch) add(got outcome, err error) {
	if err != nil {
		b.errs = append(b.errs, err)
	}

	out, clash := b.out.merge(got)
	b.out = out

	if clash != nil {
		b.errs = append(b.errs, clash)
	}
}

// fail records a failure at a member the walk did not enter.
func (b *batch) fail(err error) { b.errs = append(b.errs, err) }

// done is the container's own answer: what its members said, and what they
// failed at.
func (b *batch) done() (outcome, error) { return b.out, join(b.errs...) }

// outcome is what one subtree of the walk hands the walk above it.
//
// ADR-0006 makes the walk return a presence fact per subtree, and ADR-0019 is
// why that fact is a returned value rather than a location the whole walk
// shares: a subtree's own answer composes upward, where a counter read before
// and after a subtree is right only while nothing else is running, and a
// sibling's write landing between the two reads materialises a pointer over a
// subtree that said nothing.
//
// All three of the walk's per-subtree facts live here for that reason: the
// presence bit ADR-0006 names, the addresses a value determined (ADR-0003), and
// the encodes a dump stages before its first write (ADR-0011).
type outcome struct {
	// wrote is ADR-0006's presence bit: the plane spoke somewhere under here.
	// A declared default is not presence and neither is a seed, which is what
	// keeps an optional section optional.
	wrote bool

	// minted is every address a value determined under this subtree, against
	// the container that determined it, which is what lets a parent refuse an
	// address two of its subtrees both realised and name the one that took it
	// second (ADR-0003, ADR-0005).
	minted map[Path]spot

	// writes is every encode a dump staged here, in member order, held where
	// there is no plane in sight: ADR-0011's rule is that Dump encodes every
	// address before it writes any of them, and this is where the encodes wait.
	writes []stagedWrite
}

// merge folds one member's outcome into the outcome its siblings have built,
// and reports an address two of them realised.
//
// Presence is an or, the staged writes concatenate in member order, and the
// minted addresses are a set union that can fail. None of the three reads a
// location another member could still be writing, which is the whole reason the
// walk returns this rather than updating shared state (ADR-0019).
func (o outcome) merge(p outcome) (outcome, error) {
	out := outcome{
		wrote:  o.wrote || p.wrote,
		minted: o.minted,
		writes: append(o.writes, p.writes...),
	}

	err := out.took(p.minted)

	return out, err
}

// took adds one subtree's minted addresses to the accumulating set.
//
// The accumulator adopts the first non-empty set it is handed rather than
// copying it, because the subtree that built it has finished with it: one map
// per dump reaches the root where a copy per level would allocate per level.
func (o *outcome) took(minted map[Path]spot) error {
	if len(minted) == 0 {
		return nil
	}

	if o.minted == nil {
		o.minted = minted

		return nil
	}

	errs := make([]error, 0, len(minted))

	for at, by := range minted {
		if _, taken := o.minted[at]; taken {
			errs = append(errs, collided(at, by))

			continue
		}

		o.minted[at] = by
	}

	return join(errs...)
}

// collided is the one refusal an address realised twice gets, named at the
// container that realised it second.
//
// Two things collapse into one refusal here because they are one failure. Two
// map keys whose text is one text are one address, which is the injectivity
// obligation ADR-0005 states under Go's == and which no compile-time rule can
// discharge for a key type somebody registered. And an address the static pass
// already determined is one a write from a value would overwrite. Either way an
// entry is lost, and the determinism invariant is the argument that does not
// depend on anyone agreeing that a lost entry matters: which of the two writes
// survives is which the walk makes last, so there is no stable answer to give
// and a refusal is the only outcome consistent with ADR-0001.
func collided(at Path, by spot) error {
	return newError(momentWalk, ErrValue, by.at, fmt.Sprintf(
		"%s is addressed more than once, and one of the two writes would be lost: the addresses under a "+
			"%s come from the value, and this one is an address already", at, by.v.Type()))
}

// spot is one position a walk has arrived at: the compiled node, the Go value
// it holds, and the realised address it occupies.
//
// The address is carried rather than read off the node, and that is the whole
// of what the dynamic tier costs the walk. A slice element and a map value are
// compiled once, at the address shape their members share, so a node under a
// dynamic composite holds a shape and never an address; the walk stands at the
// address it minted from the value and looks the node up by shape. ADR-0006
// describes exactly this - "the walk carries two paths, the realised one it asks
// the plane about, and the static one it looks declarations up by". In the
// static tier the two are one path.
type spot struct {
	n  *node
	v  reflect.Value
	at Path
}

// leaf is the typed address of the value this position holds. It is minted here
// rather than carried on the node, because a node under a dynamic composite
// holds an address shape and the address is the one the walk realised
// (ADR-0016).
func (s spot) leaf() LeafAddr { return leafOf(s.at) }

// container is the typed address of the container this position occupies, at
// the kind the compiler decided for it. Its callers are the three arms that
// stand at a container, so a position with no container address never reaches
// it.
func (s spot) container() Container {
	if k, ok := containerKind(s.n); ok && k == kindSection {
		return sectionOf(s.at)
	}

	return compositeOf(s.at)
}

// composite is the typed address of a dynamic container, which is what a driver
// is asked to enumerate.
func (s spot) composite() CompositeAddr { return compositeOf(s.at) }

// child is the address of one member a driver enumerated: the driver minted the
// segment and the schema types what is at it (ADR-0016).
func (s spot) child(seg Segment) Path { return s.at.child(seg) }

// direction is the residue Load and Dump do not share.
//
// ADR-0010 counts three operations and does not claim zero: at a leaf, Dump
// encodes a Go value and writes where Load reads and decodes; at a container,
// "is this container's own address carrying the whole answer" is one question
// with two answers; and members cannot be shared at all, because Dump reads a
// map's keys off the value while Load enumerates the plane.
//
// All three are here. The third is the two dynamic composites, and it is where
// the asymmetry is real rather than incidental: Dump covers every address
// always, because the value is in hand, and Load covers a dynamic address only
// from a source that can list.
//
// Everything else is written once and lives on [walker]: which nodes exist and
// in what order, where the context is checked, and where the scheduler sits.
type direction interface {
	// omitted is ADR-0006's omission rule asked once, before anything at this
	// position is read, written or converted. It is a Dump-side question whose
	// Load-side answer is always no, and it is asked here rather than per node
	// kind because omitzero is the one option admissible at every type.
	omitted(s spot) bool

	atLeaf(ctx context.Context, s spot) (outcome, error)

	// atStatic is a composite whose members come from the type - a struct or an
	// array - walked through into. It is a hook rather than a bare descent
	// because Load has one question to ask around it: required at such an
	// address means the plane supplied at least one of its static children,
	// which is the presence bit the subtree that just ran returned.
	atStatic(ctx context.Context, s spot, into descend) (outcome, error)

	// atNullable is the container question, at a composite that can be nil.
	// Dump answers it from the value and Load from the plane, and into is the
	// walk of what is beneath, which either direction may decline to run.
	atNullable(ctx context.Context, s spot, into descend) (outcome, error)

	// atSlice and atMap are the whole of the dynamic tier. Each mints its
	// members' addresses - from the value on Dump, from the plane on Load - and
	// walks each member through into at the address it minted for it.
	atSlice(ctx context.Context, s spot, into descend) (outcome, error)
	atMap(ctx context.Context, s spot, into descend) (outcome, error)
}

// descend walks what is under a container, over the value the direction decided
// to walk it over and at the address the direction decided it occupies. It is a
// parameter rather than a return, because a direction that has to decide
// something after the subtree ran - whether a pointer is materialised at all -
// cannot express that by returning.
//
// What it hands back is the subtree's own outcome, which is what that decision
// is taken on.
type descend func(v reflect.Value, at Path) (outcome, error)

// walker is one walk over one compiled schema, in one direction.
//
// The value is threaded down beside the node rather than resolved from the root
// per node, because a pointer is a container the walk descends through: an
// index path rooted at the whole value would step through the pointer, and
// reflect.Value.FieldByIndex panics on a nil one. So a node's index path is
// relative to the container above it, which keeps the promotion rule the
// compiler's alone: the walk cannot visit a field the schema does not hold, and
// the schema cannot hold a field the walk will not visit, because they are one
// list (ADR-0010).
type walker struct {
	dir direction
	run sched
}

// newWalker binds a direction and the scheduler this one walk runs under.
//
// The scheduler is a parameter of the walk rather than of a container, which is
// what makes ADR-0019's budget a number about the whole walk: a fanout carries
// one semaphore, and every container the walk enters shares it, so a nested
// struct cannot spend the budget once per level. Dump always passes [serial].
func newWalker(dir direction, run sched) *walker {
	return &walker{dir: dir, run: run}
}

// ctxEndedMsg is what a cancellation says at whichever node it arrived at. It
// reads at a leaf, which has an address, and at a container, which may be the
// root and have none.
//
// It carries no class, because a cancellation gets none: errors.Is against
// context.Canceled is already the match, and a ferry class for it would be a
// second spelling of a standard library one (ADR-0011).
const ctxEndedMsg = "the context ended before this part of the walk ran"

// walk is the shared half of both directions, and the whole of it.
//
// The context is checked at every node entry, which is a placement rather than
// a policy: it had to be somewhere for the walk to be written at all, and
// ADR-0019 left it exactly there. Per node entry is per task under either
// scheduler, so a cancellation is observed the same number of times whether or
// not the walk overlapped, which is one fewer way for the two to diverge.
func (w *walker) walk(ctx context.Context, s spot) (outcome, error) {
	if err := ctx.Err(); err != nil {
		return outcome{}, newError(momentWalk, nil, s.at, ctxEndedMsg).withCause(err)
	}

	// Omission is asked before the kind, because it is one question about the
	// Go value and the answer is the same at all six: this position is not
	// written at all, and nothing beneath it is either.
	if w.dir.omitted(s) {
		return outcome{}, nil
	}

	switch s.n.kind {
	case nodeLeaf:
		return w.dir.atLeaf(ctx, s)
	case nodePointer:
		return w.dir.atNullable(ctx, s, w.into(ctx, s))
	case nodeSlice:
		return w.dir.atSlice(ctx, s, w.element(ctx, s))
	case nodeMap:
		return w.dir.atMap(ctx, s, w.element(ctx, s))
	default:
		return w.dir.atStatic(ctx, s, w.into(ctx, s))
	}
}

// into is what a pointer's subtree is walked with: the same node's member list,
// over the value the direction materialised, at the address the pointer already
// occupies, because a pointer mints no segment of its own.
func (w *walker) into(ctx context.Context, s spot) descend {
	return func(v reflect.Value, at Path) (outcome, error) {
		here := spot{n: s.n, v: v, at: at}

		return w.run(len(here.n.fields), func(i int) (outcome, error) {
			return w.walk(ctx, member(here, i))
		})
	}
}

// element is what one member of a dynamic composite is walked with: the element
// shape, compiled once, at the address the direction minted for this member.
//
// It walks one member and does not run the scheduler, because a dynamic
// container's members are not a count the walk holds: the direction mints them
// as it lists them, and it aggregates them itself. So fanout reaches the members
// a type names and not the members a plane names, which is where ADR-0019 leaves
// it: a map filled from several goroutines is a write to one Go map, and the
// restructuring that would make it safe is not a scheduler.
func (w *walker) element(ctx context.Context, s spot) descend {
	return func(v reflect.Value, at Path) (outcome, error) {
		return w.walk(ctx, spot{n: s.n.fields[elemShape], v: v, at: at})
	}
}

// member is one position of a container's member list, resolved from the index
// the scheduler ran. It is built here rather than captured per member, which is
// what the count-and-one-body seam buys: a container allocates one closure and
// not one per field (ADR-0010).
func member(s spot, i int) spot {
	f := s.n.fields[i]

	return spot{n: f, v: memberOf(s.n, s.v, i, f), at: realised(s, f)}
}

// elemShape is where a dynamic composite keeps its one member: the element is
// compiled once and realised per member, so the field list holds exactly one
// node and it is that shape.
const elemShape = 0

// realised is where one member sits: its own compiled address in the static
// tier, and the same suffix under the realised address in the dynamic one.
//
// The equality is a shortcut rather than a decision, and it is safe for the
// reason it looks unsafe. Where a container stands at its own compiled address
// the two arms build the same path, so a map whose key genuinely is "*" - which
// under ferry's escaping model is ordinary segment text and not a marker - takes
// the first arm and gets the address the second one would have built.
func realised(s spot, f *node) Path {
	if s.at == s.n.addr {
		return f.addr
	}

	return s.at.concat(f.addr.below(s.n.addr))
}

// memberOf resolves one member's Go value out of the container's.
//
// The three containers reach their members three ways, and a pointer's is not
// FieldByIndex at an empty path: that panics on anything but a struct, so a
// *[N]T would fail at the array rather than reaching its elements.
func memberOf(n *node, v reflect.Value, i int, f *node) reflect.Value {
	switch n.kind {
	case nodeArray:
		return v.Index(i)
	case nodePointer:
		// A pointer mints no segment, so there is no step between it and what
		// it points at: its one member is the pointed-to value itself.
		return v
	default:
		return v.FieldByIndex(f.index)
	}
}

// loadFrom is the Load direction: read the plane, and write to the field only
// where the plane spoke.
//
// It holds the plane and nothing else. ADR-0006's presence bit is the outcome
// every subtree returns, so there is no counter here for a second subtree to
// read across (ADR-0019).
type loadFrom struct {
	r Reader

	// addrs is the address set the type determined, and it is here for one
	// question: whether a link a driver reported points at an address this
	// schema names, at the kind it was reported at. A driver cannot mint an
	// address, so it can only report one it was handed, and this catches the
	// one it could still get wrong - a member of some other schema's set, kept
	// across binds (ADR-0016).
	addrs *AddressSet
}

var _ direction = loadFrom{}

// omitted is nothing on Load. omitzero decides whether an address is written
// and Load writes none, which is ADR-0008's direction table rather than an
// omission here.
func (loadFrom) omitted(spot) bool { return false }

// atLeaf reads one address and decides what the observation means to the field.
func (l loadFrom) atLeaf(ctx context.Context, s spot) (outcome, error) {
	got, err := l.read(ctx, s)
	if err != nil {
		// A non-nil error reaches the caller as an error and is never
		// substituted with Absent (ADR-0004). Reading it as absence is how a
		// total backend outage loads as an all-zero struct with a nil error,
		// which a prototype committed and nothing saw for four rounds.
		return outcome{}, err
	}

	if got.Kind() == KindAbsent {
		return outcome{}, l.absent(s)
	}

	if err := l.apply(s, got); err != nil {
		return outcome{}, err
	}

	return outcome{wrote: true}, nil
}

// read asks the plane for one value, following a link where it reports one.
//
// The loop is core's rather than every driver's, which is the whole of what the
// reported redirect buys: a driver reports one hop and this keeps the set of
// addresses already asked, refuses a cycle, and checks that the target is one
// this schema names (ADR-0016). A driver that resolves its own aliases reports
// none and never enters the loop.
// The chain is walked in a second function that a load with no link in it never
// enters, and that split is what keeps the feature off the hot path: the visited
// set, and the pointer errors.As fills, are both allocated where there is a hop
// and nowhere else.
func (l loadFrom) read(ctx context.Context, s spot) (Value, error) {
	at := s.leaf()

	got, next, err := l.getOnce(ctx, at)
	if err != nil || next == nil {
		return got, err
	}

	return l.chase(ctx, s.at, at, *next)
}

// chase walks the rest of a leaf chain, and is entered only where the plane
// reported a link.
func (l loadFrom) chase(ctx context.Context, from Path, at, next LeafAddr) (Value, error) {
	var seen map[Path]bool

	for {
		seen = mark(seen, at.Path())

		if err := l.followed(from, next, seen); err != nil {
			return Value{}, err
		}

		at = next

		got, hop, err := l.getOnce(ctx, at)
		if err != nil || hop == nil {
			return got, err
		}

		next = *hop
	}
}

// mark records one address in the visited set, building the set where there is
// a hop to record and leaving it nil where there is not.
func mark(seen map[Path]bool, at Path) map[Path]bool {
	if seen == nil {
		seen = make(map[Path]bool, 2)
	}

	seen[at] = true

	return seen
}

// getOnce asks one address, and separates the two things a Get answers with:
// the value, or the link that says where the value is.
//
// A nil next is the answer itself. What a hop is held to is the caller's,
// because the visited set is.
func (l loadFrom) getOnce(ctx context.Context, at LeafAddr) (Value, *LeafAddr, error) {
	got, err := l.r.Get(ctx, at)
	if err == nil {
		return got, nil, nil
	}

	if next := leafHop(err); next != nil {
		return Value{}, next, nil
	}

	return Value{}, nil, fromDriver(momentWalk, at.Path(), err)
}

// leafHop is the link a Get reported, or nil where the error is an ordinary
// failure.
//
// It is a function of its own so that the pointer errors.As fills, which escapes
// into that call's interface parameter, is allocated on the path that has an
// error to examine and never on the path that read a value.
func leafHop(err error) *LeafAddr {
	var hop *LeafRedirect
	if errors.As(err, &hop) {
		return &hop.Target
	}

	return nil
}

// followed is the one check both arms of the resolution make about a hop: the
// target is not an address this chain has already been to, and it is one this
// schema names.
//
// A driver cannot mint an address, so a link whose target the schema does not
// name has nothing to report it with and stays the driver's own to resolve or
// to refuse. What is left for core is a target from some other schema's set,
// which is what this catches, and it names whose job the boundary case is
// (ADR-0016).
func (l loadFrom) followed(from Path, target Member, seen map[Path]bool) error {
	if seen[target.Path()] {
		return newError(momentWalk, ErrPlane, from,
			"reference cycle through "+readable(target))
	}

	if !l.addrs.Has(target) {
		return newError(momentWalk, ErrPlane, from, fmt.Sprintf(
			"this address refers to %s, which the schema does not address as the same kind of place: a "+
				"driver reports a link only to an address it was handed, and one the schema does not name "+
				"is the driver's own to resolve or to refuse", readable(target)))
	}

	return nil
}

// readable is how an address reads inside a refusal.
//
// The zero address renders as the empty string, because it has no segments and
// the canonical rendering of no segments is nothing (ADR-0003). It is in no
// address set, so it reaches these messages exactly when a driver reported a
// link to one it never minted, which is the case the message has to be readable
// for.
func readable(m Member) string {
	if s := m.String(); s != "" {
		return s
	}

	return "the empty address, which is no address"
}

// absent is what an address the plane does not have means to a leaf, and it is
// the only place a declaration is consulted for a value.
//
// ADR-0006's one rule stands under all three arms: Absent means ferry does not
// write what the plane said, because it said nothing. A declared default is
// applied there and only there, so an explicit empty beats a non-zero default
// and a seeded field keeps what it had. required refuses there and only there,
// because it is a presence test and nothing else, satisfied by any observation
// other than Absent - including a String("") and a Null a type can hold.
//
// The two cannot both be set: a default answers the absence required forbids,
// and schema compile refuses the pair.
func (l loadFrom) absent(s spot) error {
	switch {
	case s.n.hasDef:
		// Not counted as presence. A default fills a hole in a section and
		// never conjures the section, or no *T with a default anywhere beneath
		// it could ever be nil. A section the caller seeded already exists, so
		// it keeps the default it was filled with; that is
		// [loadFrom.materialise]'s to decide and not this one's (#253).
		return l.apply(s, s.n.def)
	case s.n.required:
		return newError(momentWalk, ErrMissing, s.at, "required, and the plane holds nothing at this address")
	default:
		return nil
	}
}

// apply hands one observation to the leaf's own codec.
//
// A declared default travels this path and no other, which is the whole of
// ADR-0006's load-bearing claim: a default is indistinguishable at the boundary
// from what a flat plane would have reported, so ferry has one conversion
// authority rather than two and a registered codec's type gets defaults for
// nothing. The Value on the node is text, decoded fresh here on every load
// rather than cached as a Go value, because a cached one aliases across loads.
//
// Which kinds this leaf takes and how it reads their text is the leaf's own,
// resolved at compile and held on the node, so the walk decides nothing about a
// type here (ADR-0005).
func (loadFrom) apply(s spot, got Value) error {
	if err := s.n.codec.decode(s.v, got); err != nil {
		return newError(momentWalk, ErrValue, s.at, err.Error()).withCause(err)
	}

	return nil
}

// atStatic walks a struct or an array and then answers required at its address.
//
// The early return is the one suppression bit the walk owns, and it is here
// rather than in the scheduler because only the walk can see the subtree
// relationship it turns on. ADR-0011's rule is to report every failure that is
// not a consequence of another it is already reporting, and a composite's own
// required failure is the summary of what its children just said: a required
// child absent under a required parent is two errors and one remediation, so
// the parent's is suppressed where a child under it already reported.
//
// The neighbouring case needs nothing, which is why this is one bit and not a
// redesign: a child that is present and fails to decode is a failure at the
// child's address, and the parent's required never had a second thing to say
// about it.
func (l loadFrom) atStatic(_ context.Context, s spot, into descend) (outcome, error) {
	out, err := into(s.v, s.at)
	if err != nil {
		return out, err
	}

	return out, l.supplied(s, out)
}

// supplied is required at a composite address: satisfied by the plane having
// supplied at least one of the address's static children (ADR-0006).
//
// It is the presence bit the subtree that just ran returned, which gives one
// meaning on a tree plane and a flat plane alike - the only row where the two
// could differ is an explicit null at the container's own address, which a flat
// plane cannot express, so the divergence cannot arise. A declared default
// beneath the address is not presence, so a section whose every field carries
// one is still refused.
func (loadFrom) supplied(s spot, out outcome) error {
	if !s.n.required || out.wrote {
		return nil
	}

	return newError(momentWalk, ErrMissing, s.at, "required, and the plane supplied nothing under it")
}

// atNullable materialises a pointer exactly where the plane spoke about it or
// under it.
//
// The three observations are three answers. A null at the section's own address
// is the plane saying the section is there and is nothing, so the pointer is
// nil. Present is the plane saying the section is there, so the pointer is
// materialised whether or not anything was written beneath it, which is how a
// present-but-empty section survives a reload. Absent is the plane saying
// nothing about the section itself, so what decides is whether it said anything
// beneath it: a declared default is not presence and neither is a seed, so an
// optional section stays optional (ADR-0006).
func (l loadFrom) atNullable(ctx context.Context, s spot, into descend) (outcome, error) {
	info, err := l.probe(ctx, s)
	if err != nil {
		return outcome{}, err
	}

	if info.Presence() == PresenceNull {
		s.v.SetZero()

		return outcome{wrote: true}, nil
	}

	return l.materialise(s, into, info.Presence() == PresencePresent)
}

// probe asks the plane what it holds at a container's own address.
//
// A plane that cannot answer is not a failure: [Prober] is optional, in the
// same idiom as [Releaser], because a plane that cannot list often cannot say
// whether a container is there either (ADR-0016). Such a plane answers absence,
// which is the plane saying nothing about the container itself and leaves what
// is under it to decide.
// It also follows a link where the plane reports one, so what comes back is
// never a redirect and every caller of this may switch on three answers rather
// than four (ADR-0016).
func (l loadFrom) probe(ctx context.Context, s spot) (SectionInfo, error) {
	pr, ok := l.r.(Prober)
	if !ok {
		return sectionAbsent, nil
	}

	at := s.container()

	info, next, err := l.probeOnce(ctx, pr, at)
	if err != nil || next == nil {
		return info, err
	}

	return l.chaseContainer(ctx, pr, s.at, at, next)
}

// chaseContainer walks the rest of a container chain, and is entered only where
// the plane reported a link, for the reason [loadFrom.chase] gives.
func (l loadFrom) chaseContainer(ctx context.Context, pr Prober, from Path, at, next Container,
) (SectionInfo, error) {
	var seen map[Path]bool

	for {
		seen = mark(seen, at.Path())

		if err := l.hop(from, at, next, seen); err != nil {
			return sectionAbsent, err
		}

		at = next

		info, target, err := l.probeOnce(ctx, pr, at)
		if err != nil || target == nil {
			return info, err
		}

		next = target
	}
}

// probeOnce asks one container address, and separates the two things a probe
// answers with: what the plane holds there, or the link that says where the
// container is.
//
// An answer that says elsewhere and names nowhere is neither, and it is refused
// rather than read as one of them: a nil target reads as absence at a pointer,
// as an empty container at a composite and as a refusal at an unlistable one, so
// the same inconsistent answer would mean three different things (ADR-0016).
func (l loadFrom) probeOnce(ctx context.Context, pr Prober, at Container) (SectionInfo, Container, error) {
	info, err := pr.Probe(ctx, at)
	if err != nil {
		return sectionAbsent, nil, fromDriver(momentWalk, at.Path(), err)
	}

	target, linked := info.Redirect()
	if linked {
		return sectionAbsent, target, nil
	}

	if info.Presence() == PresenceElsewhere {
		return sectionAbsent, nil, newError(momentWalk, ErrPlane, at.Path(), fmt.Sprintf(
			"%T reported that what this address names lives elsewhere and named no target: a link is "+
				"reported with the address it points at, and an answer carrying none is neither a place "+
				"to go nor a report about this one", l.r))
	}

	return info, nil, nil
}

// hop holds one reported container link to the rule the kinds put on it, and
// then to the two every link obeys: a section may only name a section and a
// composite only a composite.
//
// The kinds are not interchangeable, so a link across them would name a place
// whose members are decided by the other rule - a section's from the type and a
// composite's from the value - and there would be nothing consistent to walk
// there (ADR-0016).
func (l loadFrom) hop(from Path, at, target Container, seen map[Path]bool) error {
	if rank(at) != rank(target) {
		return newError(momentWalk, ErrPlane, from, fmt.Sprintf(
			"this address refers to %s, which is not the same kind of place: what is under a section comes "+
				"from the type and what is under a composite comes from the value, so a link between them "+
				"names somewhere its own members could not be", readable(target)))
	}

	return l.followed(from, target, seen)
}

// materialise builds the pointee fresh, walks into it, and publishes it only
// where the walk wrote.
//
// The fresh allocation is not a tidiness. LoadOver's failure property rests on
// a shallow copy of the seed, so a walk that wrote through the seed's own
// pointer would publish a partial load into a value the caller still holds and
// the property would break in silence.
//
// The early return is [loadFrom.atStatic]'s suppression bit at the other
// composite shape, and it is one rule rather than two: a pointer's required is
// the same summary of the same children.
//
// A section the caller seeded is published whatever the plane said, and that is
// ADR-0006's rule rather than a second one: a section that exists gets its
// defaults, and a non-nil pointer exists. Publishing it changes nothing the
// caller can see except that the value is the walk's own copy, because the walk
// wrote nothing the seed did not already carry - a default it declared, and
// nothing else (#253). What stays exactly as it was is the nil seed: a declared
// default beneath an absent section is not presence, so the pointer is still
// nil, or no *T with a default anywhere beneath it could ever be nil.
// present says whether the plane reported the section there in its own right,
// which materialises the pointer even where nothing was written beneath it.
func (l loadFrom) materialise(s spot, into descend, present bool) (outcome, error) {
	seeded := !s.v.IsNil()

	fresh := reflect.New(s.v.Type().Elem())
	if seeded {
		fresh.Elem().Set(s.v.Elem())
	}

	out, err := into(fresh.Elem(), s.at)
	if err != nil {
		return out, err
	}

	out.wrote = out.wrote || present

	if out.wrote || seeded {
		s.v.Set(fresh)
	}

	return out, l.supplied(s, out)
}

// atSlice builds the sequence the plane holds under this address.
func (l loadFrom) atSlice(ctx context.Context, s spot, into descend) (outcome, error) {
	segs, out, err := l.members(ctx, s)
	if len(segs) == 0 {
		return out, err
	}

	return l.buildSlice(s, segs, into)
}

// atMap builds the mapping the plane holds under this address.
func (l loadFrom) atMap(ctx context.Context, s spot, into descend) (outcome, error) {
	segs, out, err := l.members(ctx, s)
	if len(segs) == 0 {
		return out, err
	}

	return l.buildMap(s, segs, into)
}

// members is what a dynamic container holds: the segments the plane mints under
// its address, or the answer at the address itself where there are none.
//
// The walk asks Children first and probes the container's own address only where
// nothing came back, because a member list is the whole answer wherever there is
// one. A member list that comes back empty leaves the outcome beside it to say
// what the address itself held.
func (l loadFrom) members(ctx context.Context, s spot) (segs []Segment, out outcome, err error) {
	lister, ok := l.r.(Enumerator)
	if !ok {
		return l.unlistable(ctx, s)
	}

	segs, err = lister.Children(ctx, s.composite())
	if err != nil {
		return nil, outcome{}, fromDriver(momentWalk, s.at, err)
	}

	if len(segs) == 0 {
		out, err = l.empty(ctx, s)

		return nil, out, err
	}

	return segs, outcome{}, nil
}

// empty is a dynamic container the plane listed nothing under: its own address
// is the whole answer.
//
// A null there and an absence there land on the same Go value, because nil and
// empty are one value for a slice and a map. They differ only in what they say
// to the section above: a null is the plane speaking, and absence is not.
func (l loadFrom) empty(ctx context.Context, s spot) (outcome, error) {
	info, err := l.probe(ctx, s)
	if err != nil {
		return outcome{}, err
	}

	if info.Presence() == PresenceAbsent {
		return outcome{}, nil
	}

	s.v.SetZero()

	return outcome{wrote: true}, nil
}

// unlistable is a dynamic container over a source that cannot list: its own
// address is the only thing there is to ask, and anything short of a complete
// answer there is a refusal.
//
// The refusal names the field and the source rather than loading an empty
// composite, which is the most plausible-looking wrong answer available and the
// silent one ADR-0001 rules out (ADR-0004).
func (l loadFrom) unlistable(ctx context.Context, s spot) (segs []Segment, out outcome, err error) {
	info, err := l.probe(ctx, s)
	if err != nil {
		return nil, outcome{}, err
	}

	if info.Presence() == PresenceNull {
		s.v.SetZero()

		return nil, outcome{wrote: true}, nil
	}

	return nil, outcome{}, newError(momentWalk, ErrPlane, s.at, fmt.Sprintf(
		"the addresses under a %s come from the value, and %T cannot list what a plane holds under an "+
			"address: a source that does not implement ferry.Enumerator reaches every static address and "+
			"no dynamic one, which is a property of that plane rather than of this schema", s.v.Type(), l.r))
}

// buildSlice fills a sequence the length of what the plane enumerated, and
// publishes it only once every element has landed.
//
// The allocation is fresh rather than the seed's, and that is LoadOver's failure
// property rather than tidiness: over := seed is a shallow copy, so a slice
// written through the seed's own backing array would publish a partial load into
// a value the caller still holds. It is also ADR-0006's replacement rule - a
// composite is a single decision, and if the plane has any children under the
// address then it has said what the composite is.
func (loadFrom) buildSlice(s spot, segs []Segment, into descend) (outcome, error) {
	segs = slices.Clone(segs)
	slices.SortFunc(segs, compareSegments)

	if err := contiguous(s, segs); err != nil {
		return outcome{}, err
	}

	fresh := reflect.MakeSlice(s.v.Type(), len(segs), len(segs))

	var b batch
	for i, seg := range segs {
		b.add(into(fresh.Index(i), s.child(seg)))
	}

	out, err := b.done()
	if err != nil {
		return out, err
	}

	s.v.Set(fresh)
	out.wrote = true

	return out, nil
}

// contiguous refuses an enumerated member the sequence has no place for.
//
// A slice's members are positions, so a Name segment under one is a driver
// saying the plane holds a mapping where the schema holds a sequence. The
// positions then have to be 0 to n-1 with none missing: a gap would leave ferry
// choosing between a short sequence and an absent element with nothing to
// choose on, and a position past the count is a length ferry would be
// allocating for on a plane's say-so.
func contiguous(s spot, segs []Segment) error {
	for at, seg := range segs {
		if seg.Kind() != Index {
			return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
				"the plane holds a member named under a sequence, and a %s is addressed by position: model "+
					"a container whose members the plane names as a map", s.v.Type()))
		}

		if want := IndexSegment(uint(at)); seg != want {
			return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
				"the plane holds position %s under a sequence of %d, and a %s is addressed from 0 upwards "+
					"with no position missing: fill the gap, or model a sequence whose positions are chosen "+
					"by the plane as a map keyed by those positions",
				seg.Text(), len(segs), s.v.Type()))
		}
	}

	return nil
}

// buildMap fills a mapping out of what the plane enumerated, fresh for the
// reason [loadFrom.buildSlice] gives: a shallow copy of the seed shares a map's
// buckets outright, so writing into a seeded map mutates the caller's map.
func (loadFrom) buildMap(s spot, segs []Segment, into descend) (outcome, error) {
	fresh := reflect.MakeMapWithSize(s.v.Type(), len(segs))

	var b batch
	for _, seg := range segs {
		b.add(atKey(s, fresh, seg, into))
	}

	out, err := b.done()
	if err != nil {
		return out, err
	}

	// Injectivity has a Load side, and it is the mirror of the Dump-side one:
	// there, two Go keys rendering to one address lose an entry as it is
	// written; here, two plane keys parsing to one Go key lose one as it is
	// read. /m/1 and /m/01 are two addresses and one int.
	if fresh.Len() != len(segs) {
		return outcome{}, newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %d addresses under this mapping and %s takes %d of them: two plane keys read "+
				"back as one Go key, so an entry would be lost", len(segs), s.v.Type(), fresh.Len()))
	}

	s.v.Set(fresh)
	out.wrote = true

	return out, nil
}

// atKey reads one key out of an enumerated segment and walks the value under it.
//
// A position under a mapping is refused: it is the plane saying the container
// is a sequence where the schema says it is keyed by name, and reading the
// digits as a key would be ferry deciding that the plane meant something else.
func atKey(s spot, fresh reflect.Value, seg Segment, into descend) (outcome, error) {
	if seg.Kind() != Index {
		return atName(s, fresh, seg, into)
	}

	return outcome{}, newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
		"the plane holds a member by position under this mapping, and a %s takes one member per name",
		s.v.Type()))
}

// atName reads one named member out of an enumerated segment and walks the
// value under it.
//
// An empty name is refused rather than read, which is the load side of the
// refusal [sortedKeys] makes. Both are one rule: an empty segment names no
// address, so a plane that spells a member with one is offering a member at an
// address ferry declares illegal, and reading it would load a mapping that could
// never be written back (#258).
func atName(s spot, fresh reflect.Value, seg Segment, into descend) (outcome, error) {
	if seg.Text() == "" {
		return outcome{}, newError(momentWalk, ErrValue, s.at, emptySegmentMsg)
	}

	key := reflect.New(s.v.Type().Key()).Elem()
	at := s.child(seg)

	if err := s.n.key.parse(key, seg.Text()); err != nil {
		return outcome{}, newError(momentWalk, ErrValue, at, err.Error()).withCause(err)
	}

	val := reflect.New(s.v.Type().Elem()).Elem()

	out, err := into(val, at)
	if err != nil {
		return out, err
	}

	fresh.SetMapIndex(key, val)

	return out, nil
}

// dumpTo is the Dump direction: hand the plane one Value per address, or stage
// it where there is no plane in sight.
type dumpTo struct {
	// w is the plane, and nil is a dump with none: the encodes are staged in
	// the outcome instead, which is how ADR-0011's rule that every address is
	// encoded before any of them is written is carried without a second
	// direction and without a buffer the whole walk shares.
	w Writer

	// u is w's retraction capability, resolved once at the walk's start rather
	// than at each composite. It is nil exactly where w is, and where w is a
	// writer that cannot forget an address - which is a state no schema holding
	// a composite reaches, because the open refused it there (ADR-0004).
	u Unsetter

	// addrs is the address set the type determined. An address minted from a
	// map key or a sequence index is checked against it and against what the
	// rest of the walk realised, which is ADR-0003's dynamic tier: the check is
	// a map insert per address, and what the walk realised travels up in the
	// outcome rather than sitting in one collection every subtree writes to.
	addrs *AddressSet
}

var _ direction = dumpTo{}

// omitted is ADR-0006's omission rule, whole: a comparison against the Go zero
// value, evaluated before anything converts it, which is omitzero's shape in
// encoding/json/v2 and not omitempty's.
//
// It is not a comparison against the default. A field holding its declared
// default is dumped like any other, for two independent reasons: ferry cannot
// tell "still at its default" from "explicitly set to the same value", because
// they are the same bits, and omitting it would make the stored artefact
// under-specified, so what it denotes would be decided by whichever version of
// the code reads it.
//
// An omission is the absence of a Set call rather than a Set carrying nothing,
// which is what keeps Absent a Reader-side kind and Writer at one method.
func (dumpTo) omitted(s spot) bool { return s.n.omitzero && s.v.IsZero() }

// atStatic walks a struct or an array, which have nothing of their own to write
// at their address: a struct has no address at all, and an array's membership
// is the type's.
func (dumpTo) atStatic(_ context.Context, s spot, into descend) (outcome, error) {
	return into(s.v, s.at)
}

// atLeaf writes one address. It never writes an Absent, which is a Reader-side
// kind: an omitted address is one that gets no Set call at all rather than one
// that gets a Set of nothing (ADR-0006).
//
// A value the leaf's representation does not cover is reported rather than
// swallowed, which in core's set is a time.Time outside years 0 to 9999 and
// nothing else. A codec that produced a kind other than the one it declared
// used to be checked here too, and is not, because ADR-0017's payload-typed
// halves leave a registrant nothing to declare and core the one that wraps.
func (d dumpTo) atLeaf(ctx context.Context, s spot) (outcome, error) {
	v, err := s.n.codec.encode(s.v)
	if err != nil {
		return outcome{}, newError(momentWalk, ErrValue, s.at, err.Error()).withCause(err)
	}

	return d.write(ctx, s.leaf(), v)
}

// write hands the plane one Value at one leaf, and reports a driver's refusal
// as the driver's.
//
// A dump with no plane in sight stages the write in its outcome instead, and
// the two are one function because they are one decision made once: what the
// walk does with an encoded value is where it goes, not what it is (ADR-0011).
//
// The outcome says a write happened, which is what a section above reads to
// decide whether it emitted anything of its own (ADR-0016).
func (d dumpTo) write(ctx context.Context, at LeafAddr, v Value) (outcome, error) {
	if d.w == nil {
		return outcome{wrote: true, writes: []stagedWrite{{leaf: at, v: v}}}, nil
	}

	if err := d.w.Set(ctx, at, v); err != nil {
		return outcome{}, fromDriver(momentWalk, at.Path(), err)
	}

	return outcome{wrote: true}, nil
}

// ensure says at a container's own address what the value has to say there: a
// null, or that the container is present and holds nothing.
//
// It is a capability rather than a [Writer] method, because a plane with no
// spelling for a container should refuse rather than receive a write it will
// mis-store, and the refusal names the address and the plane (ADR-0016).
func (d dumpTo) ensure(ctx context.Context, at Container, p Presence) (outcome, error) {
	if d.w == nil {
		return outcome{wrote: true, writes: []stagedWrite{{at: at, p: p}}}, nil
	}

	e, ok := d.w.(Ensurer)
	if !ok {
		return outcome{}, newError(momentWalk, ErrPlane, at.Path(), unspellableMsg(p, d.w))
	}

	if err := e.Ensure(ctx, at, p); err != nil {
		return outcome{}, fromDriver(momentWalk, at.Path(), err)
	}

	return outcome{wrote: true}, nil
}

// unset tells the plane to let go of a composite's address and everything under
// it, which is what makes a dump replace that composite rather than add to it
// (ADR-0004).
//
// It reports no presence. Forgetting is not the plane speaking, so a composite
// whose members wrote nothing has still written nothing, and an unset can never
// materialise a section above it (ADR-0006).
//
// It never asks whether the plane can forget an address, which is the one place
// this differs from [dumpTo.ensure]. What the value has to say at a container's
// own address depends on the value, so a missing [Ensurer] is knowable no
// earlier than the address it is needed at; whether a schema can need a
// retraction is the schema's own property, so a writer with no [Unsetter] was
// refused at the open and never reaches here (ADR-0004).
func (d dumpTo) unset(ctx context.Context, at CompositeAddr) (outcome, error) {
	if d.w == nil {
		return outcome{writes: []stagedWrite{{forget: true, comp: at}}}, nil
	}

	return outcome{}, forgotten(ctx, d.u, at)
}

// replacing opens a composite's own writes with the unset that makes the rest of
// them a replacement, so that both composite arms below say it once and in the
// order the plane has to see it.
func (d dumpTo) replacing(ctx context.Context, at CompositeAddr) batch {
	var b batch

	b.add(d.unset(ctx, at))

	return b
}

// unspellableMsg refuses a container-level write on a plane that cannot spell
// one, naming what the value needed said and what the plane is.
func unspellableMsg(p Presence, w Writer) string {
	return fmt.Sprintf("the value says this container is %s, and %T cannot spell a container at its own "+
		"address: a sink that does not implement ferry.Ensurer writes the addresses beneath a container "+
		"and never the container itself, which is a property of that plane rather than of this schema", p, w)
}

// unforgettableMsg refuses, at the open, a dump of a schema holding a composite
// against a plane that cannot forget an address, and is the retraction half of
// [unspellableMsg]: the same two capabilities, worded once each.
func unforgettableMsg(w Writer) string {
	return fmt.Sprintf("this schema holds a composite at this address and a dump replaces one, and %T cannot "+
		"forget an address: a sink that does not implement ferry.Unsetter would keep whatever an earlier "+
		"dump left under this address, which loads back as a value nobody wrote", w)
}

// atNullable writes a null where the pointer is nil and descends where it is
// not, which are the two states a pointer has.
//
// It never writes anything at the container address when the pointer is set and
// something was written beneath it, because the answer is then under it: a
// container address is never realised at the same time as anything beneath it
// (ADR-0003). A pointer that is set and wrote nothing is the one case with
// nowhere else to be said: Go can express present-and-empty, and without one
// section-level write the round trip turns it into absence (ADR-0016).
func (d dumpTo) atNullable(ctx context.Context, s spot, into descend) (outcome, error) {
	if s.v.IsNil() {
		return d.ensure(ctx, s.container(), PresenceNull)
	}

	out, err := into(s.v.Elem(), s.at)
	if err != nil || out.wrote {
		return out, err
	}

	return d.ensure(ctx, s.container(), PresencePresent)
}

// atSlice writes one address per element, and a null at the sequence's own
// address where it has none.
//
// Nil and empty are one value there, and the collision is forced rather than
// chosen: three Go states meet two observations at a container address, and
// measured through a real YAML plane a missing key, an empty list and an empty
// mapping are one observation. The draft that chose the other collision - Null
// for nil and nothing for empty - made a map key whose value minted nothing
// vanish entirely, which is a silently dropped entry (ADR-0005).
func (d dumpTo) atSlice(ctx context.Context, s spot, into descend) (outcome, error) {
	b := d.replacing(ctx, s.composite())

	if s.v.Len() == 0 {
		b.add(d.ensure(ctx, s.composite(), PresenceNull))

		return b.done()
	}

	r := d.realising(s, s.v.Len())

	// The position is counted in uint rather than converted from one, because
	// Path.Elem takes it unsigned: a negative position has no meaning and a
	// conversion is a place the constraint could be lost.
	var at uint

	for i := range s.v.Len() {
		r.member(s.v.Index(i), s.at.Elem(at), into)
		at++
	}

	b.add(r.b.done())

	return b.done()
}

// atMap writes one address per key, in the order of the key text, and a Null at
// the mapping's own address where it has no entries.
func (d dumpTo) atMap(ctx context.Context, s spot, into descend) (outcome, error) {
	b := d.replacing(ctx, s.composite())

	if s.v.Len() == 0 {
		b.add(d.ensure(ctx, s.composite(), PresenceNull))

		return b.done()
	}

	keys, err := sortedKeys(s)
	if err != nil {
		b.fail(err)

		return b.done()
	}

	r := d.realising(s, len(keys))

	for _, k := range keys {
		r.member(s.v.MapIndex(k.key), s.at.At(k.text), into)
	}

	b.add(r.b.done())

	return b.done()
}

// emptySegmentMsg is what an empty map key is refused with, and it is one
// message because it is one rule read from two ends: a value that renders one
// and a plane that spells one both name an address the model does not have
// (#258). Measured before the dump-side refusal existed, a map key rendering to
// empty text minted /m/ and the dump returned nil; the load side had no refusal
// at all, so /m/ loaded clean and could not be written back.
const emptySegmentMsg = "a key of this mapping renders to empty text, and an empty segment names no address: " +
	"an entry at it could not be read back, and the address it would mint is not one"

// entry is one map key with the text it addresses by, computed once.
type entry struct {
	key  reflect.Value
	text string
}

// sortedKeys orders a map's members by their key text, with the text computed
// once per key rather than inside the comparator.
//
// The sort is ADR-0001's determinism invariant applied rather than re-decided:
// Go's map iteration order is randomised, and measured over 300 dumps of an
// eight-key map it is 1 distinct ordering with the sort and 8 over 50 marshals
// without it upstream. Computing the text once is the shape a duplicate check
// needs anyway, and it is far the cheaper one: inside the comparator a key's
// text is recomputed O(n log n) times, measured at 1146337 ns against 158116 ns
// over 512 keys.
//
// A key whose text a registered codec could not produce fails the whole mapping
// at the mapping's own address, because the key has no address yet: what failed
// is the thing that would have named one (ADR-0009).
func sortedKeys(s spot) ([]entry, error) {
	keys := s.v.MapKeys()
	out := make([]entry, 0, len(keys))

	for _, k := range keys {
		text, err := s.n.key.text(k)
		if err != nil {
			return nil, newError(momentWalk, ErrValue, s.at, err.Error()).withCause(err)
		}

		// An empty segment names no address, and the address model says so from
		// both ends: a tag cannot write one and neither may a value. Measured
		// before the refusal existed, a map key rendering to empty text minted
		// /m/ and the dump returned nil, so a plane was written at an address
		// ferry declares illegal (#258).
		if text == "" {
			return nil, newError(momentWalk, ErrValue, s.at, emptySegmentMsg)
		}

		out = append(out, entry{key: k, text: text})
	}

	slices.SortFunc(out, func(a, b entry) int { return cmp.Compare(a.text, b.text) })

	return out, nil
}

// realising is one dynamic container mid-walk: the addresses its own members
// have taken, and what those members have said so far.
//
// The container mints its members' addresses itself, in the order it lists
// them, so nothing is minted across the scheduler seam and the refusal is the
// same on every run whatever order the members are walked in.
type realising struct {
	s     spot
	kind  addrKind
	addrs *AddressSet
	b     batch
}

// realising starts one, with room for the members the container is about to
// list.
func (d dumpTo) realising(s spot, n int) *realising {
	return &realising{
		s:     s,
		kind:  elemKind(s),
		addrs: d.addrs,
		b:     batch{out: outcome{minted: make(map[Path]spot, n)}},
	}
}

// elemKind is the address kind one member of a dynamic composite takes. The
// driver mints the segment and the schema types the child, so the kind comes
// from the element compiled under the composite (ADR-0016).
func elemKind(s spot) addrKind {
	if k, ok := containerKind(s.n.fields[elemShape]); ok {
		return k
	}

	return kindLeaf
}

// member mints one address and walks the member it belongs to, and does neither
// where the address is one this walk has already realised.
func (r *realising) member(v reflect.Value, at Path, into descend) {
	if err := r.mint(at); err != nil {
		r.b.fail(err)

		return
	}

	r.b.add(into(v, at))
}

// mint records an address this value determined, and refuses one that is
// already taken - before the write it belongs to, and never after it.
//
// It answers for this container's own members and for the set the static pass
// determined. An address two containers realised is refused where their
// outcomes meet, which is the same refusal for the same reason and is stated
// once, at [collided].
func (r *realising) mint(at Path) error {
	_, taken := r.b.out.minted[at]
	if taken || r.addrs.Has(memberAt(r.kind, at)) {
		return collided(at, r.s)
	}

	r.b.out.minted[at] = r.s

	return nil
}
