package ferrytest

import (
	"math"
	"time"

	"github.com/onhotpath/ferry"
)

// CoreTypes is ferry's own supported type set, discharged: nineteen rows and 57
// cases, each row carrying the equality relation its type comes back under and
// each case carrying the boundary [ferry.Value] ferry must produce for it.
//
//	ferrytest.RoundTrip(t, ferrytest.MemPlane(), ferrytest.CoreTypes())
//
// A driver author runs it through [Driver] rather than directly. A codec author
// appends their own proofs to it, which is why this is a function returning a
// fresh slice rather than a variable:
//
//	proofs := append(ferrytest.CoreTypes(), mine...)
//
// # It is a published artefact and not a test fixture
//
// The third column is what ends up in every user's config files, key-value
// stores and secret backends, and it is the only thing their stored data
// consists of. Changing one of these strings breaks that data while the Go API
// stays stable, so no tool in the Go toolchain can see it: apidiff, go vet,
// gofmt and a consumer's own round-trip test all report nothing across a release
// that moves a duration from "30s" to a nanosecond count.
//
// So a change to a row here is a major version of the module that owns it, and
// it ships with a written migration. Editing one is not editing a test.
//
// Every row carries its type's zero value, its extremes and the values that
// historically break it. For floats that is 0, -0, 0.1, 1.0/3.0, the largest and
// the smallest non-zero magnitude, both infinities and NaN; for integers the
// zero and both bounds of the width; for strings the empty string, an embedded
// NUL, non-UTF-8 bytes and text containing a separator; for composites nil and
// empty.
func CoreTypes() []Proof {
	out := make([]Proof, 0, coreRows)

	out = append(out, boolRow(), stringRow())
	out = append(out, signedRows()...)
	out = append(out, unsignedRows()...)
	out = append(out, floatRows()...)
	out = append(out, byteRows()...)
	out = append(out, identityRows()...)
	out = append(out, compositeRow())

	return out
}

// coreRows is the row count, and it is here to size one allocation rather than
// to state the decision: what the table holds is the rows below, and the count
// is asserted against them rather than trusted.
const coreRows = 19

// numZero is base-10 zero, which every integer row and both float rows carry as
// their zero case.
//
// A golden belongs beside the case it is the assertion for, and this one is
// lifted out only because it is spelled twelve times and the repetition is
// mechanical rather than meaningful: every one of them is the same statement,
// that ferry writes an integer in base 10 and a float at its own bit size, and
// that both spell zero as one character.
const numZero = "0"

// boolRow is bool, whose zero value and whose only other value are the whole
// type. ADR-0005 represents it with strconv.FormatBool, so the kind carries the
// canonical spelling and never 1 or "yes".
func boolRow() Proof {
	return Type("bool", Eq[bool],
		At(false, ferry.Bool(false)),
		At(true, ferry.Bool(true)),
	)
}

// stringRow is ADR-0005's four mandated string values.
//
// A Go string is a byte sequence and ferry writes the bytes unmodified, so it is
// not required to be UTF-8 and a NUL is not a terminator. The last case carries
// both separators ferry's own address rendering uses and the comma a flat
// encoder would join a list with, which is the value xload's string-splitting
// composites destroy: it round-trips here because a list element is an address
// rather than a substring.
func stringRow() Proof {
	return Type("string", Eq[string],
		At("", ferry.String("")),
		At("a\x00b", ferry.String("a\x00b")),
		At("\xff\xfe", ferry.String("\xff\xfe")),
		At("a/b,c#0", ferry.String("a/b,c#0")),
	)
}

// signed is the five signed integer widths, which are five rows and not one:
// a codec that silently truncates to a narrower type is caught by the bound of
// the width it truncated from and by nothing else.
type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

// unsigned is the five unsigned widths, on the same argument.
type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// signedRow is one signed width: the zero and both bounds, in base 10.
//
// The two bounds arrive with their text rather than with a formatter, because a
// golden a formatter produced would assert that the formatter agrees with
// itself.
func signedRow[T signed](name string, lo T, loText string, hi T, hiText string) Proof {
	return Type(name, Eq[T],
		At(lo, ferry.Number(loText)),
		At(T(0), ferry.Number(numZero)),
		At(hi, ferry.Number(hiText)),
	)
}

// unsignedRow is one unsigned width. It carries two cases and not three, and
// that is the mandated list rather than a shortcut: the zero and the lower bound
// of an unsigned width are one value.
func unsignedRow[T unsigned](name string, hi T, hiText string) Proof {
	return Type(name, Eq[T],
		At(T(0), ferry.Number(numZero)),
		At(hi, ferry.Number(hiText)),
	)
}

// signedRows is int and the four fixed widths. int and int64 are the same three
// numbers on this platform and are still two rows, because they are two
// reflect.Type values and the completeness check joins by type.
func signedRows() []Proof {
	return []Proof{
		signedRow[int]("int", math.MinInt, "-9223372036854775808", math.MaxInt, "9223372036854775807"),
		signedRow[int8]("int8", math.MinInt8, "-128", math.MaxInt8, "127"),
		signedRow[int16]("int16", math.MinInt16, "-32768", math.MaxInt16, "32767"),
		signedRow[int32]("int32", math.MinInt32, "-2147483648", math.MaxInt32, "2147483647"),
		signedRow[int64]("int64", math.MinInt64, "-9223372036854775808", math.MaxInt64, "9223372036854775807"),
	}
}

// unsignedRows is uint and the four fixed widths.
func unsignedRows() []Proof {
	return []Proof{
		unsignedRow[uint]("uint", math.MaxUint, "18446744073709551615"),
		unsignedRow[uint8]("uint8", math.MaxUint8, "255"),
		unsignedRow[uint16]("uint16", math.MaxUint16, "65535"),
		unsignedRow[uint32]("uint32", math.MaxUint32, "4294967295"),
		unsignedRow[uint64]("uint64", math.MaxUint64, "18446744073709551615"),
	}
}

// floatRows is the two float widths, and they are written out rather than
// generated because their goldens differ: ADR-0005 formats a float at its own
// bit size, so one third is "0.3333333333333333" as a float64 and "0.33333334"
// as a float32, and a float32 formatted at 64 bits gives 0.10000000149011612 -
// a value that re-rounds to the same float32 and is a wrong-looking config file.
func floatRows() []Proof { return []Proof{float32Row(), float64Row()} }

// float32Row is ADR-0005's nine float values at 32 bits.
//
// The relation is [BitEq] and not [Eq], because NaN == NaN is false and because
// == conflates +0 with -0 where a text boundary does not.
func float32Row() Proof {
	return Type("float32", BitEq[float32],
		At(float32(0), ferry.Number(numZero)),
		At(float32(math.Copysign(0, -1)), ferry.Number("-0")),
		At(float32(oneTenth), ferry.Number("0.1")),
		At(float32(1.0/3.0), ferry.Number("0.33333334")),
		At(float32(math.MaxFloat32), ferry.Number("3.4028235e+38")),
		At(float32(math.SmallestNonzeroFloat32), ferry.Number("1e-45")),
		At(float32(math.Inf(1)), ferry.Number(posInf)),
		At(float32(math.Inf(-1)), ferry.Number(negInf)),
		At(float32(math.NaN()), ferry.Number(notANumber)),
	)
}

// float64Row is the same nine values at 64 bits.
//
// The three specials at the end are Go's spellings and no JSON plane can hold
// any of them, which is a driver-fidelity boundary rather than a core one: a
// JSON driver refuses them rather than mangling them, and core still writes what
// the type has.
func float64Row() Proof {
	return Type("float64", BitEq[float64],
		At(float64(0), ferry.Number(numZero)),
		At(math.Copysign(0, -1), ferry.Number("-0")),
		At(oneTenth, ferry.Number("0.1")),
		At(1.0/3.0, ferry.Number("0.3333333333333333")),
		At(math.MaxFloat64, ferry.Number("1.7976931348623157e+308")),
		At(math.SmallestNonzeroFloat64, ferry.Number("5e-324")),
		At(math.Inf(1), ferry.Number(posInf)),
		At(math.Inf(-1), ferry.Number(negInf)),
		At(math.NaN(), ferry.Number(notANumber)),
	)
}

// oneTenth is the value no binary float holds exactly, and it is the first
// thing a lossy codec gets wrong. Both float rows carry it.
const oneTenth = 0.1

// The three float spellings both widths share. strconv writes them with a sign
// on the infinities and without one on NaN, and they are named here because each
// is spelled twice and neither spelling is free to differ from the other.
const (
	posInf     = "+Inf"
	negInf     = "-Inf"
	notANumber = "NaN"
)

// rawBytes is the three-byte sample both byte rows pin: a NUL, a byte no UTF-8
// sequence can contain, and an ASCII letter.
//
// It is a function rather than a package-level slice because CoreTypes is a
// published artefact, and a caller holding a mutable one could change what every
// later call proves.
func rawBytes() []byte { return []byte("\x00\xffA") }

// byteRows is []byte and [N]byte, both admitted at kind Bytes.
//
// # This is where the golden column earned its place
//
// ADR-0005 says a composite with no elements writes Null at its own address,
// whether it is nil or empty. []byte is admitted as a leaf and not as a
// composite, so that rule does not reach it: []byte(nil) writes null and
// []byte{} writes bytes(""). [SliceEq] conflates the two deliberately, because
// for a real composite they are one value; the golden column does not, and it
// reported the difference the first time this table ran. Nothing is wrong in
// either ADR - the rule is about composites and this is a leaf - and a reader
// following the prose alone gets it wrong, which is what the third column is
// for.
//
// Base64 is not ferry's business. Bytes carries the bytes and how a plane spells
// them is the driver's, which is why the golden here is the raw bytes and the
// driver suite has an artefact case of its own.
func byteRows() []Proof {
	return []Proof{
		Type("[]byte", SliceEq(Eq[byte]),
			At([]byte(nil), ferry.Null()),
			At([]byte{}, ferry.Bytes(nil)),
			At(rawBytes(), ferry.Bytes(rawBytes())),
		),
		Type("[3]byte", Eq[[3]byte],
			At([3]byte(rawBytes()), ferry.Bytes(rawBytes())),
		),
	}
}

// identityRows is the two leaves ferry owns by type identity rather than by
// kind, and the ordering is the whole rule: time.Duration's kind is int64 and
// time.Time's kind is struct, so a kind-first walk writes a nanosecond count and
// three unexported fields.
//
// time.Duration is "30s" and not 30000000000, which departs from
// encoding/json/v2 deliberately - v2 refuses a duration outright and its legacy
// option gives nanoseconds, and for a mapper whose commonest application is
// configuration, 30s is what people write.
//
// time.Time is RFC 3339 with nanoseconds, and its relation is time.Time.Equal
// rather than ==, which the standard library asks for by name because == also
// compares the monotonic reading and the *Location pointer. The relation is also
// a statement about what this table may not notice: measured, a codec that
// discards the zone entirely passes every case under .Equal, because .Equal
// compares instants. The golden is the column that would catch it.
func identityRows() []Proof {
	return []Proof{
		Type("time.Duration", Eq[time.Duration],
			At(30*time.Second, ferry.String("30s")),
		),
		Type("time.Time", time.Time.Equal,
			At(pinnedInstant(), ferry.String("2026-08-02T12:00:00.123456789Z")),
		),
	}
}

// pinnedInstant is the one time.Time the table carries: an ordinary UTC
// timestamp with a nanosecond part that has no trailing zeros, so a
// representation that dropped the fractional second, or truncated it to
// milliseconds, cannot spell the golden.
func pinnedInstant() time.Time {
	const (
		year  = 2026
		day   = 2
		hour  = 12
		nanos = 123456789
	)

	return time.Date(year, time.August, day, hour, 0, 0, nanos, time.UTC)
}

// compositeRow is the one row that is not a member of core's leaf set, and it is
// here because the leaf rows cannot state the rule it states.
//
// A composite with no elements writes Null at its own address, whether it is nil
// or empty, and both cases below carry that same golden. Three Go states meet
// two observations at a container address and the collision is forced rather
// than chosen: measured through a real plane, a missing key, an empty list and
// an empty map are one observation, and the draft that chose the other collision
// made a map key whose value minted nothing vanish entirely.
//
// The third value ADR-0005's composite list names, one containing an empty
// element, has no row here because it has no golden to carry: a composite with
// elements writes nothing at its own address, so the only [ferry.Value] this
// harness could read for it is Absent, which is a plane reporting that it does
// not hold an address rather than a representation ferry chose. It is a
// round-trip case rather than a golden one, and it belongs to whichever suite
// can read an element address.
func compositeRow() Proof {
	return Type("[]string", SliceEq(Eq[string]),
		At([]string(nil), ferry.Null()),
		At([]string{}, ferry.Null()),
	)
}
