package ferry

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/internal/testdata/badtags"
)

// Every rule in this file is asserted through Compile, Bind, Load, Dump and
// ExtensionTable. Two are not, and both are argued where they sit: the
// canonical form of a declaration is compared as a schemaKey, because
// order-independence is a statement about a map key and two registries keep
// two caches, and the cache size is counted with [cachedSchemas] for the reason
// stated there.

// extConf is one field carrying three tag keys: ferry's own, a declared
// extension, and a key nobody declared.
type extConf struct {
	Wait string `ferry:"wait,required" mylib:"node=mycompany:duration,secret" json:"wait,omitempty"`
	Host string `ferry:"host" mylib:"node=text"`
	Bare string `ferry:"bare"`
	Off  string `ferry:"-" mylib:"node=nowhere"`
}

// mylibExt is the extension every case here declares: one word with a value, one
// without.
func mylibExt() KeyExtension {
	return KeyExtension{TagKey: "mylib", Words: []Word{
		{Name: "node", TakesValue: true},
		{Name: "secret"},
	}}
}

func docsExt() KeyExtension {
	return KeyExtension{TagKey: "docs", Words: []Word{{Name: "desc", TakesValue: true}}}
}

// extRegistry is a fresh registry per test, because a registry is complete at
// birth and the schema cache hangs off it.
func extRegistry(t *testing.T, exts ...KeyExtension) *Registry {
	t.Helper()

	reg, err := NewRegistry(WithTagKeys(exts...))
	if err != nil {
		t.Fatalf("the declaration was refused: %+v", err)
	}

	return reg
}

// boundTo binds src to T and hands back the address set the driver received,
// which is the handoff the table rides.
func boundTo[T any](t *testing.T, opts ...Option) *AddressSet {
	t.Helper()

	p := &probe{values: map[Path]Value{}, presence: map[Path]Presence{}}

	if _, err := Bind[T](p, opts...); err != nil {
		t.Fatalf("the binding was refused: %+v", err)
	}

	return p.bound
}

// TestADeclaredKeyReachesTheDriverAtItsOwnAddresses is the mechanism end to
// end: a driver reads its own key's view off the address set it already
// receives, and every word is at the address the field's ferry tag named.
func TestADeclaredKeyReachesTheDriverAtItsOwnAddresses(t *testing.T) {
	t.Parallel()

	addrs := boundTo[extConf](t, WithRegistry(extRegistry(t, mylibExt())))

	view := addrs.Extension("mylib")

	want := map[string]map[string]string{
		"/wait": {"node": "mycompany:duration", "secret": ""},
		"/host": {"node": "text"},
	}

	got := map[string]map[string]string{}
	for addr, words := range view {
		got[addr.String()] = words
	}

	if !extEqual(got, want) {
		t.Errorf("the view is %v, and %v was expected", got, want)
	}
}

// TestAWordReachesNoAddressWhereTheFieldNamesNone is the address-keyed rule's
// other half: a field marked "-" names no address, so its extension words have
// nowhere to be and are not in the table.
func TestAWordReachesNoAddressWhereTheFieldNamesNone(t *testing.T) {
	t.Parallel()

	addrs := boundTo[extConf](t, WithRegistry(extRegistry(t, mylibExt())))

	for addr := range addrs.Extension("mylib") {
		if strings.Contains(addr.String(), "nowhere") || addr.String() == "/Off" {
			t.Errorf("the skipped field reached the table at %s", addr)
		}
	}

	if len(addrs.Extension("mylib")) != 2 {
		t.Errorf("the view holds %d addresses, and 2 were expected", len(addrs.Extension("mylib")))
	}
}

// TestAnUndeclaredKeyIsNeverClaimed is Go's own convention kept: a key ferry
// was not told about belongs to another library, whatever it holds.
func TestAnUndeclaredKeyIsNeverClaimed(t *testing.T) {
	t.Parallel()

	type conf struct {
		Host string `ferry:"host" mylib:"node=text,unknown=1" validate:"required,min=3"`
	}

	if err := Compile[conf](); err != nil {
		t.Errorf("a struct carrying two undeclared keys was refused: %+v", err)
	}

	addrs := boundTo[conf](t)
	if n := len(addrs.Extension("mylib")); n != 0 {
		t.Errorf("an undeclared key yielded %d addresses, and none was expected", n)
	}
}

// TestFerryTagStaysClosed is the sentence this whole mechanism exists to keep
// true: declaring an extension adds no word to ferry's own tag, and a
// namespaced word inside it is refused exactly as it was.
func TestFerryTagStaysClosed(t *testing.T) {
	t.Parallel()

	type conf struct {
		Host string `ferry:"host,mylib.retry=3"`
	}

	err := Compile[conf](WithRegistry(extRegistry(t, mylibExt())))
	if err == nil {
		t.Fatal("a namespaced word inside ferry's own tag was accepted")
	}

	mustRefuse(t, err, `unknown option "mylib.retry"`)
}

// TestADeclaredWordIsNotFerrysAndFerrysIsNotDeclared keeps the two
// vocabularies apart in both directions: a declared word written in ferry's tag
// is unknown to ferry, and ferry's own word written in the extension's tag is
// unknown to it.
func TestADeclaredWordIsNotFerrysAndFerrysIsNotDeclared(t *testing.T) {
	t.Parallel()

	reg := extRegistry(t, mylibExt())

	type ferrysTag struct {
		Host string `ferry:"host,secret"`
	}

	type extTag struct {
		Host string `ferry:"host" mylib:"required"`
	}

	mustRefuse(t, Compile[ferrysTag](WithRegistry(reg)), `unknown option "secret"`)
	mustRefuse(t, Compile[extTag](WithRegistry(reg)), `mylib tag, unknown word "required"`)
}

// TestNearMissCoversTheExtensionAndLeavesFerrysAlone is the diagnostics claim:
// a misspelled declared word gets the same remedy ferry gives its own, from the
// extension's vocabulary, and ferry's own is unchanged by the declaration.
func TestNearMissCoversTheExtensionAndLeavesFerrysAlone(t *testing.T) {
	t.Parallel()

	reg := extRegistry(t, mylibExt())

	cases := []struct {
		name string
		err  error
		want string
	}{{
		name: "a misspelled extension word",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"noed=x"`
		}](WithRegistry(reg)),
		want: `mylib tag, unknown word "noed": did you mean "node"?`,
	}, {
		name: "an extension word near nothing",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"omitempty"`
		}](WithRegistry(reg)),
		want: "the mylib tag key declares node, secret",
	}, {
		name: "an unknown word under a key declaring none",
		err: Compile[struct {
			Host string `ferry:"host" empty:"anything"`
		}](WithRegistry(extRegistry(t, KeyExtension{TagKey: "empty"}))),
		want: "the empty tag key is declared with no words at all",
	}, {
		name: "ferry's own near miss is undegraded",
		err: Compile[struct {
			Host string `ferry:"host,requird"`
		}](WithRegistry(reg)),
		want: `did you mean "required"?`,
	}, {
		name: "ferry's own neighbourhood is not widened",
		err: Compile[struct {
			Host string `ferry:"host,node=x"`
		}](WithRegistry(reg)),
		want: `unknown option "node"`,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuse(t, c.err, c.want)
		})
	}
}

// TestAWordIsWrittenTheWayItWasDeclared is what the typed declaration buys at a
// tag: a word declared with a value and written without one is a refusal, and
// so is the other way round.
func TestAWordIsWrittenTheWayItWasDeclared(t *testing.T) {
	t.Parallel()

	reg := extRegistry(t, mylibExt())

	cases := []struct {
		name string
		err  error
		want string
	}{{
		name: "a value word written bare",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"node"`
		}](WithRegistry(reg)),
		want: `word "node" needs a value`,
	}, {
		name: "a bare word written with a value",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"secret=yes"`
		}](WithRegistry(reg)),
		want: `word "secret" takes no value`,
	}, {
		name: "one word twice",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"node=a,node=b"`
		}](WithRegistry(reg)),
		want: `word "node" is given twice`,
	}, {
		name: "two commas with nothing between them",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"secret,,node=a"`
		}](WithRegistry(reg)),
		want: "empty word",
	}, {
		name: "surrounding whitespace",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"secret, node=a"`
		}](WithRegistry(reg)),
		want: "surrounding whitespace",
	}, {
		name: "an unterminated quoted value",
		err: Compile[struct {
			Host string `ferry:"host" mylib:"node='a"`
		}](WithRegistry(reg)),
		want: `value "'a" is not terminated`,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuse(t, c.err, c.want)
		})
	}
}

// TestAnExtensionValueIsATokenLikeFerrysOwn is one grammar rather than two: a
// value holding a comma is quoted the way a declared default is, and a doubled
// quote is a literal one.
func TestAnExtensionValueIsATokenLikeFerrysOwn(t *testing.T) {
	t.Parallel()

	type conf struct {
		Host string `ferry:"host" mylib:"node='a,b'"`
		Port string `ferry:"port" mylib:"node='it''s'"`
	}

	view := boundTo[conf](t, WithRegistry(extRegistry(t, mylibExt()))).Extension("mylib")

	got := map[string]string{}
	for addr, words := range view {
		got[addr.String()] = words["node"]
	}

	want := map[string]string{"/host": "a,b", "/port": "it's"}
	if !maps.Equal(got, want) {
		t.Errorf("the values are %v, and %v were expected", got, want)
	}
}

// TestWhatADeclarationMayNotBe is the collision rule: every refusal fires once,
// at the registry's birth, before any tag is parsed.
func TestWhatADeclarationMayNotBe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		items []Registration
		want  []string
	}{{
		name:  "ferry's own key",
		items: []Registration{WithTagKeys(KeyExtension{TagKey: "ferry"})},
		want:  []string{`"ferry" is the key ferry reads`},
	}, {
		name:  "no key at all",
		items: []Registration{WithTagKeys(KeyExtension{})},
		want:  []string{"the empty string names none"},
	}, {
		name:  "a punctuated key",
		items: []Registration{WithTagKeys(KeyExtension{TagKey: "my.lib"})},
		want:  []string{`"my.lib" contains "."`},
	}, {
		name:  "a key holding a space",
		items: []Registration{WithTagKeys(KeyExtension{TagKey: "my lib"})},
		want:  []string{`"my lib" contains " "`},
	}, {
		name:  "one key twice in one call",
		items: []Registration{WithTagKeys(mylibExt(), mylibExt())},
		want:  []string{`"mylib" is declared twice`},
	}, {
		name:  "one key across two calls",
		items: []Registration{WithTagKeys(mylibExt()), WithTagKeys(mylibExt())},
		want:  []string{`"mylib" is declared twice`},
	}, {
		name:  "a word with no name",
		items: []Registration{WithTagKeys(KeyExtension{TagKey: "mylib", Words: []Word{{}}})},
		want:  []string{"declares a word with no name"},
	}, {
		name:  "a word holding a comma",
		items: []Registration{WithTagKeys(KeyExtension{TagKey: "mylib", Words: []Word{{Name: "a,b"}}})},
		want:  []string{`"a,b"`, `contains ","`},
	}, {
		name:  "a word holding an equals sign",
		items: []Registration{WithTagKeys(KeyExtension{TagKey: "mylib", Words: []Word{{Name: "a=b"}}})},
		want:  []string{`"a=b"`, `contains "="`},
	}, {
		name: "one word twice",
		items: []Registration{WithTagKeys(KeyExtension{
			TagKey: "mylib",
			Words:  []Word{{Name: "node", TakesValue: true}, {Name: "node"}},
		})},
		want: []string{`"node" under tag key "mylib" is declared twice`},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuseAtConstruction(t, c.items, c.want...)
		})
	}
}

// TestTheKeyFerryReadsMayNotAlsoBeDeclared is the collision the registry cannot
// see on its own, because the key ferry reads is a property of the call: it is
// refused at that call, with the Option list, before any type is described.
func TestTheKeyFerryReadsMayNotAlsoBeDeclared(t *testing.T) {
	t.Parallel()

	type conf struct {
		Host string `mylib:"host"`
	}

	err := Compile[conf](TagKey("mylib"), WithRegistry(extRegistry(t, mylibExt())))
	if err == nil {
		t.Fatal("one key was read two ways")
	}

	mustRefuse(t, err, `declares "mylib" as an extension key`)

	if !errors.Is(err, ErrSchema) {
		t.Errorf("the refusal is %v, and does not answer to ErrSchema", err)
	}
}

// TestATagUnderADeclaredKeyIsScannedLikeFerrysOwn is the scanner reused rather
// than a second one written: a declared key whose value is not a Go quoted
// string is diagnosed, and the field's own address is where it is reported.
func TestATagUnderADeclaredKeyIsScannedLikeFerrysOwn(t *testing.T) {
	t.Parallel()

	err := Compile[badtags.DuplicateExtension](WithRegistry(extRegistry(t, mylibExt())))

	mustRefuse(t, err, "the field carries two mylib tags", "reflect.StructTag.Get returns the first")
}

// TestDeclarationOrderIsOneSchemaKey is the canonical form: two registries
// declaring the same extensions in opposite orders key one schema and not two.
//
// It is asserted as a key rather than through the seam because that is what the
// claim is about. Two registries keep two caches, so no call can observe the
// difference, and what would go wrong if the form were order-dependent is a
// second entry in one cache that is silent until the day it disagrees.
func TestDeclarationOrderIsOneSchemaKey(t *testing.T) {
	t.Parallel()

	one := registryWith(t, WithTagKeys(mylibExt(), docsExt()))
	other := registryWith(t, WithTagKeys(docsExt()), WithTagKeys(mylibExt()))

	if one.exts.decl != other.exts.decl {
		t.Errorf("the declarations canonicalise to %q and %q, and one form was expected",
			one.exts.decl.canon, other.exts.decl.canon)
	}

	if one.exts.decl == (extDecl{}) {
		t.Error("two declared extensions canonicalise to the empty form, which is what declaring nothing is")
	}

	if MustRegistry().exts.decl != (extDecl{}) {
		t.Error("a registry declaring nothing does not canonicalise to the empty form")
	}
}

// TestOneTypeUnderOneDeclarationIsOneEntry is the cache half: reading a
// declared key adds nothing to how often a type compiles.
func TestOneTypeUnderOneDeclarationIsOneEntry(t *testing.T) {
	reg := registryWith(t, WithTagKeys(mylibExt()))

	for range 3 {
		if err := Compile[extConf](WithRegistry(reg)); err != nil {
			t.Fatalf("the type was refused: %+v", err)
		}

		boundTo[extConf](t, WithRegistry(reg))
	}

	if n := cachedSchemas(reg); n != 1 {
		t.Errorf("the cache holds %d entries, and 1 was expected", n)
	}
}

// TestAnExtensionIsInertToCore is the line the parked question was afraid of:
// core validates the declared words and acts on none of them, so a load and a
// dump are what they were without the declaration.
func TestAnExtensionIsInertToCore(t *testing.T) {
	t.Parallel()

	type conf struct {
		Host string `ferry:"host" mylib:"node=text,secret"`
		Port string `ferry:"port"`
	}

	plain := extLoadDump[conf](t)
	declared := extLoadDump[conf](t, WithRegistry(extRegistry(t, mylibExt())))

	if !maps.Equal(plain, declared) {
		t.Errorf("the plane holds %v with the declaration and %v without it", declared, plain)
	}
}

// extLoadDump runs one dump and one load over the same plane, and reports what
// the plane holds afterwards as text.
func extLoadDump[T any](t *testing.T, opts ...Option) map[string]string {
	t.Helper()

	p := &probe{values: map[Path]Value{}, presence: map[Path]Presence{}}
	p.values[At("host")] = String("db1")
	p.values[At("port")] = String("5432")

	v, err := Load[T](context.Background(), p, opts...)
	if err != nil {
		t.Fatalf("the load was refused: %+v", err)
	}

	if err := Dump(context.Background(), v, extSink{p: p}, opts...); err != nil {
		t.Fatalf("the dump was refused: %+v", err)
	}

	out := map[string]string{}
	for addr, val := range p.values {
		out[addr.String()] = val.GoString()
	}

	return out
}

// extSink is the write half over the same map the probe reads, so one plane
// carries a load and the dump after it.
type extSink struct{ p *probe }

func (s extSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return s.p, nil }, nil
}

// TestExtensionTableReadsWithoutAPlane is the out-of-band door: a consumer that
// never meets a driver reads the same table.
func TestExtensionTableReadsWithoutAPlane(t *testing.T) {
	t.Parallel()

	table, err := ExtensionTable[extConf](WithRegistry(extRegistry(t, mylibExt(), docsExt())))
	if err != nil {
		t.Fatalf("the type was refused: %+v", err)
	}

	if n := len(table.Extension("mylib")); n != 2 {
		t.Errorf("the mylib view holds %d addresses, and 2 were expected", n)
	}

	if n := len(table.Extension("docs")); n != 0 {
		t.Errorf("a declared key no field carried holds %d addresses, and none was expected", n)
	}

	if n := len(table.Extension("nobody")); n != 0 {
		t.Errorf("an undeclared key holds %d addresses, and none was expected", n)
	}

	if _, err := ExtensionTable[struct{ Host string }](); err == nil {
		t.Error("a type ferry refuses was accepted by ExtensionTable")
	}
}

// TestTheViewIsTheCallersToKeep is what protects a cached schema: the view is
// freshly allocated, so writing to one changes nothing about the next.
func TestTheViewIsTheCallersToKeep(t *testing.T) {
	t.Parallel()

	reg := extRegistry(t, mylibExt())
	addrs := boundTo[extConf](t, WithRegistry(reg))

	view := addrs.Extension("mylib")
	for addr, words := range view {
		delete(view, addr)
		words["node"] = "rewritten"
	}

	after := addrs.Extension("mylib")
	if len(after) != 2 {
		t.Fatalf("the second view holds %d addresses, and 2 were expected", len(after))
	}

	for addr, words := range after {
		if words["node"] == "rewritten" {
			t.Errorf("a write to one view reached the next, at %s", addr)
		}
	}
}

// TestANilAddressSetHasNoExtension is the nil receiver every other AddressSet
// method already answers for.
func TestANilAddressSetHasNoExtension(t *testing.T) {
	t.Parallel()

	var a *AddressSet

	if n := len(a.Extension("mylib")); n != 0 {
		t.Errorf("a nil address set yielded %d addresses", n)
	}
}

// TestADeclarationIsNotACodec is ADR-0021's shape at the type level: the two
// kinds of registration are handed to one constructor and neither is spelled as
// the other.
func TestADeclarationIsNotACodec(t *testing.T) {
	t.Parallel()

	if _, ok := WithTagKeys(mylibExt()).(Codec); ok {
		t.Error("a tag key declaration satisfies Codec, so it can be registered as one")
	}

	reg := registryWith(t, DurationLike[pollInterval](), WithTagKeys(mylibExt()))

	if got := len(reg.Types()); got != 1 {
		t.Errorf("the registry holds %d types, and 1 was expected", got)
	}

	if !slices.Contains(reg.exts.keys, "mylib") {
		t.Errorf("the declaration did not reach the registry, whose keys are %v", reg.exts.keys)
	}
}

// extEqual compares two address-keyed views as data.
func extEqual(got, want map[string]map[string]string) bool {
	if len(got) != len(want) {
		return false
	}

	for addr, words := range want {
		if !maps.Equal(got[addr], words) {
			return false
		}
	}

	return true
}

// TestAWordUnderADynamicContainerIsHeldAndNotRecorded is the scoping property:
// a driver sees extension data only for addresses it was bound to, and what is
// under a map is an address shape rather than an address.
func TestAWordUnderADynamicContainerIsHeldAndNotRecorded(t *testing.T) {
	t.Parallel()

	type elem struct {
		Host string `ferry:"host" mylib:"node=text"`
	}

	type conf struct {
		Servers map[string]elem `ferry:"servers"`
		Region  string          `ferry:"region" mylib:"node=text"`
	}

	view := boundTo[conf](t, WithRegistry(extRegistry(t, mylibExt()))).Extension("mylib")

	if len(view) != 1 {
		t.Errorf("the view holds %d addresses, and only the static one was expected: %v", len(view), view)
	}

	if _, ok := view[At("region")]; !ok {
		t.Error("the static address is missing from the view")
	}

	type broken struct {
		Servers map[string]struct {
			Host string `ferry:"host" mylib:"noed=x"`
		} `ferry:"servers"`
	}

	mustRefuse(t, Compile[broken](WithRegistry(extRegistry(t, mylibExt()))), `unknown word "noed"`)
}
