package ferrytest

// This file is package ferrytest rather than ferrytest_test, because the two
// columns it holds [CoreTypes] to - the relation and the golden - are typed by a
// parameter no caller can name, so `columns` is the only route to them and it is
// unexported by decision.
//
// Everything about the table that a plane can observe is asserted through the
// seam instead, in core's own coretypes_test.go, which runs the rows the engine
// can carry through [RoundTrip] and asserts that the ones it cannot still fail.

import (
	"flag"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
)

// wantRows and wantCases are ADR-0014's counts, and they are here as an
// assertion rather than as documentation: the table is a published artefact, so
// a row or a case appearing or disappearing is a change to a compatibility
// promise and has to be a change somebody made on purpose.
const (
	wantRows  = 19
	wantCases = 58
)

// TestCoreTypesIsNineteenRowsAndFiftyEightCases is the count, taken from the
// table rather than from the comment above it.
func TestCoreTypesIsNineteenRowsAndFiftyEightCases(t *testing.T) {
	t.Parallel()

	rows := CoreTypes()
	if len(rows) != wantRows {
		t.Errorf("CoreTypes has %d rows, want %d", len(rows), wantRows)
	}

	total := 0

	for _, p := range rows {
		_, _, goldens := p.columns()
		total += len(goldens)
	}

	if total != wantCases {
		t.Errorf("CoreTypes has %d cases, want %d", total, wantCases)
	}
}

// TestEveryRowCarriesARelationAndEveryCaseAGolden is ADR-0005's "a proof is a
// triple" held to structurally, so a row added without either fails here rather
// than being discovered as a nil dereference inside somebody's driver CI.
//
// The golden is checked for being the zero [ferry.Value], which is Absent - a
// plane reporting that it does not hold an address. No value ferry encodes at a
// leaf is ever that, so an Absent in this column is a case whose third column
// was never written rather than a case that pins absence.
func TestEveryRowCarriesARelationAndEveryCaseAGolden(t *testing.T) {
	t.Parallel()

	for _, p := range CoreTypes() {
		checkColumns(t, p)
	}
}

// checkColumns is one row's unreadable columns.
func checkColumns(t *testing.T, p Proof) {
	t.Helper()

	hasRelation, _, goldens := p.columns()
	if !hasRelation {
		t.Errorf("%s carries no equality relation", p.Name())
	}

	if len(goldens) == 0 {
		t.Errorf("%s carries no cases", p.Name())
	}

	for i, g := range goldens {
		if g == (ferry.Value{}) {
			t.Errorf("%s: case %d carries no golden", p.Name(), i)
		}
	}
}

// TestNoRowIsDuplicatedByType is the join the completeness check makes, run
// against the table itself: two rows for one type mean one of them discharges
// nothing, and the name column cannot say so because a name is a label.
func TestNoRowIsDuplicatedByType(t *testing.T) {
	t.Parallel()

	seen := map[reflect.Type]string{}

	for _, p := range CoreTypes() {
		if first, dup := seen[p.Type()]; dup {
			t.Errorf("%s and %s are both rows for %s", first, p.Name(), p.Type())

			continue
		}

		seen[p.Type()] = p.Name()
	}
}

// TestCoreTypesIsFreshPerCall is what makes the table safe to append to, which
// is how a registrant asks the completeness check about their own types:
// `Complete(reg, append(CoreTypes(), proofs...)...)` must not be able to reach
// the next caller's table.
func TestCoreTypesIsFreshPerCall(t *testing.T) {
	t.Parallel()

	first, second := CoreTypes(), CoreTypes()
	if len(first) == 0 || &first[0] == &second[0] {
		t.Error("two calls to CoreTypes share a backing array")
	}
}

// TestCoreTypesCoversEveryAdmittedMember is the completeness check, and it is
// the reason the row count is not counted by hand.
//
// Run for the first time against the table it replaces, this reported eighteen
// admitted members against eleven rows, and the seven with no coverage - int16,
// int32, int64, uint, uint8, uint16 and uint32 - were exactly the ones nobody
// would think to doubt. ADR-0013 states why that is not housekeeping: the
// compatibility promise is exactly as wide as this table, so an admitted member
// with no row is outside the promise by accident rather than by decision.
//
// It is [Complete] since #79, and the member list it used to carry is that
// function's two tables. The check core makes about its own set and the one a
// registrant makes about theirs are one function over the union of three tables
// (ADR-0014), so this test is now the same call an ordinary consumer writes.
func TestCoreTypesCoversEveryAdmittedMember(t *testing.T) {
	t.Parallel()

	for _, s := range Complete(nil, CoreTypes()...) {
		t.Errorf("core type set: %s", s)
	}
}

// goldensOf is one row's golden column, which is what most of the assertions
// below read.
func goldensOf[T any](t *testing.T) []ferry.Value {
	t.Helper()

	_, goldens := casesOf[T](t)

	return goldens
}

// TestTheCompositeRowCarriesTheMandatedValues is ADR-0005's composite list: nil,
// empty, and one containing an empty element.
//
// The three are asserted by the property that puts each on the list rather than
// by restating the row. The first two write Null at the composite's own address,
// which is the zero [ferry.Path] here, and are one observation through a plane;
// the third writes nothing at that address at all, so its golden is pinned at the
// element and is the only case in the table whose address is not the value's own.
// That is the whole of what the third value is for, and it is what the count
// moved for.
func TestTheCompositeRowCarriesTheMandatedValues(t *testing.T) {
	t.Parallel()

	addrs, goldens := casesOf[[]string](t)

	const mandated = 3
	if len(goldens) != mandated {
		t.Fatalf("[]string carries %d cases, want ADR-0005's %d values", len(goldens), mandated)
	}

	for i := range 2 {
		if addrs[i] != (ferry.Path{}) || goldens[i] != ferry.Null {
			t.Errorf("[]string: case %d pins %#v at %q, want null at the composite's own address",
				i, goldens[i], addrs[i])
		}
	}

	if want := (ferry.Path{}).Elem(0); addrs[2] != want {
		t.Errorf("[]string: case 2 pins its golden at %q, want the element address %q", addrs[2], want)
	}

	if goldens[2] != ferry.String("") {
		t.Errorf("[]string: case 2 pins %#v, want the empty element it contains", goldens[2])
	}
}

// casesOf is one row's address and golden columns, looked up by the type it
// discharges, which is the same join the completeness check makes.
func casesOf[T any](t *testing.T) ([]ferry.Path, []ferry.Value) {
	t.Helper()

	want := reflect.TypeFor[T]()

	for _, p := range CoreTypes() {
		if p.Type() == want {
			_, addrs, goldens := p.columns()

			return addrs, goldens
		}
	}

	t.Fatalf("no row for %s", want)

	return nil, nil
}

// TestNilAndEmptyBytesCarryDifferentGoldens is the case that earned the golden
// column on its first run.
//
// ADR-0005 says a composite with no elements writes Null at its own address
// whether it is nil or empty, and []byte is admitted as a leaf at kind Bytes, so
// that rule does not reach it. [SliceEq] conflates the two deliberately; the
// column does not, and a reader following the prose alone gets it wrong.
func TestNilAndEmptyBytesCarryDifferentGoldens(t *testing.T) {
	t.Parallel()

	goldens := goldensOf[[]byte](t)
	if goldens[0] != ferry.Null {
		t.Errorf("[]byte(nil) pins %#v, want %#v", goldens[0], ferry.Null)
	}

	if goldens[1] != ferry.Bytes(nil) {
		t.Errorf("[]byte{} pins %#v, want %#v", goldens[1], ferry.Bytes(nil))
	}

	if goldens[0] == goldens[1] {
		t.Errorf("[]byte(nil) and []byte{} pin one golden, %#v", goldens[0])
	}
}

// TestTheBytesGoldensAreTheRawBytes is ADR-0005's "Bytes carries the bytes, and
// how a plane spells them is the driver's": no base64 anywhere in core's column,
// and the same three bytes off the slice row and the array row.
func TestTheBytesGoldensAreTheRawBytes(t *testing.T) {
	t.Parallel()

	want := ferry.Bytes([]byte{0x00, 0xff, 0x41})
	for _, tc := range []struct {
		name string
		got  ferry.Value
	}{
		{"[]byte", goldensOf[[]byte](t)[2]},
		{"[3]byte", goldensOf[[3]byte](t)[0]},
	} {
		if tc.got != want {
			t.Errorf("%s pins %#v, want %#v", tc.name, tc.got, want)
		}
	}
}

// TestTheIdentityGoldensArePinned is the pair ferry owns by type identity, and
// they are the two representations ADR-0013 uses as its worked examples of a
// change no tool in the Go toolchain can see.
func TestTheIdentityGoldensArePinned(t *testing.T) {
	t.Parallel()

	if got, want := goldensOf[time.Duration](t)[0], ferry.String("30s"); got != want {
		t.Errorf("time.Duration pins %#v, want %#v", got, want)
	}

	got := goldensOf[time.Time](t)[0]

	text, err := got.AsString()
	if err != nil {
		t.Fatalf("time.Time pins %#v, which is not a String: %v", got, err)
	}

	// RFC 3339 with nanoseconds, asserted by reading the text back with the
	// layout it claims to be in and requiring the same string out. A layout that
	// dropped the fractional second would re-render differently and fail here.
	when, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatalf("time.Time pins %q, which is not RFC 3339: %v", text, err)
	}

	if !strings.Contains(text, ".") || when.Format(time.RFC3339Nano) != text {
		t.Errorf("time.Time pins %q, want RFC 3339 with nanoseconds", text)
	}
}

// TestTheIntegerGoldensAreBaseTen reads every integer row's column back with
// base 10 and requires the canonical spelling out again, so a golden with a
// leading zero, a sign on a zero or a hexadecimal digit fails here.
func TestTheIntegerGoldensAreBaseTen(t *testing.T) {
	t.Parallel()

	for _, p := range CoreTypes() {
		if !isIntegerRow(p.Type()) {
			continue
		}

		_, _, goldens := p.columns()
		for i, g := range goldens {
			checkBaseTen(t, p.Name(), i, g)
		}
	}
}

// isIntegerRow is the ten widths ADR-0005 admits as Number.
//
// The unnamed-type test is the whole of ADR-0005's ordering rule, asked from the
// table's side: time.Duration's kind is int64 and its representation is a
// string, because identity is consulted before kind, so a row whose type has a
// package is not an integer row however its kind reads.
func isIntegerRow(t reflect.Type) bool {
	k := t.Kind()

	return t.PkgPath() == "" && k >= reflect.Int && k <= reflect.Uint64
}

// checkBaseTen is one integer golden: a Number, parsed in base 10, and spelling
// itself the same way again.
func checkBaseTen(t *testing.T, name string, i int, g ferry.Value) {
	t.Helper()

	text, err := g.AsNumber()
	if err != nil {
		t.Errorf("%s: case %d pins %#v, which is not a Number", name, i, g)

		return
	}

	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		if strconv.FormatInt(n, 10) != text {
			t.Errorf("%s: case %d pins %q, which is not canonical base 10", name, i, text)
		}

		return
	}

	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		t.Errorf("%s: case %d pins %q, which does not parse in base 10", name, i, text)

		return
	}

	if strconv.FormatUint(n, 10) != text {
		t.Errorf("%s: case %d pins %q, which is not canonical base 10", name, i, text)
	}
}

// TestTheFloatGoldensAreShortestRoundTripAtTheirOwnBitSize is the one pinning
// that cannot be checked by eye, and it is the one where the two rows have to
// disagree.
//
// Every golden is parsed at its row's own bit size and re-formatted at that size
// with 'g' and precision -1, which is exactly ADR-0005's representation, and the
// text has to come back identical. A float32 formatted at 64 bits gives
// 0.10000000149011612 - a value that re-rounds to the same float32 and is a
// wrong-looking config file - so the bit size is load-bearing rather than
// incidental.
func TestTheFloatGoldensAreShortestRoundTripAtTheirOwnBitSize(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		bits    int
		goldens []ferry.Value
	}{
		{"float32", 32, goldensOf[float32](t)},
		{"float64", 64, goldensOf[float64](t)},
	} {
		for i, g := range tc.goldens {
			checkShortest(t, tc.name, tc.bits, i, g)
		}
	}
}

// checkShortest is one float golden at one bit size.
func checkShortest(t *testing.T, name string, bits, i int, g ferry.Value) {
	t.Helper()

	text, err := g.AsNumber()
	if err != nil {
		t.Errorf("%s: case %d pins %#v, which is not a Number", name, i, g)

		return
	}

	f, err := strconv.ParseFloat(text, bits)
	if err != nil {
		t.Errorf("%s: case %d pins %q, which does not parse at %d bits", name, i, text, bits)

		return
	}

	if again := strconv.FormatFloat(f, 'g', -1, bits); again != text {
		t.Errorf("%s: case %d pins %q, and %d bits shortest is %q", name, i, text, bits, again)
	}
}

// TestTheTwoFloatWidthsDisagree is the half the check above cannot make: both
// rows would pass it if float32's column had been written at 64 bits and simply
// held values that survive the wider spelling. One third does not, so the two
// rows are required to differ on it.
func TestTheTwoFloatWidthsDisagree(t *testing.T) {
	t.Parallel()

	wide := goldensOf[float64](t)
	narrow := goldensOf[float32](t)

	if wide[3] == narrow[3] {
		t.Errorf("float32 and float64 pin one third identically, as %#v", wide[3])
	}

	if got, want := narrow[3], ferry.Number(strconv.FormatFloat(1.0/3.0, 'g', -1, 32)); got != want {
		t.Errorf("float32 pins one third as %#v, want %#v", got, want)
	}
}

// TestTheFloatRowsCarryTheMandatedValues is ADR-0005's list, held to by the one
// property that separates its members: nine goldens, the two zeros apart, and
// all three specials present.
//
// It is the list rather than the count that is mandated, and the count alone
// would pass against nine ordinary numbers.
func TestTheFloatRowsCarryTheMandatedValues(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"float32", "float64"} {
		checkMandatedFloats(t, name, floatGoldens(t, name))
	}
}

// checkMandatedFloats is one float row against ADR-0005's list.
func checkMandatedFloats(t *testing.T, name string, goldens []ferry.Value) {
	t.Helper()

	const mandated = 9

	if len(goldens) != mandated {
		t.Errorf("%s carries %d cases, want ADR-0005's %d values", name, len(goldens), mandated)

		return
	}

	for _, want := range []ferry.Value{
		ferry.Number("0"), ferry.Number("-0"), ferry.Number("0.1"),
		ferry.Number("+Inf"), ferry.Number("-Inf"), ferry.Number("NaN"),
	} {
		if !slices.Contains(goldens, want) {
			t.Errorf("%s carries no %#v", name, want)
		}
	}
}

// floatGoldens is a float row's column by name, because the two rows differ by
// type parameter and this test wants them in one loop.
func floatGoldens(t *testing.T, name string) []ferry.Value {
	t.Helper()

	if name == "float32" {
		return goldensOf[float32](t)
	}

	return goldensOf[float64](t)
}

// TestTheStringRowCarriesTheMandatedValues is the other list ADR-0005 fixes: the
// empty string, an embedded NUL, non-UTF-8 bytes, and text containing a
// separator. Each is asserted by the property that puts it on the list rather
// than by its literal, so replacing one with a different value of the same shape
// stays green and dropping the shape does not.
func TestTheStringRowCarriesTheMandatedValues(t *testing.T) {
	t.Parallel()

	var seen mandatedStrings

	for _, g := range goldensOf[string](t) {
		text, err := g.AsString()
		if err != nil {
			t.Errorf("string pins %#v, which is not a String", g)

			continue
		}

		seen = seen.seeing(text)
	}

	if !seen.complete() {
		t.Errorf("string carries %+v, want all four", seen)
	}
}

// mandatedStrings is the four shapes ADR-0005 fixes for the string row, each
// recognised by the property that put it on the list rather than by its literal:
// swapping one value for another of the same shape stays green, and dropping a
// shape does not.
type mandatedStrings struct{ Empty, NUL, NonUTF8, Separator bool }

// seeing folds one of the row's values in.
func (m mandatedStrings) seeing(text string) mandatedStrings {
	return mandatedStrings{
		Empty:     m.Empty || text == "",
		NUL:       m.NUL || strings.Contains(text, "\x00"),
		NonUTF8:   m.NonUTF8 || !utf8.ValidString(text),
		Separator: m.Separator || strings.ContainsAny(text, "/#,"),
	}
}

// complete reports whether the row carried all four.
func (m mandatedStrings) complete() bool {
	return m.Empty && m.NUL && m.NonUTF8 && m.Separator
}

// TestTheIntegerRowsCarryTheZeroAndBothBounds is ADR-0005's integer list, held
// to by arithmetic rather than by restating the numbers: the zero is present,
// the bounds are the widest values the width holds, and a signed row therefore
// carries three cases where an unsigned one carries two, because an unsigned
// width's zero and lower bound are one value.
func TestTheIntegerRowsCarryTheZeroAndBothBounds(t *testing.T) {
	t.Parallel()

	for _, p := range CoreTypes() {
		if !isIntegerRow(p.Type()) {
			continue
		}

		_, _, goldens := p.columns()
		checkWidth(t, p.Name(), p.Type().Kind(), goldens)
	}
}

// checkWidth is one integer row against the width its type declares.
func checkWidth(t *testing.T, name string, k reflect.Kind, goldens []ferry.Value) {
	t.Helper()

	texts := make([]string, 0, len(goldens))

	for _, g := range goldens {
		text, err := g.AsNumber()
		if err != nil {
			t.Errorf("%s pins %#v, which is not a Number", name, g)

			return
		}

		texts = append(texts, text)
	}

	if !slices.Contains(texts, "0") {
		t.Errorf("%s carries no zero", name)
	}

	want := 2
	if signedKind(k) {
		want = 3
	}

	if len(texts) != want {
		t.Errorf("%s carries %d cases, want the zero and both bounds of the width (%d)", name, len(texts), want)
	}

	checkBounds(t, name, k, texts)
}

// signedKind separates the two integer families, which carry different numbers
// of mandated values.
func signedKind(k reflect.Kind) bool { return k >= reflect.Int && k <= reflect.Int64 }

// checkBounds asserts the extremes are the width's own, computed from the
// reflect.Kind rather than restated: a row carrying int16's bounds under the
// int32 label passes every other check in this file.
func checkBounds(t *testing.T, name string, k reflect.Kind, texts []string) {
	t.Helper()

	v := reflect.New(reflect.TypeOf(kindZero(k))).Elem()

	if signedKind(k) {
		bits := v.Type().Bits()
		lo, hi := int64(-1)<<(bits-1), int64(1)<<(bits-1)-1

		if !slices.Contains(texts, strconv.FormatInt(lo, 10)) ||
			!slices.Contains(texts, strconv.FormatInt(hi, 10)) {
			t.Errorf("%s carries %v, want both bounds of a %d-bit signed width", name, texts, bits)
		}

		return
	}

	bits := v.Type().Bits()

	hi := uint64(math.MaxUint64) >> (64 - bits)
	if !slices.Contains(texts, strconv.FormatUint(hi, 10)) {
		t.Errorf("%s carries %v, want the upper bound of a %d-bit unsigned width", name, texts, bits)
	}
}

// kindZero is the zero value of one integer kind, which is how this file gets a
// reflect.Type for a width it only holds a reflect.Kind for.
func kindZero(k reflect.Kind) any {
	widths := map[reflect.Kind]any{
		reflect.Int: int(0), reflect.Int8: int8(0), reflect.Int16: int16(0),
		reflect.Int32: int32(0), reflect.Int64: int64(0),
		reflect.Uint: uint(0), reflect.Uint8: uint8(0), reflect.Uint16: uint16(0),
		reflect.Uint32: uint32(0), reflect.Uint64: uint64(0),
	}

	return widths[k]
}

// TestThereIsNoGoldenFileAndNoUpdateFlag is the refusal ADR-0014 records, made
// executable rather than left as prose.
//
// A golden file in testdata would be the same table with a better diff, and a
// reviewer would see a representation change as a file change - which is what
// ADR-0013 wants. It is refused because a golden file grows an -update flag
// within one release, and then the change ADR-0013 exists to make visible is a
// flag. Both halves are asserted, because the flag is what the file turns into.
func TestThereIsNoGoldenFileAndNoUpdateFlag(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("testdata"); err == nil {
		t.Error("ferrytest has a testdata directory, and the golden column is refused one")
	}

	for _, name := range []string{"update", "u", "regenerate", "rewrite"} {
		if flag.Lookup(name) != nil {
			t.Errorf("the test binary registers a -%s flag, which is what a golden file grows", name)
		}
	}
}
