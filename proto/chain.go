package main

// The candidate codec chain.
//
// The one structural departure from xload: a codec is selected ONCE per type,
// as a PAIR, and both directions use that selection. xload's chain is
// one-directional so it never had to make encode and decode agree; ferry's
// must, because the two halves of a round trip are chosen by the same list.

import (
	"encoding"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"reflect"
)

// codec is what an arm produces once it has claimed a type. It is the same
// shape as the identity table's leafCodec plus the boundary kind, which is
// the thing the donor rule needs and which no stdlib interface can state.
type codec struct {
	name string
	arm  string
	kind VKind
	enc  func(reflect.Value) (Value, error)
	dec  func(Value, reflect.Value) error
}

// --- calling a method that may be on either receiver -------------------------

// asIface returns v as something satisfying i, taking the address when the
// method is on the pointer receiver and copying when v is not addressable.
// The copy is the case that makes map values and unaddressable composites
// work at all, and it is where a pointer-receiver method silently operates on
// a copy rather than on the original.
func asIface(v reflect.Value, i reflect.Type) (any, bool) {
	if v.Type().Implements(i) {
		return v.Interface(), true
	}
	pt := reflect.PointerTo(v.Type())
	if !pt.Implements(i) {
		return nil, false
	}
	if v.CanAddr() {
		return v.Addr().Interface(), true
	}
	p := reflect.New(v.Type())
	p.Elem().Set(v)
	return p.Interface(), true
}

func implementsEither(t reflect.Type, i reflect.Type) bool {
	return t.Implements(i) || reflect.PointerTo(t).Implements(i)
}

// --- the arms ----------------------------------------------------------------

// textCodecFor builds the text arm's codec for t, or reports that the pair is
// incomplete. TextAppender and TextMarshaler are two spellings of the SAME
// representation, not two arms: there is no "AppendFrom", so both are answered
// by one TextUnmarshaler.
func textCodecFor(t reflect.Type) (codec, bool, string) {
	hasA := implementsEither(t, ifTextA.t)
	hasM := implementsEither(t, ifTextM.t)
	hasU := implementsEither(t, ifTextU.t)
	switch {
	case (hasA || hasM) && hasU:
	case hasA || hasM:
		return codec{}, false, "implements encoding.TextMarshaler but not encoding.TextUnmarshaler"
	case hasU:
		return codec{}, false, "implements encoding.TextUnmarshaler but not encoding.TextMarshaler"
	default:
		return codec{}, false, ""
	}
	c := codec{name: t.String(), arm: "text", kind: VString}
	c.enc = func(v reflect.Value) (Value, error) {
		if hasA {
			if a, ok := asIface(v, ifTextA.t); ok {
				b, err := a.(encoding.TextAppender).AppendText(nil)
				return String(string(b)), err
			}
		}
		m, ok := asIface(v, ifTextM.t)
		if !ok {
			return Value{}, fmt.Errorf("%s: no text marshaler", t)
		}
		b, err := m.(encoding.TextMarshaler).MarshalText()
		return String(string(b)), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		s, err := val.AsString()
		if err != nil {
			return err
		}
		u, ok := asIface(dst, ifTextU.t)
		if !ok {
			return fmt.Errorf("%s: no text unmarshaler", t)
		}
		return u.(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}
	return c, true, ""
}

// jsonCodecFor is built only so the ADR can measure what recognising the JSON
// arm would actually put on a plane. It is not proposed.
func jsonCodecFor(t reflect.Type) (codec, bool, string) {
	hasM := implementsEither(t, ifJSONM.t)
	hasU := implementsEither(t, ifJSONU.t)
	if !hasM || !hasU {
		if hasM {
			return codec{}, false, "implements json.Marshaler but not json.Unmarshaler"
		}
		if hasU {
			return codec{}, false, "implements json.Unmarshaler but not json.Marshaler"
		}
		return codec{}, false, ""
	}
	c := codec{name: t.String(), arm: "json", kind: VString}
	c.enc = func(v reflect.Value) (Value, error) {
		m, _ := asIface(v, ifJSONM.t)
		b, err := m.(json.Marshaler).MarshalJSON()
		return String(string(b)), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		s, err := val.AsString()
		if err != nil {
			return err
		}
		u, _ := asIface(dst, ifJSONU.t)
		return u.(json.Unmarshaler).UnmarshalJSON([]byte(s))
	}
	return c, true, ""
}

func binaryCodecFor(t reflect.Type) (codec, bool, string) {
	hasM := implementsEither(t, ifBinM.t)
	hasU := implementsEither(t, ifBinU.t)
	if !hasM || !hasU {
		if hasM {
			return codec{}, false, "implements encoding.BinaryMarshaler but not BinaryUnmarshaler"
		}
		if hasU {
			return codec{}, false, "implements encoding.BinaryUnmarshaler but not BinaryMarshaler"
		}
		return codec{}, false, ""
	}
	c := codec{name: t.String(), arm: "binary", kind: VBytes}
	c.enc = func(v reflect.Value) (Value, error) {
		m, _ := asIface(v, ifBinM.t)
		b, err := m.(encoding.BinaryMarshaler).MarshalBinary()
		return Bytes(b), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		b, err := val.AsBytes()
		if err != nil {
			return err
		}
		u, _ := asIface(dst, ifBinU.t)
		return u.(encoding.BinaryUnmarshaler).UnmarshalBinary(b)
	}
	return c, true, ""
}

func gobCodecFor(t reflect.Type) (codec, bool, string) {
	hasM := implementsEither(t, ifGobE.t)
	hasU := implementsEither(t, ifGobD.t)
	if !hasM || !hasU {
		return codec{}, false, ""
	}
	c := codec{name: t.String(), arm: "gob", kind: VBytes}
	c.enc = func(v reflect.Value) (Value, error) {
		m, _ := asIface(v, ifGobE.t)
		b, err := m.(gob.GobEncoder).GobEncode()
		return Bytes(b), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		b, err := val.AsBytes()
		if err != nil {
			return err
		}
		u, _ := asIface(dst, ifGobD.t)
		return u.(gob.GobDecoder).GobDecode(b)
	}
	return c, true, ""
}

type armFn func(reflect.Type) (codec, bool, string)

var armByName = map[string]armFn{
	"text":   textCodecFor,
	"json":   jsonCodecFor,
	"binary": binaryCodecFor,
	"gob":    gobCodecFor,
}

// --- selection ---------------------------------------------------------------

// selectPaired walks the arm list in order and takes the FIRST arm whose pair
// is complete. This is ferry's proposed rule.
func selectPaired(t reflect.Type, order []string) (codec, []string, bool) {
	var halves []string
	for _, name := range order {
		c, ok, half := armByName[name](t)
		if ok {
			return c, halves, true
		}
		if half != "" {
			halves = append(halves, half)
		}
	}
	return codec{}, halves, false
}

// selectPerDirection picks the encode half and the decode half independently,
// which is what a chain written as two type switches produces and what xload's
// shape would become if an Encode counterpart were bolted on. It exists to be
// measured against selectPaired, not to be proposed.
func selectPerDirection(t reflect.Type, order []string) (codec, bool) {
	// Each direction takes the first arm whose OWN half is present, ignoring
	// whether the other half exists. That is what two independent type
	// switches produce.
	var enc, dec *codec
	var encArm, decArm string
	for _, name := range order {
		switch name {
		case "text":
			if enc == nil && (implementsEither(t, ifTextA.t) || implementsEither(t, ifTextM.t)) {
				c, _, _ := forceText(t)
				enc, encArm = &c, name
			}
			if dec == nil && implementsEither(t, ifTextU.t) {
				c, _, _ := forceText(t)
				dec, decArm = &c, name
			}
		case "json":
			if enc == nil && implementsEither(t, ifJSONM.t) {
				c, _, _ := forceJSON(t)
				enc, encArm = &c, name
			}
			if dec == nil && implementsEither(t, ifJSONU.t) {
				c, _, _ := forceJSON(t)
				dec, decArm = &c, name
			}
		case "binary":
			if enc == nil && implementsEither(t, ifBinM.t) {
				c, _, _ := forceBinary(t)
				enc, encArm = &c, name
			}
			if dec == nil && implementsEither(t, ifBinU.t) {
				c, _, _ := forceBinary(t)
				dec, decArm = &c, name
			}
		}
	}
	if enc == nil || dec == nil {
		return codec{}, false
	}
	return codec{
		name: t.String(),
		arm:  encArm + "/" + decArm,
		kind: enc.kind,
		enc:  enc.enc,
		dec:  dec.dec,
	}, true
}

// force* build a codec from whichever half exists, so the per-direction
// selector can be measured even where the pair is incomplete.
func forceText(t reflect.Type) (codec, bool, string) {
	c := codec{name: t.String(), arm: "text", kind: VString}
	c.enc = func(v reflect.Value) (Value, error) {
		if a, ok := asIface(v, ifTextA.t); ok {
			b, err := a.(encoding.TextAppender).AppendText(nil)
			return String(string(b)), err
		}
		m, ok := asIface(v, ifTextM.t)
		if !ok {
			return Value{}, fmt.Errorf("%s: no text marshaler", t)
		}
		b, err := m.(encoding.TextMarshaler).MarshalText()
		return String(string(b)), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		s, err := val.AsString()
		if err != nil {
			return err
		}
		u, ok := asIface(dst, ifTextU.t)
		if !ok {
			return fmt.Errorf("%s: no text unmarshaler", t)
		}
		return u.(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}
	return c, true, ""
}

func forceJSON(t reflect.Type) (codec, bool, string) {
	c := codec{name: t.String(), arm: "json", kind: VString}
	c.enc = func(v reflect.Value) (Value, error) {
		m, ok := asIface(v, ifJSONM.t)
		if !ok {
			return Value{}, fmt.Errorf("%s: no json marshaler", t)
		}
		b, err := m.(json.Marshaler).MarshalJSON()
		return String(string(b)), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		s, err := val.AsString()
		if err != nil {
			return err
		}
		u, ok := asIface(dst, ifJSONU.t)
		if !ok {
			return fmt.Errorf("%s: no json unmarshaler", t)
		}
		return u.(json.Unmarshaler).UnmarshalJSON([]byte(s))
	}
	return c, true, ""
}

func forceBinary(t reflect.Type) (codec, bool, string) {
	c := codec{name: t.String(), arm: "binary", kind: VBytes}
	c.enc = func(v reflect.Value) (Value, error) {
		m, ok := asIface(v, ifBinM.t)
		if !ok {
			return Value{}, fmt.Errorf("%s: no binary marshaler", t)
		}
		b, err := m.(encoding.BinaryMarshaler).MarshalBinary()
		return Bytes(b), err
	}
	c.dec = func(val Value, dst reflect.Value) error {
		b, err := val.AsBytes()
		if err != nil {
			return err
		}
		u, ok := asIface(dst, ifBinU.t)
		if !ok {
			return fmt.Errorf("%s: no binary unmarshaler", t)
		}
		return u.(encoding.BinaryUnmarshaler).UnmarshalBinary(b)
	}
	return c, true, ""
}

// --- wiring the chain into classify() ---------------------------------------

// chainOrder is the arm list. ADR-0007 decides it and decides that it has
// exactly one member:
//
//	"json.Marshaler/Unmarshaler, encoding.BinaryMarshaler/BinaryUnmarshaler and
//	 gob.GobEncoder/GobDecoder are NOT arms."
//
// so the chain is step 2 of three, "the text pair: encoding.TextAppender or
// encoding.TextMarshaler, together with encoding.TextUnmarshaler". The other
// three arms stay in this file because P5 and P16 measure them; they are not
// in the default list.
//
// DEFECT FOUND BY #41 (D3): this was `var chainOrder []string`, so the chain
// was OFF, and chainBeforeKind was false, so the ordering was the one ADR-0007
// rejected. Every P12 and R probe that measures the chain sets both in its own
// body and reverts them in a defer; no E16 or B25 probe sets either, so
// ADR-0010's eleven probes and ADR-0012's thirteen were all taken with
// ADR-0007's decision switched off.
var chainOrder = []string{"text"}

// chainBeforeKind is ADR-0007's headline: "The text pair is consulted BEFORE
// reflect.Kind admission. A declaration beats an inference."
var chainBeforeKind = true

// chainCodec is the seam classify() consults. It returns the codec the chain
// selected for t, if any. Core's identity table is consulted first and is not
// part of the chain: a type core owns is never re-decided by an interface.
func chainCodec(t reflect.Type) (codec, bool) {
	if len(chainOrder) == 0 {
		return codec{}, false
	}
	if _, ok := identityLookup(t); ok {
		return codec{}, false
	}
	c, _, ok := selectPaired(t, chainOrder)
	return c, ok
}

// activeChainCodec mirrors classify()'s decision, so encLeaf and decLeaf use
// exactly the codec the schema compiled with. Deriving it twice from two
// places is how a chain drifts.
func activeChainCodec(t reflect.Type) (codec, bool) {
	if chainBeforeKind {
		return chainCodec(t)
	}
	if kindWouldRefuse(t) {
		return chainCodec(t)
	}
	return codec{}, false
}

// kindWouldRefuse is the fair reading of "the chain runs AFTER kind": the
// chain rescues exactly what kind admission refuses, which is ADR-0005's own
// framing of its rules as backstops. Refusing to include the maps-no-address
// backstop here would make the after-kind arm inert by construction, and the
// comparison worthless.
func kindWouldRefuse(t reflect.Type) bool {
	return kindRefuses(t, map[reflect.Type]bool{})
}

func kindRefuses(t reflect.Type, stack map[reflect.Type]bool) bool {
	if _, ok := identityLookup(t); ok {
		return false
	}
	if stack[t] {
		return true // recursive: unbounded address set
	}
	switch kindClassify(t) {
	case shapeUnsupported:
		return true
	case shapeLeaf:
		return false
	}
	stack[t] = true
	defer delete(stack, t)
	switch kindClassify(t) {
	case shapePointer:
		return kindRefuses(t.Elem(), stack)
	case shapeSlice, shapeMap:
		return kindRefuses(t.Elem(), stack)
	case shapeStruct:
		mapped := false
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if kindRefuses(f.Type, stack) {
				return true
			}
			mapped = true
		}
		return !mapped // maps no address
	}
	return true
}
