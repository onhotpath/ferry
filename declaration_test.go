package ferry

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every assertion in this file goes through Compile[T], Load, LoadOver and
// Dump. A declaration is a statement about what a plane was asked, what came
// back, and what the field holds afterwards, so it is asserted as exactly those
// three things and never by reading a compiled node.
//
// The one thing this file cannot assert directly is the negative half of "a
// declaration attaches to the address shape". A lookup by the realised address
// is a design that is not in the code, so what stands for it is the fact that
// makes it fail: the realised address is not in the schema, and neither is the
// shape, so a table keyed by either holds no declaration for /servers/a/port
// while the default still applies there.

// defName is ADR-0006's own worked leaf: one field, one declared default.
type defName struct {
	Name string `ferry:"name,default=anonymous"`
}

// TestADeclaredDefaultAppliesOnlyOnAbsent is ADR-0006's one rule with a
// declaration under it: present beats absent, and empty is present.
//
// The seed is what makes the first row say something. Where a seed and a
// declared default both apply, the declared one wins, because ferry cannot tell
// a seeded value from a zero one and inferring it is the survey defect ferry
// exists in order not to have.
func TestADeclaredDefaultAppliesOnlyOnAbsent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  Value
		want string
	}{
		{name: "absent takes the default, over the seed", got: Value{}, want: "anonymous"},
		{name: "an explicit empty beats a non-zero default", got: String(""), want: ""},
		{name: "and a real value replaces it", got: String("svc"), want: "svc"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkDeclaredLeaf(t, c.got, c.want)
		})
	}
}

// checkDeclaredLeaf presents one observation over a seeded field declaring a
// default, and holds the result to what the rule says it should be.
func checkDeclaredLeaf(t *testing.T, got Value, want string) {
	t.Helper()

	src := planeSource{p: newPlane(map[Path]Value{At("name"): got})}

	out, err := LoadOver(t.Context(), defName{Name: "seeded"}, src)
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	if out.Name != want {
		t.Errorf("%#v at the address left %q, want %q", got, out.Name, want)
	}
}

// defSecret is the aliasing case ADR-0006 measured, and []byte is the one leaf
// in core's set where it is observable: a Go value cached at schema compile is
// one backing array shared by every load of that schema.
type defSecret struct {
	Secret []byte `ferry:"secret,default=Secret"`
}

// TestADefaultIsTextDecodedFreshOnEveryLoad is the reason a default is held as
// a Value rather than as a Go value: two independently loaded structs shared
// one backing array, and mutating either corrupted the other.
func TestADefaultIsTextDecodedFreshOnEveryLoad(t *testing.T) {
	t.Parallel()

	a := loadEmpty[defSecret](t)
	b := loadEmpty[defSecret](t)

	if string(a.Secret) != "Secret" || string(b.Secret) != "Secret" {
		t.Fatalf("the default did not apply: %q and %q", a.Secret, b.Secret)
	}

	a.Secret[0] = 's'

	if string(b.Secret) != "Secret" {
		t.Errorf("two loads of one schema share a backing array: mutating one left the other %q", b.Secret)
	}
}

// loadEmpty loads T from a plane that holds nothing, over a plane built fresh
// per call because a plane shared across loads is the same defect as a
// destination shared across subtests.
func loadEmpty[T any](t *testing.T) T {
	t.Helper()

	out, err := Load[T](t.Context(), planeSource{p: newPlane(map[Path]Value{})})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	return out
}

// defServer is the element ADR-0006 measures the shape rule on: a leaf with a
// declaration, sitting under a composite whose addresses come from the value.
type defServer struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=8080"`
}

type defDynamic struct {
	Servers map[string]defServer `ferry:"servers"`
	Pool    []defServer          `ferry:"pool"`
}

// TestADeclarationAttachesToTheAddressShape is the probe that overturned
// ADR-0006's draft, and its failure mode is silence rather than an error.
//
// A map key's address and a slice element's index come from the value, so
// /servers/a/port is not in the compiled schema and never can be: the
// declaration lives at /servers/*/port and the walk carries two paths, the
// realised one it asks the plane about and the static one it looks declarations
// up by. Looked up by the realised address instead, every default under a map
// or a slice silently does not apply.
func TestADeclarationAttachesToTheAddressShape(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{
		At("servers", "a", "host"):    String("h1"),
		At("pool").Elem(0).At("host"): String("h2"),
	})

	got, err := Load[defDynamic](t.Context(), treeSource{p: p})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := (defServer{Host: "h1", Port: 8080}); got.Servers["a"] != want {
		t.Errorf("the map entry loaded as %+v, want %+v", got.Servers["a"], want)
	}

	if want := (defServer{Host: "h2", Port: 8080}); len(got.Pool) != 1 || got.Pool[0] != want {
		t.Errorf("the sequence loaded as %+v, want one %+v", got.Pool, want)
	}

	mustHoldNoDeclarationKey(t, p)
}

// mustHoldNoDeclarationKey is the negative half: neither the realised address
// nor the shape is anything the schema holds, so a declaration looked up by
// either would not be found, and the defaults above landed anyway.
func mustHoldNoDeclarationKey(t *testing.T, p *plane) {
	t.Helper()

	for _, realised := range []Path{At("servers", "a", "port"), At("pool").Elem(0).At("port")} {
		if p.bound.Has(realised) {
			t.Errorf("%s is in the compiled schema, and an address minted from a value never is", realised)
		}
	}
}

// TestTheAddressShapeNeverReachesADriver is the other half of the rule, and it
// is a rule about the driver contract rather than about defaults: the shape is
// the walk's own lookup key, nothing is at it, and every member of the set a
// driver is bound to is one it can fetch, write, name and check.
func TestTheAddressShapeNeverReachesADriver(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	v := defDynamic{Servers: map[string]defServer{"a": {Host: "h1"}}, Pool: []defServer{{Host: "h2"}}}

	if err := Dump(t.Context(), v, planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	bound := slices.Collect(p.bound.All())
	mustBeAddresses(t, bound, []string{"/pool", "/servers"})

	for _, addr := range bound {
		if strings.Contains(addr.String(), wildcard) {
			t.Errorf("the bound set holds the shape %s, which nothing is at", addr)
		}
	}
}

// defCred is an optional section whose first field carries a declaration, which
// is ADR-0006's fixture for "a default fills a hole and never conjures the
// section".
type defCred struct {
	User string `ferry:"user,default=admin"`
	Pass string `ferry:"pass"`
}

type defOptional struct {
	Auth *defCred `ferry:"auth"`
	Port *int     `ferry:"port,default=5"`
}

// TestADefaultFillsAHoleAndNeverConjuresTheSection is the difference between
// the two candidate rules, and the second one is self-defeating: were a default
// presence, no *T with a default anywhere beneath it could ever be nil, which is
// the whole meaning of the pointer.
//
// A pointer to a leaf is a different case and is not affected, because its
// default sits at its own address rather than under one.
func TestADefaultFillsAHoleAndNeverConjuresTheSection(t *testing.T) {
	t.Parallel()

	t.Run("nothing under the section leaves it nil", func(t *testing.T) {
		t.Parallel()

		got := loadEmpty[defOptional](t)
		if got.Auth != nil {
			t.Errorf("the section was conjured as %+v, want nil", got.Auth)
		}

		mustPointAt(t, got.Port, 5)
	})

	t.Run("and one address under it fills the rest from the declarations", func(t *testing.T) {
		t.Parallel()

		src := planeSource{p: newPlane(map[Path]Value{At("auth", "pass"): String("p")})}

		got, err := Load[defOptional](t.Context(), src)
		if err != nil {
			t.Fatalf("load: %+v", err)
		}

		if want := (defCred{User: "admin", Pass: "p"}); got.Auth == nil || *got.Auth != want {
			t.Errorf("the section loaded as %+v, want %+v", got.Auth, want)
		}
	})
}

func mustPointAt(t *testing.T, got *int, want int) {
	t.Helper()

	if got == nil {
		t.Fatalf("a *int with a default loaded as nil, want a pointer to %d: its default sits at its own "+
			"address rather than under one", want)
	}

	if *got != want {
		t.Errorf("the pointer holds %d, want %d", *got, want)
	}
}

type defArray struct {
	Arr [2]defServer `ferry:"arr"`
}

type defPool struct {
	Pool []defServer `ferry:"pool"`
}

// TestAnArrayElementTakesItsDefaultAndASliceElementDoesNotExist is ADR-0005's
// array-against-slice asymmetry surfacing in a place ADR-0005 did not look.
//
// An array element is a static address and is walked either way, so it behaves
// like a struct field: element 1 has nothing on the plane and still takes its
// declaration. A slice element in the same position does not exist at all,
// because a slice's length is whatever the plane enumerated.
func TestAnArrayElementTakesItsDefaultAndASliceElementDoesNotExist(t *testing.T) {
	t.Parallel()

	t.Run("an array element with nothing on the plane", anArrayElementTakesItsDefault)
	t.Run("a slice element in the same position", aSliceElementDoesNotExist)
}

func anArrayElementTakesItsDefault(t *testing.T) {
	t.Parallel()

	src := planeSource{p: newPlane(map[Path]Value{At("arr").Elem(0).At("host"): String("h")})}

	got, err := Load[defArray](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	want := [2]defServer{{Host: "h", Port: 8080}, {Port: 8080}}
	if got.Arr != want {
		t.Errorf("the array loaded as %+v, want %+v", got.Arr, want)
	}
}

func aSliceElementDoesNotExist(t *testing.T) {
	t.Parallel()

	got, err := Load[defPool](t.Context(), treeSource{p: newPlane(map[Path]Value{})})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Pool != nil {
		t.Errorf("the sequence loaded as %+v, want nil: a declaration mints no element", got.Pool)
	}
}

type reqLeaf struct {
	V string `ferry:"v,required"`
}

type reqBytes struct {
	V []byte `ferry:"v,required"`
}

type reqPointer struct {
	V *int `ferry:"v,required"`
}

// presenceCase is one observation at a required leaf's address.
type presenceCase struct {
	name    string
	refused bool
	load    func(*testing.T) error
}

// TestRequiredIsAPresenceTestAndNothingElse is 5.1's most-cited consequence
// fixed. xload implements required as val == "" && meta.required, so FOO=
// cannot satisfy it there; here it is satisfied by any observation other than
// Absent, and a Null at a type that can hold one satisfies it while yielding
// nil, which is the user getting exactly what their type asked for.
func TestRequiredIsAPresenceTestAndNothingElse(t *testing.T) {
	t.Parallel()

	for _, c := range presenceCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkPresence(t, c)
		})
	}
}

func checkPresence(t *testing.T, c presenceCase) {
	t.Helper()

	err := c.load(t)
	if c.refused {
		mustBeMissingAt(t, err, At("v"))

		return
	}

	if err != nil {
		t.Fatalf("refused a present observation: %+v", err)
	}
}

func presenceCases() []presenceCase {
	return []presenceCase{{
		name:    "absent is the one observation that refuses",
		refused: true,
		load: func(t *testing.T) error {
			_, err := loadAt[reqLeaf](t, Value{})

			return err
		},
	}, {
		name: "an explicit empty satisfies it",
		load: func(t *testing.T) error {
			got, err := loadAt[reqLeaf](t, String(""))

			return mustHoldText(t, err, got.V, "")
		},
	}, {
		name: "and so does a value",
		load: func(t *testing.T) error {
			got, err := loadAt[reqLeaf](t, String("svc"))

			return mustHoldText(t, err, got.V, "svc")
		},
	}, {
		name: "a null at a leaf that has one satisfies it, and yields the nil",
		load: func(t *testing.T) error {
			got, err := loadAt[reqBytes](t, Null())

			return mustHoldNil(t, err, got.V)
		},
	}, {
		name: "a null at a pointer satisfies it, and yields nil",
		load: func(t *testing.T) error {
			got, err := loadAt[reqPointer](t, Null())

			return mustHoldNil(t, err, got.V)
		},
	}}
}

// mustHoldText reports a string leaf's value where the load succeeded, and
// hands the error back so the case says whether one was expected.
func mustHoldText(t *testing.T, err error, got, want string) error {
	t.Helper()

	if err == nil && got != want {
		t.Errorf("the field holds %q, want %q", got, want)
	}

	return err
}

// mustHoldNil is the same for a type with a null: required never means
// non-nil, so a Null satisfies it and yields the type's own zero.
func mustHoldNil[T any](t *testing.T, err error, got T) error {
	t.Helper()

	if err == nil && !reflect.ValueOf(&got).Elem().IsZero() {
		t.Errorf("the field holds %v, want the type's own nil", got)
	}

	return err
}

// loadAt presents one observation at /v and loads T over it.
func loadAt[T any](t *testing.T, got Value) (T, error) {
	t.Helper()

	return Load[T](t.Context(), planeSource{p: newPlane(map[Path]Value{At("v"): got})})
}

func mustBeMissingAt(t *testing.T, err error, want Path) {
	t.Helper()

	if err == nil {
		t.Fatal("an absent address satisfied required, and required is a presence test")
	}

	if !errors.Is(err, ErrMissing) {
		t.Errorf("%+v is not an ErrMissing", err)
	}

	// One address, one refusal. A pointer and the composite beneath it are one
	// address, so an option held at both would report it twice.
	if n := len(Elements(err)); n != 1 {
		t.Errorf("%+v holds %d elements, want one per address", err, n)
	}

	e, ok := errors.AsType[*Error](err)
	if !ok || e.Address() != want {
		t.Errorf("%+v does not name %s", err, want)
	}
}

// The two composite shapes required means one thing at, which is the repair
// ADR-0006 records and #118 filed against the draft that shipped the refusal
// instead: a plain struct's children come from the type exactly as a pointer's
// do, so required means the same thing at both.
type (
	reqSection struct {
		Auth cred `ferry:"auth,required"`
	}
	reqOptionalSection struct {
		Auth *cred `ferry:"auth,required"`
	}
	// reqDefaulted is the section whose every field could be filled from a
	// declaration, which is what makes "a default is not presence" observable
	// through required rather than only through a nil pointer.
	reqDefaulted struct {
		Auth defCred `ferry:"auth,required"`
	}
)

// TestRequiredAtACompositeIsTheStaticChildrenRule is ADR-0006's seven measured
// rows, run through a plane that can express a null at a container address and
// through one that cannot.
//
// The only row where the two could differ is an explicit null at the section's
// own address, which a flat plane cannot express, so the divergence cannot
// arise and every shared row is asserted against both.
func TestRequiredAtACompositeIsTheStaticChildrenRule(t *testing.T) {
	t.Parallel()

	shared := []struct {
		name   string
		values map[Path]Value
		check  func(*testing.T, string, error)
	}{
		{name: "nothing under the address", values: map[Path]Value{}, check: sectionRefused},
		{
			name:   "one static child",
			values: map[Path]Value{At("auth", "user"): String("u")},
			check:  sectionSatisfied,
		},
		{
			name:   "the other static child",
			values: map[Path]Value{At("auth", "pass"): String("p")},
			check:  sectionSatisfied,
		},
	}

	for _, c := range shared {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			for _, shape := range compositeShapes() {
				// Fresh contents per shape, because a plane a walk has already
				// been over is not the plane the next shape was meant to see.
				c.check(t, shape.name, shape.load(t, copyOf(c.values)))
			}
		})
	}

	t.Run("a null at the section's own address satisfies it, and yields nil", nullSatisfiesRequired)
	t.Run("an array, whose members are N static addresses", requiredAtAnArray)
}

type reqArray struct {
	Ports [2]string `ferry:"ports,required"`
}

// requiredAtAnArray is the third admissible composite. An array's N element
// addresses come from the type, which is the tier required is admissible in, so
// it means there exactly what it means at a struct.
func requiredAtAnArray(t *testing.T) {
	t.Parallel()

	empty, err := Load[reqArray](t.Context(), planeSource{p: newPlane(map[Path]Value{})})
	mustBeMissingAt(t, err, At("ports"))

	if empty != (reqArray{}) {
		t.Errorf("a refused load yielded %+v, want the zero value", empty)
	}

	one := map[Path]Value{At("ports").Elem(1): String("b")}
	if _, err := Load[reqArray](t.Context(), planeSource{p: newPlane(one)}); err != nil {
		t.Errorf("one element on the plane did not satisfy required: %+v", err)
	}
}

// sectionSatisfied is the row where the plane supplied at least one of the
// address's static children.
func sectionSatisfied(t *testing.T, shape string, err error) {
	t.Helper()

	if err != nil {
		t.Errorf("%s: refused a section the plane spoke under: %+v", shape, err)
	}
}

// sectionRefused is the row where it supplied none of them.
func sectionRefused(t *testing.T, shape string, err error) {
	t.Helper()

	if err == nil {
		t.Errorf("%s: satisfied required with nothing under the address", shape)

		return
	}

	mustBeMissingAt(t, err, At("auth"))
}

// requiredShape is one composite shape carrying required, loaded through a
// plane the caller supplies the contents of.
type requiredShape struct {
	name string
	load func(*testing.T, map[Path]Value) error
}

// compositeShapes is the pointer and the plain struct, which required means one
// thing at, and each through both a plane that can list and one that cannot.
func compositeShapes() []requiredShape {
	return []requiredShape{{
		name: "a plain struct, on a plane that cannot list",
		load: func(t *testing.T, v map[Path]Value) error {
			_, err := Load[reqSection](t.Context(), planeSource{p: newPlane(v)})

			return err
		},
	}, {
		name: "a plain struct, on a plane that can",
		load: func(t *testing.T, v map[Path]Value) error {
			_, err := Load[reqSection](t.Context(), treeSource{p: newPlane(v)})

			return err
		},
	}, {
		name: "a pointer, on a plane that cannot list",
		load: func(t *testing.T, v map[Path]Value) error {
			_, err := Load[reqOptionalSection](t.Context(), planeSource{p: newPlane(v)})

			return err
		},
	}, {
		name: "a pointer, on a plane that can",
		load: func(t *testing.T, v map[Path]Value) error {
			_, err := Load[reqOptionalSection](t.Context(), treeSource{p: newPlane(v)})

			return err
		},
	}, {
		name: "a section whose every field carries a declaration",
		load: func(t *testing.T, v map[Path]Value) error {
			_, err := Load[reqDefaulted](t.Context(), planeSource{p: newPlane(v)})

			return err
		},
	}}
}

// copyOf copies a case's contents, so one case's plane cannot be written
// through by the shape that ran before it.
func copyOf(v map[Path]Value) map[Path]Value {
	out := make(map[Path]Value, len(v))
	for k, got := range v {
		out[k] = got
	}

	return out
}

func nullSatisfiesRequired(t *testing.T) {
	t.Parallel()

	src := treeSource{p: newPlane(map[Path]Value{At("auth"): Null()})}

	got, err := Load[reqOptionalSection](t.Context(), src)
	if err != nil {
		t.Fatalf("a null at the section's own address refused required: %+v", err)
	}

	if got.Auth != nil {
		t.Errorf("the section loaded as %+v, want nil: required never means non-nil", got.Auth)
	}
}

// omitLeaves is the "before conversion" half in one struct: false and 0s are
// both non-empty text, and neither is written.
type omitLeaves struct {
	A string        `ferry:"a,omitzero"`
	B bool          `ferry:"b,omitzero"`
	D time.Duration `ferry:"d,omitzero"`
	K string        `ferry:"k"`
}

// omitShapes is omitzero at every type, which is the one option admissible
// everywhere, because it asks a question about the Go value rather than about
// an address.
type omitShapes struct {
	L string         `ferry:"l,omitzero"`
	S []string       `ferry:"s,omitzero"`
	M map[string]int `ferry:"m,omitzero"`
	P *cred          `ferry:"p,omitzero"`
	N cred           `ferry:"n,omitzero"`
	A [2]string      `ferry:"a,omitzero"`
	K string         `ferry:"k"`
}

// TestOmitzeroSkipsAFieldAtItsGoZeroValue is ADR-0006's omission rule, and the
// three interesting rows are the ones whose text is not empty: a false bool and
// a zero duration encode to "false" and "0s", and omitzero is a comparison
// against the Go value evaluated before anything converts it.
func TestOmitzeroSkipsAFieldAtItsGoZeroValue(t *testing.T) {
	t.Parallel()

	t.Run("at a leaf, before conversion", func(t *testing.T) {
		t.Parallel()
		mustWriteExactly(t, func(s Sink) error { return Dump(t.Context(), omitLeaves{}, s) }, []string{"/k"})
	})

	t.Run("and a non-zero value is written", func(t *testing.T) {
		t.Parallel()

		v := omitLeaves{A: "a", B: true, D: time.Second}
		mustWriteExactly(t, func(s Sink) error { return Dump(t.Context(), v, s) },
			[]string{"/a", "/b", "/d", "/k"})
	})

	t.Run("at every type in the set", func(t *testing.T) {
		t.Parallel()
		mustWriteExactly(t, func(s Sink) error { return Dump(t.Context(), omitShapes{}, s) }, []string{"/k"})
	})

	t.Run("and nothing beneath a skipped composite either", func(t *testing.T) {
		t.Parallel()

		v := omitShapes{N: cred{User: "u"}, P: &cred{}}
		mustWriteExactly(t, func(s Sink) error { return Dump(t.Context(), v, s) },
			[]string{"/k", "/n/pass", "/n/user", "/p/pass", "/p/user"})
	})
}

// mustWriteExactly runs one dump and holds it to the addresses it handed the
// sink, which is what an omission is: the absence of a Set call rather than a
// Set carrying nothing.
func mustWriteExactly(t *testing.T, dump func(Sink) error, want []string) {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := dump(planeSink{p: p}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got := slices.Clone(p.set)
	slices.SortFunc(got, Path.Compare)

	mustBeAddresses(t, got, want)
}

type dumpedDefault struct {
	Port int    `ferry:"port,default=8080"`
	Name string `ferry:"name"`
}

// TestAFieldAtItsDefaultIsDumped is the rule that has two independent reasons
// and needs only the first. ferry cannot tell "still at its default" from
// "explicitly set to the same value", because they are the same bits; and
// omitting it would make the stored artefact under-specified, so what it
// denotes would be decided by whichever version of the code read it.
func TestAFieldAtItsDefaultIsDumped(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		port int
	}{
		{name: "a field still holding its declared default", port: 8080},
		{name: "and one explicitly set to the zero value", port: 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			mustWriteExactly(t, func(s Sink) error {
				return Dump(t.Context(), dumpedDefault{Port: c.port}, s)
			}, []string{"/name", "/port"})
		})
	}
}

// TestTheFiveSchemaCompileRefusals is ADR-0006's list, driven by ADR-0008's
// grammar rather than by the placeholder that stood in for it.
func TestTheFiveSchemaCompileRefusals(t *testing.T) {
	t.Parallel()

	run(t, slices.Concat(unparseableDefaults(), inadmissibleDeclarations(), contradictions()))
}

// unparseableDefaults is the first refusal: every declaration is checked from
// reflect.TypeFor[T]() alone, with no value in hand and no plane reachable.
func unparseableDefaults() []compileCase {
	return []compileCase{{
		name: "a default that is not a valid int",
		run: Compile[struct {
			P int `ferry:"p,default=abc"`
		}],
		want:     []string{`/p: default "abc" is not a valid int`, "invalid syntax"},
		elements: 1,
	}, {
		name: "a default outside the field's own width",
		run: Compile[struct {
			B int8 `ferry:"b,default=99999"`
		}],
		want:     []string{`/b: default "99999" is not a valid int8`, "out of range"},
		elements: 1,
	}, {
		name: "a duration with no unit, which is the leaf's own parser and not a general one",
		run: Compile[struct {
			T time.Duration `ferry:"t,default=30"`
		}],
		want:     []string{`/t: default "30" is not a valid time.Duration`, "missing unit"},
		elements: 1,
	}, {
		name: "and a byte array given the wrong number of bytes, which has no cause to name",
		run: Compile[struct {
			K [4]byte `ferry:"k,default=abc"`
		}],
		want:     []string{`/k: default "abc" is not a valid [4]uint8`, "parsed by exactly the parser"},
		elements: 1,
	}}
}

// inadmissibleDeclarations is the second and third refusals, which are the two
// about where an option may be written at all.
func inadmissibleDeclarations() []compileCase {
	return []compileCase{{
		name: "a default on a composite",
		run: Compile[struct {
			Tags []string `ferry:"tags,default=a"`
		}],
		want:     []string{"/tags: []string is not a leaf", "seed the value instead"},
		elements: 1,
	}, {
		name: "required on a slice, with the remedy the user's intent deserves",
		run: Compile[struct {
			Origins []string `ferry:"origins,required"`
		}],
		want: []string{
			"/origins: required is not available on []string",
			"a plane cannot report present and empty at a container address",
			"model the distinction as a struct with a set flag, or check len() after Load",
		},
		elements: 1,
	}, {
		name: "required on a map",
		run: Compile[struct {
			Limits map[string]int `ferry:"limits,required"`
		}],
		want:     []string{"/limits: required is not available on map[string]int"},
		elements: 1,
	}, {
		name: "required on a pointer to a slice",
		run: Compile[struct {
			Origins *[]string `ferry:"origins,required"`
		}],
		want:     []string{"/origins: required is not available on []string"},
		elements: 1,
	}, {
		name: "required on a pointer to a map",
		run: Compile[struct {
			Limits *map[string]int `ferry:"limits,required"`
		}],
		want:     []string{"/limits: required is not available on map[string]int"},
		elements: 1,
	}}
}

// contradictions is the fourth and fifth refusals, which are the two about two
// options that are each legal here and disagree with one another.
func contradictions() []compileCase {
	return []compileCase{{
		name: "required with a default",
		run: Compile[struct {
			S string `ferry:"s,required,default=x"`
		}],
		want:     []string{"/s: required and default contradict", "answers the absence required forbids"},
		elements: 1,
	}, {
		name: "omitzero with a default that is not the field's zero value",
		run: Compile[struct {
			B int `ferry:"b,omitzero,default=8080"`
		}],
		want: []string{
			"/b: omitzero and default=8080 contradict",
			"an explicit zero would be omitted and would load back as the default",
		},
		elements: 1,
	}}
}

// TestOmitzeroWithAZeroDefaultCompiles is the row that is not a contradiction:
// omitting a value equal to the default and reapplying it land on the same
// value.
func TestOmitzeroWithAZeroDefaultCompiles(t *testing.T) {
	t.Parallel()

	if err := Compile[struct {
		C int `ferry:"c,omitzero,default=0"`
	}](); err != nil {
		t.Errorf("omitzero beside a zero default did not compile: %+v", err)
	}
}

// TestRequiredOnAPlainStructCompilesAndIsEnforced is #118 closed on ADR-0006's
// own terms.
//
// The draft accepted required on a non-pointer struct and enforced it with
// nothing; a later draft refused it at schema compile, with the first draft's
// own reasoning as the message. ADR-0006 says it is admissible and means what it
// means on *struct, which is what TestRequiredAtACompositeIsTheStaticChildrenRule
// asserts and this one pins at the compiler.
func TestRequiredOnAPlainStructCompilesAndIsEnforced(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		run  func(...Option) error
	}{
		{"a plain struct", Compile[reqSection]},
		{"a pointer to one", Compile[reqOptionalSection]},
		{"an array, whose N element addresses come from the type", Compile[struct {
			Ports [3]string `ferry:"ports,required"`
		}]},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.run(); err != nil {
				t.Errorf("required was refused where its children come from the type: %+v", err)
			}
		})
	}
}

// TestAdmissibilityIsCheckedBeforeContradictions is the diagnostic rule that
// keeps one field's single mistake from reporting as three errors: a
// contradiction between two options is only meaningful if both are individually
// legal there.
func TestAdmissibilityIsCheckedBeforeContradictions(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "two inadmissible options and no contradiction between them",
		run: Compile[struct {
			O []string `ferry:"o,required,default=v"`
		}],
		want: []string{
			"/o: required is not available on []string",
			"/o: []string is not a leaf",
		},
		elements: 2,
	}, {
		name: "a default that does not parse has no value to compare against zero",
		run: Compile[struct {
			P int `ferry:"p,omitzero,default=abc"`
		}],
		want:     []string{`/p: default "abc" is not a valid int`},
		elements: 1,
	}, {
		name: "and where both are admissible, the contradiction is the one report",
		run: Compile[struct {
			S string `ferry:"s,required,default=x"`
		}],
		want:     []string{"/s: required and default contradict"},
		elements: 1,
	}})
}

// defEmpty is ADR-0006's one grammar requirement, asserted through a load
// rather than through the compiler: an empty default has to be expressible and
// has to stay distinguishable from no default at all, because "" is a
// legitimate value and "leave the field alone" is a different instruction.
type defEmpty struct {
	WithEmpty string `ferry:"a,default="`
	WithNone  string `ferry:"b"`
	WithText  string `ferry:"c,default=x"`
}

func TestAnEmptyDefaultIsDistinctFromNoDefault(t *testing.T) {
	t.Parallel()

	seed := defEmpty{WithEmpty: "seeded", WithNone: "seeded", WithText: "seeded"}

	got, err := LoadOver(t.Context(), seed, planeSource{p: newPlane(map[Path]Value{})})
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	want := defEmpty{WithNone: "seeded", WithText: "x"}
	if got != want {
		t.Errorf("an empty plane left %+v, want %+v", got, want)
	}
}

// TestDefaultWithNoValueIsRefused is the other half of the same requirement.
// The option's value is not optional, because "default" on its own would have
// to mean one of the two things the pair above keeps apart.
func TestDefaultWithNoValueIsRefused(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "default with no equals sign",
		run: Compile[struct {
			A string `ferry:"a,default"`
		}],
		want: []string{
			`option "default" needs a value`,
			"default= on its own is the empty string",
		},
		elements: 1,
	}})
}

// defBase64 is ADR-0007's documentation obligation, discharged with the
// measurement rather than the sentence.
type defBase64 struct {
	Secret []byte `ferry:"secret,default=aGk="`
}

// TestADeclaredDefaultIsNotDecoded is the sharp edge stated as narrowly as it
// is true. A declared default is text, schema compile turns it into a String
// Value, and String donates to Bytes as a relabel, so the field holds the four
// bytes that were written in the tag. Base64 is not ferry's business, and how a
// plane spells bytes is the driver's.
func TestADeclaredDefaultIsNotDecoded(t *testing.T) {
	t.Parallel()

	got := loadEmpty[defBase64](t)

	if string(got.Secret) != "aGk=" {
		t.Errorf("the default landed as %q, want the four bytes aGk= rather than the decoded hi", got.Secret)
	}
}
