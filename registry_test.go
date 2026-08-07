package ferry

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every assertion in this file goes through the exported surface: a
// registration through NewRegistry, a claim through what Compile refused or what
// a recording driver's Bind was handed, and a representation through what a
// plane was written. Nothing reaches the compiled schema, the node tree or the
// registry's map.
//
// The one thing asserted structurally rather than behaviourally is the shape of
// the API itself - that no registration by reflect.Type exists, and that a
// registration has no exported accessor - because those are claims about what is
// absent, and absence has no behaviour to observe it through.

// pollInterval is a named type over time.Duration, which ADR-0005 left as a
// documented sharp edge and DurationLike closes.
type pollInterval time.Duration

// retryCount is a named int, registered under a null policy so that its codec
// accepts a Null and returns the Go zero. Its kind admits it already, and the
// registration is what gives the type a null it does not otherwise have
// (ADR-0006).
type retryCount int

// plainCount is retryCount's counterpart with no null policy, which is the
// measured reason [NullValue] exists: a payload-typed decode half never sees a
// Null at all (ADR-0017).
type plainCount int

// host is a struct with no text pair, so nothing but a registration collapses
// it to a leaf.
type host struct {
	Name string `ferry:"name"`
	Port int    `ferry:"port"`
}

func hostText(h host) (string, error) { return h.Name + ":" + strconv.Itoa(h.Port), nil }

func parseHost(text string) (host, error) {
	name, port, ok := strings.Cut(text, ":")
	if !ok {
		return host{}, errNotAHost
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return host{}, err
	}

	return host{Name: name, Port: n}, nil
}

var errNotAHost = errors.New("not a host")

// greeter is the interface case, which ADR-0005 makes the headline
// demonstration that a codec collapses a type to a leaf: an interface has no
// address set of its own, and its zero value is a nil interface with no
// receiver to call.
type greeter interface{ greeting() string }

// wave is greeter's one implementation, and it is a struct with its own two
// addresses, which is what makes "the registration claims the interface and not
// the implementation" observable as two different address sets.
type wave struct {
	Name string `ferry:"name"`
	Loud bool   `ferry:"loud"`
}

func (w wave) greeting() string { return w.Name }

// greeterCodec is the interface case: a null policy over a string registration,
// so a nil interface writes a Null and a Null loads back as a nil. That is the
// mechanism that makes an interface expressible at all, and it is closed under
// isNull(load()) because load returns the nil the policy calls null.
func greeterCodec() Codec {
	return NullValue(
		StringValue(greeterText, parseGreeter),
		func() (greeter, error) { return nil, nil }, //nolint:nilnil // a nil greeter is what a Null carries.
		func(g greeter) bool { return g == nil },
	)
}

func greeterText(g greeter) (string, error) { return g.greeting(), nil }

func parseGreeter(s string) (greeter, error) { return wave{Name: s}, nil }

// severity is a type that already declares a text pair, so ADR-0007's chain
// claims it at String with nothing registered. It is the fixture for the rule
// that a registration is step one and beats a text pair the type already has.
type severity int

const warn severity = 4

var severityNames = map[severity]string{warn: "warn"}

func (s severity) MarshalText() ([]byte, error) { return []byte(severityNames[s]), nil }

func (s *severity) UnmarshalText(text []byte) error {
	for k, v := range severityNames {
		if v == string(text) {
			*s = k

			return nil
		}
	}

	*s = 0

	return nil
}

// The registrations the inference test writes, lifted out only because a method
// expression needs a value receiver: url.URL.String is
// "invalid method expression url.URL.String (needs pointer receiver)", which is
// a property of the standard library's receivers and the difference between a
// one-line registration and a seven-line one.
func urlText(u url.URL) (string, error) { return u.String(), nil }

func parseURL(text string) (url.URL, error) {
	u, err := url.Parse(text)
	if err != nil {
		return url.URL{}, err
	}

	return *u, nil
}

func macText(a net.HardwareAddr) (string, error) { return a.String(), nil }

func parseMAC(text string) (net.HardwareAddr, error) {
	if text == "" {
		return nil, nil
	}

	return net.ParseMAC(text)
}

func countText(c plainCount) (string, error) { return strconv.Itoa(int(c)), nil }

func parseCount(text string) (plainCount, error) {
	n, err := strconv.Atoi(text)

	return plainCount(n), err
}

func bigText(x big.Int) (string, error) { return x.String(), nil }

func parseBig(s string) (big.Int, error) {
	var x big.Int

	if _, ok := x.SetString(s, numBase); !ok {
		return x, errNotAnInteger
	}

	return x, nil
}

var errNotAnInteger = errors.New("not an integer")

// retryCodec is the escape hatch ADR-0006's strictness rests on: a plain int
// refuses a Null, and a registration carrying a null policy for its own type
// accepts one and returns 0.
func retryCodec() Codec {
	return NullValue(
		NumberValue(retryText, parseRetry),
		func() (retryCount, error) { return 0, nil },
		func(c retryCount) bool { return c == 0 },
	)
}

func retryText(c retryCount) (string, error) { return strconv.Itoa(int(c)), nil }

func parseRetry(s string) (retryCount, error) {
	n, err := strconv.Atoi(s)

	return retryCount(n), err
}

// mustRefuse holds a refusal to naming every part of the diagnosis a reader
// has to act on, which is the whole of what a message is for here: a
// registration is refused at a call site with no address and no field, so the
// text is all there is.
func mustRefuse(t *testing.T, err error, want ...string) {
	t.Helper()

	if err == nil {
		t.Fatalf("no failure was reported, and one containing %q was expected", want)
	}

	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal is %q, and does not contain %q", err, w)
		}
	}
}

// registryWith builds a fresh registry per test, because a registry is complete
// at birth and a shared one would make every test read a table it did not write.
func registryWith(t *testing.T, codecs ...Codec) *Registry {
	t.Helper()

	return NewRegistry(codecs...)
}

// mustRefuseAtConstruction is [mustRefuse] for the refusals [NewRegistry] and
// the constructors raise, which are panics rather than errors: a registry is
// complete at birth, so there is no call left to return an error from.
//
// It recovers the panic and asserts the value is ferry's own located error, so
// that a caller who wraps a NewRegistry in a recover reads the report ferry
// gives every other refusal rather than a bare string (ADR-0017).
func mustRefuseAtConstruction(t *testing.T, build func(), want ...string) {
	t.Helper()

	err := refusalFrom(build)
	if err == nil {
		t.Fatalf("no panic was raised, and one containing %q was expected", want)
	}

	mustRefuse(t, err, want...)

	if !errors.Is(err, ErrSchema) {
		t.Errorf("the refusal is %v, and does not answer to ErrSchema", err)
	}
}

// refusal runs build and reports the *Error it panicked with, or nil where it
// returned. A panic with anything else fails the test where it happened.
func refusalFrom(build func()) (err error) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}

		fe, ok := p.(*Error)
		if !ok {
			panic(p)
		}

		err = fe
	}()

	build()

	return nil
}

// TestInferenceWorksAtEveryCallSiteWithAValueArgument is ADR-0009's ergonomic
// claim, and the assertion is that this function compiles.
//
// Ten registrations, written the way a user would write them, and not one
// carries an explicit type argument. Five of them are one line, because T is
// inferred from a method expression on one side and a package parse function on
// the other; the rest cost a wrapper because the standard library put the
// receiver the other way round, which is a property of those types rather than
// of this API.
//
// What is asserted at run time is that inference picked the right T, which is
// read off the registry's own member list rather than off any Codec.
func TestInferenceWorksAtEveryCallSiteWithAValueArgument(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(
		StringValue(macText, parseMAC),
		StringValue(countText, parseCount),
		StringValue(hostText, parseHost),
		StringValue(urlText, parseURL),
		NumberValue(bigText, parseBig),
		greeterCodec(),
		retryCodec(),
	)

	want := []string{
		"github.com/onhotpath/ferry.greeter", "github.com/onhotpath/ferry.host",
		"github.com/onhotpath/ferry.plainCount", "github.com/onhotpath/ferry.retryCount",
		"math/big.Int", "net.HardwareAddr", "net/url.URL",
	}

	mustHoldTypes(t, reg, want)
}

// mustHoldTypes asserts a registry's membership through Types, which is the one
// thing this package exports for ferrytest's sake.
func mustHoldTypes(t *testing.T, reg *Registry, want []string) {
	t.Helper()

	got := make([]string, 0, len(want))
	for _, m := range reg.Types() {
		got = append(got, qualified(m))
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("the registry holds\n\t%v\nwant\n\t%v", got, want)
	}
}

// qualified renders a type by its package path rather than by its short name,
// because two types can share the short one and the whole of ADR-0005's
// identity rule is that ferry never confuses them.
func qualified(t reflect.Type) string {
	if t.PkgPath() == "" {
		return t.String()
	}

	return t.PkgPath() + "." + t.Name()
}

// TestTheZeroValueCheckRefuses is ADR-0009's check on the three registrations
// it was written for.
//
// The refusals are the point. String and Parse is the shape a user is most
// likely to reach for, it is not an inverse at the zero value for three common
// standard-library types, and registration is step one of the chain, so
// registering it replaces a correct text pair with a codec that dumps
// string("invalid IP") and cannot load it back: the type worked before the user
// tried to help it.
func TestTheZeroValueCheckRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		codec Codec
	}{
		{name: "netip.Addr through String and ParseAddr", codec: StringValue(addrText, netip.ParseAddr)},
		{name: "netip.AddrPort through String and ParseAddrPort",
			codec: StringValue(addrPortText, netip.ParseAddrPort)},
		{name: "netip.Prefix through String and ParsePrefix",
			codec: StringValue(prefixText, netip.ParsePrefix)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuseAtConstruction(t, func() { NewRegistry(c.codec) }, "not total over the zero value")
		})
	}
}

// The three one-liners a registrant is most likely to reach for, and the three
// that are not an inverse at the zero value.
func addrText(a netip.Addr) (string, error) { return a.String(), nil }

func addrPortText(a netip.AddrPort) (string, error) { return a.String(), nil }

func prefixText(p netip.Prefix) (string, error) { return p.String(), nil }

// TestTheZeroValueCheckAccepts is the other four registrations ADR-0009
// measured, and the interface is the case that had to keep working: its zero is
// a nil interface, it emits Null, and it accepts Null back.
func TestTheZeroValueCheckAccepts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		codec Codec
	}{
		{name: "netip.Addr through its text pair", codec: StringText[netip.Addr]()},
		{name: "url.URL through two wrappers", codec: StringValue(urlText, parseURL)},
		{name: "a named duration", codec: DurationLike[pollInterval]()},
		{name: "an interface, whose zero writes Null and takes Null back", codec: greeterCodec()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := refusalFrom(func() { NewRegistry(c.codec) }); err != nil {
				t.Fatalf("the registration was refused: %+v", err)
			}
		})
	}
}

// TestTheZeroCheckReportsWhatTheCodecEncodedTo holds the diagnostic to naming
// both halves of what went wrong, because a registrant reading "your codec is
// wrong" cannot act on it.
func TestTheZeroCheckReportsWhatTheCodecEncodedTo(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() { NewRegistry(StringValue(addrText, netip.ParseAddr)) },
		"netip.Addr", `string("invalid IP")`, "decoding that back fails")
}

// TestAnEncodeFailureAtTheZeroValueIsRefused is the other half of the check,
// which the netip cases cannot reach: a codec whose encode half errors never
// gets as far as decoding.
func TestAnEncodeFailureAtTheZeroValueIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() {
		NewRegistry(StringValue(
			func(retryCount) (string, error) { return "", errNotAnInteger },
			func(string) (retryCount, error) { return 0, nil }))
	}, "encoding one failed")
}

// TestARegistrationRefusalKeepsItsOwnClass is #228: a refusal about a
// registration call site stays a schema refusal whatever the registrant's own
// error was made of.
//
// ADR-0011 invites a registrant to wrap ferry's sentinels in the errors their
// codec returns, and the walk is where that opinion counts. A registration is
// not that moment: nothing has been read and no plane has been reached, so a
// codec whose zero value fails with an ErrPlane inside it must not turn "your
// registration is wrong" into "the plane failed".
func TestARegistrationRefusalKeepsItsOwnClass(t *testing.T) {
	t.Parallel()

	err := refusalFrom(func() {
		NewRegistry(StringValue(
			countText,
			func(string) (plainCount, error) { return 0, fmt.Errorf("%w: the store is down", ErrPlane) }))
	})

	if err == nil {
		t.Fatal("a codec that is not total over its zero value was accepted")
	}

	// The class is what the cause used to overwrite, and it is read off the
	// report rather than off errors.Is: the registrant's own error stays in the
	// chain by design, so errors.Is answers for it either way, and the class is
	// the thing that was wrong.
	report := fmt.Sprintf("%+v", err)

	if !strings.Contains(report, momentRegister.String()+listSep+ErrSchema.Error()) {
		t.Errorf("the report is\n\t%s\nand its class is not %s", report, ErrSchema)
	}

	if strings.Contains(report, ErrPlane.Error()) {
		t.Errorf("the report is\n\t%s\nand it claims %s: a registration reached no plane", report, ErrPlane)
	}
}

// TestWhatARegistrationMayNotBe is ADR-0009's three refusals plus the nil codec
// a variadic constructor makes writable, plus the one case that must be
// accepted, which is the escape the first refusal names.
func TestWhatARegistrationMayNotBe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		codec Codec
		want  string
	}{{
		name:  "a type core owns by identity",
		codec: DurationLike[time.Duration](),
		want:  "time.Duration is in core's own set",
	}, {
		name:  "a type core owns by kind",
		codec: StringValue(itoa, strconv.Atoi),
		want:  "int is in core's own set",
	}, {
		name:  "a pointer type",
		codec: NumberValue(func(*big.Int) (string, error) { return "0", nil }, parseBigPtr),
		want:  "pointer indirection is structural",
	}, {
		name:  "nothing at all",
		codec: nil,
		want:  "was given a nil codec",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuseAtConstruction(t, func() { NewRegistry(c.codec) }, c.want)
		})
	}

	t.Run("a named type over one core owns is accepted", func(t *testing.T) {
		t.Parallel()

		if err := refusalFrom(func() { NewRegistry(DurationLike[pollInterval]()) }); err != nil {
			t.Errorf("the named duration was refused: %+v", err)
		}
	})
}

func itoa(n int) (string, error) { return strconv.Itoa(n), nil }

func parseBigPtr(string) (*big.Int, error) { return new(big.Int), nil }

// TestADuplicateIsRefused is the second refusal, and under ADR-0017 it is one
// call rather than two: a registry takes its whole codec set at once, so the
// duplicate is two codecs in one list and is refused before the registry exists.
func TestADuplicateIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t,
		func() { NewRegistry(DurationLike[pollInterval](), DurationLike[pollInterval]()) },
		"is already registered")
}

// TestAHalfThatIsNilPanicsAtTheCompositionSite is ADR-0017's one departure from
// "ferry returns errors and never panics", and it is scoped to a program's
// construction.
//
// A nil half is a programming error at a program's birth, in the family of
// regexp.MustCompile, and the alternative is an error return on a line nobody
// checks. It fires at the constructor rather than at NewRegistry, because that
// is where the missing half was written.
func TestAHalfThatIsNilPanicsAtTheCompositionSite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func()
	}{{
		name:  "a nil encode half",
		build: func() { StringValue(nil, parseCount) },
	}, {
		name:  "a nil decode half",
		build: func() { StringValue[plainCount](countText, nil) },
	}, {
		name:  "a nil null policy",
		build: func() { NullValue(StringValue(countText, parseCount), nil, func(plainCount) bool { return false }) },
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuseAtConstruction(t, c.build, "one of them is nil")
		})
	}
}

// TestANullPolicyOverAnotherTypesRegistrationIsRefused is the mismatch the
// inference makes reachable: T comes from the two policies rather than from the
// registration they wrap.
func TestANullPolicyOverAnotherTypesRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() {
		NullValue(StringValue(countText, parseCount),
			func() (host, error) { return host{}, nil },
			func(h host) bool { return h.Name == "" })
	}, "one registration covers one type")
}

// TestATextRegistrationRefusesAValueReceiverDecodeHalf is #131's first item,
// and it is the constraint's own blind spot: *T's method set contains T's, so an
// UnmarshalText on a value receiver satisfies [TextPointer] and decodes into a
// copy.
func TestATextRegistrationRefusesAValueReceiverDecodeHalf(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() { StringText[copies]() }, "value receiver")
}

// copies declares both halves of the text pair with the decode half on the
// value receiver, so it writes to a copy and leaves the field unchanged.
type copies string

func (c copies) MarshalText() ([]byte, error) { return []byte(c), nil }

func (copies) UnmarshalText([]byte) error { return nil }

// TestRegisteringATypeTheChainWouldClaimWins is how a user overrides a
// representation a dependency chose.
//
// severity carries a text pair, so ADR-0007's chain claims it at String with
// nothing registered. A registration is step one and beats it, which is not a
// loophole: ADR-0007 recorded the drift exposure of a dependency adding a text
// pair and left the remedy here.
func TestRegisteringATypeTheChainWouldClaimWins(t *testing.T) {
	t.Parallel()

	type conf struct {
		Level severity `ferry:"level"`
	}

	unregistered := dumpedValue(t, conf{Level: warn}, At("level"))
	if want := String("warn"); unregistered != want {
		t.Errorf("unregistered, the chain wrote %#v, want %#v", unregistered, want)
	}

	reg := registryWith(t, NumberValue(
		func(s severity) (string, error) { return strconv.Itoa(int(s)), nil },
		func(s string) (severity, error) {
			n, err := strconv.Atoi(s)

			return severity(n), err
		}))

	registered := dumpedValue(t, conf{Level: warn}, At("level"), WithRegistry(reg))
	if want := Number("4"); registered != want {
		t.Errorf("registered, the table wrote %#v, want %#v", registered, want)
	}
}

// dumpedValue is what a plane was handed at one address for one dump.
func dumpedValue[T any](t *testing.T, v T, addr Path, opts ...Option) Value {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), v, planeSink{p: p}, opts...); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	return p.values[addr]
}

// TestRegisteringAnInterfaceClaimsTheInterfaceAlone is the rule that is correct
// and is not obvious: identity is ==, so a codec for an interface says nothing
// about any concrete type implementing it.
//
// The two address sets are the assertion, because that is the consequence a
// reviewer would not see: changing a field from the interface to the concrete
// type is not a serialization change in anyone's reading, and it moves one
// address to two.
func TestRegisteringAnInterfaceClaimsTheInterfaceAlone(t *testing.T) {
	t.Parallel()

	type asInterface struct {
		G greeter `ferry:"g"`
	}

	type asConcrete struct {
		G *wave `ferry:"g"`
	}

	reg := registryWith(t, greeterCodec())

	mustBeAddresses(t, boundBy(t, func(ctx context.Context, s Sink) error {
		return Dump(ctx, asInterface{G: wave{Name: "hi"}}, s, WithRegistry(reg))
	}), []string{"/g"})

	mustBeAddresses(t, boundBy(t, func(ctx context.Context, s Sink) error {
		return Dump(ctx, asConcrete{G: &wave{Name: "hi"}}, s, WithRegistry(reg))
	}), []string{"/g", "/g/loud", "/g/name"})
}

// TestDurationLikeClosesTheNamedDurationHole is ADR-0005's documented sharp
// edge, closed at one line per type.
//
// Unregistered, a named type over time.Duration is a distinct reflect.Type, so
// it misses the identity table and falls to kind int64 and a nanosecond count.
func TestDurationLikeClosesTheNamedDurationHole(t *testing.T) {
	t.Parallel()

	type conf struct {
		Poll pollInterval `ferry:"poll"`
	}

	value := conf{Poll: pollInterval(90 * time.Second)}

	if got, want := dumpedValue(t, value, At("poll")), Number("90000000000"); got != want {
		t.Errorf("unregistered, the kind rule wrote %#v, want %#v", got, want)
	}

	reg := registryWith(t, DurationLike[pollInterval]())

	if got, want := dumpedValue(t, value, At("poll"), WithRegistry(reg)), String("1m30s"); got != want {
		t.Errorf("registered, the table wrote %#v, want %#v", got, want)
	}

	back, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("poll"): String("1m30s")}),
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if back != value {
		t.Errorf("the load gave %v, want %v", time.Duration(back.Poll), time.Duration(value.Poll))
	}
}

// TestAMapKeyedByARegisteredTypeNeedsAsMapKey is the opt-in rule, and the
// refusal it produces is where the injectivity obligation is communicated.
func TestAMapKeyedByARegisteredTypeNeedsAsMapKey(t *testing.T) {
	t.Parallel()

	type conf struct {
		Limits map[netip.Addr]int `ferry:"limits"`
	}

	plain := registryWith(t, StringText[netip.Addr]())

	mustRefuse(t, Compile[conf](WithRegistry(plain)), "netip.Addr", "injective", ".AsMapKey()")

	declared := registryWith(t, StringText[netip.Addr]().AsMapKey())
	if err := Compile[conf](WithRegistry(declared)); err != nil {
		t.Fatalf("with .AsMapKey() the schema still refused: %+v", err)
	}
}

// TestARegisteredMapKeyAddressesByItsOwnText is the other half: the segment a
// key mints is what the registered codec's encode half produced, and it parses
// back out of it through the same codec.
func TestARegisteredMapKeyAddressesByItsOwnText(t *testing.T) {
	t.Parallel()

	type conf struct {
		Limits map[netip.Addr]int `ferry:"limits"`
	}

	reg := registryWith(t, StringText[netip.Addr]().AsMapKey())
	addr := netip.MustParseAddr("192.0.2.1")

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), conf{Limits: map[netip.Addr]int{addr: 7}},
		planeSink{p: p}, WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	at := At("limits", "192.0.2.1")
	if got, want := p.values[at], Number("7"); got != want {
		t.Errorf("the plane holds %#v at %s, want %#v", got, at, want)
	}

	back, err := Load[conf](t.Context(), &listing{
		values:   p.values,
		children: map[Path][]Path{At("limits"): {at}},
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if back.Limits[addr] != 7 {
		t.Errorf("the load gave %v, want one entry at %s", back.Limits, addr)
	}
}

// TestTheDeclaredKindIsADonationTargetOnly is criterion ten, and it is the
// difference the bool this hook replaced could not express.
//
// The registration is a number codec under a null policy, so the kind it writes
// and the kinds it accepts are two separate declarations. A plain int refuses a
// Null, which is ADR-0006's strictness, and it is recoverable exactly because a
// null policy accepts one.
func TestTheDeclaredKindIsADonationTargetOnly(t *testing.T) {
	t.Parallel()

	type conf struct {
		N retryCount `ferry:"n"`
	}

	reg := registryWith(t, retryCodec())

	cases := []struct {
		name string
		held Value
		want retryCount
	}{
		{name: "a null the type's kind has no null for", held: Null, want: 0},
		{name: "its own declared kind", held: Number("7"), want: 7},
		{name: "a String donated to the declared kind", held: String("7"), want: 7},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load[conf](t.Context(), planeSource{
				p: newPlane(map[Path]Value{At("n"): c.held}),
			}, WithRegistry(reg))
			mustLoadRetry(t, got.N, c.want, err)
		})
	}
}

func mustLoadRetry(t *testing.T, got, want retryCount, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != want {
		t.Errorf("the load gave %d, want %d", got, want)
	}
}

// TestARegistrationWithNoNullPolicyStillRefusesANull is the measured reason
// NullValue exists: a payload-typed decode half never sees the kind, so core
// refuses the Null before the registrant's own function runs.
func TestARegistrationWithNoNullPolicyStillRefusesANull(t *testing.T) {
	t.Parallel()

	type conf struct {
		N plainCount `ferry:"n"`
	}

	reg := registryWith(t, StringValue(countText, parseCount))

	_, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): Null}),
	}, WithRegistry(reg))

	if !errors.Is(err, ErrValue) {
		t.Fatalf("the load reported %v, want a wrong-kind refusal", err)
	}
}

// TestAPlainIntStillRefusesANull is the rule the escape hatch is an escape
// from, asserted beside it so the pair cannot drift.
func TestAPlainIntStillRefusesANull(t *testing.T) {
	t.Parallel()

	type conf struct {
		N int `ferry:"n"`
	}

	_, err := Load[conf](t.Context(), planeSource{p: newPlane(map[Path]Value{At("n"): Null})})
	if !errors.Is(err, ErrValue) {
		t.Fatalf("the load reported %v, want a wrong-kind refusal", err)
	}
}

// TestARegistryAnswersTheSameFromTheMomentItExists is #227 and #262 stated as
// the property that replaced the freeze.
//
// ADR-0009 arranged for a registry to freeze at its first retained compile, and
// every defect in that class lived in the window between the two moments. There
// is no window: a registry takes its whole codec set at construction and has no
// mutator, so the answer a compile resolves is the answer every later call gets
// and no ordering rule has to be kept.
func TestARegistryAnswersTheSameFromTheMomentItExists(t *testing.T) {
	t.Parallel()

	type conf struct {
		Poll pollInterval `ferry:"poll"`
	}

	reg := registryWith(t, DurationLike[pollInterval]())
	value := conf{Poll: pollInterval(90 * time.Second)}

	if err := Compile[conf](WithRegistry(reg)); err != nil {
		t.Fatalf("compile: %+v", err)
	}

	for _, when := range []string{"after a discarded compile", "after a retained one"} {
		if got, want := dumpedValue(t, value, At("poll"), WithRegistry(reg)), String("1m30s"); got != want {
			t.Errorf("%s the plane holds %#v, want %#v", when, got, want)
		}
	}
}

// TestTheBuiltInSetIsUnderEveryRegistry is ADR-0017's amendment: core's own type
// set is a frozen base rather than a default a program writes to.
//
// A call with no registry gets it, and a registry built for one domain type
// still has it, so registering one type never costs a caller string, int, bool
// or time.Duration. There is nothing to add to the base, which is the whole of
// why a package-level registry is affordable here.
func TestTheBuiltInSetIsUnderEveryRegistry(t *testing.T) {
	t.Parallel()

	type conf struct {
		Name string        `ferry:"name"`
		Port int           `ferry:"port"`
		Wait time.Duration `ferry:"wait"`
		Poll pollInterval  `ferry:"poll"`
	}

	value := conf{Name: "db", Port: 5432, Wait: 90 * time.Second, Poll: pollInterval(time.Minute)}

	want := map[Path]Value{
		At("name"): String("db"),
		At("port"): Number("5432"),
		At("wait"): String("1m30s"),
	}

	// With no Option at all, which is the base on its own.
	for at, w := range want {
		if got := dumpedValue(t, value, at); got != w {
			t.Errorf("with no registry named, %s holds %#v, want %#v", at, got, w)
		}
	}

	// And with a registry built for one named duration, which adds a codec and
	// takes nothing away.
	reg := registryWith(t, DurationLike[pollInterval]())
	want[At("poll")] = String("1m0s")

	for at, w := range want {
		if got := dumpedValue(t, value, at, WithRegistry(reg)); got != w {
			t.Errorf("with one codec registered, %s holds %#v, want %#v", at, got, w)
		}
	}
}

// TestOverridingABuiltInIsRefused is the third sentence of the same amendment,
// and it is the duplicate rule applied to the base with no special case:
// overriding a built-in codec would make user code a second authority over the
// standard types.
func TestOverridingABuiltInIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() { NewRegistry(StringValue(itoa, strconv.Atoi)) },
		"int is in core's own set", "define a named type over it and register that")
}

// TestConcurrentCompilesAgainstOneRegistryAreClean is the race-detector case,
// and what it asserts is the absence of a lock rather than the presence of one.
//
// A registry read by a compile while something writes it is a data race whether
// or not any ADR mentions goroutines, and no mutex inside ferry fixes it,
// because the unlocked read is the point. #227 found the shipped version of this
// test blind: it performed one retained compile before starting any goroutine,
// which froze the registry, so it never observed the unfrozen read Compile
// actually took. There is nothing to warm up now, because the write happened
// before the registry existed - so this starts cold, which is the case the old
// arrangement could not survive.
func TestConcurrentCompilesAgainstOneRegistryAreClean(t *testing.T) {
	t.Parallel()

	reg := registryWith(t,
		DurationLike[pollInterval](),
		retryCodec(),
		StringText[netip.Addr](),
	)

	const goroutines = 8

	done := make(chan error, goroutines*readersPerRound)

	for range goroutines {
		readAgainst(reg, done)
	}

	for range cap(done) {
		if err := <-done; err != nil {
			t.Errorf("a concurrent operation reported %+v", err)
		}
	}
}

// shared is the type every goroutine of the race test compiles, and it is
// declared here rather than inside the test because the goroutine bodies are
// out of line.
type shared struct {
	Poll pollInterval `ferry:"poll"`
	N    retryCount   `ferry:"n"`
	Addr netip.Addr   `ferry:"addr"`
}

// readersPerRound is how many goroutines one round starts: a compile that
// retains nothing, a load that retains its schema, and a read of the table
// through Types.
const readersPerRound = 3

// readAgainst starts one round, out of line because a goroutine body inside a
// range is a nesting level the limits are right to count.
func readAgainst(reg *Registry, done chan<- error) {
	go func() { done <- Compile[shared](WithRegistry(reg)) }()

	go func() {
		_, err := Load[shared](context.Background(), planeSource{p: newPlane(map[Path]Value{})},
			WithRegistry(reg))
		done <- err
	}()

	go func() { done <- expectThreeTypes(reg.Types()) }()
}

// expectThreeTypes turns the table a concurrent reader saw into the nil the
// collector above wants, so that a registry that lost or gained an entry under
// concurrent reads fails the test.
func expectThreeTypes(got []reflect.Type) error {
	if len(got) == 3 {
		return nil
	}

	return fmt.Errorf("a concurrent read of the registry saw %d types, want 3", len(got))
}

// TestNewRegistryRefusesTheFirstBadCodecAndNamesIt is ADR-0001's determinism
// invariant under a constructor that panics.
//
// The shipped surface applied a variadic Register one registration at a time and
// joined every failure, because a mutable registry could half succeed. This one
// cannot: a refusal happens before the registry exists, so there is exactly one
// report, it names the codec that caused it, and the same list produces the same
// report every time.
func TestNewRegistryRefusesTheFirstBadCodecAndNamesIt(t *testing.T) {
	t.Parallel()

	build := func() {
		NewRegistry(
			DurationLike[pollInterval](),
			DurationLike[time.Duration](),
			StringValue(addrText, netip.ParseAddr),
		)
	}

	first := refusalFrom(build)
	mustRefuse(t, first, "time.Duration is in core's own set")

	if second := refusalFrom(build); second == nil || second.Error() != first.Error() {
		t.Errorf("the same codec list reported\n\t%v\nand then\n\t%v", first, second)
	}
}

// TestWithRegistryIsRefusedTwiceAndNil holds the Option to the same rule TagKey
// keeps: ferry resolves against exactly one registry, and a nil one is a
// mistake in the program rather than a spelling of the default.
func TestWithRegistryIsRefusedTwiceAndNil(t *testing.T) {
	t.Parallel()

	type conf struct {
		N int `ferry:"n"`
	}

	cases := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "nil", opts: []Option{WithRegistry(nil)}, want: "nil registry"},
		{
			name: "twice",
			opts: []Option{WithRegistry(NewRegistry()), WithRegistry(NewRegistry())},
			want: "given twice",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			mustRefuse(t, Compile[conf](c.opts...), c.want)
		})
	}
}

// TestTypesIsSortedAndTheCallersToKeep holds the one thing this package exports
// for ferrytest's sake to the two properties a caller can rely on.
func TestTypesIsSortedAndTheCallersToKeep(t *testing.T) {
	t.Parallel()

	reg := registryWith(t,
		retryCodec(),
		DurationLike[pollInterval](),
		StringValue(hostText, parseHost),
	)

	first, second := reg.Types(), reg.Types()

	if !slices.IsSortedFunc(first, func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) }) {
		t.Errorf("Types reports %v, which is not sorted", first)
	}

	if len(first) == 0 || &first[0] == &second[0] {
		t.Error("two calls to Types share a backing array")
	}

	if got := (*Registry)(nil).Types(); got != nil {
		t.Errorf("a nil registry reports %v, want nothing", got)
	}
}

// TestARegisteredKeyWhoseTextFailsIsReported is the failure arm a core key type
// does not have: a registrant's encode half is theirs, the zero-value check
// discharges one value of it, and nothing discharges the rest.
func TestARegisteredKeyWhoseTextFailsIsReported(t *testing.T) {
	t.Parallel()

	type conf struct {
		Limits map[plainCount]int `ferry:"limits"`
	}

	reg := registryWith(t, StringKey(
		func(c plainCount) (string, error) {
			if c == 0 {
				return "", nil
			}

			return "", errNotAnInteger
		},
		func(string) (plainCount, error) { return 0, nil }).AsMapKey())

	err := Dump(t.Context(), conf{Limits: map[plainCount]int{1: 1}},
		planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg))

	if !errors.Is(err, ErrValue) {
		t.Fatalf("the dump reported %v, want the key's own failure", err)
	}
}

// TestThereIsNoDynamicRegistration is criterion twelve, asserted structurally
// because it is a claim about what is absent.
//
// A method taking a reflect.Type would compile and nobody could call it: T is
// written in source at every entry point ferry has, so a reflect.StructOf type
// can never be one, and the refusal is on reversibility - adding it later is
// additive, and removing it is not.
func TestThereIsNoDynamicRegistration(t *testing.T) {
	t.Parallel()

	var methods []string

	registry := reflect.TypeFor[*Registry]()
	for i := range registry.NumMethod() {
		methods = append(methods, registry.Method(i).Name)
	}

	if want := []string{"Types"}; !slices.Equal(methods, want) {
		t.Errorf("*Registry exports the methods %v, want %v", methods, want)
	}
}

// TestNothingIsExportedFromARegistration holds the finding that keeps a
// registration opaque: a proof exercises a codec through the ordinary walk, so a
// harness needs no accessor on one, and an accessor would be exported surface
// for ever.
//
// The two types are asked separately because they carry different promises. A
// Codec is an interface whose one method is unexported, so it cannot be
// implemented outside this package and every one in existence came from a
// constructor here; a KeyCodec is a struct, so it is asked about its fields too,
// and AsMapKey is the one method it may have.
func TestNothingIsExportedFromARegistration(t *testing.T) {
	t.Parallel()

	if got := methodNames(reflect.TypeFor[Codec]()); len(got) != 0 {
		t.Errorf("Codec exports the methods %v, and its whole method set is meant to be unexported", got)
	}

	key := reflect.TypeFor[KeyCodec]()

	for i := range key.NumField() {
		if key.Field(i).IsExported() {
			t.Errorf("KeyCodec exports the field %s", key.Field(i).Name)
		}
	}

	if want, got := []string{"AsMapKey"}, methodNames(key); !slices.Equal(got, want) {
		t.Errorf("KeyCodec exports the methods %v, want %v", got, want)
	}
}

// methodNames is the exported methods reflect reports for a type. An
// interface's method set includes its unexported methods, so the filter is what
// separates "has an unexported method" from "exports one".
func methodNames(t reflect.Type) []string {
	var out []string

	for i := range t.NumMethod() {
		if m := t.Method(i); m.PkgPath == "" {
			out = append(out, m.Name)
		}
	}

	return out
}

// bomb is a text pair whose encode half refuses one value and accepts the zero,
// which is what it takes to reach a registered encode failure at all: the
// zero-value check refuses a codec that fails on its own zero, so the only
// codec that can fail during a walk is one that succeeds there.
type bomb string

const boom bomb = "boom"

func (b bomb) MarshalText() ([]byte, error) {
	if b == boom {
		return nil, errNotAnInteger
	}

	return []byte(b), nil
}

func (b *bomb) UnmarshalText(text []byte) error {
	*b = bomb(text)

	return nil
}

// TestATextRegistrationsEncodeFailureSurfaces is the failure arm of the one
// thing a text registration adds to the chain's own arm: the kind is the
// registrant's and the text is still the type's, so the type's own refusal is
// what reaches the walk.
func TestATextRegistrationsEncodeFailureSurfaces(t *testing.T) {
	t.Parallel()

	type conf struct {
		B bomb `ferry:"b"`
	}

	reg := registryWith(t, NumberText[bomb]())

	err := Dump(t.Context(), conf{B: boom}, planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg))
	if !errors.Is(err, ErrValue) {
		t.Fatalf("the dump reported %v, want the type's own refusal", err)
	}
}

// TestARegistrationsDecodeFailureSurfaces is the same on the way in: the plane
// holds a bool at a number registration, which is a kind that registration
// neither writes nor takes, so it is refused at the field that could not have it.
func TestARegistrationsDecodeFailureSurfaces(t *testing.T) {
	t.Parallel()

	type conf struct {
		N retryCount `ferry:"n"`
	}

	reg := registryWith(t, retryCodec())

	_, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): Bool(true)}),
	}, WithRegistry(reg))

	if !errors.Is(err, ErrValue) {
		t.Fatalf("the load reported %v, want the codec's own refusal", err)
	}
}

// TestAPointerToARegisteredTypeReachesTheSameCodec is the text half of a
// whole-Value codec doing its job: a pointer leaf decodes through the pointee's
// own parse, so a registered codec under a pointer is the registrant's codec
// and not a second one, and the pointer contributes the null.
func TestAPointerToARegisteredTypeReachesTheSameCodec(t *testing.T) {
	t.Parallel()

	type conf struct {
		N *retryCount `ferry:"n"`
	}

	reg := registryWith(t, retryCodec())

	got, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): String("7")}),
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.N == nil || *got.N != 7 {
		t.Fatalf("the load gave %v, want a pointer to 7", got.N)
	}

	unset, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): Null}),
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if unset.N != nil {
		t.Errorf("a null gave %v, want a nil pointer: the pointer owns the null", *unset.N)
	}
}

// TestTextRegistrationChangesTheKind is what a text registration is for, and it
// is narrower
// than "rescuing the type": the chain already claims big.Int through its text
// pair, correctly, at kind String.
//
// What the registration buys is the second line. Declaring String would work
// against a flat plane and fail against a structured one that reports Number
// for a run of digits; declaring Number loads from both, because String is the
// universal donor and Number is not.
func TestTextRegistrationChangesTheKind(t *testing.T) {
	t.Parallel()

	type conf struct {
		Max big.Int `ferry:"max"`
	}

	const digits = "1099511627776"

	var biggest big.Int

	if _, ok := biggest.SetString(digits, numBase); !ok {
		t.Fatalf("the fixture value %q is not an integer", digits)
	}

	if got, want := dumpedValue(t, conf{Max: biggest}, At("max")), String(digits); got != want {
		t.Errorf("unregistered, the chain wrote %#v, want %#v", got, want)
	}

	reg := registryWith(t, NumberText[big.Int]())

	if got, want := dumpedValue(t, conf{Max: biggest}, At("max"), WithRegistry(reg)), Number(digits); got != want {
		t.Errorf("registered, the table wrote %#v, want %#v", got, want)
	}

	for _, held := range []Value{Number(digits), String(digits)} {
		mustLoadBigInt(t, reg, held, digits)
	}
}

// mustLoadBigInt loads one plane observation into the registered type and holds
// it to the digits, which is the second half of what declaring Number buys: the
// same codec answers to a plane that says Number and to one that says String.
func mustLoadBigInt(t *testing.T, reg *Registry, held Value, digits string) {
	t.Helper()

	type conf struct {
		Max big.Int `ferry:"max"`
	}

	got, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("max"): held}),
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load from %#v: %+v", held, err)
	}

	if got.Max.String() != digits {
		t.Errorf("loading from %#v gave %s, want %s", held, got.Max.String(), digits)
	}
}
