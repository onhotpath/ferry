package main

// P9: where the precomputed key table lives.
//
// The benchmark says it has to be precomputed: 188 ns against 2927 ns for the
// same six-address load, 1 alloc against 60. ADR-0003 already said so
// ("precomputing is a requirement of the design and not an optimisation").
//
// This probe audits the shape this prototype reached first - one Open method
// plus a core-side memo - and finds two defects in it, then measures the two
// shapes that do not have them.

import (
	"context"
	"errors"
	"fmt"
)

func p9Memo() {
	head("P9  where the key table lives: auditing this prototype's first answer")

	ctx := context.Background()
	addrs := NewAddressSet([]Path{path("metric", "http", "port")})

	// (a) Defect one: the memo key. Two configurations of one driver share it.
	fmt.Println("    (a) shape A: Open(ctx, addrs), core memoises on a driver name")
	wide, _ := EnvSourceA{Sep: "__"}.Open(ctx, addrs)
	narrow, _ := EnvSourceA{Sep: "_"}.Open(ctx, addrs)
	fmt.Printf("        EnvSourceA{Sep:\"__\"} -> %v\n", tableOf(wide))
	fmt.Printf("        EnvSourceA{Sep:\"_\"}  -> %v\n", tableOf(narrow))
	fmt.Println("        The second driver silently got the first one's keys. The")
	fmt.Println("        separator is the one thing ADR-0003 made a driver option, so")
	fmt.Println("        this is the headline case rather than an exotic misuse.")

	// (b) Defect two: the obvious fix does not work either. "Memoise on a
	//     comparable identity the driver supplies" is unsound, because driver
	//     structs routinely are not comparable.
	fmt.Println("\n    (b) and the obvious fix is unsound")
	fmt.Printf("        map[any]... keyed by EnvSource{}: %s\n",
		mustRecover(func() string {
			m := map[any]int{}
			m[EnvSource{}] = 1
			return "ok"
		}))
	fmt.Println("        EnvSource holds a func field. So does any driver taking a")
	fmt.Println("        dialer, a hook, or a clock. A contract whose correctness")
	fmt.Println("        depends on a driver author supplying a comparable identity")
	fmt.Println("        that captures everything its key function reads is a prose")
	fmt.Println("        rule with a runtime panic behind it, which is the shape")
	fmt.Println("        ADR-0001 rules out.")

	// (c) Shape B: give the precompute its own phase. No cache exists, so
	//     neither defect can. The lifetimes are genuinely different - the key
	//     table depends on (driver, address set), the data depends on when you
	//     asked - and the contract stops pretending otherwise.
	fmt.Println("\n    (c) shape B: Bind(addrs) (Binding, error), Binding.Open(ctx) (Reader, error)")
	for _, sep := range []string{"__", "_"} {
		b, err := EnvSourceB{Sep: sep}.Bind(addrs)
		if err != nil {
			fmt.Println("        bind:", err)
			return
		}
		r, _ := b.Open(ctx)
		fmt.Printf("        EnvSourceB{Sep:%-4q} -> %v\n", sep, tableOf(r))
	}
	fmt.Println("        Nothing is memoised, so nothing can be memoised wrongly.")
	fmt.Println("        Bind takes no context because it does no I/O, which is also")
	fmt.Println("        how the type says where ADR-0003's before-any-I/O checks run.")

	// (d) Shape C: the same split, but the driver pushes instead of ferry
	//     pulling. Two methods, which is koanf's bar exactly.
	fmt.Println("\n    (d) shape C: Bind(addrs) (Binding, error), Binding.Load(ctx, yield) error")
	bc, _ := EnvSourceC{Sep: "_"}.Bind(NewAddressSet([]Path{path("a"), path("b"), path("c")}))
	got := map[Path]Value{}
	err := bc.Load(ctx, func(p Path, v Value) error { got[p] = v; return nil })
	fmt.Printf("        pushed %d addresses, err=%v\n", len(got), err)
	fmt.Println("        Two methods, and the driver controls iteration, which lets a")
	fmt.Println("        streaming plane avoid materialising a snapshot.")
	fmt.Println("        Rejected anyway: ferry loses the iteration order, and")
	fmt.Println("        ADR-0001 makes determinism a package-wide invariant; it loses")
	fmt.Println("        error aggregation, which 5.4 makes a requirement; and a")
	fmt.Println("        driver can push an address that was never in the set, so the")
	fmt.Println("        engine has to validate every push and the saving evaporates.")
	fmt.Println("        Pull keeps all three in ferry, which is where the obligations")
	fmt.Println("        core cannot delegate already live.")
}

func tableOf(r Reader) []string {
	var tab map[Path]string
	switch t := r.(type) {
	case envReader:
		tab = t.tab
	case envReaderB:
		tab = t.tab
	default:
		return nil
	}
	out := make([]string, 0, len(tab))
	for _, v := range tab {
		out = append(out, v)
	}
	return out
}

// ---------------------------------------------------------------------------
// Shape A, the rejected one: one Open method, with a core-side memo
// ---------------------------------------------------------------------------

type EnvSourceA struct {
	Sep    string
	Lookup func(string) (string, bool)
}

var shapeACache = map[cacheKeyA]map[Path]string{}

type cacheKeyA struct {
	a    *AddressSet
	name string
}

func (s EnvSourceA) Open(_ context.Context, a *AddressSet) (Reader, error) {
	ck := cacheKeyA{a, "env"}
	tab, ok := shapeACache[ck]
	if !ok {
		var err error
		tab, err = buildKeyTable(a, "env", envKeyFunc(s.Sep))
		if err != nil {
			return nil, err
		}
		shapeACache[ck] = tab
	}
	return envReaderB{tab, func(string) (string, bool) { return "", false }}, nil
}

// ---------------------------------------------------------------------------
// Shape B, in full, so its cost is visible
// ---------------------------------------------------------------------------

type EnvSourceB struct {
	Sep    string
	Lookup func(string) (string, bool)
}

func (s EnvSourceB) Bind(a *AddressSet) (Binding, error) {
	tab, err := buildKeyTable(a, "env", envKeyFunc(s.Sep))
	if err != nil {
		return nil, err
	}
	look := s.Lookup
	if look == nil {
		look = func(string) (string, bool) { return "", false }
	}
	return envBinding{tab, look}, nil
}

type envBinding struct {
	tab  map[Path]string
	look func(string) (string, bool)
}

func (b envBinding) Open(context.Context) (Reader, error) { return envReaderB(b), nil }

type envReaderB struct {
	tab  map[Path]string
	look func(string) (string, bool)
}

func (r envReaderB) Get(_ context.Context, addr Path) (Value, error) {
	k, ok := r.tab[addr]
	if !ok {
		return Absent, errors.New("env: address not in the bound set: " + addr.String())
	}
	s, ok := r.look(k)
	if !ok {
		return Absent, nil
	}
	return String(s), nil
}

// ---------------------------------------------------------------------------
// Shape C, the push model, measured before being rejected
// ---------------------------------------------------------------------------

type EnvSourceC struct{ Sep string }

type bindingC interface {
	Load(context.Context, func(Path, Value) error) error
}

func (s EnvSourceC) Bind(a *AddressSet) (bindingC, error) {
	tab, err := buildKeyTable(a, "env", envKeyFunc(s.Sep))
	if err != nil {
		return nil, err
	}
	return bindC{a, tab}, nil
}

type bindC struct {
	a   *AddressSet
	tab map[Path]string
}

func (b bindC) Load(_ context.Context, yield func(Path, Value) error) error {
	for _, p := range b.a.All() {
		if err := yield(p, String("x")); err != nil {
			return err
		}
	}
	return nil
}

func envKeyFunc(sep string) KeyFunc {
	s := EnvSource{Sep: sep}
	return func(p Path) (string, error) {
		out := ""
		for i, seg := range p.Segments() {
			if seg.Text == "" {
				return "", errors.New("empty segment")
			}
			if i > 0 {
				out += s.sep()
			}
			out += envSafe(seg.Text)
		}
		return out, nil
	}
}
