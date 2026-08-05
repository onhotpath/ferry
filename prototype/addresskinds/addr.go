// Package addresskinds prototypes session 03: the typed address split
// (S1), presence, typed address sets, enumeration-minted children,
// and arrays-as-sections — every board claim asserted by a test.
package addresskinds

import (
	"fmt"
	"strconv"
	"strings"
)

// Segment is one path element: a Name ("db", "http") or an Index (0, 1).
// It is what Children mints; the schema types the child it names.
type Segment struct {
	name  string
	index int
	isIdx bool
}

func Name(s string) Segment { return Segment{name: s} }
func Index(i int) Segment   { return Segment{index: i, isIdx: true} }

func (s Segment) String() string {
	if s.isIdx {
		return strconv.Itoa(s.index)
	}
	return s.name
}

// path is the shared, unexported representation all three address
// kinds wrap — the D2 sealing applied to addresses.
type path struct{ p string }

func (p path) String() string { return p.p }

func (p path) child(s Segment) path {
	return path{p: p.p + "/" + s.String()}
}

// The three sealed address kinds (S1). Only this package mints them,
// which in ferry means: only the compiler, from the schema.

// LeafAddr holds a value.
type LeafAddr struct{ p path }

// SectionAddr has static children, known at compile.
type SectionAddr struct{ p path }

// CompositeAddr has dynamic children, minted by the value.
type CompositeAddr struct{ p path }

func (a LeafAddr) String() string      { return a.p.String() }
func (a SectionAddr) String() string   { return a.p.String() }
func (a CompositeAddr) String() string { return a.p.String() }

// Minting: a composite's children are typed by the schema's element
// kind, so even minted addresses stay in the typed graph — the
// enumerator's result ties back, it does not float free.
func (a CompositeAddr) Leaf(s Segment) LeafAddr           { return LeafAddr{p: a.p.child(s)} }
func (a CompositeAddr) Section(s Segment) SectionAddr     { return SectionAddr{p: a.p.child(s)} }
func (a CompositeAddr) Composite(s Segment) CompositeAddr { return CompositeAddr{p: a.p.child(s)} }

// prefix reports whether p sits under prefix (both rendered forms).
func underPrefix(p, prefix string) bool {
	return strings.HasPrefix(p, prefix+"/")
}

// Presence is everything a section can be observed as.
type Presence uint8

const (
	Absent  Presence = iota // the address itself is missing
	Present                 // the section exists (possibly empty)
	Null                    // the address is present and explicitly null
)

func (p Presence) String() string {
	switch p {
	case Absent:
		return "absent"
	case Present:
		return "present"
	case Null:
		return "null"
	}
	return fmt.Sprintf("presence(%d)", uint8(p))
}

// Value is a deliberately tiny stand-in for ferry's Value; the real
// one is prototyped in proto/02b-value-seam.
type VKind uint8

const (
	KindAbsent VKind = iota
	KindNull
	KindString
)

type Value struct {
	Kind VKind
	Text string
}
