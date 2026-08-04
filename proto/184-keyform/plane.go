package keyform

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// values is what both planes in this package are: a map from a flat name to a
// sequence of values. url.Values and http.Header are both exactly this.
type values = map[string][]string

// plane is one driver's configuration: a name for diagnostics, the key
// function, how a child is cut out of a key, and how a minted segment's text is
// spelled back.
type plane struct {
	name  string
	keyf  ferry.KeyFunc
	cut   cutter
	mintf func(string) string
}

// cutter turns a plane key into the immediate child of a prefix that it lies
// under, which is the whole of what invertibility means here.
type cutter interface {
	// head is the text of the immediate child of prefixKey that key lies
	// under. atRoot says prefixKey is the empty path's, which is not a key.
	head(key, prefixKey string, atRoot bool) (string, bool)
}

// bracketCut counts bracket depth rather than cutting at the first ], so a
// segment holding a balanced pair - "a[b]" as a map key - survives enumeration.
// An unbalanced one does not, and that is the residue the strict form refuses.
type bracketCut struct{}

func (bracketCut) head(key, prefixKey string, atRoot bool) (string, bool) {
	if atRoot {
		h, _, _ := strings.Cut(key, "[")

		return h, h != ""
	}

	rest, ok := strings.CutPrefix(key, prefixKey+"[")
	if !ok {
		return "", false
	}

	depth := 1

	for i := range len(rest) {
		switch rest[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return rest[:i], i > 0
			}
		}
	}

	return "", false
}

type flatCut struct{ sep string }

func (f flatCut) head(key, prefixKey string, atRoot bool) (string, bool) {
	rest := key

	if !atRoot {
		var ok bool
		if rest, ok = strings.CutPrefix(key, prefixKey+f.sep); !ok {
			return "", false
		}
	}

	h, _, _ := strings.Cut(rest, f.sep)

	return h, h != ""
}

// bind is the whole of Bind for both directions: NewKeys, then the reverse
// table env calls staticNames.
func (p plane) bind(addrs *ferry.AddressSet) (*ferry.Keys, map[string]ferry.Path, error) {
	keys, err := ferry.NewKeys(addrs, p.name, p.keyf)
	if err != nil {
		return nil, nil, err
	}

	static := map[string]ferry.Path{}
	name := keys.Open()

	if addrs != nil {
		for addr := range addrs.All() {
			key, kerr := name(addr)
			if kerr != nil {
				return nil, nil, kerr
			}

			static[key] = addr
		}
	}

	return keys, static, nil
}

// reader is one open over one snapshot of a plane.
type reader struct {
	p      plane
	keys   ferry.KeyFunc
	static map[string]ferry.Path
	vals   values
}

var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
)

// Get answers with the first value the plane holds at this name. A repeated
// parameter's later values are unreachable through a ferry.KeyFunc, which
// returns a string and not a position; see README.md.
func (r *reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.keys(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	vs, ok := r.vals[key]
	if !ok || len(vs) == 0 {
		return ferry.Value{}, nil
	}

	return ferry.String(vs[0]), nil
}

// Children lists what the plane holds immediately under an address.
func (r *reader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	atRoot := prefix == ferry.Path{}

	var prefixKey string

	if !atRoot {
		var err error
		if prefixKey, err = r.keys(prefix); err != nil {
			return nil, err
		}
	}

	kids := map[ferry.Path]struct{}{}

	for key := range r.vals {
		if kid, ok := r.child(prefix, prefixKey, key, atRoot); ok {
			kids[kid] = struct{}{}
		}
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	return out, nil
}

func (r *reader) child(prefix ferry.Path, prefixKey, key string, atRoot bool) (ferry.Path, bool) {
	if addr, ok := r.static[key]; ok {
		return step(prefix, addr)
	}

	head, ok := r.p.cut.head(key, prefixKey, atRoot)
	if !ok {
		return ferry.Path{}, false
	}

	if i, isPos := position(head); isPos {
		return prefix.Elem(i), true
	}

	return prefix.At(r.p.mintf(head)), true
}

// writer is one open dump into a plane.
type writer struct {
	keys ferry.KeyFunc
	vals values
}

var _ ferry.Writer = (*writer)(nil)

func (w *writer) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	key, err := w.keys(addr)
	if err != nil {
		return err
	}

	text, err := textOf(v)
	if err != nil {
		return err
	}

	w.vals[key] = []string{text}

	return nil
}

func textOf(v ferry.Value) (string, error) {
	switch v.Kind() {
	case ferry.KindString:
		return v.AsString()
	case ferry.KindNumber:
		return v.AsNumber()
	case ferry.KindBool:
		b, err := v.AsBool()

		return strconv.FormatBool(b), err
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err
	case ferry.KindAbsent, ferry.KindNull:
		return "", illegal("this plane holds no value of that kind")
	default:
		return "", illegal("this plane holds no value of that kind")
	}
}

// position, step and extend are driver/env's, copied so the prototype's
// enumeration behaves the way a shipped driver's does.
func position(text string) (uint, bool) {
	if text == "" || len(text) > 1 && text[0] == '0' {
		return 0, false
	}

	i, err := strconv.ParseUint(text, 10, 0)
	if err != nil {
		return 0, false
	}

	return uint(i), true
}

func step(prefix, addr ferry.Path) (ferry.Path, bool) {
	depth := 0
	pre := slices.Collect(prefix.Segments())

	for seg := range addr.Segments() {
		if depth == len(pre) {
			return extend(prefix, seg), true
		}

		if seg != pre[depth] {
			return ferry.Path{}, false
		}

		depth++
	}

	return ferry.Path{}, false
}

func extend(p ferry.Path, s ferry.Segment) ferry.Path {
	if s.Kind() == ferry.Index {
		i, _ := position(s.Text())

		return p.Elem(i)
	}

	return p.At(s.Text())
}
