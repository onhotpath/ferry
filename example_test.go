package ferry_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// Config is the annotated struct the examples below load and dump. One struct
// and one tag grammar serve both directions.
type Config struct {
	Host    string        `ferry:"host,required"`
	Port    int           `ferry:"port,default=8080"`
	Timeout time.Duration `ferry:"timeout,default=30s"`
	Tags    []string      `ferry:"tags"`
	DB      DB            `ferry:"db"`
}

// DB is the nested struct, which contributes /db/user rather than a second
// top-level address.
type DB struct {
	User string `ferry:"user"`
}

// Example loads an annotated struct from a plane.
//
// The plane here is [ferrytest.Static], a source of constants, so the example
// is self-contained. Ordinary use names a driver instead: yaml.Source{Path:
// "app.yaml"}, env.New(), and so on.
func Example() {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("host"):         ferry.String("db.internal"),
		ferry.At("tags").Elem(0): ferry.String("eu"),
		ferry.At("db", "user"):   ferry.String("checkout"),
	})

	cfg, err := ferry.Load[Config](context.Background(), src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Host:db.internal Port:8080 Timeout:30s Tags:[eu] DB:{User:checkout}}
}

// ExampleLoadOver loads over a seed, which is how a composite default is
// spelled and how a reload carries the previous value forward.
//
// The plane names only /host, so every other field keeps what the seed gave it
// - except where a tag declares a default, which beats a seed.
func ExampleLoadOver() {
	seed := Config{Host: "localhost", Tags: []string{"default"}}

	src := ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("host"): ferry.String("db.internal"),
	})

	cfg, err := ferry.LoadOver(context.Background(), seed, src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Host:db.internal Port:8080 Timeout:30s Tags:[default] DB:{User:}}
}

// ExampleDump writes a value to a plane and loads it back.
//
// [ferrytest.MemPlane] is a plane with nothing of its own, so what comes back
// is what ferry wrote. A real sink is named the same way:
// yaml.Sink{Path: "app.yaml"}.
func ExampleDump() {
	plane := ferrytest.MemPlane().Open()

	cfg := Config{Host: "db.internal", Port: 5432, Timeout: time.Minute, DB: DB{User: "checkout"}}

	if err := ferry.Dump(context.Background(), cfg, plane.Sink); err != nil {
		fmt.Println(err)

		return
	}

	back, err := ferry.Load[Config](context.Background(), plane.Source)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(back.Host, back.Port, back.Timeout, back.DB.User)
	// Output: db.internal 5432 1m0s checkout
}

// ExampleBind binds a source once and loads through it many times, which is
// what a handler does: the compile and the driver's own bind happen at startup,
// and each load is the open, the walk and the release.
//
// The plane here is a source of constants so that the example is
// self-contained. A plane that is the request carries its contents in the
// context instead, and the shape of the code does not change.
func ExampleBind() {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("host"):       ferry.String("db.internal"),
		ferry.At("db", "user"): ferry.String("checkout"),
	})

	b, err := ferry.Bind[Config](src)
	if err != nil {
		fmt.Println(err)

		return
	}

	for range 2 {
		cfg, err := b.Load(context.Background())
		if err != nil {
			fmt.Println(err)

			return
		}

		fmt.Println(cfg.Host, cfg.Port, cfg.DB.User)
	}
	// Output:
	// db.internal 8080 checkout
	// db.internal 8080 checkout
}

// ExampleBindSink is the same split on the write side: bind the sink once, dump
// through it as often as there is something to write.
func ExampleBindSink() {
	plane := ferrytest.MemPlane().Open()

	b, err := ferry.BindSink[DB](plane.Sink)
	if err != nil {
		fmt.Println(err)

		return
	}

	if err = b.Dump(context.Background(), DB{User: "checkout"}); err != nil {
		fmt.Println(err)

		return
	}

	back, err := ferry.Load[DB](context.Background(), plane.Source)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(back.User)
	// Output: checkout
}

// Untagged is a type that does not compile: an exported field must name the
// segment it addresses, or be marked "-".
type Untagged struct {
	Host string
}

// ExampleCompile checks a type's annotation with no value in hand and no plane
// reachable, which is what a test does.
func ExampleCompile() {
	fmt.Println(ferry.Compile[Config]())
	fmt.Println(errors.Is(ferry.Compile[Untagged](), ferry.ErrSchema))
	// Output:
	// <nil>
	// true
}

// Required is a type whose two fields must both be present.
type Required struct {
	Host string `ferry:"host,required"`
	User string `ferry:"user,required"`
}

// ExampleElements ranges the failures one call reported.
//
// Both required fields are unset, and both are reported: a failed call carries
// every failure rather than the first. Match on the sentinels and on the
// address, never on the message text.
func ExampleElements() {
	_, err := ferry.Load[Required](context.Background(), ferrytest.Static(nil))

	for _, e := range ferry.Elements(err) {
		fe, ok := errors.AsType[*ferry.Error](e)
		if !ok {
			continue
		}

		fmt.Println(fe.Address(), errors.Is(fe, ferry.ErrMissing))
	}
	// Output:
	// /host true
	// /user true
}

// Peers keys a map by a type ferry does not own, which needs a registration.
type Peers struct {
	Names map[netip.Addr]string `ferry:"names"`
}

// ExampleWithRegistry resolves one call against a registry of the caller's own.
//
// netip.Addr carries a text pair, so ferry already knows how to store one. What
// it cannot know is that the text is injective, which is what a map key needs,
// so keying a map by it takes a registration declaring [ferry.Reg.AsMapKey].
func ExampleWithRegistry() {
	fmt.Println(errors.Is(ferry.Compile[Peers](), ferry.ErrSchema))

	reg := ferry.NewRegistry()
	if err := reg.Register(ferry.TextCodec[netip.Addr](ferry.KindString).AsMapKey()); err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(ferry.Compile[Peers](ferry.WithRegistry(reg)))
	// Output:
	// true
	// <nil>
}

// Service carries its annotation under the tag key `mylib` rather than `ferry`.
type Service struct {
	Name    string `mylib:"service,required"`
	Timeout int    `mylib:"timeout,default=30"`
}

// ExampleTagKey reads the annotation from another struct tag key.
//
// It names where to look and never what the content means: the grammar under
// `mylib` is still ferry's, held to ferry's strictness. It applies to every
// struct the call reaches, not only the top-level one.
func ExampleTagKey() {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("service"): ferry.String("checkout"),
	})

	cfg, err := ferry.Load[Service](context.Background(), src, ferry.TagKey("mylib"))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Name:checkout Timeout:30}
}

// ExampleAt builds an address and walks its segments.
//
// The rendering identifies the address. It is not a plane key: how segments are
// joined, or walked as a tree, is the driver's business.
func ExampleAt() {
	addr := ferry.At("servers").Elem(1).At("host")

	fmt.Println(addr)

	for seg := range addr.Segments() {
		fmt.Println(seg.Kind(), seg.Text())
	}
	// Output:
	// /servers#1/host
	// Name servers
	// Index 1
	// Name host
}

// ExampleValue shows what a driver hands across the boundary.
//
// A number and a string over the same text are two different observations, and
// that is how a quoted 8080 and an unquoted one stay distinguishable across a
// round trip. An accessor asked for the wrong kind returns
// [ferry.ErrWrongKind] rather than panicking.
func ExampleValue() {
	n, s := ferry.Number("8080"), ferry.String("8080")

	fmt.Println(n.Kind(), s.Kind(), n == s)

	i, err := n.AsInt()
	fmt.Println(i, err)

	_, err = s.AsInt()
	fmt.Println(errors.Is(err, ferry.ErrWrongKind))
	// Output:
	// number string false
	// 8080 <nil>
	// true
}
