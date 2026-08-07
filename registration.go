package ferry

import (
	"encoding"
	"reflect"
	"strconv"
	"time"
)

// This file is ADR-0017's registration surface: the constructors a caller
// extends ADR-0005's type set with, and the two opaque types they return.
//
// Three properties of it are decisions rather than implementation.
//
// A registration is named after the kind it declares, so there is no kind to
// declare. The halves are typed by payload - an encode half returns a bool, a
// string or a []byte and core wraps it - so a codec that declares one kind and
// emits another is unwritable rather than checked, and #231's KindAbsent and
// VKind(200) arguments have nowhere to be written.
//
// Key eligibility is structural: [KeyCodec.AsMapKey] is a method that exists
// only on the two constructors whose kind may key a map, so a bytes-keyed map
// does not compile rather than being refused at registration (ADR-0009's "a key
// codec says so" made a compile fact).
//
// A half that cannot work panics here, at the composition site, rather than
// being carried as an error to whatever call happens to read it. That is a
// programming error at a program's birth, in regexp.MustCompile's family, and
// the alternative is an error return on a line nobody checks (ADR-0017).

// Codec is one registration: a type, and both halves of the codec that carries
// it across the boundary.
//
// It is opaque, and the constructors in this package are the only way to build
// one: [BoolValue], [NumberValue], [StringValue] and [BytesValue] take two
// functions each, [NumberText] and [StringText] take none, [NumberKey] and
// [StringKey] are the forms that may key a map, [DurationLike] is a one-line
// spelling of one of them, and [NullValue] is a modifier over any of them.
//
// Hand it to [NewRegistry], which is the only thing that takes one.
type Codec interface {
	// entry keeps the set closed. An interface with an unexported method cannot
	// be implemented outside this package, so every Codec in existence came from
	// a constructor here and carries a kind that constructor chose (ADR-0017).
	entry() registration
}

// registration is one entry of a registry's table, and it is the concrete type
// every [Codec] is.
type registration struct {
	typ   reflect.Type
	codec leafCodec

	// key is the registrant declaring this codec's text injective over its own
	// type, which is what [KeyCodec.AsMapKey] says and what nothing else can
	// check.
	key bool
}

func (g registration) entry() registration { return g }

// KeyCodec is a registration whose kind may address a map key, which is String
// and Number and nothing else.
//
// It is a [Codec] and is handed to [NewRegistry] like any other. What it adds is
// [KeyCodec.AsMapKey], and a constructor that does not return one is a
// registration no map can be keyed by, at compile time rather than at
// registration.
type KeyCodec struct{ reg registration }

func (k KeyCodec) entry() registration { return k.reg }

// AsMapKey declares this codec's text injective over its type under Go's ==,
// which is what a map key needs.
//
//	ferry.StringText[netip.Addr]().AsMapKey()
//
// It is a claim ferry cannot check, and it is opt-in because the failure it
// prevents is silent: two keys rendering to one text are one address, so one
// entry is lost with no error anywhere and which one survives is map iteration
// order. Using a registered type as a map key without it is a schema compile
// error naming this method.
//
// The registration it returns is the one that carries the claim, and the
// receiver is unchanged. Hand the result to [NewRegistry]; calling this and
// discarding what it gives back registers a codec that keys nothing.
//
// ferrytest.Injective discharges the claim over the values a registrant cares
// about.
func (k KeyCodec) AsMapKey() Codec {
	k.reg.key = true

	return k.reg
}

// TextPointer is what [NumberText] and [StringText] take instead of two
// functions: a *T that declares both halves of the text pair, so both halves
// come from the type.
//
// A call site never writes it. PT is inferred from T, so ferry.StringText[T]()
// is the whole spelling, and this name is here because it appears in the
// compiler error a caller reads when their type does not qualify.
//
// It is written over *T rather than over T because the decode half has to write
// back. *T's method set contains T's, so a type declaring either half on a value
// receiver still satisfies this constraint, and the one that then does not work
// - UnmarshalText on a value receiver, which decodes into a copy - is refused by
// the constructor with the diagnostic that names the receiver.
type TextPointer[T any] interface {
	*T
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

// payload is how one kind's Go payload crosses core's half of the boundary:
// what core wraps a registrant's encode result into, and what core hands a
// registrant's decode half out of a plane observation.
//
// It is the whole of why a registration takes no kind argument (ADR-0017). The
// kind is a property of which of these four a constructor picked, so it is
// right by construction and there is nothing left for an emit check to compare.
type payload[P any] struct {
	kind   VKind
	wrap   func(P) Value
	unwrap func(Value) (P, error)
}

// The four payloads, one per kind a registration can name.
//
// Each unwrap admits its own kind and a String, and nothing else. That is
// ADR-0007's one coercion, applied where the payload is read rather than by
// rewriting the observation's kind first: String is the universal donor because
// String is what a plane with no types of its own says, and without it the same
// codec would work against YAML and fail against env, Consul and a query string.
var (
	boolPayload   = payload[bool]{kind: KindBool, wrap: Bool, unwrap: asBool}
	numberPayload = payload[string]{kind: KindNumber, wrap: Number, unwrap: asNumber}
	stringPayload = payload[string]{kind: KindString, wrap: String, unwrap: Value.AsString}
	bytesPayload  = payload[[]byte]{kind: KindBytes, wrap: Bytes, unwrap: asBytes}
)

// asBool reads a bool out of an observation, parsing a String rather than
// guessing at one.
//
// This is where #223's class went. The accessor used to compare the text to
// "true" and answer false for TRUE, yes, 1 and junk alike, with a nil error;
// the payload is a Go bool now, so the only text left to read is a String the
// plane spelled, and strconv.ParseBool refuses what it does not recognise
// exactly as core's own bool leaf does.
func asBool(v Value) (bool, error) {
	if v.kind != KindString {
		return v.AsBool()
	}

	return strconv.ParseBool(v.s)
}

// asNumber reads number text out of an observation, taking a String as the
// plane's own spelling of it.
func asNumber(v Value) (string, error) {
	if v.kind == KindString {
		return v.s, nil
	}

	return v.AsNumber()
}

// asBytes reads bytes out of an observation, taking a String's own bytes.
//
// How a plane spells bytes is the driver's business (ADR-0004), so a flat plane
// that reports String hands over the bytes it holds and no decoding is invented
// here.
func asBytes(v Value) ([]byte, error) {
	if v.kind == KindString {
		return []byte(v.s), nil
	}

	return v.AsBytes()
}

// BoolValue registers a type carried across the boundary as a boolean.
//
//	ferry.BoolValue(
//	    func(f Flag) (bool, error) { return f == On, nil },
//	    func(b bool) (Flag, error) { if b { return On, nil }; return Off, nil })
//
// T is inferred from either function, so no call site writes a type argument.
// The encode half returns a bool and the decode half is handed one, so the kind
// this registration writes is this constructor's and there is no way to declare
// one kind and emit another.
//
// On Load the decode half sees the plane's own bool, or the bool a plane with no
// types of its own spelled as text, which is parsed as core's bool leaf parses
// it: true and false and the spellings strconv.ParseBool takes, and a refusal
// for everything else. A null never reaches it; [NullValue] is what takes one.
//
// Both halves are required, and a nil one panics here rather than at the load
// that would have called it.
func BoolValue[T any](enc func(T) (bool, error), dec func(bool) (T, error)) Codec {
	return codecFor(boolPayload, enc, dec)
}

// NumberValue registers a type carried across the boundary as a number, spelled
// by the registrant's own text.
//
//	ferry.NumberValue(
//	    func(x big.Int) (string, error) { return x.String(), nil },
//	    func(s string) (big.Int, error) { ... })
//
// The text is the plane's spelling of a number and is never parsed into a
// machine width by ferry, which is what makes a type wider than any Go number
// expressible at all. T is inferred from either function.
//
// On Load the decode half is handed the plane's own number text, or a String's
// text where the plane has no numbers of its own. A null never reaches it.
//
// Use [NumberKey] instead where the type also keys a map.
func NumberValue[T any](enc func(T) (string, error), dec func(string) (T, error)) Codec {
	return codecFor(numberPayload, enc, dec)
}

// StringValue registers a type carried across the boundary as a string.
//
//	ferry.StringValue(
//	    func(u url.URL) (string, error) { return u.String(), nil },
//	    func(s string) (url.URL, error) {
//	        p, err := url.Parse(s)
//	        if err != nil {
//	            return url.URL{}, err
//	        }
//	        return *p, nil
//	    })
//
// T is inferred from either function. A plane's number is not donated to a
// string, which is what keeps a quoted 8080 and an unquoted one distinguishable
// across a round trip, so the decode half sees a String and nothing else. A null
// never reaches it.
//
// Check the codec against the zero value of T before writing one, because
// [NewRegistry] does: netip.Addr, netip.AddrPort and netip.Prefix all render
// their zero as "invalid IP" and cannot parse it back, so the obvious one-liner
// over String and Parse is refused for all three. Those are also the types that
// are better left to [StringText], since the text pair they already carry is
// correct.
//
// Use [StringKey] instead where the type also keys a map.
func StringValue[T any](enc func(T) (string, error), dec func(string) (T, error)) Codec {
	return codecFor(stringPayload, enc, dec)
}

// BytesValue registers a type carried across the boundary as an opaque byte
// sequence, valid UTF-8 or not.
//
//	ferry.BytesValue(
//	    func(id UUID) ([]byte, error) { return id[:], nil },
//	    func(b []byte) (UUID, error) { ... })
//
// T is inferred from either function. How a plane spells bytes - base64, hex,
// raw - is the driver's business, so this says what the value is and never how
// it is written. A null never reaches the decode half.
//
// Bytes may not key a map, which is why this constructor returns a [Codec] and
// not a [KeyCodec]: an address segment is text, and there is no spelling of
// arbitrary bytes as text that ferry gets to choose on a registrant's behalf.
func BytesValue[T any](enc func(T) ([]byte, error), dec func([]byte) (T, error)) Codec {
	return codecFor(bytesPayload, enc, dec)
}

// NumberKey registers a type carried as a number that may also key a map.
//
//	ferry.NumberKey(
//	    func(v Version) (string, error) { return strconv.Itoa(int(v)), nil },
//	    func(s string) (Version, error) { ... }).AsMapKey()
//
// It is [NumberValue] with [KeyCodec.AsMapKey] available, and the two are
// otherwise identical: a registration built here and handed to [NewRegistry]
// without that call keys nothing.
func NumberKey[T any](enc func(T) (string, error), dec func(string) (T, error)) KeyCodec {
	return KeyCodec{reg: codecFor(numberPayload, enc, dec)}
}

// StringKey registers a type carried as a string that may also key a map.
//
//	ferry.StringKey(
//	    func(r Region) (string, error) { return string(r), nil },
//	    func(s string) (Region, error) { return Region(s), nil }).AsMapKey()
//
// It is [StringValue] with [KeyCodec.AsMapKey] available, and the two are
// otherwise identical: a registration built here and handed to [NewRegistry]
// without that call keys nothing.
func StringKey[T any](enc func(T) (string, error), dec func(string) (T, error)) KeyCodec {
	return KeyCodec{reg: codecFor(stringPayload, enc, dec)}
}

// codecFor is every payload-typed constructor above, which differ only in which
// payload they name.
func codecFor[T, P any](p payload[P], enc func(T) (P, error), dec func(P) (T, error)) registration {
	mustHalves(enc, dec)

	accept := acceptVia(p, dec)

	return registration{
		typ: reflect.TypeFor[T](),
		codec: leafCodec{
			kind:   p.kind,
			encode: encodeVia(enc, p.wrap),
			// The text half is the whole-observation half over a String, so a
			// registered type under a pointer and a registered map key both reach
			// the registrant's own decode rather than a second one.
			parse:  func(v reflect.Value, text string) error { return accept(v, String(text)) },
			accept: accept,
		},
	}
}

// encodeVia is the encode half of every payload-typed constructor: the
// registrant's own function under the fence, and core wrapping what it returned.
//
// The wrap is core's, which is the whole mechanism: a registrant hands over a
// bool, a string or a []byte and never a [Value], so there is no kind for one to
// get wrong (ADR-0017, #231).
func encodeVia[T, P any](enc func(T) (P, error), wrap func(P) Value) func(reflect.Value) (Value, error) {
	return func(v reflect.Value) (Value, error) {
		out, err := fenced(enc, valueOf[T](v))
		if err != nil {
			return Value{}, encodeFailed(v, err)
		}

		return wrap(out), nil
	}
}

// acceptVia is the decode half: core reads the payload out of the observation
// and the registrant's own function is handed that and nothing else.
func acceptVia[T, P any](p payload[P], dec func(P) (T, error)) func(reflect.Value, Value) error {
	return func(v reflect.Value, got Value) error {
		in, err := p.unwrap(got)
		if err != nil {
			return parseFailed(v, err)
		}

		out, err := fenced(dec, in)
		if err != nil {
			return parseFailed(v, err)
		}

		setFrom(v, out)

		return nil
	}
}

// StringText registers a type that already declares its own text form and its
// own inverse, carried across the boundary as a string.
//
//	ferry.StringText[netip.Addr]()
//
// It takes no functions, because *T supplies both halves. Its purpose is
// declaring the boundary kind and, through [KeyCodec.AsMapKey], declaring the
// text injective: any type carrying a text pair is already claimed at
// [KindString] with nothing registered, and what a registration adds is those
// two declarations.
//
// It is one of the two constructors that name their type argument, because
// there is no value argument to infer it from. PT is inferred from T, so a call
// site writes one type argument and never two.
//
// It panics where T declares UnmarshalText on a value receiver: that method is
// in *T's method set and satisfies the constraint, and it decodes into a copy
// and leaves the field unchanged, so it is not a decode half a registration can
// use.
func StringText[T any, PT TextPointer[T]]() KeyCodec { return textCodec[T](KindString) }

// NumberText registers a type that already declares its own text form and its
// own inverse, carried across the boundary as a number.
//
//	ferry.NumberText[big.Int]()
//
// It is [StringText] at [KindNumber], and the kind is what it is for. big.Int's
// text is a run of digits, so unregistered it lands as string("1099511627776")
// and does not load from a YAML plane that reports a number; declaring a number
// loads from both, because a plane's string is donated to the declared kind on
// the way in.
//
// It panics on the value-receiver UnmarshalText [StringText] describes.
func NumberText[T any, PT TextPointer[T]]() KeyCodec { return textCodec[T](KindNumber) }

// textCodec is both text constructors, which differ only in the kind they name.
//
// PT is not a parameter here: the constraint has already done its work at the
// exported call site, and what is left is the one incompleteness *T's method set
// cannot express.
func textCodec[T any](kind VKind) KeyCodec {
	t := reflect.TypeFor[T]()

	// The constraint guarantees *T declares both halves, so the only way the pair
	// is still incomplete is the one it cannot express: an UnmarshalText on a
	// value receiver is in *T's method set and writes to a copy anyway (#131).
	if !armOf(t).complete() {
		panic(regError(t.String() + " declares UnmarshalText on a value receiver, which decodes into a copy and " +
			"leaves the field unchanged, so it is not the decode half a registration can use - move UnmarshalText " +
			"to *" + t.String() + ", or register a ferry.StringValue whose decode half returns the value"))
	}

	return KeyCodec{reg: registration{
		typ:   t,
		codec: leafCodec{kind: kind, encode: encodeTextAs(kind), parse: parseText},
	}}
}

// encodeTextAs is the text pair's encode half at a declared kind, which is the
// whole of what a text registration adds to the chain's own arm.
//
// The text is unchanged and only the kind differs, because that is what the
// distinction is: encoding.TextMarshaler produces text and says nothing about
// what kind a plane should hold it at, and the registrant is the party that
// knows.
func encodeTextAs(kind VKind) func(reflect.Value) (Value, error) {
	return func(v reflect.Value) (Value, error) {
		out, err := encodeText(v)
		if err != nil {
			return Value{}, err
		}

		return Value{kind: kind, s: out.s}, nil
	}
}

// DurationLike registers a named type over int64 with time.Duration's own
// representation, so it dumps "30s" rather than a nanosecond count.
//
//	type PollInterval time.Duration
//
//	reg := ferry.NewRegistry(ferry.DurationLike[PollInterval]())
//
// It is the remedy for a sharp edge: a named type over time.Duration is a
// distinct reflect.Type, so it misses the type ferry pins and falls through to
// kind int64. ferry cannot close that by matching on the underlying type
// instead, because that would capture every ordinary `type Port int` too.
//
// It is the other constructor with no value argument to infer T from, so T is
// named. It is a [KeyCodec] because time.Duration's text is injective over the
// type, which is why core keys a map by one; add [KeyCodec.AsMapKey] where the
// named type keys a map too.
func DurationLike[T ~int64]() KeyCodec {
	return StringKey(
		func(d T) (string, error) { return time.Duration(d).String(), nil },
		func(text string) (T, error) {
			d, err := time.ParseDuration(text)

			return T(d), err
		})
}

// NullValue grafts a null policy onto any registration: what a plane's null
// becomes, and which values write one back.
//
//	ferry.NullValue(
//	    ferry.StringValue(
//	        func(l Level) (string, error) { return string(l), nil },
//	        func(s string) (Level, error) { return Level(s), nil }),
//	    func() (Level, error) { return "", nil },
//	    func(l Level) bool { return l == "" })
//
// load is the Load policy: the T a null observation becomes. isNull is the Dump
// policy: the T values that write a null. Both are required, and it wraps any of
// the constructors above, so one modifier covers all four kinds.
//
// The law, and it is the sharp edge: isNull(load()) must hold. A policy that
// loads a sentinel it cannot recognise on the way back makes the round trip lie,
// silently, and only on the null path. ferrytest.Codec checks it.
//
// It is for the case where null is wanted as a value of T. A *T with no policy
// at all already writes a null for a nil pointer and loads one back, through
// ferry's own rules and no codec, and that is what keeps null and the zero
// distinct; this merges them by design, which is exactly its contract.
//
// So pick one or the other, because a policy under a pointer is neither. At a
// *T field the pointer's own null wins in both directions: a nil pointer writes
// a null whatever isNull says, and a null loads as a nil pointer without running
// load. A policied T dumped through a *T field therefore comes back nil.
//
// It panics where inner is nil, where either policy is nil, where inner is not a
// registration for T, and where inner declared itself usable as a map key: a key
// becomes the segment text of an address and never crosses the boundary as a
// value, so it has no null to carry and two null-ish keys would render to one
// empty segment.
func NullValue[T any](inner Codec, load func() (T, error), isNull func(T) bool) Codec {
	mustHalves(load, isNull)

	g := mustInner[T](inner)
	base := g.codec

	// The text half stays the inner codec's: a null is a kind and never a
	// spelling, so it does not arrive as the address text of a map key nor as a
	// declared default.
	g.codec.encode = nullOrEncode(base.encode, isNull)
	g.codec.accept = loadOrDecode(base.decode, load)

	return g
}

// nullOrEncode is [NullValue]'s dump policy in front of the inner encode half.
func nullOrEncode[T any](
	encode func(reflect.Value) (Value, error),
	isNull func(T) bool,
) func(reflect.Value) (Value, error) {
	return func(v reflect.Value) (Value, error) {
		yes, err := fencedIsNull(isNull, valueOf[T](v))
		if err != nil {
			return Value{}, encodeFailed(v, err)
		}

		if yes {
			return nullValue, nil
		}

		return encode(v)
	}
}

// loadOrDecode is [NullValue]'s load policy in front of the inner decode half.
func loadOrDecode[T any](
	decode func(reflect.Value, Value) error,
	load func() (T, error),
) func(reflect.Value, Value) error {
	return func(v reflect.Value, got Value) error {
		if got.kind != KindNull {
			return decode(v, got)
		}

		out, err := fencedLoad(load)
		if err != nil {
			return parseFailed(v, err)
		}

		setFrom(v, out)

		return nil
	}
}

// mustInner holds [NullValue]'s inner registration to what a null policy can be
// grafted onto at all, at the composition site.
//
// The type mismatch is reachable because T is inferred from load and isNull
// rather than from inner, so ferry.NullValue(stringCodecForA, loadB, isNullB)
// compiles and would otherwise fail as a type assertion at the first load.
//
// The key refusal is the one that would corrupt a plane rather than fail. A key
// never crosses the boundary as a Value: it becomes the segment text of an
// address, which [registeredKey] reads off whatever the encode half produced.
// Under a policy that half returns Null for a null-ish key, and a Null's text is
// empty, so every null-ish key in a map addresses the container's own empty
// segment. Two of them are one address, one entry is lost, and nothing reports
// it - which is exactly the silent failure .AsMapKey() exists to make a
// registrant think about. A null is about a kind and a key has no kind, so the
// two are structurally incompatible and this is a refusal rather than a rule
// (ADR-0017, ADR-0009).
func mustInner[T any](inner Codec) registration {
	if inner == nil {
		panic(regError("ferry.NullValue was given no registration to wrap: it is a modifier over one of the " +
			"kind-named constructors, which is where the codec it adds a null policy to comes from"))
	}

	g := inner.entry()

	if want := reflect.TypeFor[T](); g.typ != want {
		panic(regError("ferry.NullValue was given a registration for " + g.typ.String() + " and a null policy " +
			"over " + want.String() + ": one registration covers one type, and a policy for another type would " +
			"be read against values it was never written for"))
	}

	if g.key {
		panic(regError(g.typ.String() + ": a null policy may not be grafted onto a registration declared usable " +
			"as a map key, because a key becomes the segment text of an address and never crosses the boundary " +
			"as a value, so it has no null to carry - two null-ish keys would render to one empty segment and " +
			"one entry would be lost with no error anywhere. Declare the key without the policy, or wrap a " +
			"registration that is not a key"))
	}

	return g
}

// mustHalves is the nil-half refusal, and it is a panic at the composition site
// rather than an error (ADR-0017).
//
// A nil half is a programming error at a program's birth, in the family of
// regexp.MustCompile, and the alternative is an error return on a line nobody
// checks. It is written over any rather than over the two function types because
// every constructor here has a different pair, and a typed nil function is only
// visible through reflect.
func mustHalves(enc, dec any) {
	if isNilFunc(enc) || isNilFunc(dec) {
		panic(regError("a registration takes both halves and one of them is nil: a codec ferry can only run in " +
			"one direction would fail at whichever of Load and Dump reached the missing half first"))
	}
}

// valueOf reads a T back out of the reflect.Value the walk holds, in the
// comma-ok form.
//
// The comma is one token wide and it is the difference between working and
// panicking at an interface field: v.Interface() on a nil interface is a nil
// any, and the single-result assertion panics on it - inside ferry, on third
// party code, at Dump. Measured, the comma-ok form is not slower than the
// panicking one (ADR-0009).
func valueOf[T any](v reflect.Value) T {
	t, _ := v.Interface().(T)

	return t
}

// setFrom writes a T back into the field the walk is holding.
//
// It goes through a fresh pointer rather than reflect.ValueOf(out) for the
// mirror image of the reason above: reflect.ValueOf of a nil interface is the
// zero reflect.Value and Set panics on it, where &out yields a Value of static
// type T whatever the dynamic value turns out to be.
func setFrom[T any](v reflect.Value, out T) {
	v.Set(reflect.ValueOf(&out).Elem())
}

// isNilFunc reports a nil function value, whether it arrived as a nil interface
// or as a typed nil.
//
// Every caller reaches it from a generic parameter, so what it is handed is
// always a typed nil and the nil-interface arm is the defensive one. It is one
// expression rather than an early return because the defensive arm has no
// behaviour to reach it through, and a branch nothing can take is a branch
// nothing can prove.
func isNilFunc(f any) bool {
	v := reflect.ValueOf(f)

	return !v.IsValid() || (v.Kind() == reflect.Func && v.IsNil())
}
