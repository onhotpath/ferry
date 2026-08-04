package ferry

import (
	"context"
	"encoding"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every assertion in this file goes through Compile[T], Load, LoadOver and
// Dump: a chain rule is a rule about what a plane was handed and what a field
// was given back, and about what the compiler refused. The one exception is
// [TestACodecThatDeclaresOneKindAndEmitsAnotherIsCaught], which builds a
// leafCodec directly, and the reason it has to is written there.

// tick is the smallest complete text pair, over a kind ferry admits on its own.
//
// The decode half records what it was handed, so "the chain ran" is observable
// rather than inferred: a tick loaded through kind admission holds the plane's
// text, and one loaded through the chain holds it with saw: in front. That is
// also what makes step 2 beating step 3 assertable for a type both would take.
type tick string

const sawPrefix = "saw:"

func (k tick) MarshalText() ([]byte, error) { return []byte(k), nil }

func (k *tick) UnmarshalText(text []byte) error {
	*k = tick(sawPrefix + string(text))

	return nil
}

// uuid16 is a UUID's shape, reproduced here because core's go.mod require block
// is empty and stays empty (ADR-0002), so github.com/google/uuid - the type
// ADR-0007 measures - cannot be imported. What matters for the ordering rule is
// the shape: [16]byte, which kind admission takes as Bytes, carrying a text
// pair that kind admission never gets to see.
type uuid16 [16]byte

func (u uuid16) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16]), nil
}

func (u *uuid16) UnmarshalText(text []byte) error {
	raw, err := hex.DecodeString(strings.ReplaceAll(string(text), "-", ""))
	if err != nil {
		return err
	}

	if len(raw) != len(u) {
		return errors.New("a UUID is sixteen bytes")
	}

	copy(u[:], raw)

	return nil
}

func pinnedUUID() uuid16 {
	return uuid16{0x0e, 0x37, 0xdf, 0x36, 0xf6, 0x98, 0x11, 0xe6, 0x8d, 0xd4, 0xcb, 0x9c, 0xed, 0x3d, 0xf9, 0x76}
}

const pinnedUUIDText = "0e37df36-f698-11e6-8dd4-cb9ced3df976"

// legible is one Dump of one leaf value, deferred so a table can hold rows of
// different types.
func legible[T any](v T) func(*testing.T) Value {
	return func(t *testing.T) Value {
		t.Helper()

		return dumped(t, leafHolder[T]{V: v})
	}
}

// TestTheChainRunsBeforeKindAdmission is the largest question ADR-0007 owns,
// and the artefact is the argument.
//
// Four of these seven are refused outright without the chain, for mapping no
// address. The other three are the ordering: kind admission would answer, and
// answer with a representation nobody chose - net.IP as sixteen raw bytes, a
// UUID as sixteen more, and slog.Level as the integer 4. Both orders drift
// under an unrelated edit; the case for this one is that adding a MarshalText
// is a visibly serialization-shaped edit and exporting a field is not.
func TestTheChainRunsBeforeKindAdmission(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  func(*testing.T) Value
		want Value
	}{
		{"net.IP", legible(net.ParseIP("192.0.2.1")), String("192.0.2.1")},
		{"netip.Addr", legible(netip.MustParseAddr("192.0.2.1")), String("192.0.2.1")},
		{"netip.AddrPort", legible(netip.MustParseAddrPort("192.0.2.1:80")), String("192.0.2.1:80")},
		{"netip.Prefix", legible(netip.MustParsePrefix("10.0.0.0/8")), String("10.0.0.0/8")},
		{"big.Int", legible(*big.NewInt(1099511627776)), String("1099511627776")},
		{"a UUID", legible(pinnedUUID()), String(pinnedUUIDText)},
		{"slog.Level", legible(slog.LevelWarn), String("WARN")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := c.got(t); got != c.want {
				t.Errorf("%s lands as %#v, want %#v", c.name, got, c.want)
			}
		})
	}
}

// TestAChainClaimedTypeRoundTripsThroughEveryPosition is the audit ADR-0009
// asks for, at the positions a leaf can occupy: a bare field, behind a pointer,
// in a slice, in an array and as a map value.
//
// The map value is the one that is not decoration. It is the only position
// whose Go value is not addressable, so the encode half reaches a
// pointer-receiver method through a copy rather than through Addr, and taking
// the address instead panics.
func TestAChainClaimedTypeRoundTripsThroughEveryPosition(t *testing.T) {
	t.Parallel()

	t.Run("a leaf, a pointer and a slice", func(t *testing.T) {
		t.Parallel()

		want := everyPosition{
			Leaf:  netip.MustParseAddr("10.0.0.1"),
			Ptr:   ptrTo(netip.MustParseAddr("10.0.0.2")),
			Slice: []netip.Addr{netip.MustParseAddr("10.0.0.3")},
		}

		got, _ := roundTrip(t, want)
		if got.Leaf != want.Leaf || *got.Ptr != *want.Ptr || got.Slice[0] != want.Slice[0] {
			t.Errorf("round tripped to %+v, want %+v", got, want)
		}
	})

	t.Run("an array and a map value, which is not addressable", func(t *testing.T) {
		t.Parallel()

		want := everyPosition{
			Arr: [2]netip.Addr{netip.MustParseAddr("10.0.0.4"), netip.MustParseAddr("10.0.0.5")},
			Map: map[string]netip.Addr{"a": netip.MustParseAddr("10.0.0.6")},
		}

		got, _ := roundTrip(t, want)
		if got.Arr != want.Arr || got.Map["a"] != want.Map["a"] {
			t.Errorf("round tripped to %+v, want %+v", got, want)
		}
	})
}

type everyPosition struct {
	Leaf  netip.Addr            `ferry:"leaf"`
	Ptr   *netip.Addr           `ferry:"ptr"`
	Slice []netip.Addr          `ferry:"slice"`
	Arr   [2]netip.Addr         `ferry:"arr"`
	Map   map[string]netip.Addr `ferry:"map"`
}

func ptrTo[T any](v T) *T { return &v }

// hostPort is a struct with two exported fields and a text pair, and neither
// field carries a tag: the chain claims the whole type before the struct rule
// is reached, so there is no field for a tag to be missing from.
type hostPort struct {
	Host string
	Port int
}

func (h hostPort) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%s:%d", h.Host, h.Port), nil
}

func (h *hostPort) UnmarshalText(text []byte) error {
	host, port, ok := strings.Cut(string(text), ":")
	if !ok {
		return errors.New("no colon")
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return err
	}

	h.Host, h.Port = host, n

	return nil
}

// TestATextPairContributesOneAddress is the consequence the chain has for every
// stored artefact: a type's address set is a property of its own methods rather
// than of its shape.
//
// Under the other order the same type contributes /v/Host and /v/Port, and
// exporting a field on it rewrites every artefact of it.
func TestATextPairContributesOneAddress(t *testing.T) {
	t.Parallel()

	mustBeAddresses(t, boundBy(t, func(ctx context.Context, s Sink) error {
		return Dump(ctx, leafHolder[hostPort]{V: hostPort{Host: "db1", Port: 5432}}, s)
	}), []string{"/v"})

	if got := dumped(t, leafHolder[hostPort]{V: hostPort{Host: "db1", Port: 5432}}); got != String("db1:5432") {
		t.Errorf("the struct lands as %#v, want one string", got)
	}

	if got, _ := roundTrip(t, leafHolder[hostPort]{V: hostPort{Host: "db1", Port: 5432}}); got.V.Port != 5432 {
		t.Errorf("round tripped to %+v", got)
	}
}

// The three arms ADR-0007 dropped on a census rather than on taste. Each type
// carries exactly one of them and no text pair, so being claimed by kind
// admission instead is the whole assertion.
type (
	jsonOnly struct {
		N int `ferry:"n"`
	}
	binaryOnly struct {
		N int `ferry:"n"`
	}
	gobOnly struct {
		N int `ferry:"n"`
	}
)

// The fixtures really do carry the arms they are named for, checked by the
// compiler rather than by this file's own reading of them.
var (
	_ json.Marshaler             = jsonOnly{}
	_ json.Unmarshaler           = &jsonOnly{}
	_ encoding.BinaryMarshaler   = binaryOnly{}
	_ encoding.BinaryUnmarshaler = &binaryOnly{}
	_ gob.GobEncoder             = gobOnly{}
	_ gob.GobDecoder             = &gobOnly{}
)

func (j jsonOnly) MarshalJSON() ([]byte, error)  { return []byte(strconv.Itoa(j.N)), nil }
func (j *jsonOnly) UnmarshalJSON(b []byte) error { return intoInt(&j.N, string(b)) }

func (b binaryOnly) MarshalBinary() ([]byte, error)  { return []byte(strconv.Itoa(b.N)), nil }
func (b *binaryOnly) UnmarshalBinary(d []byte) error { return intoInt(&b.N, string(d)) }

func (g gobOnly) GobEncode() ([]byte, error) { return []byte(strconv.Itoa(g.N)), nil }
func (g *gobOnly) GobDecode(d []byte) error  { return intoInt(&g.N, string(d)) }

func intoInt(dst *int, raw string) error {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return err
	}

	*dst = n

	return nil
}

// TestOnlyTheTextPairIsAnArm is the census turned into three cases.
//
// gob is the sole arm for none of 29 measured types; json's sole rescue is
// json.RawMessage, which kind admission already carries as Bytes; and binary's
// sole rescue is url.URL, which it would rescue by base64-encoding a string. A
// type carrying one of them and no text pair is therefore not claimed at all,
// and its fields are addressed as any other struct's.
func TestOnlyTheTextPairIsAnArm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dump func(context.Context, Sink) error
	}{{
		name: "json.Marshaler",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, leafHolder[jsonOnly]{}, s) },
	}, {
		name: "encoding.BinaryMarshaler",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, leafHolder[binaryOnly]{}, s) },
	}, {
		name: "gob.GobEncoder",
		dump: func(ctx context.Context, s Sink) error { return Dump(ctx, leafHolder[gobOnly]{}, s) },
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustBeAddresses(t, boundBy(t, c.dump), []string{"/v/n"})
		})
	}

	if got, _ := roundTrip(t, leafHolder[jsonOnly]{V: jsonOnly{N: 7}}); got.V.N != 7 {
		t.Errorf("a json-only type round tripped to %+v", got)
	}
}

// bothSpellings carries both spellings of the encode half, and they disagree on
// purpose, so which one ferry calls is visible in the artefact.
//
// Nothing enforces that a type's AppendText and MarshalText agree - the
// standard library implements one in terms of the other on every type carrying
// both - so a fixture where they agreed would assert nothing about preference.
type bothSpellings struct{ n int }

func (b bothSpellings) AppendText(dst []byte) ([]byte, error) {
	return append(dst, "appended:"+strconv.Itoa(b.n)...), nil
}

func (b bothSpellings) MarshalText() ([]byte, error) {
	return []byte("marshalled:" + strconv.Itoa(b.n)), nil
}

func (b *bothSpellings) UnmarshalText(text []byte) error {
	_, num, _ := strings.Cut(string(text), ":")

	return intoInt(&b.n, num)
}

// TestTextAppenderIsPreferredOverTextMarshaler is ADR-0007's one preference
// inside the arm, and it is a spelling rather than an arm because encoding
// exports no appending decoder to pair it with.
func TestTextAppenderIsPreferredOverTextMarshaler(t *testing.T) {
	t.Parallel()

	if got := dumped(t, leafHolder[bothSpellings]{V: bothSpellings{n: 7}}); got != String("appended:7") {
		t.Errorf("the leaf lands as %#v, want the appender's text", got)
	}
}

// The half pairs. Every one of these is a type somebody wrote a method on for
// exactly this purpose, which is why falling through to kind admission is a
// silence ADR-0001 rules out rather than a kindness.
type (
	encOnly    struct{ N int }
	appendOnly struct{ N int }
	decOnly    struct {
		N int `ferry:"n"`
	}
	textEncJSONDec struct{ N int }
	binEncTextDec  struct{ N int }
	valueDec       struct{ N int }
)

func (e encOnly) MarshalText() ([]byte, error) { return []byte(strconv.Itoa(e.N)), nil }

func (a appendOnly) AppendText(dst []byte) ([]byte, error) {
	return append(dst, strconv.Itoa(a.N)...), nil
}

func (d *decOnly) UnmarshalText(text []byte) error { return intoInt(&d.N, string(text)) }

func (x textEncJSONDec) MarshalText() ([]byte, error)  { return []byte(strconv.Itoa(x.N)), nil }
func (x *textEncJSONDec) UnmarshalJSON(b []byte) error { return intoInt(&x.N, string(b)) }

func (y binEncTextDec) MarshalBinary() ([]byte, error)   { return []byte(strconv.Itoa(y.N)), nil }
func (y *binEncTextDec) UnmarshalText(text []byte) error { return intoInt(&y.N, string(text)) }

func (v valueDec) MarshalText() ([]byte, error) { return []byte(strconv.Itoa(v.N)), nil }

// UnmarshalText on a value receiver, which is the whole point of this fixture:
// it writes to a copy and the destination is unchanged.
func (v valueDec) UnmarshalText(text []byte) error { return intoInt(&v.N, string(text)) }

// TestAnIncompletePairDoesNotCompile is the refusal, from reflect.TypeFor[T]()
// alone and identically in both directions.
//
// Three corpora hold zero half pairs between them - 29 config types probed in
// process, the whole go1.27rc2 public standard library, and eleven third-party
// modules with their transitive dependencies - so refusing costs nothing that
// exists, and the two alternatives each cost something that does: using the
// half is a value that dumps and never loads, and falling through to kind
// admission ignores a method the user wrote, with no diagnostic.
func TestAnIncompletePairDoesNotCompile(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "an encoder and no decoder",
		run:      Compile[leafHolder[encOnly]],
		want:     []string{"/v:", "implements encoding.TextMarshaler but not encoding.TextUnmarshaler"},
		elements: 1,
	}, {
		name:     "an appender and no decoder",
		run:      Compile[leafHolder[appendOnly]],
		want:     []string{"/v:", "implements encoding.TextAppender but not encoding.TextUnmarshaler"},
		elements: 1,
	}, {
		name:     "a decoder and no encoder",
		run:      Compile[leafHolder[decOnly]],
		want:     []string{"/v:", "implements encoding.TextUnmarshaler but not encoding.TextMarshaler"},
		elements: 1,
	}, {
		name:     "a text encoder and a json decoder",
		run:      Compile[leafHolder[textEncJSONDec]],
		want:     []string{"/v:", "implements encoding.TextMarshaler but not encoding.TextUnmarshaler"},
		elements: 1,
	}, {
		name:     "a binary encoder and a text decoder",
		run:      Compile[leafHolder[binEncTextDec]],
		want:     []string{"/v:", "implements encoding.TextUnmarshaler but not encoding.TextMarshaler"},
		elements: 1,
	}, {
		name:     "a decoder on a value receiver",
		run:      Compile[leafHolder[valueDec]],
		want:     []string{"/v:", "UnmarshalText on a value receiver", "decodes into a copy"},
		elements: 1,
	}})
}

// TestAnIncompletePairDoesNotFallThroughToKind is the assertion the refusal
// exists for, and it is the one a naive implementation passes by accident.
//
// decOnly has an exported field with a tag, so kind admission would compile it
// to /v/n and round-trip it, silently ignoring the UnmarshalText its author
// wrote. Nothing reaches a plane in either direction.
func TestAnIncompletePairDoesNotFallThroughToKind(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{At("v", "n"): Number("7")})

	got, err := Load[leafHolder[decOnly]](t.Context(), planeSource{p: p})
	if err == nil {
		t.Fatalf("the load compiled and returned %+v, so the half pair fell through to kind", got)
	}

	if len(p.got) != 0 || p.bound != nil {
		t.Errorf("a plane was bound or asked for %v under a schema that does not compile", p.got)
	}

	dumpErr := Dump(t.Context(), leafHolder[decOnly]{}, planeSink{p: newPlane(map[Path]Value{})})
	if reportOf(dumpErr) != reportOf(err) {
		t.Errorf("Dump reports\n\t%s\nand Load reports\n\t%s: the pair is one claim serving both",
			reportOf(dumpErr), reportOf(err))
	}

	mustBeClass(t, err, ErrSchema)
}

// TestAValueReceiverIsNotTheDecodeHalf is the sharpest of the half pairs,
// because the method exists and does nothing.
//
// Measured, an UnmarshalText on a value receiver writes to a copy and the
// destination is unchanged, so the probe is "T or *T implements the encoder"
// and "*T implements the decoder". Admitting it would be a load that reports
// success and writes nothing.
func TestAValueReceiverIsNotTheDecodeHalf(t *testing.T) {
	t.Parallel()

	var v valueDec
	if err := v.UnmarshalText([]byte("7")); err != nil || v.N != 0 {
		t.Fatalf("the fixture's UnmarshalText left N=%d, so it is not the silent-copy case", v.N)
	}

	err := Compile[leafHolder[valueDec]]()
	if err == nil {
		t.Fatal("a value-receiver UnmarshalText compiled, so a load would write to a copy")
	}

	mustContain(t, reportOf(err), []string{"move UnmarshalText to *ferry.valueDec"})
}

// TestThePointerShapeIsResolvedBeforeTheChain is the first defect ADR-0007's
// prototype found, and it is one omitted line.
//
// *big.Int implements the whole text pair in its own right, because big.Int's
// text methods are on the pointer receiver. Consulted before the pointer shape,
// the chain makes a *big.Int field a leaf, ADR-0005's nil-pointer rule never
// runs, a nil dumps string("<nil>") and the load segfaults inside math/big on a
// nil receiver: a wrong value on the way out and a crash on the way in.
func TestThePointerShapeIsResolvedBeforeTheChain(t *testing.T) {
	t.Parallel()

	if got := dumped(t, leafHolder[*big.Int]{}); got != Null() {
		t.Errorf("a nil *big.Int writes %#v, want a null at its own address", got)
	}

	back, err := Load[leafHolder[*big.Int]](t.Context(),
		planeSource{p: newPlane(map[Path]Value{leafAddr: Null()})})
	if err != nil {
		t.Fatalf("loading a null back: %+v", err)
	}

	if back.V != nil {
		t.Errorf("a null loaded back as %v, want a nil pointer", back.V)
	}

	if got, _ := roundTrip(t, leafHolder[*big.Int]{V: big.NewInt(42)}); got.V.Int64() != 42 {
		t.Errorf("a set *big.Int round tripped to %v", got.V)
	}
}

// TestTheTextArmDeclaresString is the kind half of the claim, and it is a field
// of the codec resolved by the same lookup that finds the codec.
//
// encoding.TextMarshaler produces text and says nothing about kind, so the arm
// declares String and core donates String to it. The cost is stated rather than
// hidden: a YAML document holding an unquoted 1099511627776 reports Number, the
// arm wants String, and a plane's kind assertion is respected rather than
// second-guessed. The remedy is a registration declaring Number.
func TestTheTextArmDeclaresString(t *testing.T) {
	t.Parallel()

	got, err := readLeaf[netip.Addr](String("10.0.0.1"))
	if err != nil || got != "10.0.0.1" {
		t.Errorf("String at a text-arm leaf gave %q, %v", got, err)
	}

	if _, err := readLeaf[netip.Addr](Number("10")); !errors.Is(err, ErrWrongKind) {
		t.Errorf("Number at a text-arm leaf gave %v, want a wrong kind", err)
	}
}

// TestADeclaredNumberLoadsFromBothItsOwnKindAndString is the donor rule from
// the other end, at core's own Number-declaring leaves.
//
// It is the property ADR-0007 calls the most consequential thing a registration
// inherits: a codec that declares Number loads from a typed plane that says
// Number and from a flat one that says String, and one that declares the wrong
// kind works on YAML and fails on env, or the reverse.
func TestADeclaredNumberLoadsFromBothItsOwnKindAndString(t *testing.T) {
	t.Parallel()

	for _, got := range []Value{Number("8080"), String("8080")} {
		out, err := readLeaf[int](got)
		if err != nil || out != "8080" {
			t.Errorf("%#v at a Number-declaring leaf gave %q, %v", got, out, err)
		}
	}

	if _, err := readLeaf[int](Bool(true)); !errors.Is(err, ErrWrongKind) {
		t.Errorf("Bool at a Number-declaring leaf gave %v, want a wrong kind", err)
	}
}

// TestACodecThatDeclaresOneKindAndEmitsAnotherIsCaught is the one check core
// can make about a codec it did not write.
//
// It builds a leafCodec directly, which every other test in this package
// refuses to do, and the reason is that no exported surface can produce a
// lying codec yet: the text arm declares String and produces String by
// construction, and the registration that lets a caller declare a kind is #79's.
// The alternative is an unasserted branch on the Dump path of every leaf, which
// is worse than one white-box unit here.
func TestACodecThatDeclaresOneKindAndEmitsAnotherIsCaught(t *testing.T) {
	t.Parallel()

	liar := leafCodec{
		kind:   KindNumber,
		encode: func(reflect.Value) (Value, error) { return String("8080"), nil },
	}

	_, err := liar.emit(reflect.ValueOf(0))
	if err == nil {
		t.Fatal("a codec declaring number and producing string was accepted")
	}

	mustContain(t, err.Error(), []string{"declared number and produced string"})

	// Null is emittable by any codec whatever it declared, because ADR-0005's
	// registered net.Addr codec returns Null for a nil interface and takes Null
	// back, and that is the mechanism that makes an interface expressible.
	nuller := leafCodec{
		kind:   KindNumber,
		encode: func(reflect.Value) (Value, error) { return Null(), nil },
	}

	if _, err := nuller.emit(reflect.ValueOf(0)); err != nil {
		t.Errorf("a Null emitted by a Number-declaring codec was refused: %v", err)
	}
}

// TestTheChainIsNeverInvokedForAbsentAndAlwaysForTheRest is ADR-0007's
// invocation rule, and it repairs survey item 5.9's last bullet from the
// opposite direction to xload's.
//
// xload's decoder is handed neither the empty value nor the missing key,
// because it cannot tell them apart. ADR-0004 made them two observations, so
// ferry hands the empty string to a decoder and withholds the absent address:
// Absent means ferry does not write to the field at all, which is the walk's
// decision rather than a codec's, and putting it in a codec would put ADR-0006's
// rule in every codec to be reimplemented and got wrong.
func TestTheChainIsNeverInvokedForAbsentAndAlwaysForTheRest(t *testing.T) {
	t.Parallel()

	t.Run("Absent does not reach it", absentDoesNotReachTheChain)
	t.Run("the empty string does", theEmptyStringReachesTheChain)
	t.Run("and Null does, and an arm with no null refuses it", nullReachesTheChain)
}

func absentDoesNotReachTheChain(t *testing.T) {
	t.Parallel()

	seed := leafHolder[tick]{V: "seeded"}

	got, err := LoadOver(t.Context(), seed, planeSource{p: newPlane(map[Path]Value{})})
	if err != nil || got.V != "seeded" {
		t.Errorf("an absent address gave %q, %v: the seed is what an unwritten field keeps", got.V, err)
	}
}

func theEmptyStringReachesTheChain(t *testing.T) {
	t.Parallel()

	got, err := readLeaf[tick](String(""))
	if err != nil || got != sawPrefix {
		t.Errorf("the empty string gave %q, %v, want the decode half to have run on it", got, err)
	}
}

func nullReachesTheChain(t *testing.T) {
	t.Parallel()

	_, err := readLeaf[tick](Null())
	if !errors.Is(err, ErrWrongKind) {
		t.Errorf("Null at a text-arm leaf gave %v, want the ordinary wrong-kind refusal", err)
	}
}

// blankText is not the Go zero value and its text is empty, which is the row
// that decides omission cannot be read off the encoded form.
type blankText struct{ Set bool }

func (blankText) MarshalText() ([]byte, error) { return nil, nil }

func (b *blankText) UnmarshalText([]byte) error {
	b.Set = true

	return nil
}

// TestOmissionIsDecidedBeforeTheChainRuns is the order confirmed from the other
// end, and the measurement is why there was never a second option.
//
// "Zero in Go" and "empty on the plane" disagree in both directions. A zero
// time.Time is Go-zero and encodes to a non-empty text, so a rule reading the
// encoded form would never omit it; a deliberately-set blankText is not
// Go-zero and encodes to nothing, so the same rule would drop a value the user
// set. The chain converts whatever survives the omission decision and is never
// the thing that decides it.
func TestOmissionIsDecidedBeforeTheChainRuns(t *testing.T) {
	t.Parallel()

	want := String("0001-01-01T00:00:00Z")
	if got := dumped(t, leafHolder[time.Time]{}); got != want {
		t.Errorf("a zero time.Time lands as %#v, want %#v at its own address", got, want)
	}

	if got := dumped(t, leafHolder[blankText]{V: blankText{Set: true}}); got != String("") {
		t.Errorf("a set value whose text is empty lands as %#v, want it written anyway", got)
	}
}

// TestNoCodecSeesAContextOrTheCallOptions is a lifecycle statement read out of
// the source, because an absent parameter is what says it.
//
// No recognised interface takes a context: MarshalText, UnmarshalText,
// MarshalJSON and xload's own Decode(string) error are all context-free, so a
// ferry-declared context-carrying arm would be the only one and whether a
// type's conversion is cancellable would depend on which arm claimed it. A
// lifecycle property decided by list order is not a property. The use case a
// context would serve - a codec doing I/O - is a wrapping Source, which
// ADR-0004 already gives a context.
//
// Options are refused by the same reasoning: a codec whose output depends on
// the call has a different representation per call and no golden row.
func TestNoCodecSeesAContextOrTheCallOptions(t *testing.T) {
	t.Parallel()

	forbidden := map[string]string{
		"context": "a codec takes no context.Context; I/O belongs in a wrapping Source",
		"Option":  "a codec cannot see call options; its output would differ per call",
		"config":  "a codec cannot see call options; its output would differ per call",
	}

	for _, name := range []string{"codec.go", "types.go"} {
		checkNames(t, name, forbidden)
	}
}

// checkNames fails for every identifier one file names that it may not see.
func checkNames(t *testing.T, name string, forbidden map[string]string) {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	for id := range identsOf(f) {
		if why, bad := forbidden[id]; bad {
			t.Errorf("%s names %s: %s", name, id, why)
		}
	}
}

// identsOf is every identifier a file names, which is what makes "this file
// cannot see that type" checkable at all.
func identsOf(f *ast.File) map[string]struct{} {
	out := map[string]struct{}{}

	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out[id.Name] = struct{}{}
		}

		return true
	})

	return out
}

// TestAChainClaimedTypeMayNotKeyAMap is ADR-0007's own sentence reversed under
// #45, and the reversal is not a claim that the text is lossy.
//
// Measured against adversarial value lists, every type the chain claims from
// the standard library is injective, including a 4-in-6 address and a zoned
// one. The refusal is that nobody can be asked: a registration has a call site
// where .AsMapKey() communicates the obligation, which ADR-0009 calls the only
// moment a registrant is guaranteed to read, and a text pair has no such
// moment. So the diagnostic names a type its author never mentioned and must
// not accuse it of anything.
func TestAChainClaimedTypeMayNotKeyAMap(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "a chain-claimed key",
		run:  Compile[chainKeyed],
		want: []string{
			"/m: netip.Addr may not key a map",
			"through its text pair rather than through a registration",
			"ferry.TextCodec[netip.Addr](ferry.KindString).AsMapKey()",
		},
		elements: 1,
	}, {
		name:     "and time.Time keeps core's own message, because identity is consulted first",
		run:      Compile[timeKeyed],
		want:     []string{"/m: time.Time is in core's own set", "== compares the *Location"},
		elements: 1,
	}})
}

type (
	chainKeyed struct {
		M map[netip.Addr]string `ferry:"m"`
	}
	timeKeyed struct {
		M map[time.Time]string `ferry:"m"`
	}
)

// TestCoresOwnGuaranteeIsUnchangedWithTheChainOn is the measurement ADR-0007
// closes on: identity is consulted before the chain, and no kind-admitted core
// type carries a text pair, so core's set is unaffected by a step inserted
// between them.
//
// The golden table itself runs in composite_test.go against the memory plane.
// What is asserted here is the mechanism that makes it stay green: every core
// leaf still lands at its own declared kind, where a type the chain had claimed
// would land as String.
func TestCoresOwnGuaranteeIsUnchangedWithTheChainOn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  func(*testing.T) Value
		want Value
	}{
		{"bool", legible(true), Bool(true)},
		{"string", legible("svc"), String("svc")},
		{"int", legible(8080), Number("8080")},
		{"float64", legible(0.5), Number("0.5")},
		{"[]byte", legible([]byte("hi")), Bytes([]byte("hi"))},
		{"[2]byte", legible([2]byte{'h', 'i'}), Bytes([]byte("hi"))},
		{"time.Duration", legible(30 * time.Second), String("30s")},
		{"time.Time", legible(pinnedTime()), String("2026-08-02T12:00:00.123456789Z")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := c.got(t); got != c.want {
				t.Errorf("%s lands as %#v, want %#v", c.name, got, c.want)
			}
		})
	}
}

// refusesEverything is a text pair whose encode half refuses the value it is
// handed, which is a partial representation and the one thing core's own set
// has an example of: time.Time outside years 0 to 9999.
type refusesEverything struct{ N int }

func (refusesEverything) MarshalText() ([]byte, error) {
	return nil, errors.New("this value has no text form")
}

func (r *refusesEverything) UnmarshalText(text []byte) error { return intoInt(&r.N, string(text)) }

// TestBothHalvesOfTheArmReportRatherThanSwallow is ADR-0001's rule at the one
// place the chain hands control to code ferry did not write.
//
// A text pair is an implicit registration and carries the registrant's
// guarantee rather than core's, so when either half refuses, the refusal is the
// caller's answer. Neither message repeats what the plane held, which ADR-0011
// makes total because ferry cannot know which addresses carry secrets.
func TestBothHalvesOfTheArmReportRatherThanSwallow(t *testing.T) {
	t.Parallel()

	dumpErr := Dump(t.Context(), leafHolder[refusesEverything]{}, planeSink{p: newPlane(map[Path]Value{})})
	if dumpErr == nil {
		t.Fatal("an encode half that refused was swallowed")
	}

	mustContain(t, reportOf(dumpErr), []string{"/v:", "no representation for this ferry.refusesEverything"})
	mustBeClass(t, dumpErr, ErrValue)

	_, loadErr := readLeaf[hostPort](String("no-colon-here"))
	if loadErr == nil {
		t.Fatal("a decode half that refused was swallowed")
	}

	mustContain(t, reportOf(loadErr), []string{"/v:", "not a valid ferry.hostPort"})

	if strings.Contains(reportOf(loadErr), "no-colon-here") {
		t.Errorf("the report\n\t%s\nrepeats a value the plane supplied", reportOf(loadErr))
	}

	mustBeClass(t, loadErr, ErrValue)
}
