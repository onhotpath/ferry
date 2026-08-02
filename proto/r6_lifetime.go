package main

// R6, R7, R8: where the identity table lives and how long it lives.
//
// This is the largest question #19 owns. ADR-0007 deliberately did not touch
// it, because it interacts with #16's schema cache rather than with
// precedence. Three candidates, each run against a real compile and a real
// dump rather than reasoned about:
//
//   R6  global and mutable
//   R7  scoped to a value, with the schema cache keyed by it
//   R8  global, frozen at the first compile
//
// Every fixture here registers AFTER a schema has been compiled, because a
// fixture that registers first cannot see the question at all. That is the
// shape of mistake the last six sessions each made once.

import (
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// --- the compiled schema, and the caches -----------------------------------

// compiledSchema is the part of #16 this probe needs: an address set computed
// from reflect.TypeFor[T]() alone, plus the codec RESOLVED per leaf, which is
// what the research recommends and what R12 measures.
type compiledSchema struct {
	addrs []Path
	codec map[string]string // address -> the codec that claimed it, for display
	err   error
}

func compileSchema(t reflect.Type) *compiledSchema {
	addrs, err := compile(t)
	s := &compiledSchema{addrs: addrs, err: err, codec: map[string]string{}}
	for _, p := range addrs {
		s.codec[p.String()] = "" // filled by R12's resolved form
	}
	return s
}

// --- R6: global and mutable -------------------------------------------------

var r6Cache sync.Map // reflect.Type -> *compiledSchema

func r6Compile(t reflect.Type) (*compiledSchema, bool) {
	if v, ok := r6Cache.Load(t); ok {
		return v.(*compiledSchema), true
	}
	s := compileSchema(t)
	r6Cache.Store(t, s)
	return s, false
}

type R6Backend struct {
	Host string
	Port int
}

func runR6() {
	fmt.Println("--- R6a: a refusal computed before a registration is cached as a refusal ---")
	type withAddr struct{ A netip.Addr }
	ta := reflect.TypeFor[withAddr]()

	s, hit := r6Compile(ta)
	fmt.Printf("    compile #1, nothing registered   hit=%-5v err=%v\n", hit, s.err)

	reg := NewRegistry()
	_ = reg.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	withRegistry(reg, func() {
		s2, hit2 := r6Compile(ta)
		fmt.Printf("    compile #2, netip.Addr REGISTERED hit=%-5v err=%v\n", hit2, s2.err)
		fresh := compileSchema(ta)
		fmt.Printf("    the same compile, uncached                    err=%v addrs=%v\n",
			fresh.err, fresh.addrs)
	})
	fmt.Println("    ^ loud, at least. A user who registers after a first Load gets an")
	fmt.Println("      error naming a type they can see they registered, which is")
	fmt.Println("      infuriating but not dangerous.")

	fmt.Println("\n--- R6b: the dangerous one. A cached schema that SUCCEEDS is silently stale ---")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	type withIP struct{ IP net.IP }
	ti := reflect.TypeFor[withIP]()
	v := reflect.ValueOf(withIP{net.ParseIP("192.0.2.1")})

	s3, _ := r6Compile(ti)
	d3, _ := dump(v)
	fmt.Printf("    compile #1, chain claims net.IP  addrs=%v  /IP=%s\n",
		s3.addrs, d3[Path{}.Name("IP")].GoString())

	ipReg := NewRegistry()
	_ = ipReg.Register(TypeCodec(VBytes,
		func(ip net.IP) (Value, error) { return Bytes(ip), nil },
		func(val Value) (net.IP, error) { b, err := val.AsBytes(); return net.IP(b), err }))
	withRegistry(ipReg, func() {
		s4, hit := r6Compile(ti)
		d4, _ := dump(v)
		fmt.Printf("    compile #2, net.IP REGISTERED    hit=%-5v addrs=%v  /IP=%s\n",
			hit, s4.addrs, d4[Path{}.Name("IP")].GoString())
	})
	fmt.Println("    ^ the address set happens to agree, so nothing complains, and the")
	fmt.Println("      value written to the plane is the codec the user registered. The")
	fmt.Println("      cache did not go stale here because this prototype resolves the")
	fmt.Println("      codec at DUMP time. #16 and the research both say to resolve it at")
	fmt.Println("      COMPILE time, and that is the version that goes wrong:")
	r6ResolvedAtCompile(ti, v, ipReg)

	fmt.Println("\n--- R6c: and the address set itself goes stale, which is worse ---")
	type withBackend struct{ B R6Backend }
	tb := reflect.TypeFor[withBackend]()
	s5, _ := r6Compile(tb)
	fmt.Printf("    compile #1, R6Backend unregistered  addrs=%v\n", s5.addrs)

	bReg := NewRegistry()
	_ = bReg.Register(StringCodec(
		func(b R6Backend) string { return fmt.Sprintf("%s:%d", b.Host, b.Port) },
		func(s string) (R6Backend, error) {
			var b R6Backend
			_, err := fmt.Sscanf(s, "%s", &b.Host)
			return b, err
		}))
	withRegistry(bReg, func() {
		s6, hit := r6Compile(tb)
		fresh := compileSchema(tb)
		fmt.Printf("    compile #2, R6Backend REGISTERED    hit=%-5v addrs=%v\n", hit, s6.addrs)
		fmt.Printf("    the same compile, uncached                       addrs=%v\n", fresh.addrs)
		bv := reflect.ValueOf(withBackend{R6Backend{"h", 80}})
		d, _ := dump(bv)
		fmt.Printf("    what dump actually writes:                       %v\n", sortedAddrs(d))
	})
	fmt.Println("    ^ THIS is the failure that matters. The cached address set is")
	fmt.Println("      [/B/Host /B/Port] and the dump writes [/B]. ADR-0004 hands the")
	fmt.Println("      STATIC address set to Bind before any I/O, so:")
	fmt.Println("        - ADR-0003's prefix-free check ran over a set that does not exist")
	fmt.Println("        - the driver's key function was checked for injectivity over the")
	fmt.Println("          wrong set, and /B is not in the table it precomputed")
	fmt.Println("        - the write is refused as a driver error, naming an address the")
	fmt.Println("          user's schema legitimately has")
	fmt.Println("      A global mutable table makes every one of those a function of when")
	fmt.Println("      the first Load happened.")
}

// r6ResolvedAtCompile is the version #16 and the research both recommend: the
// codec is a function pointer stored in the schema. It is what makes a stale
// cache silent rather than merely wrong.
func r6ResolvedAtCompile(t reflect.Type, v reflect.Value, later *Registry) {
	type resolved struct {
		addr Path
		enc  func(reflect.Value) (Value, error)
	}
	build := func() []resolved {
		addrs, _ := compile(t)
		out := make([]resolved, 0, len(addrs))
		for _, p := range addrs {
			ft := t.Field(0).Type
			c, ok := identityLookup(ft)
			if !ok {
				cc, ok2 := activeChainCodec(ft)
				if ok2 {
					out = append(out, resolved{p, cc.enc})
					continue
				}
			} else {
				out = append(out, resolved{p, c.enc})
				continue
			}
			out = append(out, resolved{p, encLeaf})
		}
		return out
	}
	baked := build() // compiled once, before the registration

	withRegistry(later, func() {
		got, _ := baked[0].enc(v.Field(0))
		fresh := build()
		want, _ := fresh[0].enc(v.Field(0))
		fmt.Printf("      codec baked into the schema at compile #1 -> %s\n", got.GoString())
		fmt.Printf("      codec the registration asked for          -> %s\n", want.GoString())
		fmt.Println("      ^ no error, no diagnostic, and the plane now holds the")
		fmt.Println("        representation the user replaced. Silent.")
	})
}

// --- R7: scoped, and what the schema cache must be keyed by -----------------

func runR7() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	type withIP struct{ IP net.IP }
	ti := reflect.TypeFor[withIP]()

	asText := NewRegistry()
	_ = asText.Register(StringCodec(net.IP.String, func(s string) (net.IP, error) {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("bad ip %q", s)
		}
		return ip, nil
	}))
	asBytes := NewRegistry()
	_ = asBytes.Register(TypeCodec(VBytes,
		func(ip net.IP) (Value, error) { return Bytes(ip), nil },
		func(val Value) (net.IP, error) { b, err := val.AsBytes(); return net.IP(b), err }))

	fmt.Println("--- R7a: keying the cache by reflect.Type alone ---")
	var byTypeOnly sync.Map
	get := func(r *Registry) string {
		var out string
		withRegistry(r, func() {
			if v, ok := byTypeOnly.Load(ti); ok {
				out = v.(string) + "   (cache hit)"
				return
			}
			d, _ := dump(reflect.ValueOf(withIP{net.ParseIP("192.0.2.1")}))
			out = d[Path{}.Name("IP")].GoString()
			byTypeOnly.Store(ti, out)
		})
		return out
	}
	fmt.Printf("    registry A (text codec)  -> %s\n", get(asText))
	fmt.Printf("    registry B (bytes codec) -> %s\n", get(asBytes))
	fmt.Println("    ^ ADR-0004 measured this exact shape once already: EnvSource{Sep:\"__\"}")
	fmt.Println("      and EnvSource{Sep:\"_\"} shared a cache entry and the second loaded")
	fmt.Println("      the wrong keys. Same defect, one layer up.")

	fmt.Println("\n--- R7b: keying by the registry VALUE ---")
	func() {
		defer func() { fmt.Printf("    map[struct{reflect.Type; Registry}] -> PANIC: %v\n", recover()) }()
		type key struct {
			t reflect.Type
			r Registry
		}
		m := map[any]int{}
		m[key{ti, Registry{byType: asText.byType}}] = 1
		fmt.Println("    no panic")
	}()
	fmt.Println("    ^ ADR-0004's second measurement, reproduced here: `a contract whose")
	fmt.Println("      correctness depends on a driver author supplying the right identity")
	fmt.Println("      is a prose rule with a runtime panic behind it`. A registry is")
	fmt.Println("      nothing BUT func fields and maps, so it is the worst case of that.")

	fmt.Println("\n--- R7c: keying by the registered TYPE SET, which is comparable ---")
	sig := func(r *Registry) string {
		var ts []string
		for t := range r.byType {
			ts = append(ts, t.String())
		}
		sortStrings(ts)
		return fmt.Sprint(ts)
	}
	fmt.Printf("    signature(A) = %s\n", sig(asText))
	fmt.Printf("    signature(B) = %s\n", sig(asBytes))
	fmt.Printf("    equal? %v  -> two registries that disagree about net.IP share an entry\n",
		sig(asText) == sig(asBytes))
	fmt.Println("    ^ a content key can only see what is comparable, and the codec is the")
	fmt.Println("      part that differs and the part that is not comparable. Any content")
	fmt.Println("      hash has this hole, because Go cannot compare two funcs.")

	fmt.Println("\n--- R7d: keying by the registry POINTER, which does work ---")
	type k2 struct {
		t reflect.Type
		r *Registry
	}
	var byPtr sync.Map
	get2 := func(r *Registry) string {
		var out string
		withRegistry(r, func() {
			key := k2{ti, r}
			if v, ok := byPtr.Load(key); ok {
				out = v.(string) + "   (cache hit)"
				return
			}
			d, _ := dump(reflect.ValueOf(withIP{net.ParseIP("192.0.2.1")}))
			out = d[Path{}.Name("IP")].GoString()
			byPtr.Store(key, out)
		})
		return out
	}
	fmt.Printf("    registry A -> %s\n", get2(asText))
	fmt.Printf("    registry B -> %s\n", get2(asBytes))
	fmt.Printf("    registry A -> %s\n", get2(asText))

	fmt.Println("\n--- R7e: but the pointer key is sound only if the registry is FROZEN ---")
	late := NewRegistry()
	fmt.Printf("    compile with an empty registry     -> %s\n", get2(late))
	_ = late.Register(TypeCodec(VBytes,
		func(ip net.IP) (Value, error) { return Bytes(ip), nil },
		func(val Value) (net.IP, error) { b, err := val.AsBytes(); return net.IP(b), err }))
	fmt.Printf("    register into it, compile again    -> %s\n", get2(late))
	fmt.Println("    ^ same pointer, different contents, same cache entry. R6's staleness")
	fmt.Println("      returns in full. So the pointer key does not remove the freeze; it")
	fmt.Println("      makes the freeze PER REGISTRY instead of per process, which is the")
	fmt.Println("      whole difference between R7 and R8.")

	fmt.Println("\n--- R7f: what a per-CALL registry costs, if the registry is per call ---")
	var entries atomic.Int64
	var cache sync.Map
	for i := range 10000 {
		r := NewRegistry()
		_ = r.Register(StringCodec(netip.Addr.String, netip.ParseAddr))
		_ = i
		if _, loaded := cache.LoadOrStore(k2{ti, r}, &compiledSchema{}); !loaded {
			entries.Add(1)
		}
	}
	fmt.Printf("    10000 per-call registries -> %d cache entries, none evictable\n", entries.Load())
	fmt.Println("    ^ the research's own survey of eight stdlib type caches found NO")
	fmt.Println("      eviction anywhere, and states why: `the cache is bounded by the set")
	fmt.Println("      of types the program statically declares, so it converges after")
	fmt.Println("      warmup`. Keying by a per-call value destroys exactly that property.")
	fmt.Println("      A registry has to be long-lived, which is a real constraint on how")
	fmt.Println("      #16 spells the entry point and not a free choice.")

	fmt.Println("\n--- R7g: the cost of the extra key component, measured ---")
	r := asText
	hot := &sync.Map{}
	hot.Store(ti, &compiledSchema{})
	hot.Store(k2{ti, r}, &compiledSchema{})
	base := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			hot.Load(ti)
		}
	})
	pair := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			hot.Load(k2{ti, r})
		}
	})
	plainMap := map[k2]*compiledSchema{{ti, r}: {}}
	plain := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_ = plainMap[k2{ti, r}]
		}
	})
	fmt.Printf("    sync.Map hit, key reflect.Type            %6.1f ns/op\n", float64(base.NsPerOp()))
	fmt.Printf("    sync.Map hit, key {reflect.Type, *Reg}    %6.1f ns/op\n", float64(pair.NsPerOp()))
	fmt.Printf("    plain map hit, key {reflect.Type, *Reg}   %6.1f ns/op\n", float64(plain.NsPerOp()))
	fmt.Println("    ^ the pair key costs real nanoseconds in a sync.Map, because a")
	fmt.Println("      two-word struct key boxes into an interface where a reflect.Type")
	fmt.Println("      already is one. It is off #16's path once per Load rather than per")
	fmt.Println("      leaf, against ADR-0003's 476 ns twelve-key load, and #16 has the")
	fmt.Println("      cheaper shape available anyway: hang the per-type cache OFF the")
	fmt.Println("      registry, so the outer lookup is a pointer dereference and the")
	fmt.Println("      inner one is keyed by reflect.Type alone.")
	perReg := &sync.Map{}
	perReg.Store(ti, &compiledSchema{})
	nested := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			perReg.Load(ti)
		}
	})
	fmt.Printf("    registry.cache.Load(reflect.Type)         %6.1f ns/op\n", float64(nested.NsPerOp()))
}

// --- R8: global, frozen at the first compile --------------------------------

var r8Frozen atomic.Bool
var r8Table = map[reflect.Type]bool{}
var r8Mu sync.Mutex

func r8Register(name string) error {
	r8Mu.Lock()
	defer r8Mu.Unlock()
	if r8Frozen.Load() {
		return fmt.Errorf(
			"ferry: %s: registration is closed; the first schema was compiled at %s", name, r8FirstCompile)
	}
	r8Table[reflect.TypeFor[netip.Addr]()] = true
	return nil
}

var r8FirstCompile string

func r8CompileOnce(where string) {
	if r8Frozen.CompareAndSwap(false, true) {
		r8FirstCompile = where
	}
}

func runR8() {
	fmt.Println("--- R8a: the model. One table, closed by the first compile ---")
	fmt.Printf("    register before any compile   -> %v\n", r8Register("netip.Addr"))
	r8CompileOnce("main.go:42, ferry.Load[Config]")
	fmt.Printf("    register after the first Load -> %v\n", r8Register("net.IP"))
	fmt.Println("    ^ loud, early, and it removes R6 entirely: nothing can be stale")
	fmt.Println("      because nothing can change after anything was compiled.")

	fmt.Println("\n--- R8b: what it costs. Two tests wanting different codecs for one type ---")
	fmt.Println("    Under a global table this is not awkward, it is unwritable:")
	fmt.Println("      func TestAsText(t *testing.T)  { ferry.Register(ipAsText);  ... }")
	fmt.Println("      func TestAsBytes(t *testing.T) { ferry.Register(ipAsBytes); ... }")
	fmt.Println("      -> the second returns `net.IP is already registered`, and if the")
	fmt.Println("         package compiled a schema in any earlier test it returns")
	fmt.Println("         `registration is closed` instead, depending on test order.")
	fmt.Println("    Measured on this prototype's own probes: R6, R7 and R11 each need a")
	fmt.Println("    DIFFERENT codec for net.IP or for one map key type, in one process.")
	fmt.Println("    Under R8 this file could not have been written.")

	fmt.Println("\n--- R8c: and the freeze point is not a place anyone can see ---")
	fmt.Println("    The first compile is inside whichever Load or Dump ran first, which")
	fmt.Println("    across packages is init() order, which is the import graph's.")
	fmt.Println("    ADR-0007 already refused precedence-by-init-order for the same")
	fmt.Println("    reason (R5b). A freeze POINT decided by init order is the same")
	fmt.Println("    property in a different costume: correct today, broken by an import")
	fmt.Println("    a dependency adds.")

	fmt.Println("\n--- R8d: the one thing R8 has that R7 does not ---")
	fmt.Println("    Zero configuration. `ferry.Load[Config](ctx, src)` with a package-")
	fmt.Println("    level Register in an init() is the shortest thing a user can write,")
	fmt.Println("    and it is what xload, encoding/gob and database/sql all offer.")
	fmt.Println("    R7 has to answer that, and the answer is that a DEFAULT registry is")
	fmt.Println("    a registry: core ships one, package-level Register writes to it, and")
	fmt.Println("    it freezes on first use exactly like any other. The scoped form is")
	fmt.Println("    then the escape hatch a test reaches for rather than the only way in.")
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
