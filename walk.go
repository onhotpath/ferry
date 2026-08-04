package ferry

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
)

// sched runs a batch of sibling tasks and reports what they collectively failed
// at. It is the seam a concurrent mode goes behind, and it is why the walk is
// written once: a concurrent mode is a second scheduler and never a second walk
// (ADR-0010).
//
// The batch is per container rather than per leaf, which is what prices the
// seam honestly: one indirect call for a struct's whole field list, measured at
// 1141 ns against 1079 ns for the identical loop inlined.
//
// It is unexported, core ships exactly one implementation, and no exported name
// takes or returns one. That is deliberate: whether ferry ever walks
// concurrently is an open question, and an importer able to select a scheduler
// today would answer it by accident. The hazard that comes with the seam is
// stated in ADR-0010 rather than fixed here - ADR-0006's presence bit is shared
// mutable state across it, reproduced under -race - because a seam that looks
// like a drop-in and is not is worse than no seam.
type sched func(tasks []func() error) error

// serial is core's only scheduler: every task, in order, and every failure.
//
// Aggregation lives here rather than in the walk, which is ADR-0011's rule and
// ADR-0010's measurement of it: the same walk function under a first-error
// scheduler and under this one reports one error and two over a plane with two
// bad leaves, byte-identical in between. So the walk never decides whether to
// continue, and the error model costs it nothing.
func serial(tasks []func() error) error {
	errs := make([]error, 0, len(tasks))
	for _, task := range tasks {
		errs = append(errs, task())
	}

	return join(errs...)
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
	atLeaf(ctx context.Context, s spot) error

	// atNullable is the container question, at a composite that can be nil.
	// Dump answers it from the value and Load from the plane, and into is the
	// walk of what is beneath, which either direction may decline to run.
	atNullable(ctx context.Context, s spot, into descend) error

	// atArray is what an array's own address is asked before its elements are
	// walked, which is nothing on Dump and one question on Load: an array's
	// membership is the type's, so a plane holding an index outside it is
	// holding something this type cannot take.
	atArray(ctx context.Context, s spot) error

	// atSlice and atMap are the whole of the dynamic tier. Each mints its
	// members' addresses - from the value on Dump, from the plane on Load - and
	// walks each member through into at the address it minted for it.
	atSlice(ctx context.Context, s spot, into descend) error
	atMap(ctx context.Context, s spot, into descend) error
}

// descend walks what is under a container, over the value the direction decided
// to walk it over and at the address the direction decided it occupies. It is a
// parameter rather than a return, because a direction that has to decide
// something after the subtree ran - whether a pointer is materialised at all -
// cannot express that by returning.
type descend func(v reflect.Value, at Path) error

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
func (w *walker) walk(ctx context.Context, s spot) error {
	if err := ctx.Err(); err != nil {
		return newError(momentWalk, nil, s.at, ctxEndedMsg).withCause(err)
	}

	switch s.n.kind {
	case nodeLeaf:
		return w.dir.atLeaf(ctx, s)
	case nodePointer:
		return w.dir.atNullable(ctx, s, w.into(ctx, s))
	case nodeArray:
		if err := w.dir.atArray(ctx, s); err != nil {
			return err
		}

		return w.run(w.tasks(ctx, s))
	case nodeSlice:
		return w.dir.atSlice(ctx, s, w.element(ctx, s))
	case nodeMap:
		return w.dir.atMap(ctx, s, w.element(ctx, s))
	default:
		return w.run(w.tasks(ctx, s))
	}
}

// into is what a pointer's subtree is walked with: the same node's member list,
// over the value the direction materialised, at the address the pointer already
// occupies, because a pointer mints no segment of its own.
func (w *walker) into(ctx context.Context, s spot) descend {
	return func(v reflect.Value, at Path) error {
		return w.run(w.tasks(ctx, spot{n: s.n, v: v, at: at}))
	}
}

// element is what one member of a dynamic composite is walked with: the element
// shape, compiled once, at the address the direction minted for this member.
func (w *walker) element(ctx context.Context, s spot) descend {
	return func(v reflect.Value, at Path) error {
		return w.walk(ctx, spot{n: s.n.fields[elemShape], v: v, at: at})
	}
}

// elemShape is where a dynamic composite keeps its one member: the element is
// compiled once and realised per member, so the field list holds exactly one
// node and it is that shape.
const elemShape = 0

// tasks is a container's member list as a batch for the scheduler. It is built
// whole rather than run as it is built, because a batch is what the seam takes.
func (w *walker) tasks(ctx context.Context, s spot) []func() error {
	tasks := make([]func() error, 0, len(s.n.fields))

	for i, f := range s.n.fields {
		member := spot{n: f, v: memberOf(s.n, s.v, i, f), at: realised(s, f)}
		tasks = append(tasks, func() error { return w.walk(ctx, member) })
	}

	return tasks
}

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
type loadFrom struct {
	r Reader

	// wrote counts the writes the walk has made, and it is how "materialised
	// exactly where something under it was present" is decided at a pointer
	// (ADR-0006): the count before the subtree ran against the count after.
	//
	// It is shared mutable state behind the scheduler seam, which is the hazard
	// ADR-0010 records rather than one this ticket introduces. A concurrent
	// scheduler needs it per subtree; the serial one core ships needs nothing.
	wrote *int
}

var _ direction = loadFrom{}

// atLeaf reads one address and decides what the observation means to the field.
func (l loadFrom) atLeaf(ctx context.Context, s spot) error {
	got, err := l.r.Get(ctx, s.at)
	if err != nil {
		// A non-nil error reaches the caller as an error and is never
		// substituted with Absent (ADR-0004). Reading it as absence is how a
		// total backend outage loads as an all-zero struct with a nil error,
		// which a prototype committed and nothing saw for four rounds.
		return fromDriver(momentWalk, s.at, err)
	}

	// ADR-0006: Absent means ferry does not write to the field, so a seeded
	// value keeps what it had and a fresh one keeps its zero.
	if got.Kind() == KindAbsent {
		return nil
	}

	// Which kinds this leaf takes and how it reads their text is the leaf's,
	// resolved at compile and held on the node, so the walk decides nothing
	// about a type here (ADR-0005).
	if err := s.n.codec.decode(s.v, got); err != nil {
		return newError(momentWalk, ErrValue, s.at, err.Error()).withCause(err)
	}

	*l.wrote++

	return nil
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
func (l loadFrom) atNullable(ctx context.Context, s spot, into descend) error {
	if more, err := l.container(ctx, s); !more {
		return err
	}

	return l.materialise(s, into)
}

// container answers a container address, and reports whether the walk should go
// on to what is under it.
//
// The three observations are three answers, and they are the same three at a
// pointer and at a dynamic composite. A Null is the plane saying the container
// is there and holds nothing, which is the zero value - nil for a pointer, and
// nil for a slice or a map, where nil and empty are one value anyway. Anything
// else is a value at a container address, which carries absence or a null and
// never anything else (ADR-0003). Absent is the plane saying nothing about the
// container itself, so what is under it decides.
func (l loadFrom) container(ctx context.Context, s spot) (more bool, err error) {
	got, err := l.r.Get(ctx, s.at)
	if err != nil {
		return false, fromDriver(momentWalk, s.at, err)
	}

	if got.Kind() == KindNull {
		s.v.SetZero()
		*l.wrote++

		return false, nil
	}

	if got.Kind() != KindAbsent {
		return false, newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %s at a container address, which holds absence or a null and nothing else",
			got.Kind()))
	}

	return true, nil
}

// materialise builds the pointee fresh, walks into it, and publishes it only
// where the walk wrote.
//
// The fresh allocation is not a tidiness. LoadOver's failure property rests on
// a shallow copy of the seed, so a walk that wrote through the seed's own
// pointer would publish a partial load into a value the caller still holds and
// the property would break in silence.
func (l loadFrom) materialise(s spot, into descend) error {
	fresh := reflect.New(s.v.Type().Elem())
	if !s.v.IsNil() {
		fresh.Elem().Set(s.v.Elem())
	}

	before := *l.wrote
	if err := into(fresh.Elem(), s.at); err != nil {
		return err
	}

	if *l.wrote > before {
		s.v.Set(fresh)
	}

	return nil
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
func (l loadFrom) atSlice(ctx context.Context, s spot, into descend) error {
	kids, more, err := l.members(ctx, s)
	if !more {
		return err
	}

	return l.buildSlice(s, kids, into)
}

// atMap builds the mapping the plane holds under this address.
func (l loadFrom) atMap(ctx context.Context, s spot, into descend) error {
	kids, more, err := l.members(ctx, s)
	if !more {
		return err
	}

	return l.buildMap(s, kids, into)
}

// members is a dynamic container's own address answered first, and then what
// the plane holds under it.
//
// The order is the rule rather than a convenience. A Null at the container
// address is a complete answer and a source that cannot list can still give it;
// only after Absent does the walk need the members, and only then is a source
// that cannot enumerate a refusal. Nothing under an Absent container address is
// nothing written, so a seed keeps what it had: a container with no children is
// indistinguishable from an absent one on every plane surveyed, and ADR-0006
// puts that row under "does not write".
func (l loadFrom) members(ctx context.Context, s spot) (kids []Path, more bool, err error) {
	answered, err := l.container(ctx, s)
	if !answered {
		return nil, false, err
	}

	lister, ok := l.r.(Enumerator)
	if !ok {
		return nil, false, newError(momentWalk, ErrPlane, s.at, fmt.Sprintf(
			"the addresses under a %s come from the value, and %T cannot list what a plane holds under an "+
				"address: a source that does not implement ferry.Enumerator reaches every static address and "+
				"no dynamic one, which is a property of that plane rather than of this schema", s.v.Type(), l.r))
	}

	kids, err = lister.Children(ctx, s.at)
	if err != nil {
		return nil, false, fromDriver(momentWalk, s.at, err)
	}

	return kids, len(kids) > 0, nil
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
func (l loadFrom) buildSlice(s spot, kids []Path, into descend) error {
	kids = slices.Clone(kids)
	slices.SortFunc(kids, Path.Compare)

	if err := contiguous(s, kids); err != nil {
		return err
	}

	fresh := reflect.MakeSlice(s.v.Type(), len(kids), len(kids))
	errs := make([]error, 0, len(kids))

	for i, kid := range kids {
		errs = append(errs, into(fresh.Index(i), kid))
	}

	if err := join(errs...); err != nil {
		return err
	}

	s.v.Set(fresh)
	*l.wrote++

	return nil
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
					"position missing", kid, len(kids), s.v.Type(), s.at.Elem(0)))
		}

		at++
	}

	return nil
}

// buildMap fills a mapping out of what the plane enumerated, fresh for the
// reason [loadFrom.buildSlice] gives: a shallow copy of the seed shares a map's
// buckets outright, so writing into a seeded map mutates the caller's map.
func (l loadFrom) buildMap(s spot, kids []Path, into descend) error {
	fresh := reflect.MakeMapWithSize(s.v.Type(), len(kids))
	errs := make([]error, 0, len(kids))

	for _, kid := range kids {
		errs = append(errs, atKey(s, fresh, kid, into))
	}

	if err := join(errs...); err != nil {
		return err
	}

	// Injectivity has a Load side, and it is the mirror of the Dump-side one:
	// there, two Go keys rendering to one address lose an entry as it is
	// written; here, two plane keys parsing to one Go key lose one as it is
	// read. /m/1 and /m/01 are two addresses and one int.
	if fresh.Len() != len(kids) {
		return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %d addresses under this mapping and %s takes %d of them: two plane keys read "+
				"back as one Go key, so an entry would be lost", len(kids), s.v.Type(), fresh.Len()))
	}

	s.v.Set(fresh)
	*l.wrote++

	return nil
}

// atKey reads one key out of an enumerated address and walks the value under it.
//
// The address is checked against the one this key would have minted rather than
// only for its segment kind, which is one comparison for two obligations: a
// position under a mapping is a plane saying the container is a sequence, and an
// address that is not an immediate child is a driver answering a question it was
// not asked.
func atKey(s spot, fresh reflect.Value, kid Path, into descend) error {
	text := lastSegment(kid).Text()
	if s.at.At(text) != kid {
		return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"the plane holds %s under this mapping, and a %s takes one member per name immediately under it",
			kid, s.v.Type()))
	}

	key := reflect.New(s.v.Type().Key()).Elem()
	if err := s.n.key.parse(key, text); err != nil {
		return newError(momentWalk, ErrValue, kid, err.Error()).withCause(err)
	}

	val := reflect.New(s.v.Type().Elem()).Elem()
	if err := into(val, kid); err != nil {
		return err
	}

	fresh.SetMapIndex(key, val)

	return nil
}

// dumpTo is the Dump direction: hand the plane one Value per address.
type dumpTo struct {
	w Writer

	// addrs is the address set the type determined and minted is what this walk
	// has addressed from a value so far. Together they are ADR-0003's dynamic
	// tier: an address minted from a map key or a sequence index is an insert
	// into the set the static pass already built, checked as it is minted and
	// before the write it belongs to, so it stays a map insert per address.
	//
	// minted is shared mutable state behind the scheduler seam, which is the
	// hazard ADR-0010 records rather than one this ticket introduces.
	addrs  *AddressSet
	minted map[Path]struct{}
}

var _ direction = dumpTo{}

// atLeaf writes one address. It never writes an Absent, which is a Reader-side
// kind: an omitted address is one that gets no Set call at all rather than one
// that gets a Set of nothing (ADR-0006).
//
// A value the leaf's representation does not cover is reported rather than
// swallowed, which in core's set is a time.Time outside years 0 to 9999 and
// nothing else. So is a codec that produced a kind other than the one it
// declared, which is the one check core can make about a codec it did not
// write (ADR-0009).
func (d dumpTo) atLeaf(ctx context.Context, s spot) error {
	out, err := s.n.codec.emit(s.v)
	if err != nil {
		return newError(momentWalk, ErrValue, s.at, err.Error()).withCause(err)
	}

	return d.write(ctx, s.at, out)
}

// write hands the plane one Value at one address, and reports a driver's refusal
// as the driver's.
func (d dumpTo) write(ctx context.Context, at Path, v Value) error {
	if err := d.w.Set(ctx, at, v); err != nil {
		return fromDriver(momentWalk, at, err)
	}

	return nil
}

// atNullable writes a Null where the pointer is nil and descends where it is
// not, which are the two states a pointer has and the two observations a
// container address carries.
//
// It never writes anything at the container address when the pointer is set,
// because the answer is then under it: a container address is never realised at
// the same time as anything beneath it (ADR-0003).
func (d dumpTo) atNullable(ctx context.Context, s spot, into descend) error {
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
func (d dumpTo) atSlice(ctx context.Context, s spot, into descend) error {
	if s.v.Len() == 0 {
		return d.write(ctx, s.at, Null())
	}

	errs := make([]error, 0, s.v.Len())

	// The position is counted in uint rather than converted from one, because
	// Path.Elem takes it unsigned: a negative position has no meaning and a
	// conversion is a place the constraint could be lost.
	var at uint

	for i := range s.v.Len() {
		errs = append(errs, d.member(s, s.v.Index(i), s.at.Elem(at), into))
		at++
	}

	return join(errs...)
}

// atMap writes one address per key, in the order of the key text, and a Null at
// the mapping's own address where it has no entries.
func (d dumpTo) atMap(ctx context.Context, s spot, into descend) error {
	if s.v.Len() == 0 {
		return d.write(ctx, s.at, Null())
	}

	keys := sortedKeys(s)
	errs := make([]error, 0, len(keys))

	for _, k := range keys {
		errs = append(errs, d.member(s, s.v.MapIndex(k.key), s.at.At(k.text), into))
	}

	return join(errs...)
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
func sortedKeys(s spot) []entry {
	keys := s.v.MapKeys()
	out := make([]entry, 0, len(keys))

	for _, k := range keys {
		out = append(out, entry{key: k, text: s.n.key.text(k)})
	}

	slices.SortFunc(out, func(a, b entry) int { return cmp.Compare(a.text, b.text) })

	return out
}

// member mints one address and walks the member it belongs to.
func (d dumpTo) member(s spot, v reflect.Value, at Path, into descend) error {
	if err := d.mint(s, at); err != nil {
		return err
	}

	return into(v, at)
}

// mint records an address a value determined, and refuses one that is already
// taken - before the write it belongs to, and never after it.
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
func (d dumpTo) mint(s spot, at Path) error {
	_, taken := d.minted[at]
	if taken || d.addrs.Has(at) {
		return newError(momentWalk, ErrValue, s.at, fmt.Sprintf(
			"%s is addressed more than once, and one of the two writes would be lost: the addresses under a "+
				"%s come from the value, and this one is an address already", at, s.v.Type()))
	}

	d.minted[at] = struct{}{}

	return nil
}
