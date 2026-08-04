package ferrytest

import (
	"context"
	"reflect"

	"github.com/onhotpath/ferry"
)

// Plane describes one driver's plane to the suites in this package, and it is
// what every suite here takes instead of a driver type.
//
//	ferrytest.Driver(t, ferrytest.Plane{
//	    Name:  "yaml",
//	    Kinds: []ferry.VKind{ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
//	        ferry.KindNumber, ferry.KindString, ferry.KindBytes},
//	    Except: notUTF8,
//	    Open:   func() ferrytest.Instance { ... },
//	    Golden: []ferrytest.Artefact{ferrytest.Golden(cfg, "b: !!binary aGk=\n")},
//	})
//
// Nothing a suite needs about a plane is a method: what to call it in a report,
// which kinds it can carry, how to mint a fresh empty one, and what its own
// spelling of a known value looks like. So a driver this package has never heard
// of is described in a struct literal, and there is no interface to implement.
//
// [Instance] is one minted plane, which is what [Plane.Open] hands back.
type Plane struct {
	// Name labels the plane in a report. It is a label and never a key: two
	// planes with one name are a confusing report and not a collision.
	Name string

	// Kinds is every kind this plane carries end to end, declared by the driver
	// about itself.
	//
	// End to end, and not what your Get returns. A flat plane stores everything
	// as text and hands every value back as a String, and it still carries Bool
	// and Number, because a bool written to it comes back as the same bool and
	// a number as the same number. What is declared is what survives the trip.
	//
	// It is an obligation in both directions, and the second one is where
	// drivers get this wrong.
	//
	// For a kind you declare, the suite runs the proofs that need it and
	// expects them to pass. For a kind you do not declare, the suite writes a
	// value of it anyway and expects your driver to refuse it loudly. A value
	// of an undeclared kind that is quietly stored, stored as something else,
	// or stored and read back as something else, is a failure.
	//
	// So this is a declaration and not a wish. Declaring a kind you cannot
	// carry fails a proof; omitting one you can carry stops proving it and
	// demands a refusal you will not make. A flattening plane with no null -
	// environment variables, query parameters, an opaque key-value store -
	// declares Absent, Bool, Number, String and Bytes, and omitting Bool and
	// Number from that list demands a store that refuses every boolean and
	// every port number.
	//
	// Declaring a kind and then refusing one value of it is a failure and not a
	// refusal. [Plane.Except] is how a plane whose format carries a kind but not
	// every value of it says so.
	Kinds []ferry.VKind

	// Except narrows [Plane.Kinds] to the values inside a declared kind that
	// this plane's own format cannot spell. It is nil for a plane that can spell
	// every value of every kind it declares.
	//
	// Kinds is kind-granular and a format need not be. driver/yaml is the
	// example: a Go string is a byte sequence and a YAML string is a Unicode
	// one, so the plane carries String and cannot carry the strings that are not
	// valid UTF-8. Neither half of Kinds can say that. Dropping String would
	// disclaim every ordinary string the plane carries perfectly, and declaring
	// String and then refusing a value of it is a failure.
	//
	//	Except: func(v ferry.Value) bool {
	//	    s, err := v.AsString()
	//	    return err == nil && !utf8.ValidString(s)
	//	},
	//
	// It applies per case rather than per proof, so the string cases this plane
	// does carry still run, and a narrowed proof keeps each case's own number so
	// a report names the case as [CoreTypes] spells it.
	//
	// It is not a way to drop an inconvenient case. An excepted value goes to
	// the refusal half of the suite, exactly where a kind the plane never
	// declared goes, so excepting a value buys a loud refusal the driver has to
	// actually make. A driver that mangles it instead is reported the same way
	// as one that mangles an undeclared kind.
	//
	// It is a predicate and not a list, because what is being declared is a
	// property of the format - "a string that is not valid UTF-8" - rather than
	// a statement about whichever values the suite happens to carry today.
	Except func(v ferry.Value) bool

	// Open mints a fresh, empty [Instance] of the plane, on every call.
	//
	// Fresh on every call is the requirement, not the convention. A temp file,
	// a new map, a new fake store - built inside this closure, never hoisted out
	// of it. A plane shared across cases is the defect that hides a broken
	// second walk, and it is the mistake this field exists to make impossible.
	//
	//	Open: func() ferrytest.Instance {
	//	    path := filepath.Join(t.TempDir(), "plane.yaml")
	//	    return ferrytest.Instance{
	//	        Source:   yaml.NewSource(path),
	//	        Sink:     yaml.NewSink(path),
	//	        Contents: func() ([]byte, error) { return os.ReadFile(path) },
	//	    }
	//	},
	Open func() Instance

	// Golden pins this driver's own spelling of a fixed value, byte for byte,
	// and it is empty for a plane that has no serialization format.
	//
	// It is what catches an encoder and a decoder that are wrong in the same
	// direction, which no round trip can see: a round trip tests a function
	// against its own inverse, so changing both halves together is invisible to
	// it. What is pinned is the driver author's own choice, because ferry
	// constrains no indentation and no key order.
	//
	// Build the rows with [Golden]. Checking them needs [Instance.Contents], and
	// a Plane that pins a spelling while yielding no way to read it is reported
	// rather than quietly skipped.
	Golden []Artefact
}

// Instance is one freshly minted plane: both halves of it over one set of
// contents, and the way to read those contents back as the plane spells them.
//
// [Plane.Open] returns one, and returns a new one every time it is called.
// Both halves come back together, in one call, because two halves over
// different contents is the mistake a round trip cannot detect: the save would
// succeed, the load would report everything missing, and nothing would say why.
type Instance struct {
	// Source is the read half.
	Source ferry.Source

	// Sink is the write half, over the same contents as [Instance.Source].
	//
	// It is nil for a plane that has no honest way to write - environment
	// variables are the case - and the suite then runs the read-side cases only.
	Sink ferry.Sink

	// InContext puts this instance's contents into a context, and it is what a
	// driver whose plane is obtained freshly per load fills in. It is nil for
	// every plane whose halves already hold their own contents, which is every
	// plane that does not read one from a [context.Context].
	//
	// A driver like that ships a constructor for a source that carries no
	// plane, and a second one that puts a plane into a context. Both go here,
	// and the whole of it is the driver's own two calls:
	//
	//	Open: func() ferrytest.Instance {
	//	    v := url.Values{}
	//	    return ferrytest.Instance{
	//	        Source:    ferryhttp.NewQuerySource(),
	//	        InContext: func(ctx context.Context) context.Context {
	//	            return ferryhttp.WithQuery(ctx, v)
	//	        },
	//	    }
	//	},
	//
	// A sink whose plane is per request fills it in exactly the same way, and a
	// plane with both halves supplies both of them from this one function,
	// because an instance is both halves over one set of contents.
	//
	// Set it and every case runs its own I/O under the context this returns, so
	// the whole suite reaches the plane the way a request would. Leave it nil
	// and every case runs under [context.Background], which is what it has
	// always done.
	//
	// Two obligations. It closes over contents minted inside [Plane.Open] and
	// never over contents hoisted out of it, which is the shared plane Open
	// exists to make impossible. And it supplies the same contents on every
	// call, because one case opens the plane more than once and each open has
	// to find what the last one wrote.
	//
	// It is also what makes the per-request refusal checkable: the suite calls
	// this to supply the plane, and deliberately does not call it to assert that
	// a load with no plane in the context is refused at the open.
	InContext func(ctx context.Context) context.Context

	// Contents yields this instance's raw contents, exactly as the plane holds
	// them, and it is what makes [Plane.Golden] checkable.
	//
	// For a file-backed plane the whole implementation is
	// `func() ([]byte, error) { return os.ReadFile(path) }`. It is read after
	// the save has finished and after any [ferry.Committer] has committed, so a
	// driver that stages is never asked what it has not written yet.
	//
	// It is nil for a plane with no serialization format, which is [MemPlane]:
	// it stores the boundary [ferry.Value] itself, so there is no representation
	// for a golden row to hold. Leaving both this and [Plane.Golden] empty skips
	// the golden case. Leaving only this one nil, while pinning a spelling, is
	// reported rather than quietly passing.
	Contents func() ([]byte, error)
}

// ctx is the context one instance's I/O runs under, and every case in this
// package opens, reads, dumps and loads through it.
//
// It is [context.Background] for a plane whose halves hold their own contents,
// which is what every case used before [Instance.InContext] existed and is why
// a plane that sets nothing is unaffected. For a per-request plane it is
// ADR-0012's channel: the plane instance travels in the context, using the
// driver's own key, and core supplies no mechanism for it - so the suite has
// none either, and asks the description for a context instead.
//
// It is called per use rather than once per instance, which is why InContext
// documents that it supplies the same contents every time. A decorator closed
// over what Open minted satisfies that for free; one that mints fresh contents
// per call is the hoisted plane ADR-0014's fresh-destination rule refuses, and
// it would fail case 1 rather than pass quietly.
func (i Instance) ctx() context.Context {
	if i.InContext == nil {
		return context.Background()
	}

	return i.InContext(context.Background())
}

// Artefact is one fixed value and the plane contents that saving it must
// produce, byte for byte. [Golden] is the only way to build one.
//
// It is opaque on purpose, because the row has to capture its value's Go type
// while the compiler still has it: [ferry.Dump] takes its schema from its type
// parameter, so a field of type `any` would be the schema of nothing. That is
// what lets [Plane.Golden] hold rows of different types side by side.
//
// A change to one of these rows changes what every file, key or variable that
// plane has ever written means. It is a major version of the module that owns
// the driver, and not a test fixture edit.
type Artefact struct {
	// dump is [ferry.Dump] instantiated at the row's own type, closed over the
	// value the row pins.
	dump func(ctx context.Context, sink ferry.Sink, opts ...ferry.Option) error

	// want is the exact contents the plane must hold afterwards, compared
	// against what [Instance.Contents] yields.
	//
	// One string, which fits a plane holding one document and puts an
	// obligation on a plane holding more than one storage unit: a key-value
	// store is a set of pairs with no document, so what it renders for this
	// comparison must be deterministic and injective over stores. That is the
	// same obligation ADR-0003 already puts on such a driver's key function,
	// which is why it is stated rather than solved here: a rendering two
	// different stores share is a golden row that cannot see the difference,
	// exactly as a plane key two addresses share is data loss.
	want string

	// label names the row's type in a report, because a row that carries a
	// closure has nothing else a failure could name.
	label string
}

// Golden pins one value's spelling on a plane: dumping v must leave the plane
// holding exactly want.
//
//	Golden: []ferrytest.Artefact{
//	    ferrytest.Golden(struct {
//	        B []byte `ferry:"b"`
//	    }{[]byte("hi")}, "b: !!binary aGk=\n"),
//	},
//
// v is an ordinary annotated struct, exactly as it is at a [ferry.Dump] call
// site. A bare leaf is refused there for naming no address, so it is refused
// here too.
//
// want is what [Instance.Contents] must yield afterwards. A plane holding more
// than one storage unit - a key-value store is a set of pairs and not a
// document - has to render itself deterministically for this comparison to mean
// anything, and rendering two different stores alike is a row that cannot see
// the difference.
func Golden[T any](v T, want string) Artefact {
	return Artefact{
		dump: func(ctx context.Context, sink ferry.Sink, opts ...ferry.Option) error {
			return ferry.Dump(ctx, v, sink, opts...)
		},
		want:  want,
		label: reflect.TypeFor[T]().String(),
	}
}

// MemPlane is the plane with nothing of its own: a map from address to
// [ferry.Value], with no serialization format, no I/O and no key function beyond
// the identity.
//
//	ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs, ferry.WithRegistry(reg))
//
// It is what a codec author proves a registered type against, and it is where
// ferry's value-fidelity guarantee is visible, because it is the only plane that
// adds nothing between the value and what comes back.
//
// It carries all six kinds. Each call to the returned Plane's Open mints an
// empty plane, and the read and write halves it hands back share one set of
// contents. There is no Golden, because a plane with no format has no spelling
// to pin.
//
// Four properties to rely on rather than fields to set: it keys by the canonical
// rendering of an address, it never folds case and never normalises a name, it
// refuses a duplicate write loudly rather than overwriting, and it enumerates in
// address order rather than in Go's map order.
//
// It is the wrong plane to prove ferry's key-collision rule with, and that is
// worth knowing rather than a defect. A plane keying by the canonical rendering
// can never make two addresses collide, so a run against this one says nothing
// about the check a flattening driver has to make.
func MemPlane() Plane {
	return Plane{
		Name: "memory",
		Kinds: []ferry.VKind{
			ferry.KindAbsent, ferry.KindNull, ferry.KindBool,
			ferry.KindNumber, ferry.KindString, ferry.KindBytes,
		},
		Open: func() Instance {
			s := newMemStore()

			return Instance{Source: memSource{store: s}, Sink: memSink{store: s}}
			// No Contents, for the same reason there is no Golden below: this
			// plane has no serialization format, so there is nothing raw for it
			// to hand back, and the golden artefact case is skipped for it.
		},
		// No Golden. A golden artefact pins a driver's spelling, and this plane
		// has no spelling: it stores the boundary Value itself, so there is no
		// representation for a row to hold and nothing a future change could
		// silently alter (ADR-0013).
	}
}

// Static is a source of constants: the contents are fixed when it is built and
// nothing writes to it afterwards.
//
// It is the plane an ordinary user reaches for, who is not testing ferry at
// all and wants a config struct filled from a literal rather than from a file:
//
//	cfg, err := ferry.Load[Config](ctx, ferrytest.Static(map[ferry.Path]ferry.Value{
//	    ferry.At("port"):    ferry.Number("8080"),
//	    ferry.At("timeout"): ferry.String("30s"),
//	}))
//
// How it differs from [MemPlane]: this is a [ferry.Source] over contents you
// supplied, and there is no Sink beside it, so [ferry.Dump] into it does not
// compile. MemPlane describes a read-write plane that starts empty and is minted
// fresh on every Open, which is what a conformance run needs and what a user
// filling in a config does not.
//
// The map is copied, so a later write into your map cannot reach a source
// already handed out. It shares everything else with the memory plane,
// including keying by the canonical rendering of an address and never folding
// case.
func Static(values map[ferry.Path]ferry.Value) ferry.Source {
	s := newMemStore()
	for addr, v := range values {
		s.put(addr, v)
	}

	return memSource{store: s}
}
