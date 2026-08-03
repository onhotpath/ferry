package ferrytest

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/onhotpath/ferry"
)

// The memory plane, which is a map and says so.
//
// Neither half of it is exported. ADR-0014 keeps the surface a description a
// caller fills in, so what a user gets is [MemPlane] and [Static] rather than a
// type with fields, and every one of ADR-0003's five obligations is a property
// to rely on rather than something a caller could switch off.
var (
	_ ferry.Source     = memSource{}
	_ ferry.Sink       = memSink{}
	_ ferry.Reader     = memReader{}
	_ ferry.Enumerator = memReader{}
	_ ferry.Writer     = memWriter{}
)

// memStore is the contents, and the key is the canonical rendering of the
// address.
//
// Keying by the rendering is ADR-0003's first obligation, and the permission
// for it is specific rather than general: this plane has no serialization
// format, so the rendering is not competing with a plane key somebody else's
// parser has to read, and a plane with no format and no I/O is a map. Every
// driver that does have a format is barred from writing the rendering into a
// plane, because a separator is plane knowledge.
//
// It is a map[string]memEntry rather than a map[ferry.Path]ferry.Value even
// though ferry.Path is comparable and would work, because the obligation is
// about the key *function*: keying by the address type would make the identity
// of two addresses the type's business, and the whole of what this plane
// demonstrates is that the rendering already carries it.
type memStore struct {
	entries map[string]memEntry
}

// memEntry keeps the address beside the value so enumeration can hand back
// addresses rather than reconstructing them from text. ADR-0004 is explicit
// that Children returns addresses and not names, because an address carries its
// segment kind and text does not.
type memEntry struct {
	addr ferry.Path
	val  ferry.Value
}

func newMemStore() *memStore { return &memStore{entries: map[string]memEntry{}} }

// put writes unconditionally, and is what [Static] builds its contents with: a
// Go map literal cannot hold one key twice, so there is no duplicate for the
// loud refusal below to catch.
func (s *memStore) put(addr ferry.Path, v ferry.Value) {
	s.entries[addr.String()] = memEntry{addr: addr, val: v}
}

// get answers with the zero Value where the address is not held, which is
// absence without a second return value: KindAbsent is kind zero precisely so
// that a map lookup miss is the answer (ADR-0004).
func (s *memStore) get(addr ferry.Path) ferry.Value { return s.entries[addr.String()].val }

// set refuses a second write at one address, loudly, rather than overwriting.
//
// ADR-0003's third obligation, and its reason is ADR-0001's rule that nothing
// is ignored silently. Overwriting is the plausible-looking wrong answer here:
// two schema addresses that collide onto one plane key is exactly the data loss
// the address model exists to prevent, and a plane that quietly keeps the last
// writer reports success for a dump that lost a field.
//
// The refusal wraps [ferry.ErrPlane], which is the class for a driver refusing
// an address, and names the address with [ferry.ErrorAt] rather than putting it
// in the message text. ADR-0011 lets an address be named because it is
// structure; the value stored there is the plane's and is never printed.
func (s *memStore) set(addr ferry.Path, v ferry.Value) error {
	if _, ok := s.entries[addr.String()]; ok {
		return ferry.ErrorAt(addr, fmt.Errorf("%w: address already written", ferry.ErrPlane))
	}

	s.put(addr, v)

	return nil
}

// children answers with the immediate children of prefix, sorted segment-wise.
//
// ADR-0003's fourth obligation. The sort is not a nicety: Go map iteration is
// randomised, so without it a suite asserting on this plane's contents would be
// asserting on iteration order, and it would pass and fail for reasons that
// have nothing to do with the driver under test. Segment-wise rather than over
// the rendering, because sorting the rendering gives 0 1 10 11 2 for twelve
// indices, which ADR-0003 names as a subtle bug and a conformance case.
func (s *memStore) children(prefix ferry.Path) []ferry.Path {
	pre := slices.Collect(prefix.Segments())
	kids := map[string]ferry.Path{}

	for _, e := range s.entries {
		if c, ok := childOf(prefix, pre, e.addr); ok {
			kids[c.String()] = c
		}
	}

	out := slices.Collect(maps.Values(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	return out
}

// childOf reports the immediate child of prefix that addr lies under, and
// whether addr strictly extends prefix at all. pre is the prefix's own
// segments, passed in because the caller computes them once for the whole scan.
//
// An address is not a child of itself, so an entry equal to prefix reports
// false: the walk asks a container what is under it, and answering with the
// container would be an infinite descent.
func childOf(prefix ferry.Path, pre []ferry.Segment, addr ferry.Path) (ferry.Path, bool) {
	i := 0

	for seg := range addr.Segments() {
		if i == len(pre) {
			return extend(prefix, seg), true
		}

		if seg != pre[i] {
			return ferry.Path{}, false
		}

		i++
	}

	return ferry.Path{}, false
}

// extend appends one segment to an address, which is how an enumerated child is
// built back up out of a stored address without ever parsing a rendering.
func extend(p ferry.Path, s ferry.Segment) ferry.Path {
	if s.Kind() == ferry.Name {
		return p.At(s.Text())
	}

	return p.Elem(indexOf(s.Text()))
}

// base10 is the only base an Index segment is ever spelled in, which is what
// makes the rendering of an address unique.
const base10 = 10

// indexOf reads back the position an Index segment holds.
//
// It accumulates rather than calling strconv.ParseUint because there is no
// failure to report: an Index segment's text is canonical base-10 with no
// leading zero, minted by ferry.Path.Elem from a uint, so every digit is a
// digit and every value fits the type it came from.
func indexOf(text string) uint {
	var i uint

	for k := range len(text) {
		i = i*base10 + uint(text[k]-'0')
	}

	return i
}

// memSource is the read half. It carries no state of its own beyond the
// contents, because the memory plane's key function is the identity and there
// is no table to precompute.
type memSource struct{ store *memStore }

// Bind takes the address set and keeps nothing from it.
//
// That is not laziness, it is the whole of what this plane has to say about
// Bind. A flattening driver builds its key table here and checks that the table
// is injective over the set; the identity key function has nothing to build and
// nothing that could collide. Retaining the set would also break ADR-0012's
// rule that a key function keeps nothing across opens, which is asserted on the
// write side because that is where retention refuses a legal write.
//
// It returns no error, ever, and does no I/O - which makes it the trivial case
// of ADR-0004's rule that Bind must succeed against an unreachable plane.
func (s memSource) Bind(_ *ferry.AddressSet) (ferry.OpenFunc, error) {
	store := s.store

	return func(context.Context) (ferry.Reader, error) { return memReader{store: store}, nil }, nil
}

// memReader is an open read side. Nothing is snapshotted: a write through the
// sink is visible to a reader already open, which is what makes one round trip
// through one [Plane.Open] work with no ordering rule between the halves.
type memReader struct{ store *memStore }

// Get answers with what is held, or with the zero Value where nothing is.
//
// It never reports an error. A plane with no I/O has nothing to fail at, and
// this is the reason ADR-0002 says the memory plane keeps no conformance case
// honest: ADR-0014's case 4, that a non-nil error must reach the caller as an
// error and never as an Absent, has no way to fire here.
func (r memReader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	return r.store.get(addr), nil
}

// Children lists what is held immediately under prefix, segment-wise.
//
// The memory plane implements [ferry.Enumerator] because a plane that cannot
// list is a plane no map-typed field can be loaded from, and this is the plane
// a registrant proves their own codec against. Listing a map is free here and
// the ability says nothing about a driver: a Vault token with read and no list
// is the case that keeps the interface optional.
func (r memReader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	return r.store.children(prefix), nil
}

// memSink is the write half. It is a second type over the same contents,
// because one type cannot have two Bind methods - the cost ADR-0004 states for
// making a read-only plane a compile-time refusal.
type memSink struct{ store *memStore }

// Bind keeps nothing, for the reason [memSource.Bind] gives.
func (s memSink) Bind(_ *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	store := s.store

	return func(context.Context) (ferry.Writer, error) { return memWriter{store: store}, nil }, nil
}

// memWriter is an open write side.
//
// It implements neither [ferry.Committer] nor [ferry.Releaser], and that is
// ADR-0004's own table: a recorder in ferrytest stages nothing and holds
// nothing, so a Commit would be a lie and a Close would be the `return nil`
// boilerplate that is indistinguishable from a rollback somebody forgot.
type memWriter struct{ store *memStore }

// Set writes, or refuses a second write at one address.
func (w memWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	return w.store.set(addr, v)
}
