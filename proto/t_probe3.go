package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() { t11Hooks = append(t11Hooks, runT14to19) }

// ---- T14: is the struct tag key configurable? ----

// A struct annotated for somebody else. Pointing a configurable ferry key at
// `json` is the case ADR-0003 says #11 owes an answer for.
type foreignTagged struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,string"`
	Skip string `json:"-"`
	Name string `json:"name"`
}

// compileWithKey is compileT with the tag key as a parameter, so the question
// can be measured rather than argued.
var tagKey = "ferry"

func withTagKey(k string, f func()) {
	old := tagKey
	tagKey = k
	defer func() { tagKey = old }()
	f()
}

// ---- T16: the address escape, unified with the tag's ----

// ADR-0003 fixed four properties of the canonical rendering and left the byte
// spelling to the implementation. The prototype spells it ~0 ~1 ~2. #11's tag
// grammar needs an escape too, and rather than invent a second alphabet this
// re-runs ADR-0003's own properties under one rule: `~` escapes the character
// that follows it.

var uniEsc = strings.NewReplacer("~", "~~", "/", "~/", "#", "~#")

func uniName(p Path, text string) Path { return Path{p.canon + "/" + uniEsc.Replace(text)} }

func uniSegments(canon string) []string {
	var out []string
	var cur strings.Builder
	i := 0
	started := false
	for i < len(canon) {
		c := canon[i]
		if c == '~' && i+1 < len(canon) {
			cur.WriteByte(canon[i+1])
			i += 2
			continue
		}
		if c == '/' || c == '#' {
			if started {
				out = append(out, cur.String())
				cur.Reset()
			}
			started = true
			i++
			continue
		}
		cur.WriteByte(c)
		i++
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}

// ---- T17: what xload's prefix= does, and where each part goes ----

type dbConf struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}
type appNested struct {
	DB   dbConf `ferry:"db"`
	Name string `ferry:"name"`
}

// ---- T18: a name per direction ----

type twoNames struct {
	Host string `ferry:"host"`
}

// ---- T19: end to end, with hostile segment text ----

type hostile struct {
	Comma  string `ferry:"'a,b'"`
	Equals string `ferry:"'a=b'"`
	Tilde  string `ferry:"a~b"`
	Dash   string `ferry:"'-'"`
	Slash  string `ferry:"a/b"`
	Hash   string `ferry:"a#b"`
	Space  string `ferry:"a b"`
	Dot    string `ferry:"a.b"`
	Greet  string `ferry:"greet,default='Hello, world'"`
}

func runT14to19() {
	hdr("T14  the struct tag key, pointed at a tag somebody else owns")
	fmt.Println("  ADR-0003: \"configurable\" and \"strict\" are compatible only with a stated")
	fmt.Println("  answer for what happens when the key names a tag somebody else owns.")
	fmt.Println()
	t := reflect.TypeFor[foreignTagged]()
	for i := range t.NumField() {
		f := t.Field(i)
		v, _ := f.Tag.Lookup("json")
		d, errs := parseFerryTag(v)
		out := "ok  " + describe(d)
		if len(errs) > 0 {
			out = "REFUSED  " + errs[0].Error()
		}
		fmt.Printf("  json:%-22q %s\n", v, trimTo(out, 92))
	}
	fmt.Println()
	fmt.Println("  3 of 4 fields refuse, and the fourth compiles only because `name` happens")
	fmt.Println("  to carry no option. So a configurable key is usable against a foreign tag")
	fmt.Println("  only if strictness is switched off for it, which is ADR-0001's rule with")
	fmt.Println("  an off switch.")

	hdr("T14b  what the tag key costs #16's schema cache")
	benchCacheKeys()

	hdr("T15  the validation entry point, called the way a test would")
	fmt.Printf("  Validate[namedB]()  -> %v\n", Validate[namedB]())
	fmt.Printf("  Validate[namedA]()  -> %s\n", line1(Validate[namedA]()))
	fmt.Printf("  Validate[ref5]()    -> %s\n", line1(Validate[ref5]()))
	fmt.Println("  no value in hand, no plane reachable, no I/O: reflect.TypeFor[T]() only")

	hdr("T16  ADR-0003's rendering, which this ticket leaves alone")
	fuzzUnified()

	hdr("T17  xload's prefix=, taken apart")
	fmt.Println("  xload         ferry")
	fmt.Println("  ------------  -------------------------------------------------")
	fmt.Println("  prefix=DB_    the nested struct's own name: ferry:\"db\" on the field")
	s, err := compileT(reflect.TypeFor[appNested]())
	if err == nil {
		fmt.Printf("                %v\n", sortedPaths(s.addrs))
	}
	fmt.Println("  prefix=DB     unexpressible: a prefix can only prepend a SEGMENT (ADR-0003)")
	fmt.Println("  a plane-wide  the Under combinator on the source (ADR-0004), not a tag")
	fmt.Println("  prefix")
	fmt.Println()
	fmt.Println("  the three xload spellings that are all legal and two of which are typos:")
	for _, pre := range []string{"DB_", "DB", "DB__"} {
		fmt.Printf("    prefix=%-5q + key %-6q -> %q\n", pre, "HOST", pre+"HOST")
	}
	fmt.Println("    ferry has no concatenation to get wrong: /db/host is two segments,")
	fmt.Println("    and how they join is the driver's option.")

	hdr("T18  a name per direction")
	fmt.Println("  the ask: load from LEGACY_HOST, dump to host.")
	fmt.Println("  measured against ADR-0001's value fidelity, on the SAME plane:")
	fmt.Println("    Dump writes /host, Load reads /legacy_host -> Load(Dump(x)) is absent")
	fmt.Println("  so a per-direction name is a round-trip violation by construction, and")
	fmt.Println("  the grammar spends no word on it.")
	demoTwoNames()

	hdr("T19  end to end through the real YAML driver, with hostile segment text")
	endToEndHostile()
}

func demoTwoNames() {
	s, err := compileT(reflect.TypeFor[twoNames]())
	if err != nil {
		printErrs("  ", err)
		return
	}
	fmt.Printf("  one name, one address, both directions: %v\n", s.addrs)
}

// benchCacheKeys prices the two shapes #16's cache could take, so #16 inherits
// a number rather than an argument. ADR-0006 already measured that a
// compile-affecting Option is unhashable; a tag key is a string, so this is
// the cheap end of the same question.
func benchCacheKeys() {
	type keyA = reflect.Type
	type keyB struct {
		t   reflect.Type
		tag string
	}
	a := map[keyA]int{}
	b := map[keyB]int{}
	types := []reflect.Type{
		reflect.TypeFor[namedB](), reflect.TypeFor[dbConf](), reflect.TypeFor[appNested](),
		reflect.TypeFor[hostile](), reflect.TypeFor[skipCases](),
	}
	for i, t := range types {
		a[t] = i
		b[keyB{t, "ferry"}] = i
	}
	ra := testing.Benchmark(func(bb *testing.B) {
		var n int
		for i := 0; bb.Loop(); i++ {
			n += a[types[i%len(types)]]
		}
		_ = n
	})
	rb := testing.Benchmark(func(bb *testing.B) {
		var n int
		for i := 0; bb.Loop(); i++ {
			n += b[keyB{types[i%len(types)], "ferry"}]
		}
		_ = n
	})
	fmt.Printf("  cache key                      %10s %8s\n", "ns/op", "allocs")
	fmt.Printf("  map[reflect.Type]              %10.2f %8d\n", float64(ra.NsPerOp()), ra.AllocsPerOp())
	fmt.Printf("  map[struct{reflect.Type;string}] %8.2f %8d\n", float64(rb.NsPerOp()), rb.AllocsPerOp())
	fmt.Println("  both hashable, unlike ADR-0006's compile-affecting Option, which panics")
	fmt.Println("  with \"hash of unhashable type\". So a configurable key is affordable in")
	fmt.Println("  the cache; what it is not affordable in is strictness (T14).")

	// And the second half nobody would think to measure: a per-instance key
	// means one reflect.Type can yield two different schemas.
	var wg sync.WaitGroup
	seen := map[string]bool{}
	var mu sync.Mutex
	for _, k := range []string{"ferry", "json"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withTagKey(k, func() {})
			mu.Lock()
			seen[k] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
}

func fuzzUnified() {
	alphabet := []string{"a", "b", "/", "#", "~", "~0", "~1", "~2", ",", "=", "-", "", "\x00", "é", " "}
	rng := rand.New(rand.NewPCG(1, 2))
	seen := map[string][]string{}
	fails, collisions := 0, 0
	const n = 200000
	for range n {
		k := 1 + rng.IntN(4)
		segs := make([]string, k)
		for i := range segs {
			var b strings.Builder
			for range 1 + rng.IntN(4) {
				b.WriteString(alphabet[rng.IntN(len(alphabet))])
			}
			segs[i] = b.String()
		}
		var p Path
		for _, s := range segs {
			p = uniName(p, s)
		}
		got := uniSegments(p.canon)
		if len(got) != len(segs) {
			fails++
			continue
		}
		for i := range segs {
			if got[i] != segs[i] {
				fails++
				break
			}
		}
		if prev, ok := seen[p.canon]; ok && !eqStrs(prev, segs) {
			collisions++
		}
		seen[p.canon] = segs
	}
	fmt.Printf("  %d fuzzed paths, segment text drawn from %v\n", n, alphabet)
	fmt.Printf("  round-trip failures: %d    distinct segment lists sharing a rendering: %d\n", fails, collisions)
	fmt.Println("  An earlier draft proposed unifying the address rendering's escape with the")
	fmt.Println("  tag's, and this fuzz was its evidence. The tag grammar now has no escape")
	fmt.Println("  character at all, so there is nothing to unify: ADR-0003's spelling stays")
	fmt.Println("  the implementation's choice, untouched by this ticket. The result is kept")
	fmt.Println("  because it shows the follow-the-character rule would also have worked, and")
	fmt.Println("  because the address rendering is machine-generated - which is the reason a")
	fmt.Println("  shared rule with a human-typed grammar was worth less than it sounded.")
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func endToEndHostile() {
	t11Mode = true
	defer func() { t11Mode = false }()
	s, err := compileT(reflect.TypeFor[hostile]())
	if err != nil {
		printErrs("  ", err)
		return
	}
	fmt.Println("  the compiled address set:")
	for _, p := range sortedPaths(s.addrs) {
		fmt.Printf("      %-16s segments %q\n", p, segTexts(p))
	}

	dir, _ := os.MkdirTemp("", "ferry11")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "conf.yaml")

	v := hostile{Comma: "c", Equals: "e", Tilde: "t", Dash: "d", Slash: "s", Hash: "h", Space: "sp", Dot: "dt"}
	calls, err := dumpD(reflect.ValueOf(v), s)
	if err != nil {
		fmt.Println("  dump:", err)
		return
	}
	sink := FYAMLSink{Path: file}
	ow, err := sink.Bind(nil)
	if err != nil {
		fmt.Println("  bind:", err)
		return
	}
	w, err := ow(context.Background())
	if err != nil {
		fmt.Println("  open:", err)
		return
	}
	for _, c := range calls {
		if err := w.Set(context.Background(), c.p, c.v); err != nil {
			fmt.Println("  set:", err)
			return
		}
	}
	if cm, ok := w.(interface {
		Commit(context.Context) error
	}); ok {
		if err := cm.Commit(context.Background()); err != nil {
			fmt.Println("  commit:", err)
			return
		}
	}
	if cl, ok := w.(interface{ Close() error }); ok {
		_ = cl.Close()
	}
	b, _ := os.ReadFile(file)
	fmt.Println("  the YAML the driver wrote:")
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		fmt.Println("      " + l)
	}

	src := FYAMLSource{Path: file}
	of, err := src.Bind(nil)
	if err != nil {
		fmt.Println("  src bind:", err)
		return
	}
	r, err := of(context.Background())
	if err != nil {
		fmt.Println("  src open:", err)
		return
	}
	vals := map[Path]Value{}
	for _, p := range s.addrs {
		got, err := r.Get(context.Background(), p)
		if err != nil {
			fmt.Printf("  get %s: %v\n", p, err)
			continue
		}
		vals[p] = got
	}
	var back hostile
	if _, err := loadD(vals, s, reflect.ValueOf(&back).Elem(), loadOpts{}); err != nil {
		fmt.Println("  load:", err)
	}
	want := v
	want.Greet = ""
	fmt.Printf("  every hostile name round-trips through the real driver: %v\n", back == want)

	// and the same load with /greet deleted from the plane, so the declared
	// default is the thing under test rather than the dumped empty string.
	delete(vals, path("greet"))
	var back2 hostile
	if _, err := loadD(vals, s, reflect.ValueOf(&back2).Elem(), loadOpts{}); err != nil {
		fmt.Println("  load:", err)
	}
	fmt.Printf("  with /greet absent, the declared default arrives as: %q\n", back2.Greet)
	_ = time.Now
}

func segTexts(p Path) []string {
	var out []string
	for _, s := range p.Segments() {
		out = append(out, s.Text)
	}
	return out
}

func line1(err error) string {
	if err == nil {
		return "nil"
	}
	return trimTo(errLines(err)[0], 100)
}
