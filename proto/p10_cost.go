package main

// P10b: the naive Segments() allocates a slice per call. jsontext.Pointer
// exposes Tokens() iter.Seq[string] instead. And the address set is known
// before any I/O, so a driver's key function is computable once per schema.
// Both matter, because a 483 ns/key key function would dominate the 476 ns
// twelve-key cached load the research measured.

import (
	"iter"
	"strings"
)

// SegmentsSeq is the allocation-free form, modelled on Pointer.Tokens.
func (p Path) SegmentsSeq() iter.Seq[Segment] {
	return func(yield func(Segment) bool) {
		rest := p.canon
		for len(rest) > 0 {
			k := Name
			if rest[0] == sigilIndex {
				k = Index
			}
			rest = rest[1:]
			j := strings.IndexAny(rest, string([]byte{sigilName, sigilIndex}))
			var raw string
			if j < 0 {
				raw, rest = rest, ""
			} else {
				raw, rest = rest[:j], rest[j:]
			}
			if k == Name && strings.IndexByte(raw, escape) >= 0 {
				raw = nameUnesc.Replace(raw)
			}
			if !yield(Segment{Kind: k, Text: raw}) {
				return
			}
		}
	}
}

func envKeySeq(p Path) string {
	var b strings.Builder
	b.Grow(len(p.canon))
	first := true
	for s := range p.SegmentsSeq() {
		if !first {
			b.WriteByte('_')
		}
		first = false
		for i := range len(s.Text) {
			c := s.Text[i]
			if c >= 'a' && c <= 'z' {
				c -= 32
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

// planeKeys is what a compiled schema would hold: the driver's key function
// applied once to the whole address set, before any I/O.
type planeKeys struct {
	byAddr map[Path]string
}

func newPlaneKeys(addrs []Path, f keyFunc) *planeKeys {
	m := make(map[Path]string, len(addrs))
	for _, a := range addrs {
		m[a] = f(a)
	}
	return &planeKeys{byAddr: m}
}

func (pk *planeKeys) key(a Path) string { return pk.byAddr[a] }
