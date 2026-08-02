package main

// W2: what an Enumerator can say about a plane that has no list type.
//
// ADR-0004 made `Enumerator` return ADDRESSES rather than names, and gave one
// reason: "measured, /limits yields Name segments and /tags yields Index
// segments, so the plane answers which composite it is rather than the caller
// guessing from base-10 text - the limitation ADR-0003 quotes jsontext.Pointer's
// own godoc admitting."
//
// The Windows Registry is a plane that CANNOT answer. It has subkeys and
// values and no array, so a []string and a map[string]string with numeric keys
// are byte-identical in a hive - W0 confirms the storage shape. So the
// Registry is the concrete test of a justification ADR-0004 wrote from a plane
// that happened to be able to answer.
//
// This probe needs no hive, because the question is what CORE does with the
// answer.

import (
	"context"
	"fmt"
	"strconv"
)

// wKindSource wraps the real Registry driver and lets a probe force what KIND
// its Children reports, so the question "does core read it" can be asked
// directly. Everything else is the driver from w_driver.go.
type wKindSource struct {
	Store wStore
	Base  string
	Kind  Kind
}

func (s wKindSource) Bind(a *AddressSet) (FOpenFunc, error) {
	kf := wRegKey{base: s.Base}
	if _, err := kf.bind(a.All()); err != nil {
		return nil, err
	}
	return func(context.Context) (FReader, error) {
		return wKindReader{&wRegReader{store: s.Store, kf: kf}, s.Kind}, nil
	}, nil
}

type wKindReader struct {
	r    *wRegReader
	kind Kind
}

func (r wKindReader) Get(ctx context.Context, p Path) (Value, error) { return r.r.Get(ctx, p) }

func (r wKindReader) Children(ctx context.Context, prefix Path) ([]Path, error) {
	kids, err := r.r.Children(ctx, prefix)
	if err != nil || r.kind == Name {
		return kids, err
	}
	out := make([]Path, 0, len(kids))
	for _, k := range kids {
		segs := k.Segments()
		last := segs[len(segs)-1]
		if i, err := strconv.Atoi(last.Text); err == nil {
			out = append(out, prefix.Index(i))
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

type WSliceConf struct {
	Tags []string `ferry:"tags"`
}

type WMapConf struct {
	Tags map[string]string `ferry:"tags"`
}

func runW2() {
	ctx := context.Background()

	fmt.Println("(a) a []string and a map[string]string are one thing in a hive")
	fmt.Println("    W0 measured the storage: a Registry key holds VALUES, each with a name")
	fmt.Println("    and a type, and no array type exists. So both of these produce three")
	fmt.Println("    values named \"0\", \"1\", \"2\" under a subkey `tags`, and a reader")
	fmt.Println("    enumerating that key gets three strings back and nothing else.")

	base := `Software\Acme`
	hive := newFake()
	for i, v := range []string{"a", "b", "c"} {
		_ = hive.SetValue(base+`\tags`, strconv.Itoa(i), wVal{typ: wSZ, s: v})
	}
	fmt.Printf("    the hive: %v\n", hive.dump())

	fmt.Println("\n(b) so what happens when the plane answers with the WRONG kind?")
	fmt.Println("    Here is the same plane read by a source that reports every child as a")
	fmt.Println("    Name segment - which is all a Registry can honestly report - into a")
	fmt.Println("    []string field, whose addresses ferry minted as Index segments.")
	for _, k := range []struct {
		label string
		kind  Kind
	}{{"Name (all a Registry can say)", Name}, {"Index (a guess from base-10 text)", Index}} {
		sl, errS := Load[WSliceConf](ctx, wKindSource{hive, base, k.kind}, WithSched(tAggregating))
		mp, errM := Load[WMapConf](ctx, wKindSource{hive, base, k.kind}, WithSched(tAggregating))
		fmt.Printf("      plane says %-34s []string -> %v err=%v\n", k.label, sl.Tags, errShortW(errS))
		fmt.Printf("      %-45s map      -> %v err=%v\n", "", mp.Tags, errShortW(errM))
	}

	fmt.Println("\n(c) THE FINDING: all four agree, and they agree because core never reads")
	fmt.Println("    the kind the enumerator returned.")
	fmt.Println("    The walk's `members` operation takes the last segment's TEXT and")
	fmt.Println("    decides what to do with it from `n.kind`, which is the COMPILED")
	fmt.Println("    schema's - `strconv.Atoi(last.Text)` for a slice, the key type's")
	fmt.Println("    decoder for a map. `last.Kind` is carried into the member struct and")
	fmt.Println("    is never read again.")
	fmt.Println("    So ADR-0004's stated reason for the enumerator returning addresses")
	fmt.Println("    rather than names - that the plane answers which composite it is - is")
	fmt.Println("    not the mechanism ferry uses. The schema already knows.")

	fmt.Println("\n(d) which is the right answer, and it means ADR-0004's REASON is wrong")
	fmt.Println("    rather than its DECISION")
	fmt.Println("    The schema is the authority on whether /tags is a slice or a map, and")
	fmt.Println("    it has to be: ADR-0010 makes the address set a field of the thing the")
	fmt.Println("    walk iterates, precisely so the compiler and the walk cannot disagree.")
	fmt.Println("    A plane that could 'answer which composite it is' would be a SECOND")
	fmt.Println("    authority on the same question, which is ADR-0010's duplication axis 1.")
	fmt.Println("    Returning a Path is still right for two other reasons ADR-0004 did not")
	fmt.Println("    give: a Path is already the type the caller needs, and it carries the")
	fmt.Println("    escaping, so a child name containing the rendering's punctuation needs")
	fmt.Println("    no second convention.")
	fmt.Printf("      a child name containing a slash: %s\n", path("tags").Name("a/b"))

	fmt.Println("\n(e) and the Registry is the plane that makes this reachable")
	fmt.Println("    On YAML the question never arises: the document distinguishes a")
	fmt.Println("    sequence from a mapping, so the driver can answer and does. The")
	fmt.Println("    Registry cannot, and under ADR-0004's stated reasoning that would make")
	fmt.Println("    a Registry driver unable to implement Enumerator honestly. Under what")
	fmt.Println("    core actually does, it implements it by reporting Name for everything")
	fmt.Println("    and loses nothing.")
	fmt.Println("    That is the difference between a driver author reading the ADR and a")
	fmt.Println("    driver author reading the code, and it is worth an amendment.")
}
