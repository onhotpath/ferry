// Package planetransfer is plane-to-plane transfer: one annotated struct, two
// planes, and no machinery beyond the two verbs ferry already ships.
//
// A struct that describes a plane describes every plane, so moving a
// configuration from a file into a key-value store, or out of a store into a
// file for a human to read, is a Load followed by a Dump. Read the example file
// for the whole of it; the plane below is scaffolding.
//
// Both planes in the example are the same miniature, deliberately. What the
// transfer costs does not depend on which two planes they are: substitute
// driver/yaml for one and driver/kv for the other and the three lines in the
// middle are unchanged.
//
// # What the trip through the struct costs
//
// The transfer is struct-mediated, so both directions run ferry's own rules and
// the destination holds what the type says rather than what the source said.
// Three consequences, and each is a rule stated elsewhere rather than anything
// this package invents:
//
//   - An address the type does not name is not copied. The struct is the whole
//     of what moves, and a key beside it stays where it is.
//   - A value the type refuses fails the transfer loudly rather than crossing.
//     A plane's null at an int field is the plain case.
//   - A composite with no members arrives as a null, because a nil slice and an
//     empty one are one value in Go's type set as ferry maps it.
//
// It is a teaching plane and not a driver. It keys by the address itself, so it
// has no key function, no spelling of its own and no way to collide two
// addresses onto one key. Use one of the modules under driver/ for anything
// real.
package planetransfer

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// Plane is a set of addresses and the values at them, readable and writable.
//
// Build one with New. Both halves of it - Source and Sink - are over the same
// contents, so a Dump into one is visible to a Load out of it.
type Plane struct {
	values map[ferry.Path]ferry.Value
}

// New returns a plane holding the values given, which is empty for a
// destination and populated for a source.
func New(values map[ferry.Path]ferry.Value) *Plane {
	p := &Plane{values: map[ferry.Path]ferry.Value{}}
	maps.Copy(p.values, values)

	return p
}

// Contents renders what the plane holds, one address per line, in the order
// ferry enumerates addresses: segment-wise, so a position sorts numerically.
func (p *Plane) Contents() string {
	addrs := slices.SortedFunc(maps.Keys(p.values), ferry.Path.Compare)

	var b strings.Builder

	for _, at := range addrs {
		fmt.Fprintf(&b, "%s = %#v\n", at, p.values[at])
	}

	return b.String()
}

// Source is the read half, and it is a separate value from the write half
// because ferry.Source and ferry.Sink are separate interfaces.
func (p *Plane) Source() ferry.Source { return source{p: p} }

// Sink is the write half, over the same contents.
func (p *Plane) Sink() ferry.Sink { return sink{p: p} }

type source struct{ p *Plane }

// Bind is handed the addresses the schema determined and needs none of them: a
// plane keyed by the address itself has no plane key to precompute and nothing
// to check.
func (s source) Bind(_ *ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return reader{p: s.p}, nil }, nil
}

type reader struct{ p *Plane }

// Get answers one leaf. The zero ferry.Value is absence, so an address the
// plane does not have needs no sentinel error.
func (r reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.p.values[addr.Path()], nil
}

// Probe says what the plane holds at a container's own address, which is the
// question a section is asked, since its members come from the type.
func (r reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	at := addr.Path()

	if v, held := r.p.values[at]; held && v.Kind() == ferry.KindNull {
		return ferry.SectionNull, nil
	}

	for stored := range r.p.values {
		if under(at, stored) {
			return ferry.SectionPresent, nil
		}
	}

	return ferry.SectionAbsent, nil
}

// Children lists what the plane holds immediately under a composite, which is
// how the addresses that come from the value rather than from the type are
// discovered.
//
// The stored addresses are ranged in address order, so a sequence's positions
// come back in position order: ten sorts after nine here and before it in the
// rendering, which is the whole difference between the two orders.
func (r reader) Children(_ context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	at := addr.Path()
	depth := len(segments(at))

	var out []ferry.Segment

	for _, stored := range slices.SortedFunc(maps.Keys(r.p.values), ferry.Path.Compare) {
		if !under(at, stored) {
			continue
		}

		seg := segments(stored)[depth]
		if !slices.Contains(out, seg) {
			out = append(out, seg)
		}
	}

	return out, nil
}

type sink struct{ p *Plane }

// Bind needs the address set no more than the read half does.
func (s sink) Bind(_ *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return writer{p: s.p}, nil }, nil
}

type writer struct{ p *Plane }

// Set writes one value. An address the dump is silent at gets no call at all,
// so there is nothing here to interpret.
func (w writer) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	w.p.values[addr.Path()] = v

	return nil
}

// Ensure writes what the value has to say at a container's own address, which
// is a null for a nil pointer and for a composite with no members.
//
// A present container needs no entry, because the members written under it are
// what say it is there.
func (w writer) Ensure(_ context.Context, addr ferry.Container, p ferry.Presence) error {
	if p == ferry.PresenceNull {
		w.p.values[addr.Path()] = ferry.Null
	}

	return nil
}

// Unset forgets everything the plane holds strictly under a composite, which is
// what makes a dump of that composite a replacement rather than a merge.
//
// A dump writes the members the new value has, and without this the members an
// earlier dump left under the same address would still be here afterwards and
// would load back as part of a value nobody wrote. It is called before the
// members are written, so forgetting the old ones cannot forget a new one.
func (w writer) Unset(_ context.Context, addr ferry.CompositeAddr) error {
	at := addr.Path()

	maps.DeleteFunc(w.p.values, func(stored ferry.Path, _ ferry.Value) bool {
		return under(at, stored)
	})

	return nil
}

// under reports whether one address lies strictly beneath another.
func under(at, stored ferry.Path) bool {
	outer, inner := segments(at), segments(stored)
	if len(inner) <= len(outer) {
		return false
	}

	return slices.Equal(inner[:len(outer)], outer)
}

// segments is one address as a slice, which is what comparing two of them by
// prefix needs.
func segments(p ferry.Path) []ferry.Segment {
	return slices.Collect(p.Segments())
}
