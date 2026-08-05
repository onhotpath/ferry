package addresskinds

// Round-two extensions from the session-03 review: element-form
// validation (the []string vs map question), posture-B references
// (core models the redirect), and the AddressSet iterator alternative.

import "fmt"

// ── element form: how core differentiates composites ────────────────

// ElementForm is the schema's answer to "[]string or map?": a slice
// mints index children, a map mints names. The DRIVER never needs the
// distinction — it reports what the plane holds — and core validates
// the minted segments against the form, per G4's structural refusal.
type ElementForm uint8

const (
	IndexElements ElementForm = iota // slices: children are positions
	NameElements                     // maps: children are names
)

// ValidateSegments is core's check on an enumeration result. A driver
// inventing a name under a slice is refused with the segment named —
// the differentiation lives here, not in the address type.
func ValidateSegments(form ElementForm, addr CompositeAddr, segs []Segment) error {
	for _, s := range segs {
		switch form {
		case IndexElements:
			if !s.isIdx && !allDigits(s.name) {
				return fmt.Errorf("%s holds index elements and the driver minted name %q", addr, s)
			}
		case NameElements:
			if s.isIdx {
				return fmt.Errorf("%s holds named elements and the driver minted index %s", addr, s)
			}
		}
	}
	return nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ── posture B: core models the reference ────────────────────────────

// SectionState is what a posture-B Probe answers: a presence, or a
// redirect to another section the schema can address. The zero value
// is Absent, in keeping with Value.
type SectionState struct {
	presence Presence
	ref      *SectionAddr
}

func StatePresent() SectionState          { return SectionState{presence: Present} }
func StateAbsent() SectionState           { return SectionState{} }
func StateNull() SectionState             { return SectionState{presence: Null} }
func StateRef(t SectionAddr) SectionState { return SectionState{ref: &t} }

// ProberB is the posture-B contract: the driver reports, core resolves.
type ProberB interface {
	ProbeB(addr SectionAddr) (SectionState, error)
}

// ResolveSection is core's resolution loop: cycle detection and hop
// limits live HERE, once, instead of in every driver. This is the
// mechanism posture B buys.
func ResolveSection(pr ProberB, addr SectionAddr) (Presence, error) {
	seen := map[string]bool{addr.String(): true}
	for {
		st, err := pr.ProbeB(addr)
		if err != nil {
			return Absent, err
		}
		if st.ref == nil {
			return st.presence, nil
		}
		if seen[st.ref.String()] {
			return Absent, fmt.Errorf("reference cycle through %s", st.ref)
		}
		seen[st.ref.String()] = true
		addr = *st.ref
	}
}

// treeDriverB is the tree plane under posture B: it reports redirects
// instead of resolving them — but only for targets the schema can
// address. An anchor pointing at an unmapped plane location has no
// SectionAddr to report, which is posture B's honest boundary: the
// driver still resolves those internally or refuses.
type treeDriverB struct {
	inner *treeDriver
	set   *AddressSet
}

func (d *treeDriverB) ProbeB(addr SectionAddr) (SectionState, error) {
	node, ok := d.inner.lookup(addr.String())
	if !ok {
		return StateAbsent(), nil
	}
	if r, isRef := node.(ref); isRef {
		target, bound := d.set.sections[r.target]
		if !bound {
			return StateAbsent(), fmt.Errorf(
				"%s refers to %s, which the schema does not address; the driver must resolve or refuse", addr, r.target)
		}
		return StateRef(target), nil
	}
	switch node.(type) {
	case nil:
		return StateNull(), nil
	case map[string]any:
		return StatePresent(), nil
	}
	return StateAbsent(), fmt.Errorf("%s holds a value, the schema wants a section", addr)
}

// ── the AddressSet alternative: one iterator, sealed sum ────────────

// Member is the sealed sum over the three address kinds — the O2
// surface: one method on AddressSet instead of three.
type Member interface{ member() }

func (LeafAddr) member()      {}
func (SectionAddr) member()   {}
func (CompositeAddr) member() {}

// All enumerates every member; the caller type-switches once, at
// Bind, on the cold path. (In ferry this is an iter.Seq[Member].)
func (s *AddressSet) All() []Member {
	out := make([]Member, 0, len(s.leaves)+len(s.sections)+len(s.composites))
	for _, a := range s.leaves {
		out = append(out, a)
	}
	for _, a := range s.sections {
		out = append(out, a)
	}
	for _, a := range s.composites {
		out = append(out, a)
	}
	return out
}

// bindEnvViaAll proves O2 carries the same information through one
// method: the same key table bindEnv builds from three.
func bindEnvViaAll(environ map[string]string, set *AddressSet) *envDriver {
	d := &envDriver{environ: environ, keys: map[string]string{}, prefix: map[string]string{}}
	for _, m := range set.All() {
		switch a := m.(type) {
		case LeafAddr:
			d.keys[a.String()] = envKey(a.String())
		case SectionAddr:
			d.prefix[a.String()] = envKey(a.String()) + "_"
		case CompositeAddr:
			d.prefix[a.String()] = envKey(a.String()) + "_"
		}
	}
	return d
}
