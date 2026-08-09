package protect_test

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/onhotpath/ferry"
)

// store is the plane every test in this package runs over, and it is
// deliberately not a registry: the whole claim of a decorator is that it
// composes with any plane, so the one it is proved over here is an ordinary
// address-keyed store with no Windows in it anywhere.
//
// It is inspectable, which is the one thing ferrytest's memory plane is not and
// the one thing these tests need: a test asserting that a secret was encrypted
// has to be able to look at what the plane actually holds and fail if the
// plaintext is in there.
//
// It carries the optional interfaces a schema holding a slice or a map needs -
// probing, enumeration, forgetting a composite and spelling a container at its
// own address - so that a decorator dropping one of them fails a load rather
// than going unnoticed.
type store struct {
	mu    sync.Mutex
	vals  map[ferry.Path]ferry.Value
	marks map[ferry.Path]ferry.Presence
}

func newStore() *store {
	return &store{vals: map[ferry.Path]ferry.Value{}, marks: map[ferry.Path]ferry.Presence{}}
}

// seed writes one value the way another program would have left it, which is
// what stages a store that predates this decorator.
func (s *store) seed(addr ferry.Path, v ferry.Value) *store {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vals[addr] = v

	return s
}

// at is what the plane holds at one address, for a test that asserts on the
// stored form rather than on what a load gave back.
func (s *store) at(addr ferry.Path) ferry.Value {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.vals[addr]
}

// empty reports the plane holding nothing at all, which is what a refusal before
// any I/O has to leave behind.
func (s *store) empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.vals) == 0 && len(s.marks) == 0
}

// holds reports some stored value carrying this text, which is how a test says
// "the secret is not in the plane" and means it.
func (s *store) holds(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range s.vals {
		if strings.Contains(rendered(v), text) {
			return true
		}
	}

	return false
}

// rendered is one stored value as text, whichever kind it is.
func rendered(v ferry.Value) string {
	switch v.Kind() {
	case ferry.KindString:
		s, _ := v.AsString()

		return s
	case ferry.KindBytes:
		b, _ := v.AsBytes()

		return string(b)
	case ferry.KindNumber:
		s, _ := v.AsNumber()

		return s
	default:
		return v.Kind().String()
	}
}

func (s *store) children(prefix ferry.Path) []ferry.Segment {
	s.mu.Lock()
	defer s.mu.Unlock()

	pre := slices.Collect(prefix.Segments())
	kids := map[ferry.Segment]struct{}{}

	for addr := range s.vals {
		if c, ok := childOf(pre, addr); ok {
			kids[c] = struct{}{}
		}
	}

	out := slices.Collect(maps.Keys(kids))
	slices.SortFunc(out, compareSegments)

	return out
}

// childOf is ferrytest's own, copied: the segment of the immediate child of
// prefix that addr lies under, and whether addr extends prefix at all.
func childOf(pre []ferry.Segment, addr ferry.Path) (ferry.Segment, bool) {
	i := 0

	for seg := range addr.Segments() {
		if i == len(pre) {
			return seg, true
		}

		if seg != pre[i] {
			return ferry.Segment{}, false
		}

		i++
	}

	return ferry.Segment{}, false
}

// compareSegments orders members so that enumeration is not Go's randomised map
// order: positions numerically, names by their bytes.
func compareSegments(a, b ferry.Segment) int {
	if a.Kind() != b.Kind() {
		return cmp.Compare(a.Kind(), b.Kind())
	}

	if a.Kind() == ferry.Index {
		if c := cmp.Compare(len(a.Text()), len(b.Text())); c != 0 {
			return c
		}
	}

	return strings.Compare(a.Text(), b.Text())
}

// storeSource and storeSink are the two halves over one store.
type storeSource struct{ s *store }

type storeSink struct{ s *store }

var (
	_ ferry.Source     = storeSource{}
	_ ferry.Sink       = storeSink{}
	_ ferry.Reader     = storeReader{}
	_ ferry.Prober     = storeReader{}
	_ ferry.Enumerator = storeReader{}
	_ ferry.Writer     = storeWriter{}
	_ ferry.Ensurer    = storeWriter{}
	_ ferry.Unsetter   = storeWriter{}
)

func (s storeSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return storeReader{s: s.s}, nil }, nil
}

func (s storeSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return storeWriter{s: s.s}, nil }, nil
}

type storeReader struct{ s *store }

func (r storeReader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	return r.s.at(addr.Path()), nil
}

func (r storeReader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	r.s.mu.Lock()
	p, marked := r.s.marks[addr.Path()]
	r.s.mu.Unlock()

	switch {
	case marked && p == ferry.PresenceNull:
		return ferry.SectionNull, nil
	case marked:
		return ferry.SectionPresent, nil
	case len(r.s.children(addr.Path())) > 0:
		return ferry.SectionPresent, nil
	default:
		return ferry.SectionAbsent, nil
	}
}

func (r storeReader) Children(_ context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	return r.s.children(addr.Path()), nil
}

type storeWriter struct{ s *store }

func (w storeWriter) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	w.s.seed(addr.Path(), v)

	return nil
}

func (w storeWriter) Ensure(_ context.Context, addr ferry.Container, p ferry.Presence) error {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()

	w.s.marks[addr.Path()] = p

	return nil
}

func (w storeWriter) Unset(_ context.Context, addr ferry.CompositeAddr) error {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()

	pre := slices.Collect(addr.Path().Segments())

	for held := range w.s.vals {
		if _, ok := childOf(pre, held); ok {
			delete(w.s.vals, held)
		}
	}

	delete(w.s.marks, addr.Path())

	return nil
}
