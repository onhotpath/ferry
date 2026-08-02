package main

// The registration model under test for #19.
//
// #12 fixed everything about WHAT a codec is: it is an entry in the same
// identity table the chain consults first, it is a pair, it declares the
// boundary kind it produces, and it takes no context. None of that is
// reopened here.
//
// What is open is WHERE the table lives and HOW LONG it lives, which is the
// question ADR-0007 deliberately did not touch. This file builds the table as
// a first-class value so the three candidate lifetimes can be run against
// each other rather than argued.

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// --- the registration ------------------------------------------------------

// Reg is one type's registration. It is opaque and the only way to make one is
// a constructor that takes BOTH halves, which is how "a codec is a pair" is
// made unrepresentable-otherwise rather than documented.
type Reg struct {
	t     reflect.Type
	name  string
	kind  VKind
	asKey bool // may this type key a map: see R11
	c     leafCodec
}

// ValueCodec builds a registration for T from two functions over the boundary
// Value. It is the general form: it is the only constructor whose decode half
// sees the whole Value, so it is the only one that can accept a kind it never
// emits (ADR-0006's Null escape hatch, R2c).
//
// kind is required and not defaulted, per ADR-0007: a codec whose text is a
// run of digits must say Number, or it works on env and fails on YAML.
//
// Named ValueCodec rather than TypeCodec so the trio reads String / Value /
// Text, after what the two halves speak. R17b is the argument.
func ValueCodec[T any](kind VKind, enc func(T) (Value, error), dec func(Value) (T, error)) Reg {
	t := reflect.TypeFor[T]()
	return Reg{
		t:    t,
		name: t.String(),
		kind: kind,
		c: leafCodec{
			name: t.String(),
			kind: kind,
			enc: func(v reflect.Value) (Value, error) {
				// COMMA-OK, not a bare assertion. A bare `v.Interface().(T)`
				// PANICS when T is an interface type and the field holds a nil
				// interface, which is the zero value of every interface
				// registration and therefore the value the codec sees most
				// often. Measured: R14f.
				in, _ := v.Interface().(T)
				out, err := enc(in)
				if err != nil {
					return Value{}, err
				}
				if out.Kind() != kind && out.Kind() != VNull {
					return Value{}, fmt.Errorf(
						"ferry: codec for %s declared kind %v but produced %v", t, kind, out.Kind())
				}
				return out, nil
			},
			dec: func(val Value, dst reflect.Value) error {
				out, err := dec(val)
				if err != nil {
					return err
				}
				// reflect.ValueOf(&out).Elem() and not reflect.ValueOf(out).
				// The mirror of the encode-half defect: when T is an interface
				// type and the codec returns a nil interface, reflect.ValueOf
				// yields the ZERO Value and Set panics with `reflect: call of
				// reflect.Value.Set on zero Value`. Taking the address gives a
				// Value of static type T whatever the dynamic value is.
				dst.Set(reflect.ValueOf(&out).Elem())
				return nil
			},
		},
	}
}

// TypeCodec is the name #12's P18 used, kept so that probe still reads as it
// was written.
func TypeCodec[T any](kind VKind, enc func(T) (Value, error), dec func(Value) (T, error)) Reg {
	return ValueCodec(kind, enc, dec)
}

// StringCodec is the ergonomic case and the one that will be used most: the
// 90% codec is "format to text, parse from text".
func StringCodec[T any](format func(T) string, parse func(string) (T, error)) Reg {
	return TypeCodec[T](VString,
		func(v T) (Value, error) { return String(format(v)), nil },
		func(v Value) (T, error) {
			var zero T
			s, err := v.AsString()
			if err != nil {
				return zero, err
			}
			return parse(s)
		})
}

// TextCodec registers T through encoding.TextMarshaler/TextUnmarshaler, which
// is the shape a user reaches for when the type already has the pair but the
// chain cannot claim it - a POINTER-receiver pair on a type used by value is
// the case, and a named type over one core owns is the other.
func TextCodec[T any, PT interface {
	*T
	UnmarshalText([]byte) error
}](kind VKind) Reg {
	return TypeCodec[T](kind,
		func(v T) (Value, error) {
			m, ok := any(&v).(interface{ MarshalText() ([]byte, error) })
			if !ok {
				return Value{}, fmt.Errorf("%T: no MarshalText", v)
			}
			b, err := m.MarshalText()
			return Value{kind: kind, text: string(b)}, err
		},
		func(val Value) (T, error) {
			var out T
			if val.Kind() != kind && val.Kind() != VString {
				return out, errKind
			}
			return out, PT(&out).UnmarshalText([]byte(val.Text()))
		})
}

// AsMapKey marks a registration as usable for a map key. R11 measures whether
// this should be opt-in or implied by the declared kind.
func (r Reg) AsMapKey() Reg { r.asKey = true; return r }

// --- the table as a value --------------------------------------------------

// coreOwned is the pre-seeded part of the identity table, and it is not
// replaceable: ADR-0007 makes registering a type already in the table a loud
// error rather than an override.
func coreOwnedTypes() map[reflect.Type]bool {
	return map[reflect.Type]bool{
		reflect.TypeFor[time.Duration](): true,
		reflect.TypeFor[time.Time]():     true,
	}
}

var regIDs atomic.Uint64

// Registry is the identity table as a value. It carries an identity of its
// own, minted at construction, because ADR-0004 measured that a value
// containing a func field panics as a map key and a codec is nothing but func
// fields. R7 is what that identity is for.
type Registry struct {
	id     uint64
	byType map[reflect.Type]leafCodec
	keys   map[reflect.Type]bool
	frozen atomic.Bool
}

func NewRegistry() *Registry {
	return &Registry{
		id:     regIDs.Add(1),
		byType: map[reflect.Type]leafCodec{},
		keys:   map[reflect.Type]bool{},
	}
}

func (r *Registry) Register(regs ...Reg) error {
	core := coreOwnedTypes()
	var errs []string
	for _, g := range regs {
		switch {
		case r.frozen.Load():
			errs = append(errs, fmt.Sprintf(
				"ferry: %s: the registry is frozen; every registration must happen before the first schema is compiled", g.name))
		case g.t.Kind() == reflect.Pointer:
			errs = append(errs, fmt.Sprintf(
				"ferry: %s: pointer indirection is structural and a pointer type never reaches the table; register %s instead",
				g.name, g.t.Elem()))
		case core[g.t]:
			errs = append(errs, fmt.Sprintf(
				"ferry: %s is in core's own set and its representation is pinned; define a named type over it and register that", g.name))
		case r.byType[g.t].enc != nil:
			errs = append(errs, fmt.Sprintf("ferry: %s is already registered", g.name))
		default:
			r.byType[g.t] = g.c
			if g.asKey {
				r.keys[g.t] = true
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", joinLines(errs))
	}
	return nil
}

func (r *Registry) lookup(t reflect.Type) (leafCodec, bool) {
	c, ok := r.byType[t]
	return c, ok
}

// --- installing a registry into the prototype's global seams ----------------
//
// The inherited prototype reads a package-level `byIdentity`. Rather than
// thread a registry through every call site of a throwaway, install() swaps
// the global. That is exactly the "global and mutable" model under test in
// R6, and withRegistry is what the per-call model of R7 has to simulate.

var installMu sync.Mutex

// activeReg is the installed registry. identityLookup consults core's
// pre-seeded table first and then this, which is ADR-0007's "registration is
// an entry in the same identity table" with core's own entries unreplaceable
// by construction rather than by a check.
var activeReg *Registry

func identityLookup(t reflect.Type) (leafCodec, bool) {
	if c, ok := byIdentity[t]; ok {
		return c, true
	}
	if activeReg != nil {
		return activeReg.lookup(t)
	}
	return leafCodec{}, false
}

func (r *Registry) install() func() {
	installMu.Lock()
	savedReg, savedKeys := activeReg, registeredKeys
	activeReg, registeredKeys = r, r.keys
	return func() {
		activeReg, registeredKeys = savedReg, savedKeys
		installMu.Unlock()
	}
}

// registeredKeys is consulted by validMapKey. R11 measures whether it should
// exist at all.
var registeredKeys map[reflect.Type]bool

func withRegistry(r *Registry, f func()) {
	done := r.install()
	defer done()
	f()
}

// mustReg fails loudly in a probe rather than returning an error nobody reads.
func mustReg(r *Registry, regs ...Reg) *Registry {
	if err := r.Register(regs...); err != nil {
		panic(err)
	}
	return r
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n"
		}
		out += s
	}
	return out
}
