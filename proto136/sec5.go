package main

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// flatStore is an env-shaped plane: one flat namespace of upper-case names, and
// no null in its type system, which is ADR-0004's own row for env.
type flatStore struct {
	byKey map[string]ferry.Value
	addrs map[string]ferry.Path
}

func newFlatStore() *flatStore {
	return &flatStore{byKey: map[string]ferry.Value{}, addrs: map[string]ferry.Path{}}
}

// flatKey is the key function: join the segments with _, fold - onto _, and
// upper-case. All three collisions ADR-0003 names fall out of it.
func flatKey(p ferry.Path) string {
	parts := make([]string, 0, 4)
	for s := range p.Segments() {
		parts = append(parts, s.Text())
	}

	return strings.ToUpper(strings.ReplaceAll(strings.Join(parts, "_"), "-", "_"))
}

// flatSource is the read half.
type flatSource struct{ store *flatStore }

// Bind builds the key table and refuses a key function that is not injective
// over the set, naming both addresses, which is ADR-0003's obligation.
func (s flatSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if err := injective(addrs); err != nil {
		return nil, err
	}

	store := s.store

	return func(context.Context) (ferry.Reader, error) { return flatReader{store: store}, nil }, nil
}

func injective(addrs *ferry.AddressSet) error {
	seen := map[string]ferry.Path{}

	for a := range addrs.All() {
		k := flatKey(a)
		if prev, ok := seen[k]; ok {
			return ferry.ErrorAt(a, fmt.Errorf("%w: %s and %s both key %s, so one of them would be lost",
				ferry.ErrPlane, prev, a, k))
		}

		seen[k] = a
	}

	return nil
}

type flatReader struct{ store *flatStore }

func (r flatReader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	return r.store.byKey[flatKey(addr)], nil
}

func (r flatReader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	pre := slices.Collect(prefix.Segments())
	kids := map[string]ferry.Path{}

	for _, addr := range r.store.addrs {
		if c, ok := childUnder(prefix, pre, addr); ok {
			kids[c.String()] = c
		}
	}

	out := slices.Collect(maps.Values(kids))
	slices.SortFunc(out, ferry.Path.Compare)

	return out, nil
}

// childUnder is the memory plane's childOf, copied because it is unexported.
func childUnder(prefix ferry.Path, pre []ferry.Segment, addr ferry.Path) (ferry.Path, bool) {
	i := 0

	for seg := range addr.Segments() {
		if i == len(pre) {
			if seg.Kind() == ferry.Name {
				return prefix.At(seg.Text()), true
			}

			return prefix.Elem(indexText(seg.Text())), true
		}

		if seg != pre[i] {
			return ferry.Path{}, false
		}

		i++
	}

	return ferry.Path{}, false
}

func indexText(t string) uint {
	var i uint

	for k := range len(t) {
		i = i*10 + uint(t[k]-'0')
	}

	return i
}

// flatSink is the write half.
type flatSink struct{ store *flatStore }

func (s flatSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if err := injective(addrs); err != nil {
		return nil, err
	}

	store := s.store

	// The minted set belongs to the open and never to the binding, which is
	// ADR-0012's rule and ferrytest.Driver case 8.
	return func(context.Context) (ferry.Writer, error) {
		return flatWriter{store: store, minted: map[string]ferry.Path{}}, nil
	}, nil
}

type flatWriter struct {
	store  *flatStore
	minted map[string]ferry.Path
}

// Set refuses a Null loudly, which is the whole of what "this plane has no null"
// means at the boundary. FOO= is a zero-length string and not a null.
func (w flatWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	if v.Kind() == ferry.KindNull {
		return ferry.ErrorAt(addr, fmt.Errorf(
			"%w: this plane has no null, and an environment name either exists with text or does not exist",
			ferry.ErrPlane))
	}

	k := flatKey(addr)
	if prev, ok := w.minted[k]; ok {
		return ferry.ErrorAt(addr, fmt.Errorf("%w: %s and %s both key %s, so one of them would be lost",
			ferry.ErrPlane, prev, addr, k))
	}

	w.minted[k] = addr
	w.store.byKey[k] = v
	w.store.addrs[k] = addr

	return nil
}

// flatPlane is the description ferrytest.Driver takes.
func flatPlane() ferrytest.Plane {
	return ferrytest.Plane{
		Name: "flat (env-shaped, no null)",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() ferrytest.Instance {
			st := newFlatStore()

			return ferrytest.Instance{Source: flatSource{store: st}, Sink: flatSink{store: st}}
		},
	}
}

// capture collects what a suite reports instead of failing a test.
type capture struct{ lines []string }

func (c *capture) Errorf(format string, args ...any) {
	c.lines = append(c.lines, "FAIL "+fmt.Sprintf(format, args...))
}

func (c *capture) Logf(format string, args ...any) {
	c.lines = append(c.lines, "log  "+fmt.Sprintf(format, args...))
}

func (*capture) Helper() {}

func sec5() {
	head("5. The flat plane: what an empty slice does where there is no null")

	sub("5a. dumping an empty slice to a plane with no null")

	st := newFlatStore()
	err := ferry.Dump(context.Background(), tagsOnly{Tags: []string{}, M: map[string]string{}}, flatSink{store: st})
	fmt.Println("  Dump{Tags: []string{}, M: map[string]string{}} ->")

	for _, e := range ferry.Elements(err) {
		fmt.Println("     " + indent(e))
	}

	fmt.Println("  the plane afterwards:", keysOf2(st))

	sub("5b. dumping a populated one, for contrast")

	st2 := newFlatStore()
	err = ferry.Dump(context.Background(), tagsOnly{Tags: []string{"a", "b"}, M: map[string]string{"k": "v"}},
		flatSink{store: st2})
	fmt.Println("  err:", indent(err))
	fmt.Println("  the plane afterwards:", keysOf2(st2))

	sub("5c. loading a container address back off a flat plane")

	open, _ := flatSource{store: st2}.Bind(ferry.NewAddressSet(ferry.At("tags"), ferry.At("m"), ferry.At("opt")))
	r, _ := open(context.Background())

	for _, a := range []ferry.Path{ferry.At("tags"), ferry.At("m"), ferry.At("opt")} {
		v, err := r.Get(context.Background(), a)
		row(a.String(), fmt.Sprintf("%s (err %v)", show(v), err))
	}

	sub("5d. the whole Driver suite against this plane, reported rather than failed")

	cap := &capture{}
	ferrytest.Driver(cap, flatPlane())

	if len(cap.lines) == 0 {
		fmt.Println("  the suite reported nothing at all")
	}

	for _, l := range cap.lines {
		fmt.Println("  " + wrapAt(l, 108))
	}

	sub("5e. the same plane, but flattening Null onto the empty string instead of refusing it")

	lying := flatPlane()
	lying.Name = "flat, mangling Null onto String(\"\")"
	lying.Open = func() ferrytest.Instance {
		st := newFlatStore()

		return ferrytest.Instance{Source: flatSource{store: st}, Sink: lyingSink{store: st}}
	}

	cap2 := &capture{}
	ferrytest.Driver(cap2, lying)

	for _, l := range cap2.lines {
		if strings.HasPrefix(l, "FAIL") {
			fmt.Println("  " + wrapAt(l, 108))
		}
	}
}

// lyingSink is the defect case 1 exists for: a flat driver that declares no null
// and quietly writes the empty string where one was handed to it. It is xload's
// measured behaviour, recorded in ADR-0004.
type lyingSink struct{ store *flatStore }

func (s lyingSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if err := injective(addrs); err != nil {
		return nil, err
	}

	store := s.store

	return func(context.Context) (ferry.Writer, error) {
		return lyingWriter{inner: flatWriter{store: store, minted: map[string]ferry.Path{}}}, nil
	}, nil
}

type lyingWriter struct{ inner flatWriter }

func (w lyingWriter) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	if v.Kind() == ferry.KindNull {
		v = ferry.String("")
	}

	return w.inner.Set(ctx, addr, v)
}

func keysOf2(s *flatStore) []string {
	out := make([]string, 0, len(s.byKey))
	for k, v := range s.byKey {
		out = append(out, k+"="+show(v))
	}

	slices.Sort(out)

	return out
}

// wrapAt keeps one long report line readable in a fenced block.
func wrapAt(s string, n int) string {
	var b strings.Builder

	col := 0

	for _, w := range strings.Fields(s) {
		if col > 0 && col+len(w)+1 > n {
			b.WriteString("\n       ")

			col = 7
		} else if col > 0 {
			b.WriteString(" ")

			col++
		}

		b.WriteString(w)

		col += len(w)
	}

	return b.String()
}
