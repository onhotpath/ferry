package ferry_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
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
// is self-contained. Ordinary use names a driver instead:
// yaml.NewSource("app.yaml"), env.New(), and so on.
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

// ExampleLoadOver_root loads a bare value, which sits at the root address.
//
// The root is the one address no struct tag names, so it declares no default
// and the seed is the only one it has. The plane here holds nothing, so 8080 is
// what comes back.
func ExampleLoadOver_root() {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{})

	port, err := ferry.LoadOver(context.Background(), 8080, src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(port)
	// Output: 8080
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
// so keying a map by it takes a registration declaring [ferry.KeyCodec.AsMapKey].
func ExampleWithRegistry() {
	fmt.Println(errors.Is(ferry.Compile[Peers](), ferry.ErrSchema))

	reg := ferry.MustRegistry(ferry.StringText[netip.Addr]().AsMapKey())

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

// ExampleRootRequired declares the root address required, which no tag can do.
//
// A seed does not answer it, because required is a presence test about the
// plane and a seed is not an observation: the reload below carries 8080 forward
// and still refuses where the plane went silent.
func ExampleRootRequired() {
	src := ferrytest.Static(map[ferry.Path]ferry.Value{})

	port, err := ferry.LoadOver(context.Background(), 8080, src, ferry.RootRequired)

	fmt.Println(port, errors.Is(err, ferry.ErrMissing))
	fmt.Println(err)
	// Output:
	// 8080 true
	// ferry: required, and nothing is set here
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

// namesTextPointer is #164's assertion, and it is that this file compiles.
//
// The constraint appears in the compiler error a caller reads when their type
// does not qualify for ferry.StringText or ferry.NumberText, so it has to be a
// name that caller can look up and write down. This is a package outside ferry
// writing it down.
func namesTextPointer[T any, PT ferry.TextPointer[T]]() {}

var _ = namesTextPointer[netip.Addr, *netip.Addr]

// Documented carries a declared extension key beside ferry's own. The docs tag
// is another library's vocabulary, and ferry reads it because it was told to.
type Documented struct {
	Host string `ferry:"host,required" docs:"desc=where the service lives"`
	Port int    `ferry:"port,default=8080" docs:"desc=the port it listens on"`
}

// ExampleWithTagKeys declares a foreign struct tag key and reads what it
// carried, address by address.
//
// ferry's own vocabulary is unchanged: the words live under a key the declaring
// library owns, they are validated against the declaration, and core acts on
// none of them. A driver reads the same view at its own Bind, through
// [ferry.AddressSet.Extension], so nothing is plumbed through the caller.
func ExampleWithTagKeys() {
	reg := ferry.MustRegistry(ferry.WithTagKeys(ferry.KeyExtension{
		TagKey: "docs",
		Words:  []ferry.Word{{Name: "desc", TakesValue: true}},
	}))

	table, err := ferry.ExtensionTable[Documented](ferry.WithRegistry(reg))
	if err != nil {
		fmt.Println(err)

		return
	}

	view, _ := table.Extension("docs")
	for _, addr := range []ferry.Path{ferry.At("host"), ferry.At("port")} {
		fmt.Println(addr, "-", view[addr]["desc"])
	}
	// Output:
	// /host - where the service lives
	// /port - the port it listens on
}

// ExampleSpellingFunc declares how one plane spells a bool, from a pair of
// closures over words the driver owns.
//
// The accept set is wider than the write form, which is what lets a file
// written by hand load while everything ferry writes stays canonical.
func ExampleSpellingFunc() {
	words := map[string]bool{"on": true, "off": false, "true": true, "false": false}

	onOff := ferry.SpellingFunc(
		func(text string) (bool, error) {
			b, ok := words[text]
			if !ok {
				return false, errors.New("no word of this plane spells a bool that way")
			}

			return b, nil
		},
		func(v bool) (string, error) {
			if v {
				return "on", nil
			}

			return "off", nil
		},
	)

	for _, text := range []string{"true", "off", "yes"} {
		b, err := onOff.Parse(text)
		if err != nil {
			fmt.Printf("%s -> %v\n", text, err)

			continue
		}

		written, _ := onOff.Render(b)
		fmt.Printf("%s -> %t -> written back as %s\n", text, b, written)
	}
	// Output:
	// true -> true -> written back as on
	// off -> false -> written back as off
	// yes -> no word of this plane spells a bool that way
}

// ExampleWith stacks a payload step under a spelling.
//
// The step written last is closest to the payload, so it runs first on the way
// out and last on the way in: the pipeline reads as the nesting it is.
func ExampleWith() {
	spelled := ferry.With[string, string](angled{}, shouted{})

	carrier, err := spelled.Render("ready")
	if err != nil {
		fmt.Println(err)

		return
	}

	back, err := spelled.Parse(carrier)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(carrier, back)
	// Output:
	// <READY> ready
}

// angled is a plane's own bracketing of a text payload.
type angled struct{}

func (angled) Render(v string) (string, error) { return "<" + v + ">", nil }

func (angled) Parse(c string) (string, error) {
	inner, ok := strings.CutPrefix(c, "<")
	if !ok {
		return "", errors.New("the carrier is not one this plane wrote")
	}

	return strings.TrimSuffix(inner, ">"), nil
}

// shouted is the payload step, and it inverts exactly what it applies.
type shouted struct{}

func (shouted) Apply(v string) (string, error)  { return strings.ToUpper(v), nil }
func (shouted) Invert(v string) (string, error) { return strings.ToLower(v), nil }

// ExampleBindWatched streams a struct that reloads when the plane behind it
// changes.
//
// The range opens with a load, so there is no separate first load to write, and
// every value it yields afterwards is a fresh load. errf reports why the stream
// ended: a failed reload, a cancelled context, or [ferry.ErrWatchLost].
//
// The plane here is a test double that announces one change. Ordinary use names
// a watchable driver instead: env.New(env.DotEnv(".env")).Watched(), and so on.
func ExampleBindWatched() {
	src := &promoting{host: "db1"}

	wb, err := ferry.BindWatched[Endpoint](src)
	if err != nil {
		fmt.Println(err)

		return
	}

	seq, errf := wb.Watch(context.Background())
	for cfg := range seq {
		fmt.Println(cfg.Host)

		if cfg.Host == "db2" {
			break
		}
	}

	if err := errf(); err != nil {
		fmt.Println(err)
	}
	// Output:
	// db1
	// db2
}

// Endpoint is what the watched stream loads.
type Endpoint struct {
	Host string `ferry:"host,required"`
}

// promoting is a one-address plane that promotes db1 to db2 the first time it
// is waited on, so the example has exactly one change to report.
type promoting struct {
	host    string
	changed bool
}

func (p *promoting) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		return oneAddress{v: ferry.String(p.host)}, nil
	}, nil
}

func (p *promoting) Watching() (ferry.Notifier, error) { return p, nil }

func (p *promoting) Notify(context.Context) (ferry.Change, error) { return p, nil }

func (p *promoting) Wait(context.Context) (bool, error) {
	if p.changed {
		return false, nil
	}

	p.host, p.changed = "db2", true

	return true, nil
}

func (*promoting) Close() error { return nil }

// oneAddress answers /host and nothing else.
type oneAddress struct{ v ferry.Value }

func (r oneAddress) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if addr.Path() == ferry.At("host") {
		return r.v, nil
	}

	return ferry.Value{}, nil
}
