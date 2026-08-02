package main

// W5: the second data point #25 asked for.
//
// #15's body asks this ticket to feed findings back into the interface tickets,
// and the handoff names the one #25 wants: "#25 opens on a query-parameter
// source, which is one plane. Its central claim is that `Source` conflates the
// driver's key grammar with the plane instance. A Registry driver holds a real
// HKEY handle with real permissions, so it is the second data point on whether
// that conflation is a general problem or a query-params problem."
//
// ADR-0012 landed while this ran, so this probe is written against its
// decision rather than against #25's open options.

import (
	"context"
	"fmt"
	"testing"
)

// wCtxSource is ADR-0012's rule applied to the Registry: the plane instance
// arrives in the context, from the driver's own key and constructor, and the
// Source carries only the key grammar.
type wCtxKey struct{}

func WithHive(ctx context.Context, st wStore) context.Context {
	return context.WithValue(ctx, wCtxKey{}, st)
}

type wCtxSource struct{ Base string }

func (s wCtxSource) Bind(a *AddressSet) (FOpenFunc, error) {
	kf := wRegKey{base: s.Base}
	if _, err := kf.bind(a.All()); err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		st, ok := ctx.Value(wCtxKey{}).(wStore)
		if !ok {
			// ADR-0012: the refusal lands at open, which is where ADR-0004
			// already puts "the plane is not reachable", per load rather than
			// at bind.
			return nil, fmt.Errorf("registry: no hive in the context")
		}
		return &wRegReader{store: st, kf: kf}, nil
	}, nil
}

func runW5() {
	ctx := context.Background()
	base := `Software\Acme`
	addrs := []Path{path("name"), path("db", "host"), path("db", "port")}

	fmt.Println("(a) is the Registry a field-shaped driver or a context-shaped one?")
	fmt.Println("    ADR-0012's discriminator is not \"is this configuration\", it is:")
	fmt.Println("    does the caller obtain this value freshly for each load?")
	fmt.Println("    For an ordinary Registry driver the answer is NO. HKCU is already the")
	fmt.Println("    current user's hive, the subkey path is a constant, and the HKEY is")
	fmt.Println("    opened inside OpenFunc, which is exactly where ADR-0004 puts a plane's")
	fmt.Println("    handle. So the ordinary case CONFIRMS ADR-0004 as written and is not")
	fmt.Println("    the query-parameter shape at all.")

	fmt.Println("\n(b) but the conflation is real here for a DIFFERENT reason, and this is")
	fmt.Println("    the second data point")
	fmt.Println("    ADR-0004 lists what a Source holds: \"driver config: path, separator,")
	fmt.Println("    prefix, client\", changing \"never, you constructed it\". For the")
	fmt.Println("    Registry the key table is a pure function of the ADDRESS SET and the")
	fmt.Println("    base path, and depends on the hive not at all. Measured, two stores")
	fmt.Println("    that share nothing:")
	h1, h2 := newFake(), newFake()
	k1, err1 := (wRegKey{base: base}).bind(addrs)
	k2, err2 := (wRegKey{base: base}).bind(addrs)
	same := fmt.Sprint(k1) == fmt.Sprint(k2)
	fmt.Printf("      two binds over the same address set: identical=%v err=%v/%v\n", same, errShortW(err1), errShortW(err2))
	fmt.Println("    So for query parameters the two lifetimes come apart because the plane")
	fmt.Println("    is constructed per REQUEST. For the Registry they come apart because")
	fmt.Println("    the key table does not depend on the plane at all. Two independent")
	fmt.Println("    causes, one conflation, which is the answer to \"general problem or")
	fmt.Println("    query-params problem\": general.")

	fmt.Println("\n(c) and ADR-0012's context rule generalises to it unchanged")
	fmt.Println("    A service reading a DIFFERENT user's hive per request - HKU\\<SID> for")
	fmt.Println("    a logged-on user, or a profile tool walking every hive - obtains the")
	fmt.Println("    plane freshly per load, so ADR-0012's discriminator puts it in the")
	fmt.Println("    context. One bind, two hives, run:")
	_ = h1.SetValue(base, "name", wVal{typ: wSZ, s: "tenant-one"})
	_ = h1.SetValue(base+`\db`, "host", wVal{typ: wSZ, s: "db1"})
	_ = h1.SetValue(base+`\db`, "port", wVal{typ: wDWORD, n: 5432})
	_ = h2.SetValue(base, "name", wVal{typ: wSZ, s: "tenant-two"})
	_ = h2.SetValue(base+`\db`, "host", wVal{typ: wSZ, s: "db2"})
	_ = h2.SetValue(base+`\db`, "port", wVal{typ: wDWORD, n: 5433})

	src := wCtxSource{Base: base}
	for _, h := range []struct {
		label string
		st    wStore
	}{{"hive 1", h1}, {"hive 2", h2}} {
		got, err := Load[WTenant](WithHive(ctx, h.st), src, WithSched(tAggregating))
		fmt.Printf("      %s -> %+v  err=%v\n", h.label, got, errShortW(err))
	}
	got, err := Load[WTenant](ctx, src, WithSched(tAggregating))
	fmt.Printf("      no hive in the context -> %+v  err=%v\n", got, errShortW(err))
	fmt.Println("    That is ADR-0012's Program 2 with a plane that is not a request, and")
	fmt.Println("    it works with the driver's Source untouched. So the ADR's rule is")
	fmt.Println("    wider than the case it was measured on, which is worth saying because")
	fmt.Println("    its own discriminator already covers this and its examples do not.")

	fmt.Println("\n(d) what a held binding is worth HERE, which is not what it is worth for")
	fmt.Println("    query parameters")
	n := len(addrs)
	bindCost := testing.Benchmark(func(b *testing.B) {
		kf := wRegKey{base: base}
		for b.Loop() {
			_, _ = kf.bind(addrs)
		}
	})
	fmt.Printf("      the Registry key function's bind, %d addresses: %s\n", n, bindCost)
	fmt.Println("    It is the injectivity check plus a string join per address, so it is")
	fmt.Println("    linear in the address set and does no I/O, exactly as ADR-0004")
	fmt.Println("    requires. Hoisting it matters for the same reason ADR-0012 gives and")
	fmt.Println("    by a smaller margin than the query driver's, because a Registry load")
	fmt.Println("    is dominated by RegOpenKeyEx and RegQueryValueEx rather than by ferry.")

	fmt.Println("\n(e) one thing ADR-0012 hands the Registry that it does not name")
	fmt.Println("    Its amendment - \"the static table is the bind's and the minted set is")
	fmt.Println("    the open's\" - matters more here than on env, because the Registry")
	fmt.Println("    FOLDS CASE. Two tenants whose maps hold `Prod` and `prod` are two")
	fmt.Println("    minted addresses that collide under this driver's key function, and")
	fmt.Println("    with the minted set retained on the binding the second tenant is")
	fmt.Println("    refused for a collision with a hive it never touched. Run:")
	for _, retain := range []bool{true, false} {
		kf := wRegKey{base: base}
		minted := map[string]string{}
		var out []string
		for i, key := range []string{"Prod", "prod"} {
			if !retain {
				minted = map[string]string{}
			}
			k, _ := kf.checkKey(path("limits", key))
			if prev, dup := minted[k]; dup {
				out = append(out, fmt.Sprintf("write %d REFUSED (collides with %q)", i+1, prev))
				continue
			}
			minted[k] = key
			out = append(out, fmt.Sprintf("write %d ok", i+1))
		}
		fmt.Printf("      minted set %-18s %v\n", map[bool]string{true: "on the binding", false: "on the open"}[retain], out)
	}
	fmt.Println("    ADR-0012 measured this on env's `http-port` against `http_port`, where")
	fmt.Println("    the fold is the driver's choice. On the Registry the fold is the")
	fmt.Println("    PLANE's, so a driver cannot decline it, and the amendment is not")
	fmt.Println("    optional for this plane.")
}
