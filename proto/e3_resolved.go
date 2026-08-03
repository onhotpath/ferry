package main

// E3: what goes into a leaf - data, or resolved behaviour.
//
// The research calls this "the single most transferable idea" and it recurs
// in every high-quality library it surveyed: json/v2 stores a per-field
// *arshaler and an isZero predicate resolved at schema-build time, validator
// stores the validator function pointer resolved at parse time.
//
// ADR-0009 measured one instance of it on ferry's own shape - 381 ns/op for a
// lookup per leaf per call against 283 ns/op resolved at compile - and said
// the performance is not the argument. This probe re-runs that and separates
// the three things a leaf could hold, because they do not cost the same and
// only one of them is large.

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type E3Inner struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

// Twelve leaves with two nested structs, which is research 5.3's shape.
type E3Conf struct {
	Name    string        `ferry:"name"`
	Port    int           `ferry:"port"`
	Ratio   float64       `ferry:"ratio"`
	On      bool          `ferry:"on"`
	Timeout time.Duration `ferry:"timeout"`
	Retries int           `ferry:"retries,default=3"`
	Level   string        `ferry:"level,default=info"`
	Region  string        `ferry:"region"`
	Auth    E3Inner       `ferry:"auth"`
	Cache   E3Inner       `ferry:"cache"`
}

func runE3() {
	ctx := context.Background()
	o := defaultOpts()
	t := reflect.TypeFor[E3Conf]()

	s, err := compileSchema2(t, o)
	if err != nil {
		fmt.Println("  compile err:", err)
		return
	}
	fmt.Printf("--- E3a: what one compile produces, for %d addresses ---\n", len(s.addrs))
	for _, p := range s.leafAddrs {
		fmt.Printf("  %s\n", p)
	}

	plane := map[Path]Value{}
	for _, p := range s.leafAddrs {
		plane[p] = String("x")
	}
	plane[Path{}.Name("port")] = Number("8080")
	plane[Path{}.Name("ratio")] = Number("3.5")
	plane[Path{}.Name("on")] = Bool(true)
	plane[Path{}.Name("timeout")] = String("30s")
	plane[Path{}.Name("retries")] = Number("5")

	bench := func(name string, f func()) testing.BenchmarkResult {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				f()
			}
		})
		fmt.Printf("  %-46s %7d ns/op %6d B/op %3d allocs/op\n",
			name, r.NsPerOp(), r.AllocedBytesPerOp(), r.AllocsPerOp())
		return r
	}

	fmt.Println("\n--- E3b: the cached schema against no cache at all (research 5.3) ---")
	rd := mapReader{plane}
	runLoad := func(sc *schema) {
		var out E3Conf
		w := &walker{dir: loadDir(rd, ctx, o), sch: serial, ctx: ctx}
		_, _ = w.walk(sc.root, reflect.ValueOf(&out).Elem(), Path{})
	}
	bench("compile per load, then walk", func() {
		sc, _ := compileSchema2(t, o)
		runLoad(sc)
	})
	bench("compile once, walk (cache HIT, through Load)", func() {
		_, _ = loadFrom(ctx, E3Conf{}, plane)
	})
	bench("compile once, walk (no cache lookup at all)", func() { runLoad(s) })
	bench("the compile alone", func() { _, _ = compileSchema2(t, o) })

	fmt.Println("\n--- E3c: the three things a leaf could hold, priced separately ---")

	// (1) the codec, resolved at compile against looked up per leaf per call.
	fmt.Println("  (1) the codec: a function pointer in the leaf, or ADR-0007's three")
	fmt.Println("      steps re-run per leaf per call, which is xload's five-arm switch.")
	unresolved := loadDir(rd, ctx, o)
	unresolved.leaf = func(n *node, v reflect.Value, at Path) (bool, error) {
		val, _ := rd.Get(ctx, at)
		if val.Kind() == VAbsent {
			return false, nil
		}
		// resolve the codec HERE, per leaf, per call
		c, ok := resolveLeaf(n.typ)
		if !ok {
			return false, fmt.Errorf("no codec")
		}
		return true, decLeafWith(c, val, v)
	}
	res := bench("codec resolved at compile", func() { runLoad(s) })
	unres := bench("codec resolved per leaf per call", func() {
		var out E3Conf
		w := &walker{dir: unresolved, sch: serial, ctx: ctx}
		_, _ = w.walk(s.root, reflect.ValueOf(&out).Elem(), Path{})
	})
	fmt.Printf("      -> %.2fx, over %d leaves\n",
		float64(unres.NsPerOp())/float64(res.NsPerOp()), s.leaves)

	// (2) the address, minted at compile against joined per leaf per call.
	fmt.Println("\n  (2) the address: ADR-0003 already priced this on the DRIVER side")
	fmt.Println("      (10.4 ns precomputed against 109 ns per call). On the SCHEMA side")
	fmt.Println("      the static address of a leaf is already in the node, and the only")
	fmt.Println("      thing minted per call is a DYNAMIC one, which no cache can hold.")
	// Looked up by ADDRESS rather than by position. #41: this read
	// s.root.fields[0] and s.root.fields[5], so it depended on the fixture's
	// declaration order matching the compiled field list - and #50's costing of
	// a per-node sort found that index 5 would then be a node with a nil `def`,
	// segfaulting this probe. n.fields order is load-bearing for anything that
	// indexes it, and only a comment could have said so.
	leafNode := e3FieldAt(s.root, "/name")
	bench("read the compiled Path out of the leaf", func() { _ = leafNode.shape })
	bench("re-mint /auth/user from segments", func() { _ = Path{}.Name("auth").Name("user") })

	// (3) the default, compiled to a Value against parsed per call.
	fmt.Println("\n  (3) the default: ADR-0006 requires the TEXT to be re-decoded per load,")
	fmt.Println("      because a cached Go value aliases across every load of one schema.")
	fmt.Println("      What IS compiled is the text-to-Value step and its validation.")
	defNode := e3FieldAt(s.root, "/retries")
	bench("declared default, already a Value", func() {
		var i int
		_ = decLeafWith(defNode.codec, *defNode.def, reflect.ValueOf(&i).Elem())
	})

	fmt.Println("\n--- E3d: and the same for Dump, which nothing has measured ---")
	v := E3Conf{Name: "svc", Port: 8080, Timeout: time.Second, Auth: E3Inner{"u", "p"}}
	bench("dump, compile per call", func() {
		sc, _ := compileSchema2(t, o)
		out := map[Path]Value{}
		w := &walker{dir: dumpDir(out), sch: serial, ctx: ctx}
		_, _ = w.walk(sc.root, reflect.ValueOf(v), Path{})
	})
	bench("dump, cached schema", func() { _, _ = dumpTo(ctx, v) })

	fmt.Println("\n  The absolute numbers are this prototype's and not ferry's: the compile")
	fmt.Println("  runs an unmemoised codec chain that probes method sets per type, and the")
	fmt.Println("  walk builds a Path string per segment. Research 5.3's own figures are")
	fmt.Println("  3343 ns uncached against 476 ns cached on a 12-key struct. What transfers")
	fmt.Println("  is the RATIO and its direction, which reproduce.")
	fmt.Println("\n  Two things to read out of this rather than one.")
	fmt.Println("  The compile is the whole cost, and resolving the codec into the leaf is a")
	fmt.Println("  smaller and separate win. ADR-0009 already said the performance is not the")
	fmt.Println("  argument for resolving at compile; E4 and ADR-0009's staleness result are.")
}

// e3FieldAt finds a compiled child by the address it minted, which is the only
// stable handle a probe has on one: the field list's ORDER is the walk's
// business and may change without any address changing.
func e3FieldAt(n *node, addr string) *node {
	for _, f := range n.fields {
		if f.node.shape.String() == addr {
			return f.node
		}
	}
	panic("e3: no compiled field at " + addr)
}
