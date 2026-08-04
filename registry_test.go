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
// registration through Register, a claim through what Compile refused or what a
// recording driver's Bind was handed, and a representation through what a plane
// was written. Nothing reaches the compiled schema, the node tree or the
// registry's map.
//
// The one thing asserted structurally rather than behaviourally is the shape of
// the API itself - that no RegisterType exists and that a Reg has no exported
// accessor - because those are claims about what is absent, and absence has no
// behaviour to observe it through.

// The named types over one underlying type that ADR-0005 left as a documented
// sharp edge and this ticket closes.
type (
	pollInterval    time.Duration
	defaultInterval time.Duration
	lateInterval    time.Duration
)

// retryCount is a named int, registered so that its codec accepts a Null and
// returns the Go zero. Its kind admits it already, and the registration is what
// gives the type a null it does not otherwise have (ADR-0006).
type retryCount int

// plainCount is retryCount's counterpart through StringCodec, which is the
// measured reason there are three constructors and not one: a decode half over
// string cannot see a Null at all.
type plainCount int

// host is a struct with no text pair, so nothing but a registration collapses
// it to a leaf.
type host struct {
	Name string `ferry:"name"`
	Port int    `ferry:"port"`
}

func hostText(h host) string { return h.Name + ":" + strconv.Itoa(h.Port) }

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

// greeterValue emits Null for a nil interface, which is the mechanism that
// makes an interface expressible at all, and accepts one back.
func greeterValue(g greeter) (Value, error) {
	if g == nil {
		return Null(), nil
	}

	return String(g.greeting()), nil
}

func parseGreeter(v Value) (greeter, error) {
	if v.Kind() == KindNull {
		return nil, nil //nolint:nilnil // a nil greeter is the value Null carries, and it is not a failure.
	}

	s, err := v.AsString()
	if err != nil {
		return nil, err
	}

	return wave{Name: s}, nil
}

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
func urlText(u url.URL) string { return u.String() }

func parseURL(text string) (url.URL, error) {
	u, err := url.Parse(text)
	if err != nil {
		return url.URL{}, err
	}

	return *u, nil
}

func macText(a net.HardwareAddr) string { return a.String() }

func parseMAC(text string) (net.HardwareAddr, error) {
	if text == "" {
		return nil, nil
	}

	return net.ParseMAC(text)
}

func countText(c plainCount) string { return strconv.Itoa(int(c)) }

func parseCount(text string) (plainCount, error) {
	n, err := strconv.Atoi(text)

	return plainCount(n), err
}

func bigValue(x big.Int) (Value, error) { return Number(x.String()), nil }

func parseBig(v Value) (big.Int, error) {
	var x big.Int

	s, err := v.AsNumber()
	if err != nil {
		return x, err
	}

	if _, ok := x.SetString(s, numBase); !ok {
		return x, errNotAnInteger
	}

	return x, nil
}

var errNotAnInteger = errors.New("not an integer")

func retryValue(c retryCount) (Value, error) { return Number(strconv.Itoa(int(c))), nil }

// parseRetry is the escape hatch ADR-0006's strictness rests on: a plain int
// refuses a Null, and a registered codec for its own type accepts one and
// returns 0.
func parseRetry(v Value) (retryCount, error) {
	if v.Kind() == KindNull {
		return 0, nil
	}

	n, err := v.AsInt()

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

// registryWith builds a fresh registry per test, because a registry freezes and
// a shared one would make every test after the first depend on the order they
// ran in.
func registryWith(t *testing.T, regs ...Reg) *Registry {
	t.Helper()

	reg := NewRegistry()
	if err := reg.Register(regs...); err != nil {
		t.Fatalf("register: %+v", err)
	}

	return reg
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
// read off the registry's own member list rather than off any Reg: three of the
// ten are refused by the zero-value check and name their type in doing so, and
// the other seven are the table.
func TestInferenceWorksAtEveryCallSiteWithAValueArgument(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	err := reg.Register(
		StringCodec(netip.Addr.String, netip.ParseAddr),
		StringCodec(netip.AddrPort.String, netip.ParseAddrPort),
		StringCodec(netip.Prefix.String, netip.ParsePrefix),
		StringCodec(macText, parseMAC),
		StringCodec(countText, parseCount),
		StringCodec(hostText, parseHost),
		StringCodec(urlText, parseURL),
		ValueCodec(KindNumber, bigValue, parseBig),
		ValueCodec(KindString, greeterValue, parseGreeter),
		ValueCodec(KindNumber, retryValue, parseRetry),
	)

	if got := len(Elements(err)); got != len(refusedByTheZeroCheck) {
		t.Fatalf("Register reported %d failures, want %d:\n%+v", got, len(refusedByTheZeroCheck), err)
	}

	want := []string{
		"github.com/onhotpath/ferry.greeter", "github.com/onhotpath/ferry.host",
		"github.com/onhotpath/ferry.plainCount", "github.com/onhotpath/ferry.retryCount",
		"math/big.Int", "net.HardwareAddr", "net/url.URL",
	}

	mustHoldTypes(t, reg, want)
}

// refusedByTheZeroCheck is the three standard-library types whose String and
// Parse pair is not an inverse at the zero value.
var refusedByTheZeroCheck = []string{"netip.Addr", "netip.AddrPort", "netip.Prefix"}

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
		name string
		reg  Reg
	}{
		{name: "netip.Addr through String and ParseAddr", reg: StringCodec(netip.Addr.String, netip.ParseAddr)},
		{name: "netip.AddrPort through String and ParseAddrPort",
			reg: StringCodec(netip.AddrPort.String, netip.ParseAddrPort)},
		{name: "netip.Prefix through String and ParsePrefix",
			reg: StringCodec(netip.Prefix.String, netip.ParsePrefix)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuse(t, NewRegistry().Register(c.reg), "not total over the zero value")
		})
	}
}

// TestTheZeroValueCheckAccepts is the other four registrations ADR-0009
// measured, and the interface is the case that had to keep working: its zero is
// a nil interface, it emits Null, and it accepts Null back.
func TestTheZeroValueCheckAccepts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		reg  Reg
	}{
		{name: "netip.Addr through its text pair", reg: TextCodec[netip.Addr](KindString)},
		{name: "url.URL through two wrappers", reg: StringCodec(urlText, parseURL)},
		{name: "a named duration", reg: DurationLike[pollInterval]()},
		{name: "an interface, whose zero emits Null and takes Null back",
			reg: ValueCodec(KindString, greeterValue, parseGreeter)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := NewRegistry().Register(c.reg); err != nil {
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

	err := NewRegistry().Register(StringCodec(netip.Addr.String, netip.ParseAddr))
	if err == nil {
		t.Fatal("the netip.Addr one-liner was accepted")
	}

	mustRefuse(t, err, "netip.Addr", `string("invalid IP")`, "decoding that back fails")

	if !errors.Is(err, ErrSchema) {
		t.Error("the refusal does not answer to ErrSchema")
	}
}

// TestAnEncodeFailureAtTheZeroValueIsRefused is the other half of the check,
// which the netip cases cannot reach: a codec whose encode half errors never
// gets as far as decoding.
func TestAnEncodeFailureAtTheZeroValueIsRefused(t *testing.T) {
	t.Parallel()

	err := NewRegistry().Register(ValueCodec(KindString,
		func(retryCount) (Value, error) { return Value{}, errNotAnInteger },
		func(Value) (retryCount, error) { return 0, nil }))

	mustRefuse(t, err, "encoding one failed")
}

// TestACodecThatLiesAboutItsKindIsRefusedAtRegistration is the declared-kind
// check reaching the registration rather than only the walk, which falls out of
// the zero-value check running the same emit the walk runs.
func TestACodecThatLiesAboutItsKindIsRefusedAtRegistration(t *testing.T) {
	t.Parallel()

	err := NewRegistry().Register(ValueCodec(KindNumber,
		func(retryCount) (Value, error) { return String("4"), nil },
		parseRetry))

	mustRefuse(t, err, "declared number and produced string")
}

// TestWhatARegistrationMayNotBe is ADR-0009's three refusals plus the one case
// that must be accepted, which is the escape the first refusal names.
func TestWhatARegistrationMayNotBe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		reg  Reg
		want string
	}{{
		name: "a type core owns by identity",
		reg:  DurationLike[time.Duration](),
		want: "time.Duration is in core's own set",
	}, {
		name: "a type core owns by kind",
		reg:  StringCodec(strconv.Itoa, strconv.Atoi),
		want: "int is in core's own set",
	}, {
		name: "a pointer type",
		reg:  ValueCodec(KindNumber, func(*big.Int) (Value, error) { return Number("0"), nil }, parseBigPtr),
		want: "pointer indirection is structural",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustRefuse(t, NewRegistry().Register(c.reg), c.want)
		})
	}

	t.Run("a named type over one core owns is accepted", func(t *testing.T) {
		t.Parallel()

		if err := NewRegistry().Register(DurationLike[pollInterval]()); err != nil {
			t.Errorf("the named duration was refused: %+v", err)
		}
	})
}

func parseBigPtr(Value) (*big.Int, error) { return new(big.Int), nil }

// TestADuplicateIsRefused is the second refusal, and it needs two calls rather
// than a table because what makes it a duplicate is a registration that
// succeeded first.
func TestADuplicateIsRefused(t *testing.T) {
	t.Parallel()

	reg := registryWith(t, DurationLike[pollInterval]())

	mustRefuse(t, reg.Register(DurationLike[pollInterval]()), "is already registered")
}

// TestAZeroRegIsRefused holds the one hole a struct leaves in "the only way to
// make one is a constructor": Reg{} is writable, and it names no type.
func TestAZeroRegIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuse(t, NewRegistry().Register(Reg{}), "zero ferry.Reg")
}

// TestTextCodecRefusesAValueReceiverDecodeHalf is the one refusal this
// implementation adds to ADR-0009's three, and it is the constraint's own blind
// spot: *T's method set contains T's, so an UnmarshalText on a value receiver
// satisfies textPtr and decodes into a copy.
func TestTextCodecRefusesAValueReceiverDecodeHalf(t *testing.T) {
	t.Parallel()

	mustRefuse(t, NewRegistry().Register(TextCodec[copies](KindString)), "value receiver")
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

	reg := registryWith(t, ValueCodec(KindNumber,
		func(s severity) (Value, error) { return Number(strconv.Itoa(int(s))), nil },
		func(v Value) (severity, error) {
			n, err := v.AsInt()

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

	reg := registryWith(t, ValueCodec(KindString, greeterValue, parseGreeter))

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

	plain := registryWith(t, TextCodec[netip.Addr](KindString))

	mustRefuse(t, Compile[conf](WithRegistry(plain)), "netip.Addr", "injective", ".AsMapKey()")

	declared := registryWith(t, TextCodec[netip.Addr](KindString).AsMapKey())
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

	reg := registryWith(t, TextCodec[netip.Addr](KindString).AsMapKey())
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
// The codec declares Number, because that is what it produces, and separately
// accepts Null, which it never produces. A plain int refuses a Null, which is
// ADR-0006's strictness, and it is recoverable exactly because a registration
// can accept one.
func TestTheDeclaredKindIsADonationTargetOnly(t *testing.T) {
	t.Parallel()

	type conf struct {
		N retryCount `ferry:"n"`
	}

	reg := registryWith(t, ValueCodec(KindNumber, retryValue, parseRetry))

	cases := []struct {
		name string
		held Value
		want retryCount
	}{
		{name: "a null the type's kind has no null for", held: Null(), want: 0},
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

// TestStringCodecCannotExpressTheNullEscapeHatch is the measured reason the
// general constructor stays: a decode half over string never sees the kind, so
// AsString refuses the Null before the registrant's own function runs.
func TestStringCodecCannotExpressTheNullEscapeHatch(t *testing.T) {
	t.Parallel()

	type conf struct {
		N plainCount `ferry:"n"`
	}

	reg := registryWith(t, StringCodec(countText, parseCount))

	_, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): Null()}),
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

	_, err := Load[conf](t.Context(), planeSource{p: newPlane(map[Path]Value{At("n"): Null()})})
	if !errors.Is(err, ErrValue) {
		t.Fatalf("the load reported %v, want a wrong-kind refusal", err)
	}
}

// TestARegistryFreezesAtItsFirstRetainedCompile is the lifetime answer, and the
// error is required to name the freeze point rather than the type.
func TestARegistryFreezesAtItsFirstRetainedCompile(t *testing.T) {
	t.Parallel()

	type conf struct {
		N int `ferry:"n"`
	}

	reg := registryWith(t, DurationLike[pollInterval]())

	if _, err := Load[conf](t.Context(), planeSource{p: newPlane(map[Path]Value{})}, WithRegistry(reg)); err != nil {
		t.Fatalf("load: %+v", err)
	}

	mustRefuse(t, reg.Register(DurationLike[lateInterval]()),
		"the registry is frozen", "before the first Load, Dump or Bind")
}

// TestCompileDoesNotFreeze is the other half of "retained", and it is what
// keeps Compile safe during init: it compiles a schema and discards it, so
// there is no resolution for a later registration to invalidate.
func TestCompileDoesNotFreeze(t *testing.T) {
	t.Parallel()

	type conf struct {
		Poll pollInterval `ferry:"poll"`
	}

	reg := registryWith(t)

	if err := Compile[conf](WithRegistry(reg)); err != nil {
		t.Fatalf("compile: %+v", err)
	}

	if err := reg.Register(DurationLike[pollInterval]()); err != nil {
		t.Fatalf("a registration after a discarded compile was refused: %+v", err)
	}
}

// init registers into the registry core ships, which is the shape ADR-0009 says
// every consumer writes and the one the Go spec makes order-independent: every
// init in the program runs to completion before main, so a registration in one
// strictly precedes the first Load whatever the import graph is.
func init() {
	if err := Register(DurationLike[defaultInterval]()); err != nil {
		panic(err)
	}
}

// TestTheDefaultRegistryIsARegistryAndFreezesLikeAnyOther is survey item 5.14's
// first entry avoided rather than repeated: a default registry plus a scoped one
// is two ways to supply a codec only if they are two mechanisms, and they are
// one.
//
// The registration above is an ordinary Register with no Option anywhere, and
// the verbs pick it up; the freeze below is the same freeze a scoped registry
// gets. Both halves run in one test because the second is only assertable after
// the first has already happened.
func TestTheDefaultRegistryIsARegistryAndFreezesLikeAnyOther(t *testing.T) {
	t.Parallel()

	type conf struct {
		Poll defaultInterval `ferry:"poll"`
	}

	got := dumpedValue(t, conf{Poll: defaultInterval(30 * time.Second)}, At("poll"))
	if want := String("30s"); got != want {
		t.Errorf("the default registry wrote %#v, want %#v", got, want)
	}

	// The Dump above retained its schema, so the default registry is frozen now
	// whether or not another test got there first. The freeze is monotonic, so
	// this is a fact rather than a race.
	mustRefuse(t, Register(DurationLike[lateInterval]()), "the registry is frozen")
}

// TestConcurrentCompilesAgainstOneRegistryAreClean is the race-detector case,
// and what it asserts is the absence of a lock rather than the presence of one.
//
// A mutable registry read by a compile is a data race whether or not any ADR
// mentions goroutines, and no mutex inside ferry fixes it, because the unlocked
// read is the whole point. A frozen registry is written before its first reader
// exists and never again, so the reads below have nothing to synchronise with
// and the registrations racing them never touch the table.
func TestConcurrentCompilesAgainstOneRegistryAreClean(t *testing.T) {
	t.Parallel()

	reg := registryWith(t,
		DurationLike[pollInterval](),
		ValueCodec(KindNumber, retryValue, parseRetry),
		TextCodec[netip.Addr](KindString),
	)

	// One retained compile first, so the freeze is a fact before any goroutine
	// starts and the test is asserting the frozen read path rather than the
	// user error of registering during a load.
	if err := Dump(t.Context(), shared{}, planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

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

// readersPerRound is how many goroutines one round starts: two readers of the
// frozen table, and one registration that has to bounce off the freeze.
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

	go func() { done <- expectFrozen(reg.Register(DurationLike[lateInterval]())) }()
}

// expectFrozen turns the refusal a concurrent registration must get into the
// nil the collector above wants, so that a registration quietly succeeding
// against a frozen registry fails the test.
func expectFrozen(err error) error {
	if err != nil && strings.Contains(err.Error(), "the registry is frozen") {
		return nil
	}

	return fmt.Errorf("a registration against a frozen registry did not report the freeze refusal: %w", err)
}

// TestRegisterReportsEveryFailureJoinedAndSorted is ADR-0001's determinism
// invariant applied to a startup error, which ADR-0009 defers to ADR-0011's
// convention rather than deciding for itself.
func TestRegisterReportsEveryFailureJoinedAndSorted(t *testing.T) {
	t.Parallel()

	err := NewRegistry().Register(
		StringCodec(netip.Prefix.String, netip.ParsePrefix),
		DurationLike[time.Duration](),
		StringCodec(netip.Addr.String, netip.ParseAddr),
	)

	got := Elements(err)
	if len(got) != 3 {
		t.Fatalf("Register reported %d failures, want 3:\n%+v", len(got), err)
	}

	lines := make([]string, 0, len(got))
	for _, e := range got {
		lines = append(lines, e.Error())
	}

	if !slices.IsSorted(lines) {
		t.Errorf("the failures are\n\t%v\nwhich is not sorted", lines)
	}

	// One registration in the call succeeded and is not reported, which is what
	// "each is applied on its own" means.
	if !strings.Contains(lines[0], "netip.Addr") {
		t.Errorf("the first failure is %q, and the order is not the message order", lines[0])
	}
}

// TestARegistrationBesideAFailingOneStillTakes is the other half of the same
// rule: a variadic call is a list of registrations rather than a transaction.
func TestARegistrationBesideAFailingOneStillTakes(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	if err := reg.Register(DurationLike[pollInterval](), DurationLike[time.Duration]()); err == nil {
		t.Fatal("registering time.Duration was accepted")
	}

	mustHoldTypes(t, reg, []string{"github.com/onhotpath/ferry.pollInterval"})
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
		ValueCodec(KindNumber, retryValue, parseRetry),
		DurationLike[pollInterval](),
		StringCodec(hostText, parseHost),
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

	reg := registryWith(t, ValueCodec(KindString,
		func(c plainCount) (Value, error) {
			if c == 0 {
				return String(""), nil
			}

			return Value{}, errNotAnInteger
		},
		func(Value) (plainCount, error) { return 0, nil }).AsMapKey())

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

	if want := []string{"Register", "Types"}; !slices.Equal(methods, want) {
		t.Errorf("*Registry exports the methods %v, want %v", methods, want)
	}
}

// TestNothingIsExportedFromAReg holds the finding that keeps Reg opaque: a
// proof exercises a codec through the ordinary walk, so a harness needs no
// accessor on a registration, and an accessor would be exported surface for
// ever.
func TestNothingIsExportedFromAReg(t *testing.T) {
	t.Parallel()

	reg := reflect.TypeFor[Reg]()

	for i := range reg.NumField() {
		if reg.Field(i).IsExported() {
			t.Errorf("Reg exports the field %s", reg.Field(i).Name)
		}
	}

	var methods []string

	for i := range reg.NumMethod() {
		methods = append(methods, reg.Method(i).Name)
	}

	if want := []string{"AsMapKey"}; !slices.Equal(methods, want) {
		t.Errorf("Reg exports the methods %v, want %v", methods, want)
	}
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

// TestATextCodecsEncodeFailureSurfaces is the failure arm of the one thing
// TextCodec adds to the chain's own arm: the kind is the registrant's and the
// text is still the type's, so the type's own refusal is what reaches the walk.
func TestATextCodecsEncodeFailureSurfaces(t *testing.T) {
	t.Parallel()

	type conf struct {
		B bomb `ferry:"b"`
	}

	reg := registryWith(t, TextCodec[bomb](KindNumber))

	err := Dump(t.Context(), conf{B: boom}, planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg))
	if !errors.Is(err, ErrValue) {
		t.Fatalf("the dump reported %v, want the type's own refusal", err)
	}
}

// TestAValueCodecsDecodeFailureSurfaces is the same on the way in, and it is
// the arm the declared kind does not constrain: the codec is handed a Bool it
// never declared and never emits, and what happens next is the codec's.
func TestAValueCodecsDecodeFailureSurfaces(t *testing.T) {
	t.Parallel()

	type conf struct {
		N retryCount `ferry:"n"`
	}

	reg := registryWith(t, ValueCodec(KindNumber, retryValue, parseRetry))

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

	reg := registryWith(t, ValueCodec(KindNumber, retryValue, parseRetry))

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
		p: newPlane(map[Path]Value{At("n"): Null()}),
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if unset.N != nil {
		t.Errorf("a null gave %v, want a nil pointer: the pointer owns the null", *unset.N)
	}
}

// TestTextCodecChangesTheKind is TextCodec's whole purpose, and it is narrower
// than "rescuing the type": the chain already claims big.Int through its text
// pair, correctly, at kind String.
//
// What the registration buys is the second line. Declaring String would work
// against a flat plane and fail against a structured one that reports Number
// for a run of digits; declaring Number loads from both, because String is the
// universal donor and Number is not.
func TestTextCodecChangesTheKind(t *testing.T) {
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

	reg := registryWith(t, TextCodec[big.Int](KindNumber))

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
