package addresskinds

import "fmt"

// The observation contract, re-signed by kind. Prober and Enumerator
// never overlap: the address types partition the schema, so each
// question has exactly one addressee. A driver implements the set its
// plane supports and is never asked outside it.
type Reader interface {
	Get(addr LeafAddr) (Value, error)
}

type Prober interface {
	Probe(addr SectionAddr) (Presence, error)
}

type Enumerator interface {
	Children(addr CompositeAddr) ([]Segment, error)
}

// AddressSet is what Bind hands a driver: the schema's members,
// typed. Classification happens once, before any I/O — this is the
// stronger shape that retires the Container-bit idea.
type AddressSet struct {
	leaves     map[string]LeafAddr
	sections   map[string]SectionAddr
	composites map[string]CompositeAddr
}

func NewAddressSet() *AddressSet {
	return &AddressSet{
		leaves:     map[string]LeafAddr{},
		sections:   map[string]SectionAddr{},
		composites: map[string]CompositeAddr{},
	}
}

func (s *AddressSet) AddLeaf(p string) LeafAddr {
	a := LeafAddr{p: path{p: p}}
	s.leaves[p] = a
	return a
}

func (s *AddressSet) AddSection(p string) SectionAddr {
	a := SectionAddr{p: path{p: p}}
	s.sections[p] = a
	return a
}

func (s *AddressSet) AddComposite(p string) CompositeAddr {
	a := CompositeAddr{p: path{p: p}}
	s.composites[p] = a
	return a
}

func (s *AddressSet) Leaves() []LeafAddr {
	out := make([]LeafAddr, 0, len(s.leaves))
	for _, a := range s.leaves {
		out = append(out, a)
	}
	return out
}

func (s *AddressSet) Sections() []SectionAddr {
	out := make([]SectionAddr, 0, len(s.sections))
	for _, a := range s.sections {
		out = append(out, a)
	}
	return out
}

func (s *AddressSet) Composites() []CompositeAddr {
	out := make([]CompositeAddr, 0, len(s.composites))
	for _, a := range s.composites {
		out = append(out, a)
	}
	return out
}

// The write side, minimal: enough to demonstrate A2's hole and the
// section-touch option. TouchSection is switchable so the behaviour
// table can show the plane with and without P1.
type recorderSink struct {
	touch  bool
	leaves map[string]string
	marks  map[string]bool // touched sections
}

func newRecorderSink(touch bool) *recorderSink {
	return &recorderSink{touch: touch, leaves: map[string]string{}, marks: map[string]bool{}}
}

func (r *recorderSink) SetLeaf(addr LeafAddr, text string) { r.leaves[addr.String()] = text }

func (r *recorderSink) TouchSection(addr SectionAddr) error {
	if !r.touch {
		return fmt.Errorf("plane cannot spell an empty section at %s", addr)
	}
	r.marks[addr.String()] = true
	return nil
}

// probeAfterDump answers what a reload would observe for a section.
func (r *recorderSink) probeAfterDump(addr SectionAddr) Presence {
	if r.marks[addr.String()] {
		return Present
	}
	for p := range r.leaves {
		if underPrefix(p, addr.String()) {
			return Present
		}
	}
	return Absent
}
