package main

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// spySource wraps a Source and records every question the walk asks.
type spySource struct {
	inner ferry.Source
	log   *[]string
}

func (s spySource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return spyReader{inner: r, log: s.log}, nil
	}, nil
}

type spyReader struct {
	inner ferry.Reader
	log   *[]string
}

func (r spyReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	v, err := r.inner.Get(ctx, addr)
	*r.log = append(*r.log, fmt.Sprintf("Get(%s) -> %s", addr, show(v)))

	return v, err
}

func (r spyReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	kids, err := r.inner.(ferry.Enumerator).Children(ctx, prefix)

	texts := make([]string, 0, len(kids))
	for _, k := range kids {
		texts = append(texts, k.String())
	}

	*r.log = append(*r.log, fmt.Sprintf("Children(%s) -> %v", prefix, texts))

	return kids, err
}

// sec2 dumps every shape, then loads the same plane back and shows what the
// walk asked at each container address and what came back.
func sec2() {
	head("2. What Load reads back, and what it asked to get there")

	mapped, err := ferrytest.Record(context.Background(), shapesValue())
	if err != nil {
		fmt.Println("Record:", err)

		return
	}

	var log []string

	src := spySource{inner: ferrytest.Static(maps.Clone(mapped)), log: &log}

	got, err := ferry.Load[shapes](context.Background(), src)
	if err != nil {
		fmt.Println("Load:", indent(err))
	}

	sub("the round trip")
	fmt.Printf("  in  %+v\n", shapesValue())
	fmt.Printf("  out %+v\n", got)
	fmt.Printf("  NilSlice==nil %v  EmptySlice==nil %v  NilMap==nil %v  EmptyMap==nil %v\n",
		got.NilSlice == nil, got.EmptySlice == nil, got.NilMap == nil, got.EmptyMap == nil)
	fmt.Printf("  NilPtr==nil %v  SetPtr %v  NilPtrSl==nil %v\n", got.NilPtr == nil, got.SetPtr, got.NilPtrSl == nil)

	sub("every question the walk asked, at a container address")

	for _, line := range log {
		if isContainerQuestion(line) {
			fmt.Println("  " + line)
		}
	}
}

// isContainerQuestion keeps the lines that name one of the ten container
// addresses and drops the leaf reads under them.
func isContainerQuestion(line string) bool {
	for _, name := range []string{
		"nilslice", "emptyslice", "fullslice", "nilmap", "emptymap",
		"fullmap", "nilptr", "setptr", "nilptrslice", "array",
	} {
		if line == fmt.Sprintf("Get(/%s) -> Absent", name) ||
			line == fmt.Sprintf("Get(/%s) -> Null", name) ||
			hasPrefix(line, "Children(/"+name+")") {
			return true
		}
	}

	return false
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// caseThreeFixture stages the four shapes ADR-0014 case 3 names, exactly as
// Dump leaves them on a plane.
func caseThreeFixture() map[ferry.Path]ferry.Value {
	return map[ferry.Path]ferry.Value{
		ferry.At("list").Elem(0): ferry.String("a"),
		ferry.At("empty"):        ferry.Null(),
		ferry.At("emptymap"):     ferry.Null(),
	}
}

// sec3 marks each of case 3's four claims against the measurement.
func sec3() {
	head("3. Case 3's four claims, measured")

	store := caseThreeFixture()
	src := ferrytest.Static(store)

	open, err := src.Bind(ferry.NewAddressSet(
		ferry.At("list"), ferry.At("empty"), ferry.At("emptymap"), ferry.At("missing")))
	if err != nil {
		fmt.Println("Bind:", err)

		return
	}

	r, err := open(context.Background())
	if err != nil {
		fmt.Println("open:", err)

		return
	}

	claims := []struct {
		what string
		at   ferry.Path
	}{
		{"a populated list", ferry.At("list")},
		{"an empty list", ferry.At("empty")},
		{"an empty map", ferry.At("emptymap")},
		{"a missing key", ferry.At("missing")},
	}

	fmt.Printf("  %-20s %-28s %-10s %s\n", "shape", "address", "Get", "case 3 says Absent")

	for _, c := range claims {
		v, err := r.Get(context.Background(), c.at)
		verdict := "TRUE"

		if v.Kind() != ferry.KindAbsent {
			verdict = "FALSE"
		}

		fmt.Printf("  %-20s %-28s %-10s %s (err %v)\n", c.what, c.at, show(v), verdict, err)
	}

	sub("the same plane, but written by a human into a tree: an empty sequence node")
	fmt.Println("  A tree driver reading `empty: []` has no address under /empty and no null to")
	fmt.Println("  report at it, so it answers Absent. That is ADR-0005's own measured table.")
	fmt.Println("  It is a different fixture from the one above, which ferry's own Dump wrote.")

	sub("what each of the two spellings loads back into []string")

	for _, spelled := range []struct {
		name  string
		store map[ferry.Path]ferry.Value
	}{
		{"nothing at /empty (a human's `empty: []`)", map[ferry.Path]ferry.Value{}},
		{"Null at /empty (what ferry.Dump writes)", map[ferry.Path]ferry.Value{ferry.At("empty"): ferry.Null()}},
	} {
		type onlyEmpty struct {
			Empty []string `ferry:"empty"`
		}

		v, err := ferry.Load[onlyEmpty](context.Background(), ferrytest.Static(spelled.store))
		fmt.Printf("  %-42s -> %#v nil=%v err=%v\n", spelled.name, v.Empty, v.Empty == nil, err)
	}
}

// keysOf renders a map's addresses in order, for a report.
func keysOf(m map[ferry.Path]ferry.Value) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k.String())
	}

	slices.Sort(out)

	return out
}
