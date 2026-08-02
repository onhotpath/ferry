package main

// B5: the second decision on this surface.
//
// ADR-0006 fixed that the presence observation survives the walk, and left the
// spelling here: "whether the observation is spelled as a callback, a recorder,
// or a returned report is an API question that belongs with the caller-facing
// lifecycle, which is #25's."
//
// Three candidates, and a fourth the ADR did not list. The fourth is not a
// spelling of a core feature; it is the observation being a thing the contract
// already carries, so core spells nothing.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type B5Conf struct {
	Name string `ferry:"name"`
	DB   struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	} `ferry:"db"`
}

// --- the fourth candidate: a Source that records what it was asked ------------

// BObserving is a Source wrapping a Source. It is fifteen lines, it uses no
// ferry API a driver author does not already have, and core ships nothing.
type BObserving struct {
	Src FSource
	Rec *BRecord
}

type BRecord struct {
	mu   sync.Mutex
	seen map[Path]Value
	ord  []Path
}

func NewRecord() *BRecord { return &BRecord{seen: map[Path]Value{}} }

func (r *BRecord) put(p Path, v Value) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.seen[p]; !dup {
		r.ord = append(r.ord, p)
	}
	r.seen[p] = v
}

func (r *BRecord) At(p Path) Value {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[p]
}

func (r *BRecord) All() []Path {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sortedPaths(append([]Path{}, r.ord...))
}

func (s BObserving) Bind(a *AddressSet) (FOpenFunc, error) {
	open, err := s.Src.Bind(a)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (FReader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return bObservingReader{r, s.Rec}, nil
	}, nil
}

type bObservingReader struct {
	r   FReader
	rec *BRecord
}

func (o bObservingReader) Get(ctx context.Context, p Path) (Value, error) {
	v, err := o.r.Get(ctx, p)
	o.rec.put(p, v)
	return v, err
}

func (o bObservingReader) Children(ctx context.Context, p Path) ([]Path, error) {
	if e, ok := o.r.(FEnumerator); ok {
		return e.Children(ctx, p)
	}
	return nil, nil
}

// --- the probe ---------------------------------------------------------------

func runB5() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "b5")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")
	write := func(body string) { os.WriteFile(p, []byte(body), 0o644) }

	fmt.Println("--- B5a: ADR-0006's own table, reproduced through each spelling ---")
	fmt.Println("    rows 2 and 3 are ONE struct and TWO observations, which is the")
	fmt.Println("    whole feature.")
	fmt.Printf("  %-22s %-24s %-16s %s\n", "plane", "struct", "Option callback", "Source wrapper")
	for _, tc := range []struct{ label, body string }{
		{"/db/port = 5432", "name: svc\ndb:\n  host: h\n  port: 5432\n"},
		{"/db/port deleted", "name: svc\ndb:\n  host: h\n"},
		{"/db/port = 0", "name: svc\ndb:\n  host: h\n  port: 0\n"},
	} {
		write(tc.body)
		probe := Path{}.Name("db").Name("port")

		var viaOpt Value
		cfg, err := Load[B5Conf](ctx, FYAMLSource{Path: p}, Observe(func(a Path, v Value) {
			if a == probe {
				viaOpt = v
			}
		}))
		if err != nil {
			fmt.Println("  load:", err)
			continue
		}
		rec := NewRecord()
		_, err = Load[B5Conf](ctx, BObserving{FYAMLSource{Path: p}, rec})
		if err != nil {
			fmt.Println("  load:", err)
			continue
		}
		fmt.Printf("  %-22s %-24s %-16s %s\n", tc.label,
			fmt.Sprintf("{Host:%s Port:%d}", cfg.DB.Host, cfg.DB.Port),
			viaOpt.GoString(), rec.At(probe).GoString())
	}

	fmt.Println("\n--- B5b: do the two see the same set? ---")
	write("name: svc\ndb:\n  host: h\n")
	var optSeen []Path
	var mu sync.Mutex
	rec := NewRecord()
	_, _ = Load[B5Conf](ctx, BObserving{FYAMLSource{Path: p}, rec}, Observe(func(a Path, _ Value) {
		mu.Lock()
		optSeen = append(optSeen, a)
		mu.Unlock()
	}))
	fmt.Printf("  Option callback saw : %v\n", sortedPaths(optSeen))
	fmt.Printf("  Source wrapper saw  : %v\n", rec.All())
	fmt.Printf("  identical           : %v\n", fmt.Sprint(sortedPaths(optSeen)) == fmt.Sprint(rec.All()))
	fmt.Println("  They are one set because the walk reaches the plane through exactly")
	fmt.Println("  one call, Reader.Get, and the Option's callback is a hook on that call.")

	fmt.Println("\n--- B5c: what only the wrapper can say ---")
	fmt.Println("    Under a FirstOf the Option reports the COMPOSED answer, because")
	fmt.Println("    that is all the walk ever sees. A wrapper goes where it is put.")
	write("name: from-file\ndb:\n  host: from-file\n  port: 1\n")
	qrec, frec := NewRecord(), NewRecord()
	var composed []string
	_, err := Load[B5Conf](BQueryContext(ctx, b5Query()),
		BFirstOf(BObserving{BQueryCtx{}, qrec}, BObserving{FYAMLSource{Path: p}, frec}),
		Observe(func(a Path, v Value) {
			composed = append(composed, fmt.Sprintf("%s=%s", a, v.GoString()))
		}))
	if err != nil {
		fmt.Println("  load:", err)
	}
	probe := Path{}.Name("name")
	fmt.Printf("  query child   at /name -> %s\n", qrec.At(probe).GoString())
	fmt.Printf("  yaml  child   at /name -> %s\n", frec.At(probe).GoString())
	fmt.Printf("  the Option    at /name -> %v\n", composed)
	fmt.Println("  \"which layer answered\" is a question only the wrapper can be asked,")
	fmt.Println("  and it is the question ADR-0001 milestones drift detection on.")

	fmt.Println("\n--- B5d: cost ---")
	write("name: svc\ndb:\n  host: h\n  port: 1\n")
	src := FYAMLSource{Path: p}
	rows := []struct {
		name string
		fn   func()
	}{
		{"no observation", func() { _, _ = Load[B5Conf](ctx, src) }},
		{"Option callback into a map", func() {
			m := map[Path]Value{}
			_, _ = Load[B5Conf](ctx, src, Observe(func(a Path, v Value) { m[a] = v }))
		}},
		{"Source wrapper, locked", func() {
			_, _ = Load[B5Conf](ctx, BObserving{src, NewRecord()})
		}},
	}
	for _, r := range rows {
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r.fn()
			}
		})
		fmt.Printf("  %-30s %8d ns %7d B %5d allocs\n", r.name, res.NsPerOp(),
			res.AllocedBytesPerOp(), res.AllocsPerOp())
	}
}

func b5Query() url.Values { return url.Values{"name": {"from-query"}} }
