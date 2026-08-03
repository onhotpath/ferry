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
// WHAT THE TWO BRANCHES DIFFER IN, and it is one thing: whether an encode
// failure GATES the write phase.
//
// A first attempt here read "interleaved" as "stream, writing at each leaf",
// and folded the Set into a third `direction`. That reproduced the ADR's error
// sets, and it was wrong for a reason the ADR does not discuss and ADR-0003 and
// ADR-0004 both do. Writing during the walk inherits the walk's order, which is
// `reflect` source order, so a `Committer` sink got addresses in a different
// sequence from a plain one:
//
//	plain sink (no Committer):  [/alpha /beta /mid/x /mid/y /zeta]
//	Committer sink:             [/zeta /alpha /mid/x /mid/y /beta]
//
// measured unsorted on eight of eight shapes. That breaks ADR-0003's "wherever
// ferry enumerates addresses, in dumped output ... it sorts segment-wise", and
// worse, it makes an OPTIONAL INTERFACE change what ferry does rather than only
// when. ADR-0004's whole position on `Committer`, `Releaser` and `Enumerator`
// is lifecycle - "Commit runs only on success, Close always" - and none of the
// three is supposed to touch the address sequence. ferry's own flagship driver,
// yaml, implements two of the three, so the reference driver was the one on the
// unsorted path.
//
// So BOTH branches buffer and both write through one sorted loop. The ADR
// sanctions the buffer explicitly: "Whether Dump's encode phase buffers its
// values or re-walks to produce them" is listed under what it does not decide,
// priced at 546 KB against 521 ms over ten thousand addresses. And a
// `Committer` buffers anyway - staging is what the interface means.
//
// What the Committer still gets is the thing the ADR actually measured, which
// is the BETTER ERROR SET rather than the streaming: an encode failure does not
// stop it learning the plane's refusals too, so both failure kinds arrive in
// one run. It is safe precisely because `Commit` runs only on success, so the
// plane is untouched on failure without needing the gate. On a sink that cannot
// stage, the gate makes two-phase a fail-fast BETWEEN phases, so a flat sink
// pays for the untouched plane in round trips and a `Committer` pays for
// neither. That is the ADR's own argument for implementing `Committer`.
func (b *SinkBinding[T]) Dump(ctx context.Context, v T) (err error) {
	// Phase one: encode every address, writing nothing. This walk touches no
	// plane, so aggregating its failures costs the plane exactly nothing -
	// which is the distinction the ADR's first draft missed by measuring a sink
	// that could only refuse a write.
	out := map[Path]Value{}
	// STOPGAP REMOVED UNDER #58. #31 found that a registered map key's codec was
	// resolved through the package-level activeReg at walk time, which this
	// entry point does not install, and restored the registry around the walk
	// to compensate. That worked and it was the wrong shape twice over: it made
	// the walk depend on process-global state that ADR-0009's freeze exists to
	// remove, and it cost +1 allocation and +24 B per Dump, measured on B25.
	// The key codec now resolves into the compiled node like every other codec,
	// so there is nothing left to install and the walk touches no global.
	w := &walker{dir: dumpDir(out), sch: b.o.sch, ctx: ctx}
	_, encErr := w.walk(b.s.root, valueOf(v), Path{})

	// The open comes next in BOTH branches, because "Dump asks the sink whether
	// it can stage" is a question about the Writer, and there is no Writer
	// until the open. Opening is not writing: ADR-0004 put ErrReadOnly at
	// OpenWriter on the reasoning that "failing at open costs nothing, and
	// failing at the first Set has already half-written the plane", so a
	// writer that is opened and closed without a Set leaves the plane
	// untouched, which is the property ADR-0011 states.
	wr, oerr := b.open(ctx)
	if oerr != nil {
		return join(encErr, fromDriver(mOpen, Path{}, false, oerr))
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
	// THE GATE, and it is the whole difference between the two branches.
	c, staging := wr.(FCommitter)
	if encErr != nil && !staging {
		return encErr
	}

	// Phase two. sortedAddrs is ADR-0003's segment-wise order, and it is the
	// ONE write loop, so both sink kinds see one sequence.
	//
	// The realised set is iterated here, and the driver mints what the static
	// table does not hold: ADR-0004's two tiers on the write path. The Set half
	// aggregates, because a token with write access to some paths and not
	// others must report both refused addresses, and taking that away on Dump
	// alone would be an asymmetry between the directions about the same fact.
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
	setErr := b.o.sch(tasks)
	if all := join(encErr, setErr); all != nil {
		return all
	}
	// Commit runs ONLY on success, which is ADR-0004's protocol and is what
	// leaves a staging plane untouched for an encode failure with no gate.
	if staging {
		if cerr := c.Commit(ctx); cerr != nil {
			return fromDriver(mCommit, Path{}, false, cerr)
		}
	}
	return nil
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
