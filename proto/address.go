package main

// The candidate address type. A structured path with a canonical, comparable
// text form: jsontext.Pointer's uniqueness property, copied rather than
// imported (ADR-0002 bars the import from core), plus a segment kind so a
// structured plane can tell a list from a map - the one thing Pointer's own
// godoc says it cannot do.

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

type Kind uint8

const (
	Name  Kind = iota // an object member / struct field / map key
	Index             // a position in a sequence
)

type Segment struct {
	Kind Kind
	Text string // decimal, no leading zeros, when Kind == Index
}

// Path is comparable, so it is usable as a map key and as a set element with
// no encoding step at the call site. That is the whole reason the canonical
// form is a string and not a []string.
type Path struct{ canon string }

const (
	sigilName  = '/'
	sigilIndex = '#'
	escape     = '~'
)

var nameEsc = strings.NewReplacer(
	"~", "~0",
	"/", "~1",
	"#", "~2",
)

var nameUnesc = strings.NewReplacer(
	"~2", "#",
	"~1", "/",
	"~0", "~",
)

func (p Path) Name(text string) Path {
	return Path{p.canon + string(sigilName) + nameEsc.Replace(text)}
}

func (p Path) Index(i int) Path {
	return Path{p.canon + string(sigilIndex) + strconv.Itoa(i)}
}

func (p Path) String() string { return p.canon }
func (p Path) IsRoot() bool   { return p.canon == "" }

func (p Path) Segments() []Segment {
	var out []Segment
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
		if k == Name {
			raw = nameUnesc.Replace(raw)
		}
		out = append(out, Segment{Kind: k, Text: raw})
	}
	return out
}

func (p Path) Parent() Path {
	j := strings.LastIndexAny(p.canon, string([]byte{sigilName, sigilIndex}))
	if j < 0 {
		return Path{}
	}
	return Path{p.canon[:j]}
}

// CompareSegmentwise is the ordering ferry would actually publish. It is NOT
// the same as comparing the canonical strings - see P5.
func CompareSegmentwise(a, b Path) int {
	as, bs := a.Segments(), b.Segments()
	for i := 0; i < min(len(as), len(bs)); i++ {
		if c := cmp.Compare(as[i].Kind, bs[i].Kind); c != 0 {
			return c
		}
		if as[i].Kind == Index {
			ai, _ := strconv.Atoi(as[i].Text)
			bi, _ := strconv.Atoi(bs[i].Text)
			if c := cmp.Compare(ai, bi); c != 0 {
				return c
			}
			continue
		}
		if c := cmp.Compare(as[i].Text, bs[i].Text); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(as), len(bs))
}

func path(segs ...string) Path {
	var p Path
	for _, s := range segs {
		p = p.Name(s)
	}
	return p
}

func sortedPaths(ps []Path) []Path {
	out := slices.Clone(ps)
	slices.SortFunc(out, CompareSegmentwise)
	return out
}
