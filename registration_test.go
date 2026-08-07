package ferry

import (
	"errors"
	"maps"
	"strconv"
	"strings"
	"testing"
)

// The proving tests for the defect classes ADR-0017's surface retires, and for
// the two resolution bugs it uncovered on the way. Every one of them goes
// through Load, Dump or Compile.

// flag is a type carried across the boundary as a boolean, which is the payload
// #223's class lived in.
type flag bool

// flagCodec is the shape a caller writes for a type whose plane form is a bool.
func flagCodec() Codec {
	return BoolValue(
		func(f flag) (bool, error) { return bool(f), nil },
		func(b bool) (flag, error) { return flag(b), nil },
	)
}

// flagConf is one registered bool at both field shapes, so a rule that holds at
// one and not the other cannot pass.
type flagConf struct {
	V flag  `ferry:"v"`
	P *flag `ferry:"p"`
}

// TestABoolRegistrationNeverGuessesAtText is #223, and what it asserts is that
// the accessor stopped being able to guess.
//
// The old Value carried a bool as the text "true", so a plane with no types of
// its own reporting TRUE, yes, 1 or junk was donated to the bool kind and read
// back with a string comparison against "true": every one of them loaded as
// false with a nil error. A bool carries a Go bool now, so the only text left is
// a String the plane spelled, and it is parsed by exactly the parser core's own
// bool leaf uses - which takes TRUE and 1, and refuses everything it does not
// recognise instead of answering false.
//
// Both field shapes are driven, because the pointer is the one that reached the
// leaf through a second path.
func TestABoolRegistrationNeverGuessesAtText(t *testing.T) {
	t.Parallel()

	reg := registryWith(t, flagCodec())

	t.Run("the spellings ParseBool takes", func(t *testing.T) {
		t.Parallel()

		for held, want := range map[Value]flag{
			Bool(true): true, Bool(false): false,
			String("true"): true, String("false"): false,
			String("1"): true, String("0"): false,
			String("TRUE"): true, String("False"): false,
		} {
			mustLoadFlag(t, reg, held, want)
		}
	})

	t.Run("and every other text is refused rather than read as false", func(t *testing.T) {
		t.Parallel()

		for _, held := range []Value{String("yes"), String("on"), String("garbage"), String("")} {
			if _, err := loadFlags(t, reg, held); !errors.Is(err, ErrValue) {
				t.Errorf("loading %#v reported %v, want a refusal: a bool is not a guess at what a text meant",
					held, err)
			}
		}
	})
}

func mustLoadFlag(t *testing.T, reg *Registry, held Value, want flag) {
	t.Helper()

	got, err := loadFlags(t, reg, held)
	if err != nil {
		t.Errorf("loading %#v failed: %+v", held, err)

		return
	}

	if got.V != want || got.P == nil || *got.P != want {
		t.Errorf("%#v loaded as %v and %v, want %v at both shapes", held, got.V, got.P, want)
	}
}

func loadFlags(t *testing.T, reg *Registry, held Value) (flagConf, error) {
	t.Helper()

	return Load[flagConf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("v"): held, At("p"): held}),
	}, WithRegistry(reg))
}

// blob is a type carried as bytes, which is a kind no leaf's own rule admits
// from a bool.
type blob string

// TestAPointerCarriesThePointeeCodecsAcceptedSet is #229.
//
// A pointer leaf copied the kind, the encode half and the text half out of the
// pointee's codec and dropped its whole-observation half, so a *T over a
// registered T decoded through core's own rule - gated by a comparison against
// the declared kind, which is exactly the derivation ADR-0009 forbids. The
// accepted set is the codec's.
//
// The observation is a bool at a bytes registration, which both shapes must
// refuse, and which refusal it is is the assertion: at T the registered codec's
// own half ran, and at *T core used to answer first with a sentence about the
// pointer type instead.
func TestAPointerCarriesThePointeeCodecsAcceptedSet(t *testing.T) {
	t.Parallel()

	type conf struct {
		V blob  `ferry:"v"`
		P *blob `ferry:"p"`
	}

	reg := registryWith(t, blobCodec())

	for _, at := range []Path{At("v"), At("p")} {
		_, err := Load[conf](t.Context(), planeSource{
			p: newPlane(map[Path]Value{at: Bool(true)}),
		}, WithRegistry(reg))

		if !errors.Is(err, ErrValue) {
			t.Fatalf("a bool at %s reported %v, want a refusal", at, err)
		}

		if strings.Contains(err.Error(), "cannot take one") {
			t.Errorf("a bool at %s was refused by core's own kind rule with %v: the accepted set is the "+
				"registered codec's and is never derived from the kind it declared", at, err)
		}
	}
}

func blobCodec() Codec {
	return BytesValue(
		func(b blob) ([]byte, error) { return []byte(b), nil },
		func(b []byte) (blob, error) { return blob(b), nil })
}

// TestABytesRegistrationTakesThePlanesOwnSpelling is the bytes half of the
// donation the bool and number arms already pin.
//
// String is donated to the declared kind before any codec is called, so a flat
// plane - one that has no types of its own and reports every address as a text -
// must reach a bytes registration with the bytes that text holds. How a plane
// spells bytes is the driver's business (ADR-0004), so nothing is decoded here:
// the text's own bytes are what the codec is handed, and the Bytes spelling
// arrives unchanged beside it.
//
// Both field shapes are driven for the same reason #229's test drives them: the
// pointer reaches the leaf through a second path.
func TestABytesRegistrationTakesThePlanesOwnSpelling(t *testing.T) {
	t.Parallel()

	type conf struct {
		V blob  `ferry:"v"`
		P *blob `ferry:"p"`
	}

	reg := registryWith(t, blobCodec())

	for _, held := range []Value{String("hello"), Bytes([]byte("hello"))} {
		got, err := Load[conf](t.Context(), planeSource{
			p: newPlane(map[Path]Value{At("v"): held, At("p"): held}),
		}, WithRegistry(reg))
		if err != nil {
			t.Errorf("loading %#v failed: %+v: a text is donated to the declared kind before any codec "+
				"is called", held, err)

			continue
		}

		if got.V != "hello" || got.P == nil || *got.P != "hello" {
			t.Errorf("%#v loaded as %q and %v, want %q at both shapes", held, got.V, got.P, "hello")
		}
	}
}

// TestAChainClaimedTypeMayNotKeyAMapWhateverItsKindIs is #230.
//
// Map key resolution asked the registry, then the one identity entry, and then
// fell into a kind switch, and never asked the chain at all. So a type the chain
// claims through its text pair was admitted as a key by its underlying kind
// whenever that kind was a string or an integer: the same type addressed /m/4 at
// the key position and wrote string("warn") at the leaf position, which is the
// one thing identity-before-kind exists to prevent, and ADR-0007's refusal of a
// chain-claimed key was enforced only for the ones whose kind is struct.
//
// severity is that shape exactly: a text pair over an int.
func TestAChainClaimedTypeMayNotKeyAMapWhateverItsKindIs(t *testing.T) {
	t.Parallel()

	type conf struct {
		M map[severity]string `ferry:"m"`
		V severity            `ferry:"v"`
	}

	mustRefuse(t, Compile[conf](), "severity", "text pair", ".AsMapKey()")

	// And the remedy the refusal names does compile, which is what makes it a
	// remedy rather than a dead end.
	reg := registryWith(t, StringText[severity]().AsMapKey())
	if err := Compile[conf](WithRegistry(reg)); err != nil {
		t.Fatalf("the registration the refusal names still refused: %+v", err)
	}
}

// version is a type carried as a number that also keys a map, which is the pair
// of positions [NumberKey] exists to cover with one registration.
type version int

func versionText(v version) (string, error) { return strconv.Itoa(int(v)), nil }

func parseVersion(text string) (version, error) {
	n, err := strconv.Atoi(text)

	return version(n), err
}

// TestANumberKeyAddressesAMapAndCarriesAsANumber drives one registration at both
// positions it covers.
//
// At the leaf the type crosses the boundary as a Number, which is the kind the
// constructor names. At the key position it never crosses as a value at all: the
// encode half's text is the address segment, and the decode half is handed that
// segment back with no kind attached, which is the one path a registered codec's
// text half is reached through.
func TestANumberKeyAddressesAMapAndCarriesAsANumber(t *testing.T) {
	t.Parallel()

	type conf struct {
		Pinned map[version]string `ferry:"pinned"`
		Latest version            `ferry:"latest"`
	}

	reg := registryWith(t, NumberKey(versionText, parseVersion).AsMapKey())
	in := conf{Pinned: map[version]string{3: "old", 11: "new"}, Latest: 7}

	p := newPlane(map[Path]Value{})
	if err := Dump(t.Context(), in, planeSink{p: p}, WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := p.values[At("latest")]; got != Number("7") {
		t.Errorf("the leaf dumped as %#v, want %#v: the kind is the one the constructor names", got, Number("7"))
	}

	if got := p.values[At("pinned").At("3")]; got != String("old") {
		t.Errorf("the entry under key 3 landed at no address the codec's text names: %v", p.values)
	}

	// The map is read back from a plane that can list what it holds, because a
	// map's addresses come from enumeration and there is nothing else to hand
	// the codec's decode half a segment.
	src := &listing{
		values:   p.values,
		children: map[Path][]Path{At("pinned"): {At("pinned").At("3"), At("pinned").At("11")}},
	}

	back, err := Load[conf](t.Context(), src, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if !maps.Equal(back.Pinned, in.Pinned) || back.Latest != in.Latest {
		t.Errorf("loaded back %v and %d, want %v and %d", back.Pinned, back.Latest, in.Pinned, in.Latest)
	}
}

// TestANullPolicyThatRefusesTheNullIsReportedAtTheAddress is the load policy's
// failure arm.
//
// The policy's load half is the registrant's own function and it is the only
// thing that runs for a Null, so a refusal from it is the whole of what the
// address can report. The policy is null-ish nowhere near the zero value, which
// is what lets the registration pass its own totality check and leaves the
// refusal for the walk to find.
func TestANullPolicyThatRefusesTheNullIsReportedAtTheAddress(t *testing.T) {
	t.Parallel()

	type conf struct {
		N plainCount `ferry:"n"`
	}

	reg := registryWith(t, NullValue(
		NumberValue(countText, parseCount),
		func() (plainCount, error) { return 0, errNotAnInteger },
		func(c plainCount) bool { return c == -1 }))

	_, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): Null}),
	}, WithRegistry(reg))

	if !errors.Is(err, ErrValue) {
		t.Fatalf("the load reported %v, want the load policy's own refusal", err)
	}
}

// TestANullPolicyThatPanicsIsFencedLikeAnyOtherCodec is the dump policy's
// failure arm, and it is the fence's rule applied to a half a caller writes
// without thinking of it as a codec: isNull is called on every value dumped, so
// a panic in it is a panic in user code and costs one address rather than the
// process.
func TestANullPolicyThatPanicsIsFencedLikeAnyOtherCodec(t *testing.T) {
	t.Parallel()

	type conf struct {
		N plainCount `ferry:"n"`
	}

	reg := registryWith(t, NullValue(
		NumberValue(countText, parseCount),
		func() (plainCount, error) { return -1, nil },
		func(c plainCount) bool {
			if c == 9 {
				panic("nil map read in the null policy")
			}

			return c == -1
		}))

	err := Dump(t.Context(), conf{N: 9}, planeSink{p: newPlane(map[Path]Value{})}, WithRegistry(reg))
	if !errors.Is(err, ErrPanic) {
		t.Fatalf("the dump reported %v, want a recovered panic", err)
	}
}

// TestANullPolicyOverNoRegistrationIsRefused is the modifier's own precondition.
//
// It is a modifier over one of the kind-named constructors and has no codec of
// its own, so there is nothing for a policy handed no inner registration to
// wrap, and the refusal is at the composition site where the caller wrote it.
func TestANullPolicyOverNoRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() {
		NullValue[plainCount](nil,
			func() (plainCount, error) { return 0, nil },
			func(c plainCount) bool { return c == 0 })
	}, "was given no registration to wrap")
}

// TestANullPolicyOverAKeyRegistrationIsRefused is the one composition that
// compiles, is meaningless, and corrupts a plane rather than failing.
//
// A key never crosses the boundary as a Value: it becomes the segment text of an
// address, read off whatever the encode half produced. Under a policy that half
// answers Null for a null-ish key, and a Null carries no text, so every null-ish
// key in a map addresses the container's own empty segment - two of them are one
// address, one entry is lost, and nothing reports it. That is precisely the
// silent failure .AsMapKey() exists to make a registrant think about, so the
// combination is refused at the composition site rather than left to be
// discovered on a written plane.
func TestANullPolicyOverAKeyRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	mustRefuseAtConstruction(t, func() {
		NullValue(
			StringKey(countText, parseCount).AsMapKey(),
			func() (plainCount, error) { return 0, nil },
			func(c plainCount) bool { return c == 0 })
	}, "may not be grafted onto a registration declared usable as a map key")

	// The same policy over the same codec without the claim is legal, which is
	// what makes the refusal about the combination rather than about either half.
	if err := refusalFrom(func() {
		NewRegistry(NullValue(
			StringValue(countText, parseCount),
			func() (plainCount, error) { return 0, nil },
			func(c plainCount) bool { return c == 0 }))
	}); err != nil {
		t.Errorf("the same policy over a registration that is not a key was refused: %+v", err)
	}
}

// TestANullPolicyUnderAPointerIsThePointersNull is the precedence a caller has
// to pick around, asserted so that it cannot drift silently.
//
// A nil pointer writes a null at its own address and a null loads back as a nil
// pointer, both structurally and before any codec is consulted. So at a *T field
// the pointer's null wins in both directions: a non-nil pointer to a value the
// policy calls null still dumps a null, and loading that back gives a nil
// pointer rather than the value load would have produced.
func TestANullPolicyUnderAPointerIsThePointersNull(t *testing.T) {
	t.Parallel()

	type conf struct {
		N *retryCount `ferry:"n"`
	}

	reg := registryWith(t, retryCodec())
	zero := retryCount(0)

	if got := dumpedValue(t, conf{N: &zero}, At("n"), WithRegistry(reg)); got != Null {
		t.Errorf("a non-nil pointer to a null-ish value dumped as %#v, want %#v", got, Null)
	}

	back, err := Load[conf](t.Context(), planeSource{
		p: newPlane(map[Path]Value{At("n"): Null}),
	}, WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if back.N != nil {
		t.Errorf("a null loaded as %v, want a nil pointer: under a *T the pointer's own null wins and the "+
			"load policy does not run", back.N)
	}
}

// TestANullPolicyRoundTripsTheNullAndTheValue is ADR-0017's null modifier at
// both arms, and its closure law seen from the outside: what the policy loads
// for a null is a value the policy still calls null, so the null path round
// trips rather than lying on it alone.
func TestANullPolicyRoundTripsTheNullAndTheValue(t *testing.T) {
	t.Parallel()

	type conf struct {
		N retryCount `ferry:"n"`
	}

	reg := registryWith(t, retryCodec())

	for in, want := range map[retryCount]Value{0: Null, 3: Number("3")} {
		if got := dumpedValue(t, conf{N: in}, At("n"), WithRegistry(reg)); got != want {
			t.Errorf("%d dumped as %#v, want %#v", in, got, want)
		}

		back, err := Load[conf](t.Context(), planeSource{
			p: newPlane(map[Path]Value{At("n"): want}),
		}, WithRegistry(reg))
		if err != nil {
			t.Fatalf("loading %#v back: %+v", want, err)
		}

		if back.N != in {
			t.Errorf("%d dumped as %#v and loaded back as %d", in, want, back.N)
		}
	}
}
