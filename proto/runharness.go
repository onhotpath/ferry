package main

// The three planes ADR-0005 requires, "because each catches a class the others
// cannot", now built as real Source/Sink pairs so the harness can run through
// `Dump` and `Load` rather than through the superseded walk. #41 items 6 and 7.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// --- the memory plane --------------------------------------------------------
//
// ADR-0005: "the only plane that adds nothing of its own", which is where core's
// value-fidelity guarantee is stated. ADR-0002 admits a map[Path]Value to core
// as apparatus rather than as a driver, and this is that apparatus wearing the
// two interfaces so it can be handed to the entry point.

type MemSink struct{ M map[Path]Value }

func (s MemSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return memWriter{s.M}, nil }, nil
}

type memWriter struct{ m map[Path]Value }

func (w memWriter) Set(_ context.Context, p Path, v Value) error {
	if v.Kind() == VAbsent {
		return nil
	}
	w.m[p] = v
	return nil
}

type MemSource struct{ M map[Path]Value }

func (s MemSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return mapReader{s.M}, nil }, nil
}

func memoryPlane() Plane {
	return Plane{
		Name:  "memory plane (ferrytest)",
		Kinds: allKinds,
		Open: func() (FSource, FSink) {
			m := map[Path]Value{}
			return MemSource{m}, MemSink{m}
		},
	}
}

// --- the real driver ---------------------------------------------------------
//
// ADR-0005: "The memory plane alone would have proven nothing about base64, and
// it was the YAML driver's emitter that surfaced the !!binary defect."

func yamlPlane(dir string) Plane {
	n := 0
	return Plane{
		Name:  "yaml driver, real files",
		Kinds: allKinds,
		Open: func() (FSource, FSink) {
			n++
			p := filepath.Join(dir, fmt.Sprintf("c%d.yaml", n))
			return FYAMLSource{Path: p}, FYAMLSink{Path: p}
		},
	}
}

// --- the flattening plane ----------------------------------------------------
//
// The third column, and ADR-0005 says it is the point. Its Sink and Source are
// in flat.go, with the kind declaration.

func flatPlane() Plane {
	return Plane{
		Name:  "flattening plane (env/query/kv shaped)",
		Kinds: flatKinds,
		Open: func() (FSource, FSink) {
			st := NewFlatStore()
			return FlatSource{st}, FlatSink{st}
		},
	}
}

// --- the LEGACY map transforms -----------------------------------------------
//
// `identityPlane`, `flatten` and `yamlCross` are the shape the harness used
// before #41 item 6 repointed it at the entry point: a map[Path]Value ->
// map[Path]Value function. They are kept because the R and P suites' own
// probes - R10's registration proof, R15's three-plane table, R17's usage
// walkthrough - are written against a transform rather than against a Source
// and a Sink, and rewriting a probe to preserve its own output is churn.
//
// The round-trip harness no longer uses any of them. That is the point of the
// item: a transform cannot be handed to Load[T], so a harness built on one
// cannot run through the engine.
//
// NOTE ON `flatten`: it maps Null onto String(""), which is exactly the silent
// mangling ADR-0005's kind declaration exists to prevent, and it is why the
// published flat-plane numbers move once the declaration is real (see flat.go
// and the #41 report). It stays here unchanged because R15d's own probe is a
// demonstration OF that mangling and comments on it in place.

func identityPlane(in map[Path]Value) (map[Path]Value, error) { return in, nil }

func flatten(in map[Path]Value) (map[Path]Value, error) {
	out := make(map[Path]Value, len(in))
	for p, v := range in {
		switch v.Kind() {
		case VAbsent:
			// not stored
		case VNull:
			// A flat plane has no null. ADR-0004's own table says so: FOO= is a
			// zero-length string, not a null. So the container marker cannot
			// survive, and this is where that costs.
			out[p] = String("")
		default:
			out[p] = String(v.Text())
		}
	}
	return out, nil
}

// yamlCross is the yaml round trip in the transform shape, which is what
// yamlPlane used to be before it became a Plane.
func yamlCross(dir string) func(map[Path]Value) (map[Path]Value, error) {
	n := 0
	return func(in map[Path]Value) (map[Path]Value, error) {
		n++
		ctx := context.Background()
		p := filepath.Join(dir, fmt.Sprintf("x%d.yaml", n))
		as := NewAddressSet(sortedAddrs(in))
		ow, err := (FYAMLSink{Path: p}).Bind(as)
		if err != nil {
			return nil, err
		}
		if err := fDump(ctx, ow, in, as); err != nil {
			return nil, err
		}
		or, err := (FYAMLSource{Path: p}).Bind(as)
		if err != nil {
			return nil, err
		}
		return fLoad(ctx, or, as)
	}
}

func runHarness() {
	dir, _ := os.MkdirTemp("", "ferryh")
	defer os.RemoveAll(dir)
	for _, plane := range []Plane{memoryPlane(), yamlPlane(dir)} {
		fmt.Printf("\n--- %s ---\n", plane.Name)
		r := RoundTrip(plane, CoreTypes()...)
		for _, l := range r.Lines {
			fmt.Println(l)
		}
		fmt.Printf("  %s\n", r.summary())
	}
}
