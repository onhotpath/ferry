package main

// P11: the audit. Does an address that did not exist at Bind time work?
//
// ADR-0003 is explicit that not every address comes from the type: a map key's
// address and a slice's length come from the *value*, and both rules therefore
// run in two tiers, the second "as each is minted, before the write it belongs
// to".
//
// Everything above this probe assumed the address set handed to Bind is the
// whole set. For a schema containing a map or a slice it is not, and this
// probe checks what the contract as written does about that. It does the wrong
// thing, and the fix is in this file.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

func p11Dynamic() {
	head("P11  audit: an address minted after Bind")

	ctx := context.Background()

	// (a) The bug. Bind sees the static set; Dump then produces a map key.
	fmt.Println("    (a) the contract as written, dumping a map")
	static := NewAddressSet([]Path{path("name")})
	kv := newKV(nil)
	w, err := bindOpenSink(ctx, KVSink{KV: kv, Prefix: "cfg/"}, static)
	if err != nil {
		fmt.Println("        bind:", err)
		return
	}
	fmt.Printf("        Set(/name)        -> %v\n", w.Set(ctx, path("name"), String("svc")))
	fmt.Printf("        Set(/labels/env)  -> %v\n", w.Set(ctx, path("labels", "env"), String("prod")))
	fmt.Println("        A precomputed table is a closed set, so a legitimate map key")
	fmt.Println("        is refused as though it were a driver error. Every probe")
	fmt.Println("        before this one used a schema with no map and no slice, which")
	fmt.Println("        is why it went unnoticed.")

	// (b) The fix. What Bind returns is not a map, it is a key function with
	//     the static set already computed and the injectivity check already
	//     run over it. A minted address takes the slow path once.
	fmt.Println("\n    (b) with Keys instead of map[Path]string")
	keys, err := NewKeys(static, "kv", kvKey("cfg/"))
	if err != nil {
		fmt.Println("        keys:", err)
		return
	}
	for _, p := range []Path{path("name"), path("labels", "env"), path("labels", "env")} {
		k, err := keys.Key(p)
		fmt.Printf("        %-18s -> %-22q err=%v\n", p, k, err)
	}
	fmt.Printf("        precomputed=%d minted=%d\n", keys.precomputed, keys.minted)

	// (c) And the injectivity rule still bites on the minted address, which is
	//     ADR-0003's second tier and the whole reason the check cannot simply
	//     be finished at Bind.
	fmt.Println("\n    (c) tier two: a minted address that collides")
	keys2, _ := NewKeys(NewAddressSet([]Path{path("limits", "http_port")}), "env", envKeyFunc("_"))
	k1, e1 := keys2.Key(path("limits", "http_port"))
	k2, e2 := keys2.Key(path("limits", "http.port"))
	fmt.Printf("        /limits/http_port -> %q err=%v\n", k1, e1)
	fmt.Printf("        /limits/http.port -> %q err=%v\n", k2, oneLine(errStr(e2)))
	fmt.Println("        Refused as it is minted, before the write it belongs to, and")
	fmt.Println("        naming both addresses. ADR-0001's prohibition on ignoring")
	fmt.Println("        anything is honoured; ADR-0003's honest caveat that this tier")
	fmt.Println("        is loud but not early stands unchanged.")

	// (d) A tree plane needs none of this, which is worth stating because it
	//     is why the fix does not touch the interface at all.
	fmt.Println("\n    (d) the tree plane")
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.yaml")
	yw, _ := bindOpenSink(ctx, YAMLSink{Path: out}, static)
	fmt.Printf("        Set(/name)        -> %v\n", yw.Set(ctx, path("name"), String("svc")))
	fmt.Printf("        Set(/labels/env)  -> %v\n", yw.Set(ctx, path("labels", "env"), String("prod")))
	_ = yw.Commit(ctx)
	b, _ := os.ReadFile(out)
	for _, ln := range splitLines(string(b)) {
		fmt.Println("           ", ln)
	}
	fmt.Println("        A tree plane never built a table, so it never had a closed")
	fmt.Println("        set to be surprised by. The defect was entirely in what the")
	fmt.Println("        flattening drivers were handed.")

	// (e) What this changes in the contract.
	fmt.Println("\n    (e) what the interfaces have to say about it")
	fmt.Println("        Nothing. Bind, Open, Get, Set are unchanged. What changes is")
	fmt.Println("        that the address set handed to Bind is documented as the")
	fmt.Println("        static set rather than the whole set, and that the thing core")
	fmt.Println("        hands back is a key function and not a map.")
	fmt.Println("        That distinction is load-bearing: a map invites a driver to")
	fmt.Println("        treat a miss as an error, which is what (a) did.")
}

func errStr(e error) error { return e }

// ---------------------------------------------------------------------------
// Keys: the corrected return of core's key-table helper
// ---------------------------------------------------------------------------

// Keys is a driver's address-to-plane-key mapping. The static addresses are
// computed once at Bind and checked for legality and injectivity there;
// an address minted later from a map key or a sequence index takes the slow
// path once and is checked against everything already issued.
// The two tiers ADR-0003 names are two fields here rather than one map, and
// that is what keeps the static tier on the hot path it was priced at: static
// is written once by NewKeys and never again, so reading it takes no lock.
type Keys struct {
	name   string
	f      KeyFunc
	static map[Path]string // immutable after NewKeys

	mu   sync.Mutex
	dyn  map[Path]string
	used map[string]Path

	precomputed, minted int
}

func NewKeys(a *AddressSet, name string, f KeyFunc) (*Keys, error) {
	tab, err := buildKeyTable(a, name, f)
	if err != nil {
		return nil, err
	}
	used := make(map[string]Path, len(tab))
	for p, k := range tab {
		used[k] = p
	}
	return &Keys{
		name: name, f: f, static: tab,
		dyn: map[Path]string{}, used: used, precomputed: len(tab),
	}, nil
}

func (k *Keys) Key(p Path) (string, error) {
	if s, ok := k.static[p]; ok {
		return s, nil
	}
	return k.mint(p)
}

func (k *Keys) mint(p Path) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if s, ok := k.dyn[p]; ok {
		return s, nil
	}
	s, err := k.f(p)
	if err != nil {
		return "", fmt.Errorf("%s: cannot name %s: %w", k.name, p, err)
	}
	if prev, dup := k.used[s]; dup {
		return "", fmt.Errorf("%s: key function is not injective: %q <- %s and %s", k.name, s, prev, p)
	}
	k.dyn[p], k.used[s] = s, p
	k.minted++
	return s, nil
}
