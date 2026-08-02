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
	enc  func(reflect.Value) (Value, error)
	dec  func(Value, reflect.Value) error
}

var byIdentity = map[reflect.Type]leafCodec{
	reflect.TypeFor[time.Duration](): {
		name: "time.Duration",
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
	if _, ok := byIdentity[k]; ok {
		return true
	}
	// The key lookup has to consult the same chain the leaf lookup does, or a
	// type the chain admits as a leaf is still refused as a key. Two lookups
	// answering the same question differently is what identity-before-kind
	// exists to stop.
	if c, ok := activeChainCodec(k); ok && c.kind == VString {
		return true
	}
	switch k.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func classify(t reflect.Type) shape {
	if _, ok := byIdentity[t]; ok {
		return shapeLeaf
	}
	// Pointer indirection is STRUCTURAL and is resolved before the chain is
	// asked anything. Measured, without this line: *big.Int satisfies the
	// text pair in its own right, so a *big.Int field becomes a leaf, the
	// nil-pointer rule is bypassed, a nil dumps as string("<nil>") and the
	// load panics inside big.Int.UnmarshalText on a nil receiver.
	if t.Kind() == reflect.Pointer {
		if _, ok := byIdentity[t]; !ok {
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
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return shapeLeaf
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return shapeLeaf // []byte and [N]byte are Bytes, never an indexed composite
		}
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
	if c, ok := byIdentity[t]; ok {
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
	if c, ok := byIdentity[t]; ok {
		return c.dec(val, dst)
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
