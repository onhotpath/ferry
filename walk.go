package ferry

import (
	"cmp"
	"context"
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
// It is unexported, core ships exactly one implementation, and no exported name
// takes or returns one. That is deliberate: whether ferry ever walks
// concurrently is an open question, and an importer able to select a scheduler
// today would answer it by accident.
type sched func(n int, run func(i int) (outcome, error)) (outcome, error)

// serial is core's only scheduler: every member, in order, and every failure.
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

	// atArray is what an array's own address is asked before its elements are
	// walked, which is nothing on Dump and one question on Load: an array's
	// membership is the type's, so a plane holding an index outside it is
	// holding something this type cannot take.
	atArray(ctx context.Context, s spot) error

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

// newWalker binds a direction to the value it walks. The scheduler is not a
// parameter, because there is one and nobody chooses it.
func newWalker(dir direction) *walker {
	return &walker{dir: dir, run: serial}
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
// a policy: it had to be somewhere for the walk to be written at all, and the
// concurrency ticket owns whether it is per leaf, per subtree or not at all.
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
	case nodeArray:
		if err := w.dir.atArray(ctx, s); err != nil {
			return outcome{}, err
		}

		return w.dir.atStatic(ctx, s, w.into(ctx, s))
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
}

var _ direction = loadFrom{}

// omitted is nothing on Load. omitzero decides whether an address is written
// and Load writes none, which is ADR-0008's direction table rather than an
// omission here.
func (loadFrom) omitted(spot) bool { return false }

// atLeaf reads one address and decides what the observation means to the field.
func (l loadFrom) atLeaf(ctx context.Context, s spot) (outcome, error) {
	got, err := l.r.Get(ctx, s.at)
	if err != nil {
		// A non-nil error reaches the caller as an error and is never
		// substituted with Absent (ADR-0004). Reading it as absence is how a
		// total backend outage loads as an all-zero struct with a nil error,
		// which a prototype committed and nothing saw for four rounds.
		return outcome{}, fromDriver(momentWalk, s.at, err)
	}

	if got.Kind() == KindAbsent {
		return outcome{}, l.absent(s)
	}

	if err := l.apply(s, got); err != nil {
		return outcome{}, err
	}

	return outcome{wrote: true}, nil
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

// atNullable materialises a pointer exactly where the plane spoke under it.
//
// The three observations are three answers. A Null at the pointer's own address
// is the plane saying the section is there and is nothing, so the pointer is
// nil. Anything else at that address is a value at a container address, which
// carries Absent or Null and never anything else (ADR-0003). Absent is the
// plane saying nothing about the section itself, so what decides is whether it
// said anything beneath it: a declared default is not presence and neither is a
// seed, so an optional section stays optional (ADR-0006).
func (l loadFrom) atNullable(ctx context.Context, s spot, into descend) (outcome, error) {
	if out, more, err := l.container(ctx, s); !more {
		return out, err
	}

	return l.materialise(s, into)
}

// container answers a container address, and reports whether the plane left the
// answer to what is under it.
//
// The three observations are three answers, and they are the same three at a
// pointer and at a dynamic composite. A Null is the plane saying the container
// is there and holds nothing, which is the zero value - nil for a pointer, and
// nil for a slice or a map, where nil and empty are one value anyway. Anything
// else is a value at a container address, which carries absence or a null and
// never anything else (ADR-0003). Absent is the plane saying nothing about the
// container itself, so what is under it decides.
func (l loadFrom) container(ctx context.Context, s spot) (out outcome, more bool, err error) {
	got, err := l.r.Get(ctx, s.at)
	if err != nil {
		return outcome{}, false, fromDriver(momentWalk, s.at, err)
	}

	if got.Kind() == KindNull {
		s.v.SetZero()

		return outcome{wrote: true}, false, nil
	}

	if got.Kind() != KindAbsent {
		return outcome{}, false, newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %s at a container address, which holds absence or a null and nothing else",
			got.Kind()))
	}

	return outcome{}, true, nil
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
func (l loadFrom) materialise(s spot, into descend) (outcome, error) {
	seeded := !s.v.IsNil()

	fresh := reflect.New(s.v.Type().Elem())
	if seeded {
		fresh.Elem().Set(s.v.Elem())
	}

	out, err := into(fresh.Elem(), s.at)
	if err != nil {
		return out, err
	}

	if out.wrote || seeded {
		s.v.Set(fresh)
	}

	return out, l.supplied(s, out)
}

// atArray refuses a plane holding an index this array cannot.
//
// An array's length is part of its type, so an index outside it is a value with
// no field to land in, and padding or truncating it would be the silent loss
// ADR-0001 rules out. It is asked only of a plane that can enumerate, which is
// the same asymmetry that makes an array loadable from one that cannot: the
// elements are read by name either way, and only enumeration can reveal an
// index that is not one of them.
func (l loadFrom) atArray(ctx context.Context, s spot) error {
	lister, ok := l.r.(Enumerator)
	if !ok {
		return nil
	}

	kids, err := lister.Children(ctx, s.at)
	if err != nil {
		return fromDriver(momentWalk, s.at, err)
	}

	errs := make([]error, 0, len(kids))
	for _, kid := range kids {
		errs = append(errs, overLength(s, kid))
	}

	return join(errs...)
}

// overLength reports one child of an array address that the array has no
// element for, and nothing for one it has.
//
// It compares against the element addresses the compiler minted rather than
// against a number, because those addresses are the whole of what this array
// can hold and comparing what a driver enumerated with what the type determined
// is the same question stated once.
func overLength(s spot, kid Path) error {
	seg := lastSegment(kid)
	if seg.Kind() != Index || holds(s, kid) {
		return nil
	}

	return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
		"the plane holds index %s and %s holds %d", seg.Text(), s.v.Type(), len(s.n.fields)))
}

// holds reports whether one of a container's members is at this address.
func holds(s spot, addr Path) bool {
	return slices.ContainsFunc(s.n.fields, func(f *node) bool { return realised(s, f) == addr })
}

// lastSegment is an address's final step.
//
// The zero Segment is a Name with no text, so a caller may read the kind off the
// result without asking whether there was a segment at all: an empty path
// answers Name, and every caller here is looking at what a driver enumerated
// under an address it was asked about.
func lastSegment(p Path) Segment {
	var last Segment

	for seg := range p.Segments() {
		last = seg
	}

	return last
}

// atSlice builds the sequence the plane holds under this address.
func (l loadFrom) atSlice(ctx context.Context, s spot, into descend) (outcome, error) {
	kids, out, err := l.members(ctx, s)
	if len(kids) == 0 {
		return out, err
	}

	return l.buildSlice(s, kids, into)
}

// atMap builds the mapping the plane holds under this address.
func (l loadFrom) atMap(ctx context.Context, s spot, into descend) (outcome, error) {
	kids, out, err := l.members(ctx, s)
	if len(kids) == 0 {
		return out, err
	}

	return l.buildMap(s, kids, into)
}

// members is what a dynamic container holds, asked in one of two orders
// depending on whether the source can list.
//
// Over an Enumerator the walk asks Children first and asks the container's own
// address only where nothing came back, which is what makes the question
// answerable on a plane whose element addresses collapse onto the container's
// own name (ADR-0003). An address arrives at Get carrying no kind and no arity,
// so being asked for children is the only signal a driver gets that core
// considers the address a dynamic container.
//
// Over a source that cannot list the order is the other one, and the reason for
// it is unchanged: a Null at the container address is a complete answer and a
// source that cannot list can still give it; only after Absent does the walk
// need the members, and only then is a source that cannot enumerate a refusal.
//
// Both orders agree wherever both run. Nothing under an Absent container address
// is nothing written, so a seed keeps what it had: a container with no children
// is indistinguishable from an absent one on every plane surveyed, and ADR-0006
// puts that row under "does not write". What the first order gives up is an
// answer at the container's own address where there are children under it, which
// is never read, and ADR-0003 states that cost rather than leaving it to be
// found.
// A member list that comes back empty is the whole answer, and the outcome
// beside it is what the container's own address said.
func (l loadFrom) members(ctx context.Context, s spot) (kids []Path, out outcome, err error) {
	lister, ok := l.r.(Enumerator)
	if !ok {
		return l.unlistable(ctx, s)
	}

	kids, err = lister.Children(ctx, s.at)
	if err != nil {
		return nil, outcome{}, fromDriver(momentWalk, s.at, err)
	}

	if len(kids) == 0 {
		// The container's own address is the whole answer where the plane holds
		// nothing under it, and it is asked second so that a driver whose element
		// values live under the container's own name is never asked for a value
		// there while it is holding some (ADR-0003).
		out, _, err = l.container(ctx, s)

		return nil, out, err
	}

	return kids, outcome{}, nil
}

// unlistable is a dynamic container over a source that cannot list: its own
// address is the only thing there is to ask, and anything short of a complete
// answer there is a refusal.
//
// The refusal names the field and the source rather than loading an empty
// composite, which is the most plausible-looking wrong answer available and the
// silent one ADR-0001 rules out (ADR-0004).
func (l loadFrom) unlistable(ctx context.Context, s spot) (kids []Path, out outcome, err error) {
	out, answered, err := l.container(ctx, s)
	if !answered {
		return nil, out, err
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
func (loadFrom) buildSlice(s spot, kids []Path, into descend) (outcome, error) {
	kids = slices.Clone(kids)
	slices.SortFunc(kids, Path.Compare)

	if err := contiguous(s, kids); err != nil {
		return outcome{}, err
	}

	fresh := reflect.MakeSlice(s.v.Type(), len(kids), len(kids))

	var b batch
	for i, kid := range kids {
		b.add(into(fresh.Index(i), kid))
	}

	out, err := b.done()
	if err != nil {
		return out, err
	}

	s.v.Set(fresh)
	out.wrote = true

	return out, nil
}

// contiguous refuses an enumerated position the sequence has no place for.
//
// A slice's length is whatever the plane holds, so the positions have to be 0 to
// n-1 with none missing. A gap would leave ferry choosing between a short
// sequence and an absent element with nothing to choose on, and a position past
// the count is a length ferry would be allocating for on a plane's say-so. It is
// the array rule read at a type whose length the type does not fix.
func contiguous(s spot, kids []Path) error {
	var at uint

	for _, kid := range kids {
		if want := s.at.Elem(at); kid != want {
			return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
				"the plane holds %s under a sequence of %d, and a %s is addressed from %s upwards with no "+
					"position missing: fill the gap, or model a sequence whose positions are chosen "+
					"by the plane as a map keyed by those positions",
				kid, len(kids), s.v.Type(), s.at.Elem(0)))
		}

		at++
	}

	return nil
}

// buildMap fills a mapping out of what the plane enumerated, fresh for the
// reason [loadFrom.buildSlice] gives: a shallow copy of the seed shares a map's
// buckets outright, so writing into a seeded map mutates the caller's map.
func (loadFrom) buildMap(s spot, kids []Path, into descend) (outcome, error) {
	fresh := reflect.MakeMapWithSize(s.v.Type(), len(kids))

	var b batch
	for _, kid := range kids {
		b.add(atKey(s, fresh, kid, into))
	}

	out, err := b.done()
	if err != nil {
		return out, err
	}

	// Injectivity has a Load side, and it is the mirror of the Dump-side one:
	// there, two Go keys rendering to one address lose an entry as it is
	// written; here, two plane keys parsing to one Go key lose one as it is
	// read. /m/1 and /m/01 are two addresses and one int.
	if fresh.Len() != len(kids) {
		return outcome{}, newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %d addresses under this mapping and %s takes %d of them: two plane keys read "+
				"back as one Go key, so an entry would be lost", len(kids), s.v.Type(), fresh.Len()))
	}

	s.v.Set(fresh)
	out.wrote = true

	return out, nil
}

// atKey reads one key out of an enumerated address and walks the value under it.
//
// The address is checked against the one this key would have minted rather than
// only for its segment kind, which is one comparison for two obligations: a
// position under a mapping is a plane saying the container is a sequence, and an
// address that is not an immediate child is a driver answering a question it was
// not asked.
func atKey(s spot, fresh reflect.Value, kid Path, into descend) (outcome, error) {
	text := lastSegment(kid).Text()
	if s.at.At(text) != kid {
		return outcome{}, newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %s under this mapping, and a %s takes one member per name immediately under it",
			kid, s.v.Type()))
	}

	key := reflect.New(s.v.Type().Key()).Elem()
	if err := s.n.key.parse(key, text); err != nil {
		return outcome{}, newError(momentWalk, ErrValue, kid, err.Error()).withCause(err)
	}

	val := reflect.New(s.v.Type().Elem()).Elem()

	out, err := into(val, kid)
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
// nothing else. So is a codec that produced a kind other than the one it
// declared, which is the one check core can make about a codec it did not
// write (ADR-0009).
func (d dumpTo) atLeaf(ctx context.Context, s spot) (outcome, error) {
	v, err := s.n.codec.emit(s.v)
	if err != nil {
		return outcome{}, newError(momentWalk, ErrValue, s.at, err.Error()).withCause(err)
	}

	return d.write(ctx, s.at, v)
}

// write hands the plane one Value at one address, and reports a driver's refusal
// as the driver's.
//
// A dump with no plane in sight stages the write in its outcome instead, and
// the two are one function because they are one decision made once: what the
// walk does with an encoded value is where it goes, not what it is (ADR-0011).
func (d dumpTo) write(ctx context.Context, at Path, v Value) (outcome, error) {
	if d.w == nil {
		return outcome{writes: []stagedWrite{{at: at, v: v}}}, nil
	}

	if err := d.w.Set(ctx, at, v); err != nil {
		return outcome{}, fromDriver(momentWalk, at, err)
	}

	return outcome{}, nil
}

// atNullable writes a Null where the pointer is nil and descends where it is
// not, which are the two states a pointer has and the two observations a
// container address carries.
//
// It never writes anything at the container address when the pointer is set,
// because the answer is then under it: a container address is never realised at
// the same time as anything beneath it (ADR-0003).
func (d dumpTo) atNullable(ctx context.Context, s spot, into descend) (outcome, error) {
	if !s.v.IsNil() {
		return into(s.v.Elem(), s.at)
	}

	return d.write(ctx, s.at, Null())
}

// atArray asks nothing. An index outside the array is something a plane can
// hold and a value cannot, so it is a question with a Load side only.
func (dumpTo) atArray(context.Context, spot) error { return nil }

// atSlice writes one address per element, and a Null at the sequence's own
// address where it has none.
//
// Nil and empty are one value there, and the collision is forced rather than
// chosen: three Go states meet two observations at a container address, and
// measured through a real YAML plane a missing key, an empty list and an empty
// mapping are one observation. The draft that chose the other collision - Null
// for nil and nothing for empty - made a map key whose value minted nothing
// vanish entirely, which is a silently dropped entry (ADR-0005).
func (d dumpTo) atSlice(ctx context.Context, s spot, into descend) (outcome, error) {
	if s.v.Len() == 0 {
		return d.write(ctx, s.at, Null())
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

	return r.b.done()
}

// atMap writes one address per key, in the order of the key text, and a Null at
// the mapping's own address where it has no entries.
func (d dumpTo) atMap(ctx context.Context, s spot, into descend) (outcome, error) {
	if s.v.Len() == 0 {
		return d.write(ctx, s.at, Null())
	}

	keys, err := sortedKeys(s)
	if err != nil {
		return outcome{}, err
	}

	r := d.realising(s, len(keys))

	for _, k := range keys {
		r.member(s.v.MapIndex(k.key), s.at.At(k.text), into)
	}

	return r.b.done()
}

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
	addrs *AddressSet
	b     batch
}

// realising starts one, with room for the members the container is about to
// list.
func (d dumpTo) realising(s spot, n int) *realising {
	return &realising{s: s, addrs: d.addrs, b: batch{out: outcome{minted: make(map[Path]spot, n)}}}
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
	if taken || r.addrs.Has(at) {
		return collided(at, r.s)
	}

	r.b.out.minted[at] = r.s

	return nil
}
