package ferry

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	faketime "github.com/onhotpath/ferry/internal/testdata/time"
)

// Every assertion in this file goes through Compile, Load and Dump. The type
// set is a rule about what a plane is handed and what a field is given back, so
// it is asserted as what a recording plane was asked and what came out the
// other side, and never by calling a codec.
//
// The one exception is the identity table's shape, which is read out of the
// source with go/parser. "It contains no strings" is a statement about how the
// table is written rather than about any value it produces, and the behavioural
// half of it - that a type in another package called time is not matched - is
// asserted through the seam like everything else.

// leafHolder is the struct one leaf value travels in, because ADR-0010 refuses
// a root that compiles to a leaf: the empty path is not an address.
type leafHolder[T any] struct {
	V T `ferry:"v"`
}

// leafAddr is where a leafHolder's one field lands.
var leafAddr = At("v")

// dumped is what one Dump wrote, read off a recording plane.
func dumped[T any](t *testing.T, v T) Value {
	t.Helper()

	p := newPlane(map[Path]Value{})
	if err := Dump(context.Background(), v, planeSink{p}); err != nil {
		t.Fatalf("dump %T: %v", v, err)
	}

	return p.values[leafAddr]
}

// readLeaf presents one observation at a leaf of type T and renders what
// landed, so a table of inputs can assert the exact answer rather than only
// that the load succeeded.
func readLeaf[T any](got Value) (string, error) {
	src := planeSource{newPlane(map[Path]Value{leafAddr: got})}

	out, err := Load[leafHolder[T]](context.Background(), src)

	return fmt.Sprint(out.V), err
}

// TestIdentityIsConsultedBeforeKind is ADR-0005's ordering rule, and the
// ordering is the whole rule.
//
// time.Duration's kind is int64 and time.Time's kind is struct, so a kind-first
// resolution writes a nanosecond count for one and refuses the other outright
// for mapping no address - its three fields are all unexported.
func TestIdentityIsConsultedBeforeKind(t *testing.T) {
	t.Parallel()

	if got := dumped(t, leafHolder[time.Duration]{V: 30 * time.Second}); got != String("30s") {
		t.Errorf("time.Duration writes %#v, want %#v: it fell to kind int64", got, String("30s"))
	}

	if err := Compile[leafHolder[time.Time]](); err != nil {
		t.Errorf("time.Time does not compile, so it fell to kind struct and mapped no address: %v", err)
	}

	want := String("2026-08-02T12:00:00.123456789Z")
	if got := dumped(t, leafHolder[time.Time]{V: pinnedTime()}); got != want {
		t.Errorf("time.Time writes %#v, want %#v", got, want)
	}
}

// pinnedTime is an ordinary UTC timestamp whose nanosecond part has no trailing
// zeros, so a representation that dropped the fractional second cannot spell
// the text it is asserted against.
func pinnedTime() time.Time {
	return time.Date(2026, time.August, 2, 12, 0, 0, 123456789, time.UTC)
}

// TestNoTypeIsMatchedByName is the other half of the identity rule, and it is
// the defect the rule exists to fix.
//
// xload identifies time.Duration by comparing Type.String() to "time.Duration",
// which matches any type in any package called time whose name is Duration. The
// fixture under internal/testdata is exactly that: two types whose renderings
// are byte-identical to the standard library's and whose reflect.Type values
// differ from them under ==.
func TestNoTypeIsMatchedByName(t *testing.T) {
	t.Parallel()

	for _, want := range []struct{ got, name string }{
		{reflect.TypeFor[faketime.Duration]().String(), "time.Duration"},
		{reflect.TypeFor[faketime.Time]().String(), "time.Time"},
	} {
		if want.got != want.name {
			t.Fatalf("the fixture renders as %s, so it is not the collision this test is about", want.got)
		}
	}

	got := dumped(t, leafHolder[faketime.Duration]{V: faketime.Duration(30 * time.Second)})
	if got != Number("30000000000") {
		t.Errorf("a Duration in another package called time writes %#v, so a type was matched by name", got)
	}

	// The fixture carries a text pair of its own, so ADR-0007's chain claims it
	// at step 2 and writes the fixture's own constant. What a table matched by
	// name would have written is time.Time's RFC 3339 of a value this type does
	// not hold, through a Set of one struct type into another.
	if got := dumped(t, leafHolder[faketime.Time]{}); got != String("2026-08-02T12:00:00Z") {
		t.Errorf("a Time in another package called time writes %#v, so a type was matched by name", got)
	}
}

// TestTheIdentityTableIsKeyedByTypeAndHoldsNoStrings reads the table out of the
// source, because "it contains no strings" is a statement about how the table
// is written and no value it produces can be asked about it.
func TestTheIdentityTableIsKeyedByTypeAndHoldsNoStrings(t *testing.T) {
	t.Parallel()

	lit := identityTable(t)

	ast.Inspect(lit, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING {
			t.Errorf("the identity table holds the string %s, and it is keyed by type identity", b.Value)
		}

		return true
	})

	if len(lit.Elts) == 0 {
		t.Fatal("the identity table is empty")
	}

	for _, elt := range lit.Elts {
		checkTypeForKey(t, elt)
	}
}

// identityTable finds byIdentity's composite literal in types.go.
func identityTable(t *testing.T) *ast.CompositeLit {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse types.go: %v", err)
	}

	var lit *ast.CompositeLit

	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if ok && len(spec.Names) == 1 && spec.Names[0].Name == "byIdentity" {
			lit, _ = spec.Values[0].(*ast.CompositeLit)
		}

		return lit == nil
	})

	if lit == nil {
		t.Fatal("types.go declares no byIdentity composite literal")
	}

	return lit
}

// checkTypeForKey holds one entry's key to being a reflect.TypeFor call, which
// is what makes the comparison == over a reflect.Type rather than over anything
// a name could stand in for.
func checkTypeForKey(t *testing.T, elt ast.Expr) {
	t.Helper()

	kv, ok := elt.(*ast.KeyValueExpr)
	if !ok {
		t.Errorf("the identity table holds an entry with no key, %T", elt)

		return
	}

	call, ok := kv.Key.(*ast.CallExpr)
	if !ok {
		t.Errorf("the identity table is keyed by a %T rather than by reflect.TypeFor", kv.Key)

		return
	}

	if name := typeForName(call.Fun); name != "reflect.TypeFor" {
		t.Errorf("the identity table is keyed by %s, want reflect.TypeFor", name)
	}
}

// typeForName renders the callee of a generic call such as reflect.TypeFor[T],
// which the parser gives as an index over a selector.
func typeForName(fun ast.Expr) string {
	idx, ok := fun.(*ast.IndexExpr)
	if !ok {
		return fmt.Sprintf("%T", fun)
	}

	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok {
		return fmt.Sprintf("%T", idx.X)
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return fmt.Sprintf("%T", sel.X)
	}

	return pkg.Name + "." + sel.Sel.Name
}

// obs is one observation a plane could make at a leaf's address, and whether
// the leaf takes it.
type obs struct {
	got   Value
	taken bool
}

// donor is one leaf type against every kind a plane can report.
//
// The name is ADR-0005's: String is the universal donor, because String is what
// a plane says when it has nothing to say. Number, Bool and Bytes are
// assertions, and a plane that makes one is respected rather than
// second-guessed.
type donor struct {
	name string
	read func(Value) (string, error)
	all  []obs
}

// donors is every leaf shape core ships, against all five kinds a plane can
// report at an address it holds.
//
// The integer widths are one row rather than ten, because what differs between
// them is the bound rather than the kind set, and the bounds are asserted in
// their own table below.
func donors() []donor {
	return slices.Concat(scalarDonors(), byteDonors(), identityDonors())
}

// scalarDonors is bool, string and the numeric kinds.
func scalarDonors() []donor {
	return []donor{
		{"bool", readLeaf[bool], []obs{
			{Bool(true), true}, {String("true"), true},
			{Number("1"), false}, {Bytes([]byte("true")), false}, {Null, false},
		}},
		{"string", readLeaf[string], []obs{
			{String("8080"), true},
			{Number("8080"), false}, {Bool(true), false}, {Bytes([]byte("8080")), false}, {Null, false},
		}},
		{"int", readLeaf[int], []obs{
			{Number("8080"), true}, {String("8080"), true},
			{Bool(true), false}, {Bytes([]byte("8080")), false}, {Null, false},
		}},
		{"uint64", readLeaf[uint64], []obs{
			{Number("8080"), true}, {String("8080"), true},
			{Bool(true), false}, {Bytes([]byte("8080")), false}, {Null, false},
		}},
		{"float64", readLeaf[float64], []obs{
			{Number("0.1"), true}, {String("0.1"), true},
			{Bool(true), false}, {Bytes([]byte("0.1")), false}, {Null, false},
		}},
	}
}

// byteDonors is []byte and [N]byte, and the two differ on exactly one kind:
// ADR-0006 gives a nil slice a null and says an array has none.
func byteDonors() []donor {
	return []donor{
		{"[]byte", readLeaf[[]byte], []obs{
			{Bytes([]byte("ab")), true}, {String("ab"), true}, {Null, true},
			{Bool(true), false}, {Number("1"), false},
		}},
		{"[3]byte", readLeaf[[3]byte], []obs{
			{Bytes([]byte("abc")), true}, {String("abc"), true},
			{Null, false}, {Bool(true), false}, {Number("1"), false},
		}},
	}
}

// identityDonors is the two leaves ferry owns by type identity. Both produce
// String, so String is their own kind and the donor rule adds nothing to
// either: a Number is a wrong kind at a duration exactly as it is at a string.
func identityDonors() []donor {
	return []donor{
		{"time.Duration", readLeaf[time.Duration], []obs{
			{String("30s"), true},
			{Number("30000000000"), false}, {Bool(true), false},
			{Bytes([]byte("30s")), false}, {Null, false},
		}},
		{"time.Time", readLeaf[time.Time], []obs{
			{String("2026-08-02T12:00:00Z"), true},
			{Number("1785931200"), false}, {Bool(true), false},
			{Bytes([]byte("2026-08-02T12:00:00Z")), false}, {Null, false},
		}},
	}
}

// TestEveryLeafTakesItsOwnKindAndStringAndNothingElse is ADR-0005's donor rule
// and ADR-0006's null rule, asserted together because they are one question at
// a leaf: which observations does this type accept.
func TestEveryLeafTakesItsOwnKindAndStringAndNothingElse(t *testing.T) {
	t.Parallel()

	for _, d := range donors() {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			d.checkEveryKindIsAnswered(t)

			for _, o := range d.all {
				d.check(t, o)
			}
		})
	}
}

// checkEveryKindIsAnswered stops a row saying nothing about a kind, which is
// how "and nothing else" quietly stops being asserted.
func (d donor) checkEveryKindIsAnswered(t *testing.T) {
	t.Helper()

	seen := map[VKind]bool{}
	for _, o := range d.all {
		seen[o.got.Kind()] = true
	}

	for _, k := range []VKind{KindNull, KindBool, KindNumber, KindString, KindBytes} {
		if !seen[k] {
			t.Errorf("%s says nothing about %s", d.name, k)
		}
	}
}

// check is one observation at one leaf.
func (d donor) check(t *testing.T, o obs) {
	t.Helper()

	_, err := d.read(o.got)

	if o.taken {
		if err != nil {
			t.Errorf("%s does not take %#v: %v", d.name, o.got, err)
		}

		return
	}

	if err == nil {
		t.Errorf("%s took %#v, and its own kind and string are the whole of what it takes", d.name, o.got)

		return
	}

	if !errors.Is(err, ErrWrongKind) || !errors.Is(err, ErrValue) {
		t.Errorf("%s refused %#v with %v, want a wrong-kind ErrValue", d.name, o.got, err)
	}
}

// castCase is one input all seven share: the text a plane holds as a String,
// what ferry does with it, and what spf13/cast - which xload depends on - does
// with it.
type castCase struct {
	name    string
	text    string
	read    func(Value) (string, error)
	want    string
	refused bool
	cast    string
}

// TestTheParserIsTheLeafsOwnAndNotCasts is the seven inputs side by side,
// because the contrast is the point and a table split in two hides it.
//
// The parser being the leaf's own is what separates ferry's one coercion from a
// conversion library. Every row below is a refusal or an exact answer, and none
// is a guess.
func TestTheParserIsTheLeafsOwnAndNotCasts(t *testing.T) {
	t.Parallel()

	for _, c := range castCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.check(t)
		})
	}
}

func castCases() []castCase {
	return []castCase{
		{
			name: `"0080" into an int`, text: "0080", read: readLeaf[int], want: "80",
			cast: "0, invalid octal with the error swallowed",
		},
		{name: `"010" into an int`, text: "010", read: readLeaf[int], want: "10", cast: "8, base 0 reads it as octal"},
		{name: `"0x10" into an int`, text: "0x10", read: readLeaf[int], refused: true, cast: "16"},
		{name: `"1.9" into an int`, text: "1.9", read: readLeaf[int], refused: true, cast: "1, truncated"},
		{
			name: `"" into an int`, text: "", read: readLeaf[int], refused: true,
			cast: "0, indistinguishable from a real zero",
		},
		{name: `"yes" into a bool`, text: "yes", read: readLeaf[bool], refused: true, cast: "false, silently"},
		{
			name: `"30" into a time.Duration`, text: "30", read: readLeaf[time.Duration], refused: true,
			cast: "30ns",
		},
	}
}

func (c castCase) check(t *testing.T) {
	t.Helper()

	got, err := c.read(String(c.text))

	if c.refused {
		if err == nil {
			t.Errorf("%q loaded as %s, and cast makes it %s: this one is a refusal", c.text, got, c.cast)
		}

		return
	}

	if err != nil {
		t.Errorf("%q was refused (%v), and the exact answer is %s where cast makes it %s", c.text, err, c.want, c.cast)

		return
	}

	if got != c.want {
		t.Errorf("%q loaded as %s, want %s, where cast makes it %s", c.text, got, c.want, c.cast)
	}
}

// TestAnOutOfRangeIntegerIsAnErrorAndNeverATruncation is the koanf defect the
// survey measured, where Int64() turns 18446744073709551615 into MaxInt64 with
// a nil error.
//
// The parse runs at the width's own bit size rather than at 64, so the bound
// that refuses is the field's and not int64's.
func TestAnOutOfRangeIntegerIsAnErrorAndNeverATruncation(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		read func(Value) (string, error)
		text string
	}{
		{"int8 above its bound", readLeaf[int8], "128"},
		{"int8 below its bound", readLeaf[int8], "-129"},
		{"uint8 above its bound", readLeaf[uint8], "256"},
		{"uint16 above its bound", readLeaf[uint16], "65536"},
		{"int32 given int64's bound", readLeaf[int32], "9223372036854775807"},
		{"int64 given uint64's bound", readLeaf[int64], "18446744073709551615"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkOutOfRange(t, c.read, c.text)
		})
	}
}

// checkOutOfRange holds one width to reporting a range failure rather than a
// value.
func checkOutOfRange(t *testing.T, read func(Value) (string, error), text string) {
	t.Helper()

	got, err := read(Number(text))
	if err == nil {
		t.Fatalf("%s loaded as %s, and it does not fit", text, got)
	}

	if !errors.Is(err, strconv.ErrRange) || !errors.Is(err, ErrValue) {
		t.Errorf("%s was refused with %v, want a range failure classed ErrValue", text, err)
	}

	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("%s was refused with %v, which does not say the value is out of range", text, err)
	}

	if strings.Contains(err.Error(), text) {
		t.Errorf("the refusal repeats the plane's own text: %v", err)
	}
}

// TestALeafThatDoesNotParseIsLoud is the donor rule stated from the other
// side: accepting String everywhere buys env, query and Consul, and it buys
// them without buying a guess, because what a leaf cannot parse it refuses.
func TestALeafThatDoesNotParseIsLoud(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		read func(Value) (string, error)
		got  Value
	}{
		{"int8 given letters", readLeaf[int8], Number("abc")},
		{"int8 given nothing", readLeaf[int8], Number("")},
		{"float64 given letters", readLeaf[float64], Number("abc")},
		{"float32 given nothing", readLeaf[float32], String("")},
		{"time.Time given a Go rendering", readLeaf[time.Time], String("2026-08-02 12:00:00 +0000 UTC")},
		{"[3]byte given two bytes", readLeaf[[3]byte], Bytes([]byte("ab"))},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkRefused(t, c.read, c.got)
		})
	}
}

// checkRefused holds one observation to being refused as an ErrValue whose text
// repeats nothing the plane supplied.
func checkRefused(t *testing.T, read func(Value) (string, error), got Value) {
	t.Helper()

	out, err := read(got)
	if err == nil {
		t.Fatalf("%#v loaded as %s", got, out)
	}

	if !errors.Is(err, ErrValue) {
		t.Errorf("%#v was refused with %v, want an ErrValue", got, err)
	}

	if got.text() != "" && strings.Contains(err.Error(), got.text()) {
		t.Errorf("the refusal repeats the plane's own text: %v", err)
	}
}

// TestATimeWithNoTextFormIsReportedRatherThanSwallowed is the one value in
// core's set whose representation is partial over its type: MarshalText refuses
// a year outside [0,9999], and a dump that swallowed it would write nothing and
// report success.
func TestATimeWithNoTextFormIsReportedRatherThanSwallowed(t *testing.T) {
	t.Parallel()

	p := newPlane(map[Path]Value{})
	when := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)

	err := Dump(context.Background(), leafHolder[time.Time]{V: when}, planeSink{p})
	if err == nil {
		t.Fatalf("a year outside RFC 3339 dumped clean, as %#v", p.values[leafAddr])
	}

	if !errors.Is(err, ErrValue) || !strings.Contains(err.Error(), "time.Time") {
		t.Errorf("it was refused with %v, want an ErrValue naming time.Time", err)
	}

	if _, wrote := p.values[leafAddr]; wrote {
		t.Errorf("it wrote %#v at %s anyway", p.values[leafAddr], leafAddr)
	}
}

// TestFloatsFormatAtTheirOwnBitSize is the pinning that cannot be checked by
// eye, and it is where the two widths have to disagree.
//
// A float32 formatted at 64 bits gives 0.10000000149011612, which re-rounds to
// the same float32 and is a wrong-looking config file.
func TestFloatsFormatAtTheirOwnBitSize(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		got  Value
		want Value
	}{
		{"float32 one tenth", dumped(t, leafHolder[float32]{V: 0.1}), Number("0.1")},
		{"float64 one tenth", dumped(t, leafHolder[float64]{V: 0.1}), Number("0.1")},
		{"float32 one third", dumped(t, leafHolder[float32]{V: 1.0 / 3.0}), Number("0.33333334")},
		{"float64 one third", dumped(t, leafHolder[float64]{V: 1.0 / 3.0}), Number("0.3333333333333333")},
	} {
		if c.got != c.want {
			t.Errorf("%s writes %#v, want %#v", c.name, c.got, c.want)
		}
	}
}

// TestTheFourFloatSpecialsRoundTripBitExactly is the values == cannot compare
// at all: NaN == NaN is false, and == conflates +0 with -0 where a text
// boundary does not.
func TestTheFourFloatSpecialsRoundTripBitExactly(t *testing.T) {
	t.Parallel()

	for _, f := range []float64{math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.NaN()} {
		checkBitExact(t, f)
		checkBitExact(t, float32(f))
	}
}

// checkBitExact dumps one float and loads it back, comparing bit patterns.
//
// A float32 widens to float64 for the comparison, which is exact for every
// float32 value, the zeros and the infinities included.
func checkBitExact[T ~float32 | ~float64](t *testing.T, f T) {
	t.Helper()

	p := newPlane(map[Path]Value{leafAddr: dumped(t, leafHolder[T]{V: f})})

	back, err := Load[leafHolder[T]](context.Background(), planeSource{p})
	if err != nil {
		t.Fatalf("%T(%v) does not round trip: %v", f, f, err)
	}

	if math.Float64bits(float64(back.V)) != math.Float64bits(float64(f)) {
		t.Errorf("%T(%v) round trips to %v, which is a different bit pattern", f, f, back.V)
	}
}

// stringerOnly declares String() string and nothing else, which is the whole
// reason fmt.Stringer is not a route into the type set: the method declares no
// inverse, and a round-trip guarantee cannot rest on an interface that does not
// promise one.
type stringerOnly struct{ n int }

func (s stringerOnly) String() string { return strconv.Itoa(s.n) }

var _ fmt.Stringer = stringerOnly{}

// TestFmtStringerIsNeverConsulted is asserted from both sides, because the two
// halves fail differently.
//
// time.Time implements fmt.Stringer and the text pair both, and only one of them
// round-trips: String() gives "2026-08-02 12:00:00 +0000 UTC", which RFC 3339
// cannot parse, and with a monotonic reading present it also writes a trailing
// "m=+0.000240763" - process-local state in a config file. So precedence alone
// would decide correctness, and the text pair is what ferry uses.
func TestFmtStringerIsNeverConsulted(t *testing.T) {
	t.Parallel()

	when := pinnedTime()

	got := dumped(t, leafHolder[time.Time]{V: when})
	if got == String(when.String()) {
		t.Errorf("time.Time writes what fmt.Stringer gives, %#v", got)
	}

	if _, err := time.Parse(time.RFC3339Nano, got.text()); err != nil {
		t.Errorf("time.Time writes %#v, which is not the text pair's RFC 3339: %v", got, err)
	}

	err := Compile[leafHolder[stringerOnly]]()
	if err == nil {
		t.Fatal("a type whose only method is String() compiled as a leaf, so fmt.Stringer admitted it")
	}

	if !errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), "stringerOnly") {
		t.Errorf("a Stringer-only type is refused with %v, want a schema error naming the type", err)
	}
}

// namedPort is a named type over an admitted kind, which is the main thing kind
// admission buys: it round-trips with nothing registered.
type namedPort int

// namedTimeout is a named type over time.Duration, and it is the documented
// sharp edge rather than a defect.
//
// It is a distinct reflect.Type, so it misses the identity table, falls to kind
// int64 and writes a nanosecond count. Closing it would require matching on the
// underlying type, which would then also capture every ordinary namedPort, so
// the answer is a registered codec and that is a later ticket's.
type namedTimeout time.Duration

// TestANamedTypeOverAnAdmittedKindWorks is the free half of kind admission.
func TestANamedTypeOverAnAdmittedKindWorks(t *testing.T) {
	t.Parallel()

	if got := dumped(t, leafHolder[namedPort]{V: 8080}); got != Number("8080") {
		t.Errorf("a named int writes %#v, want %#v", got, Number("8080"))
	}

	back, err := readLeaf[namedPort](Number("8080"))
	if err != nil {
		t.Fatalf("a named int does not load: %v", err)
	}

	if back != "8080" {
		t.Errorf("a named int loads as %s, want 8080", back)
	}
}

// TestANamedTypeOverTimeDurationDumpsNanoseconds pins the sharp edge so that
// closing it is a change somebody makes on purpose.
func TestANamedTypeOverTimeDurationDumpsNanoseconds(t *testing.T) {
	t.Parallel()

	got := dumped(t, leafHolder[namedTimeout]{V: namedTimeout(30 * time.Second)})
	if got != Number("30000000000") {
		t.Errorf("a named time.Duration writes %#v, want the nanosecond count its kind gives", got)
	}
}

// refusedKinds holds every kind ADR-0005 refuses outright: four permanently,
// because the value does not exist outside the process, and two by policy.
type refusedKinds struct {
	C64  complex64      `ferry:"c64"`
	C128 complex128     `ferry:"c128"`
	Ch   chan int       `ferry:"ch"`
	Fn   func()         `ferry:"fn"`
	Ptr  unsafe.Pointer `ferry:"ptr"`
	Up   uintptr        `ferry:"up"`
}

// TestEveryRefusedKindIsCollectedAndSorted is ADR-0001's determinism invariant
// applied to the refusal list: every violation in a type is reported rather
// than the first one, each naming the address and the type, in one order.
func TestEveryRefusedKindIsCollectedAndSorted(t *testing.T) {
	t.Parallel()

	err := Compile[refusedKinds]()
	if err == nil {
		t.Fatal("a struct of six refused kinds compiled clean")
	}

	elems := Elements(err)
	if len(elems) != 6 {
		t.Fatalf("the report holds %d refusals, want one per field:\n%+v", len(elems), err)
	}

	report := fmt.Sprintf("%+v", err)
	for _, want := range []string{
		"/c64: complex64", "/c128: complex128", "/ch: chan int",
		"/fn: func()", "/ptr: unsafe.Pointer", "/up: uintptr",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not name %q:\n%s", want, report)
		}
	}

	checkSortedByAddress(t, elems)
}

// checkSortedByAddress holds the report to one order, which is what makes a
// diff of two runs mean something.
func checkSortedByAddress(t *testing.T, elems []error) {
	t.Helper()

	addrs := make([]Path, 0, len(elems))

	for _, e := range elems {
		var fe *Error
		if errors.As(e, &fe) {
			addrs = append(addrs, fe.Address())
		}
	}

	if len(addrs) != len(elems) {
		t.Fatalf("%d of %d refusals carry no address", len(elems)-len(addrs), len(elems))
	}

	if !slices.IsSortedFunc(addrs, Path.Compare) {
		t.Errorf("the refusals arrive as %v, which is not sorted", addrs)
	}
}

// TestThePermanentRefusalsOfferNoCodec is the half of the refusal list that a
// single message cannot say.
//
// ADR-0005 sorts the refusals by what actually limits each: a chan's identity
// is a pointer into this process's heap and a func type is not even comparable,
// so nothing text could carry rebuilds either, and offering registration as the
// remedy would be naming a remedy that does not exist. complex is refused by
// policy instead - strconv.FormatComplex and ParseComplex are a total inverse
// pair - so there registration is exactly the answer.
func TestThePermanentRefusalsOfferNoCodec(t *testing.T) {
	t.Parallel()

	report := fmt.Sprintf("%+v", Compile[refusedKinds]())

	for _, line := range strings.Split(report, "\n") {
		checkRemedy(t, line)
	}
}

// checkRemedy holds one refusal line to the remedy its category has.
func checkRemedy(t *testing.T, line string) {
	t.Helper()

	offersCodec := strings.Contains(line, "register a codec")

	switch {
	case strings.Contains(line, "chan int"), strings.Contains(line, "func()"),
		strings.Contains(line, "unsafe.Pointer"), strings.Contains(line, "uintptr"):
		if offersCodec {
			t.Errorf("a permanent refusal offers a codec that cannot be written: %s", line)
		}
	case strings.Contains(line, "complex"):
		if !offersCodec {
			t.Errorf("a refusal by policy offers no way out: %s", line)
		}
	default:
		// The header and the moment lines of a %+v report, which name no type.
	}
}

// TestOmitzeroAndTheZeroDefaultAgreeAtEveryLeaf is the contradiction check
// following the type set out.
//
// A default equal to the zero value is not a contradiction, because omitting it
// and reapplying it land on the same value. Which text that is was the empty
// string while the compiler admitted string leaves alone, and is the leaf's own
// question now: "0" is int's zero, "false" is bool's and "0s" is
// time.Duration's.
func TestOmitzeroAndTheZeroDefaultAgreeAtEveryLeaf(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		compile func(...Option) error
		clean   bool
	}{
		{"int at its zero", Compile[zeroInt], true},
		{"int away from it", Compile[nonZeroInt], false},
		{"bool at its zero", Compile[zeroBool], true},
		{"time.Duration at its zero", Compile[zeroDuration], true},
		// A default that does not decode at all is a different mistake, and it
		// is reported as that one rather than as this one: it has no value to
		// compare against zero, so naming the contradiction for it would name
		// the wrong mistake. TestDeclaredDefaultsMustParse holds the text.
		{"int given a default that does not decode", Compile[unparseableInt], false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.compile(); (err == nil) != c.clean {
				t.Errorf("compiled with %v, want clean=%v", err, c.clean)
			}
		})
	}
}

type zeroInt struct {
	V int `ferry:"v,omitzero,default=0"`
}

type nonZeroInt struct {
	V int `ferry:"v,omitzero,default=8080"`
}

type zeroBool struct {
	V bool `ferry:"v,omitzero,default=false"`
}

type zeroDuration struct {
	V time.Duration `ferry:"v,omitzero,default=0s"`
}

type unparseableInt struct {
	V int `ferry:"v,omitzero,default=abc"`
}
