package main

// B8: the same question on the Dump side, which B7a got wrong.
//
// B7a observed that this prototype's Dump binds the sink AFTER the walk, with
// the REALISED address set, and concluded that a sink binding is not hoistable.
// The observation is correct about the prototype and the conclusion does not
// follow, because the prototype is not doing what ADR-0004 says.
//
// ADR-0004, in as many words: "the address set handed to Bind is the STATIC
// set, and core hands back a key function"; a dynamic address is minted "on
// demand, before the write it belongs to". Its own worked example is a DUMP -
// Set(/labels/env) against a static set of {/name} - and the fix it records is
// precisely that the driver mints rather than that core binds later.
//
// So the prototype's Dump was a shortcut, and correcting it makes the sink
// binding hoistable for exactly the same reason the source binding is.

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// --- a flat sink, so the write side actually mints ---------------------------

// BKVSink is a Consul-shaped flat sink: it produces a plane key, so it carries
// ADR-0003's injectivity obligation and exercises both tiers on the write path.
// The yaml sink cannot: it walks segments and builds no key at all.
type BKVSink struct {
	Store *BStore
	// PerOpen selects which of the two shapes B2 measured this sink uses.
	PerOpen bool
}

type BStore struct {
	mu sync.Mutex
	kv map[string]string
}

func NewStore() *BStore { return &BStore{kv: map[string]string{}} }

func (s *BStore) put(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kv[k] = v
}

func (s *BStore) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.kv))
	for k := range s.kv {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (s *BStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kv = map[string]string{}
}

func (s BKVSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	if s.PerOpen {
		bound, err := NewBoundKeys(a, "kv", bEnvKey)
		if err != nil {
			return nil, err
		}
		return func(context.Context) (FWriter, error) {
			return bKVWriter{bound.Session().Key, s.Store}, nil
		}, nil
	}
	keys, err := NewKeys(a, "kv", bEnvKey)
	if err != nil {
		return nil, err
	}
	return func(context.Context) (FWriter, error) {
		return bKVWriter{keys.Key, s.Store}, nil
	}, nil
}

type bKVWriter struct {
	key   func(Path) (string, error)
	store *BStore
}

func (w bKVWriter) Set(_ context.Context, addr Path, v Value) error {
	k, err := w.key(addr) // the mint, and the check, before the write it belongs to
	if err != nil {
		return err
	}
	w.store.put(k, v.Text())
	return nil
}

// --- the sink binding --------------------------------------------------------

type SinkBinding[T any] struct {
	s    *schema
	o    opts
	open FOpenWriterFunc
}

// BindSink is Dump's first two phases, stopped at the same phase boundary Bind
// stops at. The address set it hands the sink is the STATIC one, which is what
// makes it a pure function of (schema, sink) and therefore hoistable.
func BindSink[T any](sink FSink, options ...Option) (*SinkBinding[T], error) {
	o := defaultOpts()
	for _, op := range options {
		op.apply(&o)
	}
	s, err := schemaFor(typeOf[T](), o)
	if err != nil {
		return nil, err
	}
	open, err := sink.Bind(s.as)
	if err != nil {
		return nil, err
	}
	return &SinkBinding[T]{s: s, o: o, open: open}, nil
}

// Dump is ADR-0011's two-phase rule, and the Committer branch that exempts a
// staging sink from it. #41 item 8.
//
//	Dump encodes every address before it writes any of them. If anything fails
//	to encode, every such failure is reported and nothing is written.
//
// and
//
//	Dump asks the sink whether it can stage. A `Committer` gets interleaved
//	aggregation, because `Commit` runs only on success, so the plane is already
//	untouched on failure. Everything else gets the encode phase.
//
// The tip HAPPENED to encode-all-then-write, so the untouched-plane property
// held by accident of the map buffer rather than by decision - and the accident
// did not extend to aggregating the encode failures, because `serial` abandoned
// the walk at the first one. The Committer branch was not implemented at all.
//
// What the Committer branch buys is not an optimisation, which is what the
// ADR's own draft assumed: the staging sink gets a BETTER ERROR SET, because
// interleaving lets it learn both failure kinds in one run. On a sink that
// cannot stage, two-phase is a fail-fast BETWEEN phases, so a flat sink pays
// for the untouched plane in round trips and a Committer pays for neither.
func (b *SinkBinding[T]) Dump(ctx context.Context, v T) (err error) {
	// The open is first in both branches, because the sink cannot be asked
	// whether it can stage until there is a Writer to ask.
	wr, oerr := b.open(ctx)
	if oerr != nil {
		return fromDriver(mOpen, Path{}, false, oerr)
	}
	// #41 D14. `defer rel.Close()` dropped the result outright; ADR-0011 makes
	// it an element with the moment first in the sort key.
	if rel, ok := wr.(FReleaser); ok {
		defer func() {
			if e := rel.Close(); e != nil {
				err = join(err, fromDriver(mClose, Path{}, false, e))
			}
		}()
	}
	c, staging := wr.(FCommitter)

	if staging {
		// Interleaved: one walk, encoding and writing at each leaf, both
		// failure kinds aggregated by the one scheduler.
		w := &walker{dir: writeDir(ctx, wr, b.o.sch), sch: b.o.sch, ctx: ctx}
		if _, werr := w.walk(b.s.root, valueOf(v), Path{}); werr != nil {
			return werr
		}
		// Commit runs ONLY on success, which is what makes the plane untouched
		// on failure without an encode phase.
		if cerr := c.Commit(ctx); cerr != nil {
			return fromDriver(mCommit, Path{}, false, cerr)
		}
		return nil
	}

	// Phase one: encode every address, writing nothing. This walk touches no
	// plane, so aggregating its failures costs the plane exactly nothing -
	// which is the distinction the ADR's first draft missed by measuring a sink
	// that could only refuse a write.
	out := map[Path]Value{}
	w := &walker{dir: dumpDir(out), sch: b.o.sch, ctx: ctx}
	if _, werr := w.walk(b.s.root, valueOf(v), Path{}); werr != nil {
		return werr
	}
	// Phase two. The realised set is iterated here, and the driver mints what
	// the static table does not hold: ADR-0004's two tiers on the write path.
	// The Set half aggregates, because a token with write access to some paths
	// and not others must report both refused addresses, and taking that away
	// on Dump alone would be an asymmetry between the directions about the
	// same fact.
	addrs := sortedAddrs(out)
	tasks := make([]func() error, 0, len(addrs))
	for _, p := range addrs {
		tasks = append(tasks, func() error {
			if serr := wr.Set(ctx, p, out[p]); serr != nil {
				return fromDriver(mWalk, p, true, serr)
			}
			return nil
		})
	}
	return b.o.sch(tasks)
}

// writeDir is dumpDir with the Set folded into the leaf, which is the whole of
// the interleaved branch. It is not a second walk: it is a third `direction`
// over the one walk in e_walk.go, which is what #16's "write the walk exactly
// once" constraint is for.
func writeDir(ctx context.Context, wr FWriter, _ sched) direction {
	set := func(at Path, val Value) error {
		if err := wr.Set(ctx, at, val); err != nil {
			return fromDriver(mWalk, at, true, err)
		}
		return nil
	}
	d := dumpDir(nil)
	d.name = "dump/staged"
	d.leaf = func(n *node, v reflect.Value, at Path) (bool, error) {
		if n.omitzero && v.IsZero() {
			return false, nil
		}
		val, err := encLeafWith(n.codec, v)
		if err != nil {
			return false, errAt(mWalk, ErrValue, at, "%s", safeEncodeMsg(n.typ)).withCause(err)
		}
		return true, set(at, val)
	}
	d.container = func(n *node, v reflect.Value, at Path) (bool, bool, error) {
		if n.kind == nPtr {
			if v.IsNil() {
				return true, true, set(at, Null())
			}
			return false, false, nil
		}
		if v.Len() == 0 {
			return true, true, set(at, Null())
		}
		return false, false, nil
	}
	return d
}

// --- the probe ---------------------------------------------------------------

type B8Conf struct {
	Name   string         `ferry:"name"`
	Limits map[string]int `ferry:"limits"`
}

func runB8() {
	ctx := context.Background()

	fmt.Println("--- B8a: what ADR-0004 actually says the sink is handed ---")
	s := mustSchema[B8Conf]()
	fmt.Printf("    the STATIC set: %v\n", s.as.All())
	fmt.Println("    ADR-0004's own worked example is a DUMP against a static set:")
	fmt.Println("      \"dumping a map[string]string field to the KV driver: Set(/labels/env)")
	fmt.Println("       returned kv: address not in the opened set\"")
	fmt.Println("    and its fix is that the driver MINTS, not that core binds later.")
	fmt.Println("    This prototype's Dump bound with the realised set instead, which is")
	fmt.Println("    what B7a measured and mistook for a property of Dump.")

	fmt.Println("\n--- B8b: a sink binding, held across three dumps of different shapes ---")
	store := NewStore()
	b, err := BindSink[B8Conf](BKVSink{Store: store, PerOpen: true})
	if err != nil {
		fmt.Println("    bind:", err)
		return
	}
	for _, v := range []B8Conf{
		{Name: "a"},
		{Name: "b", Limits: map[string]int{"rps": 1}},
		{Name: "c", Limits: map[string]int{"rps": 2, "burst": 3}},
	} {
		store.Reset()
		err := b.Dump(ctx, v)
		fmt.Printf("    %-40v -> %v err=%v\n", fmt.Sprintf("%s %v", v.Name, v.Limits), store.Keys(), err)
	}
	fmt.Println("    One binding, three different realised address sets, all three written.")
	fmt.Println("    The static table holds /name and /limits; every LIMITS_<key> is minted")
	fmt.Println("    at the Set it belongs to.")

	fmt.Println("\n--- B8c: and the amendment is load-bearing HERE rather than on Load ---")
	fmt.Println("    B2 found that a retained minted set refuses a legal write, and that")
	fmt.Println("    the case is Dump's because a minted address comes from the VALUE.")
	fmt.Println("    Now that a sink binding exists, that case is reachable through the")
	fmt.Println("    entry point rather than only through the helper.")
	for _, tc := range []struct {
		name    string
		perOpen bool
	}{
		{"minted set on the binding", false},
		{"minted set on the open", true},
	} {
		st := NewStore()
		sb, err := BindSink[B8Conf](BKVSink{Store: st, PerOpen: tc.perOpen})
		if err != nil {
			fmt.Printf("    %-28s bind err=%v\n", tc.name, err)
			continue
		}
		var out []string
		for _, v := range []B8Conf{
			{Name: "a", Limits: map[string]int{"http-port": 1}},
			{Name: "b", Limits: map[string]int{"http_port": 2}},
		} {
			st.Reset()
			if err := sb.Dump(ctx, v); err != nil {
				out = append(out, "REFUSED")
				continue
			}
			out = append(out, strings.Join(st.Keys(), ","))
		}
		fmt.Printf("    %-28s %v\n", tc.name, out)
	}
	fmt.Println("    Two dumps, each holding one of two map keys the env transform maps")
	fmt.Println("    onto one plane key. Neither dump collides with itself.")

	fmt.Println("\n--- B8d: what it saves, and it is the same shape as Load's ---")
	store2 := NewStore()
	sink := BKVSink{Store: store2, PerOpen: true}
	held, _ := BindSink[B1Filter](sink)
	v := B1Filter{Q: "widgets", Page: 3, Size: 50, Sort: "name", Desc: true, Cursor: "abc"}
	rows := []struct {
		name string
		fn   func()
	}{
		{"ferry.Dump, binding per call", func() { _ = bDumpOneShot(ctx, v, sink) }},
		{"b.Dump(ctx, v), binding held", func() { _ = held.Dump(ctx, v) }},
	}
	fmt.Printf("  %-34s %10s %8s %8s\n", "", "ns/op", "B/op", "allocs")
	for _, r := range rows {
		res := testing.Benchmark(func(bb *testing.B) {
			bb.ReportAllocs()
			for bb.Loop() {
				r.fn()
			}
		})
		fmt.Printf("  %-34s %10d %8d %8d\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}

	fmt.Println("\n--- B8e: a sink whose plane is per request, for symmetry ---")
	fmt.Println("    Nothing about the context rule is Load-only: a sink that writes into")
	fmt.Println("    a per-request buffer reads it from the context the same way.")
	rec := NewStore()
	rb, err := BindSink[B8Conf](BKVCtxSink{})
	if err != nil {
		fmt.Println("    bind:", err)
		return
	}
	e1 := rb.Dump(BStoreContext(ctx, rec), B8Conf{Name: "per-request"})
	fmt.Printf("    with a store in the context -> %v err=%v\n", rec.Keys(), e1)
	e2 := rb.Dump(ctx, B8Conf{Name: "per-request"})
	fmt.Printf("    with none                   -> err=%v\n", e2)
}

// bDumpOneShot is Dump written as BindSink followed by the method, which is
// what the one-shot verb has to be if the two are not to be two ways.
func bDumpOneShot[T any](ctx context.Context, v T, sink FSink, options ...Option) error {
	b, err := BindSink[T](sink, options...)
	if err != nil {
		return err
	}
	return b.Dump(ctx, v)
}

// --- the per-request sink ----------------------------------------------------

type bStoreCtxKey struct{}

func BStoreContext(ctx context.Context, s *BStore) context.Context {
	return context.WithValue(ctx, bStoreCtxKey{}, s)
}

type BKVCtxSink struct{}

func (BKVCtxSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	bound, err := NewBoundKeys(a, "kv", bEnvKey)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FWriter, error) {
		st, ok := ctx.Value(bStoreCtxKey{}).(*BStore)
		if !ok {
			return nil, ErrNoStore
		}
		return bKVWriter{bound.Session().Key, st}, nil
	}, nil
}
