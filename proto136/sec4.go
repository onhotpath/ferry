package main

import (
	"context"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// absentAtContainers is case 3's reading made into a driver: the plane holds
// whatever Dump wrote, and every container address answers Absent.
//
// It is the only honest way to build case 3's reading over a plane ferry itself
// wrote, because ferry writes a Null there and a driver reporting it back is
// what case 12 demands.
type absentAtContainers struct {
	inner      ferry.Source
	containers []ferry.Path
}

func (s absentAtContainers) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return absentReader{inner: r, containers: s.containers}, nil
	}, nil
}

type absentReader struct {
	inner      ferry.Reader
	containers []ferry.Path
}

func (r absentReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	for _, c := range r.containers {
		if c == addr {
			return ferry.Value{}, nil
		}
	}

	return r.inner.Get(ctx, addr)
}

func (r absentReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	return r.inner.(ferry.Enumerator).Children(ctx, prefix)
}

// noList is a reader with no Children: the Vault token with read and no list
// that ADR-0004 keeps ferry.Enumerator optional for.
type noList struct{ inner ferry.Source }

func (s noList) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return noListReader{inner: r}, nil
	}, nil
}

type noListReader struct{ inner ferry.Reader }

func (r noListReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	return r.inner.Get(ctx, addr)
}

// tagsOnly is the shape both readings are compared over.
type tagsOnly struct {
	Tags []string          `ferry:"tags"`
	Opt  *section          `ferry:"opt"`
	M    map[string]string `ferry:"m"`
}

// nested is the shape ADR-0005's vanishing-key audit was run on.
type nested struct {
	M map[string][]string `ferry:"m"`
}

func sec4() {
	head("4. What breaks under each reading")

	sub("4a. the Null reading, which is what the engine does today")

	store, err := ferrytest.Record(context.Background(), tagsOnly{Tags: []string{}, M: map[string]string{}})
	if err != nil {
		fmt.Println("Record:", err)

		return
	}

	fmt.Println("  the plane after Dump:", keysOf(store))

	fresh, err := ferry.Load[tagsOnly](context.Background(), ferrytest.Static(store))
	fmt.Printf("  Load into a zero value          -> Tags=%#v Opt=%v M=%#v err=%v\n",
		fresh.Tags, fresh.Opt, fresh.M, err)

	seed := tagsOnly{Tags: []string{"kept"}, Opt: &section{Name: "kept"}, M: map[string]string{"k": "kept"}}

	over, err := ferry.LoadOver(context.Background(), seed, ferrytest.Static(store))
	fmt.Printf("  LoadOver a populated seed       -> Tags=%#v Opt=%v M=%#v err=%v\n",
		over.Tags, over.Opt, over.M, err)

	nl, err := ferry.Load[tagsOnly](context.Background(), noList{inner: ferrytest.Static(store)})
	fmt.Printf("  Load from a source that cannot list -> Tags=%#v M=%#v err=%v\n", nl.Tags, nl.M, err)

	sub("4b. case 3's reading: the same plane, every container address answering Absent")

	containers := []ferry.Path{ferry.At("tags"), ferry.At("opt"), ferry.At("m")}
	src := absentAtContainers{inner: ferrytest.Static(store), containers: containers}

	fresh2, err := ferry.Load[tagsOnly](context.Background(), src)
	fmt.Printf("  Load into a zero value          -> Tags=%#v Opt=%v M=%#v err=%v\n",
		fresh2.Tags, fresh2.Opt, fresh2.M, err)

	over2, err := ferry.LoadOver(context.Background(), seed, src)
	fmt.Printf("  LoadOver a populated seed       -> Tags=%#v Opt=%v M=%#v err=%v\n",
		over2.Tags, over2.Opt, over2.M, err)

	nl2, err := ferry.Load[tagsOnly](context.Background(),
		noList{inner: absentAtContainers{inner: ferrytest.Static(store), containers: containers}})
	fmt.Printf("  Load from a source that cannot list -> Tags=%#v M=%#v\n", nl2.Tags, nl2.M)

	for _, e := range ferry.Elements(err) {
		fmt.Println("     err:", indent(e))
	}

	sub("4c. can a caller still tell an empty list from a missing key, under either reading")

	for _, spelled := range []struct {
		name  string
		store map[ferry.Path]ferry.Value
	}{
		{"missing key", map[ferry.Path]ferry.Value{}},
		{"Null at /tags (ferry's empty list)", map[ferry.Path]ferry.Value{ferry.At("tags"): ferry.Null()}},
	} {
		v, err := ferry.Load[tagsOnly](context.Background(), ferrytest.Static(spelled.store))
		o, _ := ferry.LoadOver(context.Background(), tagsOnly{Tags: []string{"seed"}},
			ferrytest.Static(spelled.store))
		fmt.Printf("  %-36s Load -> %#v   LoadOver{seed} -> %#v  err=%v\n", spelled.name, v.Tags, o.Tags, err)
	}

	sub("4d. the vanishing map key, and which side it lives on")

	audit := nested{M: map[string][]string{"a": {"x"}, "b": nil, "c": {}}}

	rec, err := ferrytest.Record(context.Background(), audit)
	if err != nil {
		fmt.Println("Record:", err)

		return
	}

	fmt.Println("  what Dump writes today:", keysOf(rec))

	back, err := ferry.Load[nested](context.Background(), ferrytest.Static(rec))
	fmt.Printf("  loaded back              -> %#v err=%v\n", back.M, err)

	kids := []ferry.Path{ferry.At("m"), ferry.At("m").At("a"), ferry.At("m").At("b"), ferry.At("m").At("c")}

	viaAbsent, err := ferry.Load[nested](context.Background(),
		absentAtContainers{inner: ferrytest.Static(rec), containers: kids})
	fmt.Printf("  loaded with Absent at every container -> %#v err=%v\n", viaAbsent.M, err)

	firstDraft := map[ferry.Path]ferry.Value{ferry.At("m").At("a").Elem(0): ferry.String("x")}

	draft, err := ferry.Load[nested](context.Background(), ferrytest.Static(firstDraft))
	fmt.Println("  the draft ADR-0005 rejected, which writes nothing for an empty composite:", keysOf(firstDraft))
	fmt.Printf("  loaded back              -> %#v err=%v\n", draft.M, err)

	sec4e()
}

// reqSection is the required-on-an-optional-section shape, which is where
// ADR-0006 says a Null is a presence observation.
type reqSection struct {
	Auth *section `ferry:"auth,required"`
}

// sec4e asks whether the Null at a container address is what satisfies
// `required` on an optional section, which the two readings answer differently.
func sec4e() {
	sub("4e. required on an optional section, which reads presence off the same address")

	store := map[ferry.Path]ferry.Value{ferry.At("auth"): ferry.Null()}

	v, err := ferry.Load[reqSection](context.Background(), ferrytest.Static(store))
	fmt.Printf("  Null at /auth, reported as Null   -> Auth=%v err=%v\n", v.Auth, err)

	v2, err2 := ferry.Load[reqSection](context.Background(),
		absentAtContainers{inner: ferrytest.Static(store), containers: []ferry.Path{ferry.At("auth")}})
	fmt.Printf("  Null at /auth, reported as Absent -> Auth=%v\n", v2.Auth)

	for _, e := range ferry.Elements(err2) {
		fmt.Println("     err:", indent(e))
	}
}
