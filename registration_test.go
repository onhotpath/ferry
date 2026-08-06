package ferry

import (
	"errors"
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

	reg := registryWith(t, BytesValue(
		func(b blob) ([]byte, error) { return []byte(b), nil },
		func(b []byte) (blob, error) { return blob(b), nil }))

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
