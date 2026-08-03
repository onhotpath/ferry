package main

// The candidate type set, and the walk that realises it.
//
// The load-bearing rule under test: a type is resolved by *identity* first
// (a table keyed by reflect.Type), and only then by reflect.Kind. That is the
// route ADR-0002 leaves open after ruling out xload's Type.String()=="time.Duration"
// (5.9), and it is what lets time.Duration and time.Time be owned by ferry
// without a string comparison anywhere.

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// --- the identity table -----------------------------------------------------

type leafCodec struct {
	name string
	// kind is the boundary Value kind this codec produces. It lives HERE
	// rather than beside the table, because the donor rule needs it at the
	// same lookup that finds the codec. Keeping them apart is how the
	// prototype ended up donating for a chain codec and not for a table one.
	kind VKind
	enc  func(reflect.Value) (Value, error)
	dec  func(Value, reflect.Value) error
}

var byIdentity = map[reflect.Type]leafCodec{
	reflect.TypeFor[time.Duration](): {
		name: "time.Duration",
		kind: VString,
		enc: func(v reflect.Value) (Value, error) {
			return String(time.Duration(v.Int()).String()), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			s, err := val.AsString()
			if err != nil {
				return err
			}
			d, err := time.ParseDuration(s)
			if err != nil {
				return err
			}
			dst.SetInt(int64(d))
			return nil
		},
	},
	reflect.TypeFor[time.Time](): {
		name: "time.Time",
		kind: VString,
		enc: func(v reflect.Value) (Value, error) {
			b, err := v.Interface().(time.Time).MarshalText()
			if err != nil {
				return Value{}, err
			}
			return String(string(b)), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			s, err := val.AsString()
			if err != nil {
				return err
			}
			var t time.Time
			if err := t.UnmarshalText([]byte(s)); err != nil {
				return err
			}
			dst.Set(reflect.ValueOf(t))
			return nil
		},
	},
}

// --- kind admission ---------------------------------------------------------

var byteSlice = reflect.TypeFor[[]byte]()

// keyOptIn is R11's seam between its two candidate rules, and ADR-0009 closed
// it: "A registration is usable as a map key only if it says so:
// StringCodec(...).AsMapKey(). A map[T]V whose key type is registered without it
// is a schema compile error."
//
// DEFECT FOUND BY #41 (D5): it defaulted to FALSE, so the tip shipped the rule
// ADR-0009 refused, and every measurement not taken by a probe that set it by
// hand was taken in the world the ADR rejected. THE DEFAULT IS THE DEFECT, not
// the seam: R11a's whole job is to run the implied rule and watch it drop a map
// entry, which is ADR-0009's argument FOR the opt-in, and that needs the seam.
//
// So the default is the decided rule and validMapKey still consults it. A first
// pass at #41 disconnected it instead, which left R11 steering a control that
// was no longer wired to anything and made both of ADR-0009's published rows
// unreproducible. The probe and the engine have to agree about what drives the
// behaviour, and this is that agreement.
var keyOptIn = true

type shape int

const (
	shapeLeaf shape = iota
	shapeStruct
	shapePointer
	shapeSlice
	shapeMap
	shapeUnsupported
)

// validMapKey: core ships string and the integer kinds, and a registered
// codec extends the set exactly as it extends the leaf set.
func validMapKey(k reflect.Type) bool {
	if c, ok := identityLookup(k); ok {
		// ADR-0009's opt-in. Core's own entries key a map on core's proof; a
		// REGISTERED type has to say so, which is where the injectivity
		// obligation is communicated. keyOptIn defaults to the decided rule and
		// R11a turns it off to run the rule ADR-0009 refused, which is the
		// measurement the decision rests on.
		if keyOptIn && activeReg != nil {
			if _, own := activeReg.lookup(k); own {
				return registeredKeys[k] && c.kind == VString
			}
		}
		return true
	}
	// A CHAIN-claimed type MAY NOT key a map. ADR-0007 granted it - "a type the
	// chain claims with declared kind String may key a map, on the same terms as
	// a registered codec" - and reversed itself under #45, because the terms
	// were not the same: ADR-0009 landed afterwards and made the terms for a
	// registration an explicit `.AsMapKey()` opt-in, and a chain arm has no call
	// site at which to say it.
	//
	// So the arm that used to sit here is gone. Note what that does NOT do: the
	// chain still claims the type at a leaf, as a slice element, behind a
	// pointer and as a map VALUE. Only the key position moves, which is the one
	// position where a text form that is not injective costs a map ENTRY rather
	// than legibility.
	//
	// The two-lookups-disagree objection this arm was added for still stands and
	// is answered differently: the leaf lookup and the key lookup now ask
	// DIFFERENT questions - "can ferry represent this" and "can two values of
	// this collapse into one address" - so agreeing is not the property wanted.
	// Y45=1 measures the inversion that made the old answer wrong: with the arm
	// in place, registering a type left it LESS usable than not registering it,
	// which is true nowhere else in ferry.
	switch k.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

// mapKeyRefusal is the diagnostic half of ADR-0009's opt-in, and the ADR is
// explicit that the diagnostic IS the mechanism: "the diagnostic is where the
// obligation gets communicated, which is the point: it is the only moment a
// registrant is guaranteed to read".
//
// DEFECT FOUND BY #41 (D5): the compiler said `unsupported map key type
// netip.Addr`, which communicates no obligation at all and names no remedy. The
// message below is ADR-0009's own, and it already existed - in walk.go, on the
// engine the tip no longer uses.
//
// CLOSED BY #45, which was open when the paragraph above was written: a type
// carrying the text pair that nobody registered used to be claimed by
// ADR-0007's chain and to key a map with no opt-in, so the obligation was
// defeatable by NOT registering. ADR-0007 has since reversed its own sentence
// and keying a map is registration-only, so that route is refused here with a
// message of its own.
//
// The message must not accuse the type of anything. Measured (Y45=3), every
// stdlib type the chain claims is injective on every adversarial value the
// probe could build: the refusal is because nobody can be ASKED, not because
// the answer would be no. So it names the mechanism and the remedy and makes
// no claim about the text.
//
// R11e records the sibling case for core's own pre-seeded entries, which is
// #31's and which neither this rule nor ADR-0009's reaches.
func mapKeyRefusal(p Path, k reflect.Type) error {
	if _, ok := identityLookup(k); ok && keyOptIn && activeReg != nil {
		if _, own := activeReg.lookup(k); own && !registeredKeys[k] {
			return fmt.Errorf(
				"ferry: %s: %s has a registered codec but is not declared usable as a map key; "+
					"a key codec's text must be injective over the key type, or two keys collapse "+
					"into one address; add .AsMapKey() to the registration if it is",
				pathOrRoot(p), k)
		}
	}
	if c, ok := activeChainCodec(k); ok && c.kind == VString {
		return fmt.Errorf(
			"ferry: %s: %s may not key a map: ferry claims it through its text pair rather "+
				"than through a registration, so nobody has declared its text injective over "+
				"the key type, and two keys that render alike collapse into one address; "+
				"register a codec and mark it usable as a key with "+
				"ferry.TextCodec[%s](ferry.VString).AsMapKey()",
			pathOrRoot(p), k, k)
	}
	return fmt.Errorf("ferry: %s: unsupported map key type %s", pathOrRoot(p), k)
}

func classify(t reflect.Type) shape {
	if _, ok := identityLookup(t); ok {
		return shapeLeaf
	}
	// Pointer indirection is STRUCTURAL and is resolved before the chain is
	// asked anything. Measured, without this line: *big.Int satisfies the
	// text pair in its own right, so a *big.Int field becomes a leaf, the
	// nil-pointer rule is bypassed, a nil dumps as string("<nil>") and the
	// load panics inside big.Int.UnmarshalText on a nil receiver.
	if t.Kind() == reflect.Pointer {
		if _, ok := identityLookup(t); !ok {
			return shapePointer
		}
	}
	// #12's seam. Before kind, the chain's declaration beats ferry's
	// inference; after kind, the chain is only a rescue for what kind
	// refuses outright, and never reaches a struct that maps no address.
	if chainBeforeKind {
		if _, ok := chainCodec(t); ok {
			return shapeLeaf
		}
	}
	if !chainBeforeKind && len(chainOrder) > 0 && kindWouldRefuse(t) {
		if _, ok := chainCodec(t); ok {
			return shapeLeaf
		}
	}
	return kindClassify(t)
}

func kindClassify(t reflect.Type) shape {
	// The admitted kind list is data, in x1_kinds.go, and e_schema.go's
	// kindLeaf reads the same one. #41 D2: it used to be a `case` clause here
	// and a second `case` clause there, and they disagreed about uint8.
	if kindAdmitsLeaf(t) {
		return shapeLeaf
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return shapeSlice
	case reflect.Struct:
		return shapeStruct
	case reflect.Pointer:
		return shapePointer
	case reflect.Map:
		if !validMapKey(t.Key()) {
			return shapeUnsupported
		}
		return shapeMap
	}
	return shapeUnsupported
}

// --- scalar encode / decode -------------------------------------------------

func encLeaf(v reflect.Value) (Value, error) {
	t := v.Type()
	if c, ok := identityLookup(t); ok {
		return c.enc(v)
	}
	if c, ok := activeChainCodec(t); ok {
		return c.enc(v)
	}
	switch t.Kind() {
	case reflect.Bool:
		return Bool(v.Bool()), nil
	case reflect.String:
		return String(v.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Number(strconv.FormatInt(v.Int(), 10)), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Number(strconv.FormatUint(v.Uint(), 10)), nil
	case reflect.Float32:
		return Number(strconv.FormatFloat(v.Float(), 'g', -1, 32)), nil
	case reflect.Float64:
		return Number(strconv.FormatFloat(v.Float(), 'g', -1, 64)), nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Array {
			b := make([]byte, v.Len())
			reflect.Copy(reflect.ValueOf(b), v)
			return Bytes(b), nil
		}
		if v.IsNil() {
			return Null(), nil
		}
		return Bytes(v.Bytes()), nil
	}
	return Value{}, fmt.Errorf("no leaf encoding for %s", t)
}

// asDonor normalises the one coercion the type set admits on Load: a plane
// with no type information reports String, so a String is accepted wherever
// the leaf's own kind is, and is parsed by that kind's own parser. Nothing
// else coerces: a Number is NOT accepted for a Go string field, because a
// plane that reports Number is asserting a type and ferry respects it.
func asDonor(val Value, want VKind) Value {
	if val.Kind() == VString && want != VString {
		return Value{kind: want, text: val.Text()}
	}
	return val
}

func decLeaf(val Value, dst reflect.Value) error {
	t := dst.Type()
	if c, ok := identityLookup(t); ok {
		// The donor rule is core's and applies to EVERY codec, whether it
		// came from the identity table or from the chain.
		return c.dec(asDonor(val, c.kind), dst)
	}
	if c, ok := activeChainCodec(t); ok {
		// The donor rule is core's and applies to a codec unchanged: the
		// codec declares the kind it produces, and String is donated to it.
		// A codec that re-implemented this would get G2 wrong for its own
		// users on exactly the three planes that report String for
		// everything.
		return c.dec(asDonor(val, c.kind), dst)
	}
	switch t.Kind() {
	case reflect.Bool:
		val = asDonor(val, VBool)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		val = asDonor(val, VNumber)
	case reflect.Slice, reflect.Array:
		// Bytes and String both hold raw bytes in text, so this is a
		// relabel and not a decode.
		val = asDonor(val, VBytes)
	}
	switch t.Kind() {
	case reflect.Bool:
		b, err := val.AsBool()
		if err != nil {
			return err
		}
		dst.SetBool(b)
	case reflect.String:
		s, err := val.AsString()
		if err != nil {
			return err
		}
		dst.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		s, err := val.AsNumber()
		if err != nil {
			return err
		}
		n, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return err
		}
		dst.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s, err := val.AsNumber()
		if err != nil {
			return err
		}
		n, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return err
		}
		dst.SetUint(n)
	case reflect.Float32, reflect.Float64:
		s, err := val.AsNumber()
		if err != nil {
			return err
		}
		f, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return err
		}
		dst.SetFloat(f)
	case reflect.Slice: // []byte
		if val.Kind() == VNull {
			dst.Set(reflect.Zero(t))
			return nil
		}
		b, err := val.AsBytes()
		if err != nil {
			return err
		}
		dst.SetBytes(b)
	case reflect.Array: // [N]byte
		b, err := val.AsBytes()
		if err != nil {
			return err
		}
		reflect.Copy(dst, reflect.ValueOf(b))
	default:
		return fmt.Errorf("no leaf decoding for %s", t)
	}
	return nil
}
