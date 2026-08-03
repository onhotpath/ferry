package main

// For each refused kind: what is the limiting factor, and can a registered
// typed codec lift it?
//
// The mechanism under test is one line: a codec keyed by reflect.Type is
// consulted BEFORE reflect.Kind, so a registered type is classified shapeLeaf.
// A leaf mints exactly one address and is never walked. So a codec does not
// merely "handle" a type - it collapses it to a leaf, and a leaf needs no
// address set at all. That is the general escape hatch, and it is why the
// answer differs per kind.

import (
	"encoding"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"unsafe"
)

// register is #19's mechanism, stubbed to whatever shape it lands on.
func register[T any](enc func(T) (Value, error), dec func(Value) (T, error)) {
	byIdentity[reflect.TypeFor[T]()] = leafCodec{
		name: reflect.TypeFor[T]().String(),
		// #31 surfaced this shim. It writes into byIdentity, which is CORE's
		// own pre-seeded table and not a registry, so under the rule that key
		// admissibility is declared per entry it has to declare one. These
		// entries stand in for REGISTRATIONS, which under ADR-0009 declare a
		// kind and may say .AsMapKey(), so both are set here rather than
		// leaving the zero value to mean something.
		kind:  VString,
		asKey: true,
		enc:   func(v reflect.Value) (Value, error) { return enc(v.Interface().(T)) },
		dec: func(val Value, dst reflect.Value) error {
			out, err := dec(val)
			if err != nil {
				return err
			}
			dst.Set(reflect.ValueOf(out))
			return nil
		},
	}
}

// textCodec registers any type whose text form is already an inverse pair.
func textCodec[T any](newPtr func() interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}) {
	register(func(v T) (Value, error) {
		p := newPtr()
		reflect.ValueOf(p).Elem().Set(reflect.ValueOf(v))
		b, err := p.MarshalText()
		return String(string(b)), err
	}, func(val Value) (T, error) {
		var zero T
		s, err := val.AsString()
		if err != nil {
			return zero, err
		}
		p := newPtr()
		if err := p.UnmarshalText([]byte(s)); err != nil {
			return zero, err
		}
		return reflect.ValueOf(p).Elem().Interface().(T), nil
	})
}

type Node struct {
	Name string
	Next *Node
}

func tryRT(label string, t reflect.Type, v reflect.Value) {
	h := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: t}})).Elem()
	if v.IsValid() {
		h.Field(0).Set(v)
	}
	if _, err := compile(h.Type()); err != nil {
		fmt.Printf("    %-26s REFUSED  %s\n", label, shorten(err.Error()))
		return
	}
	d, err := dump(h)
	if err != nil {
		fmt.Printf("    %-26s dump err %v\n", label, err)
		return
	}
	back := reflect.New(h.Type()).Elem()
	if err := load(d, back); err != nil {
		fmt.Printf("    %-26s load err %v\n", label, err)
		return
	}
	var parts []string
	for _, p := range sortedAddrs(d) {
		parts = append(parts, p.String()+"="+d[p].GoString())
	}
	fmt.Printf("    %-26s OK       %s\n", label, shorten(fmt.Sprint(parts)))
}

func runRefusals() {
	fmt.Println("\n--- (c) refused by POLICY: core could support these and does not ---")
	for _, c := range []complex128{complex(1, 2), complex(0, 0)} {
		s := strconv.FormatComplex(c, 'g', -1, 128)
		p, err := strconv.ParseComplex(s, 128)
		fmt.Printf("    complex128 %-12v -> %-12q -> %-12v exact=%v err=%v\n", c, s, p, p == c, err)
	}
	fmt.Println("    ^ strconv has a total inverse pair. Nothing structural refuses complex.")
	fmt.Printf("    uintptr is a uint: %v. Round-trips fine, and means nothing in another process.\n",
		reflect.TypeFor[uintptr]().Kind())

	fmt.Println("\n--- (a) refused because the VALUE does not exist outside the process ---")
	fmt.Printf("    func comparable  : %v   <- cannot even ask 'which registered func is this'\n",
		reflect.TypeFor[func()]().Comparable())
	fmt.Printf("    chan comparable  : %v   <- comparable, but the identity is a pointer\n",
		reflect.TypeFor[chan int]().Comparable())
	fmt.Printf("    unsafe.Pointer   : %v bytes of process-local address\n", unsafe.Sizeof(unsafe.Pointer(nil)))
	fmt.Println("    A codec must produce a Value (kind + text) and rebuild from text alone.")
	fmt.Println("    For these three there is nothing in the text that could rebuild them.")

	fmt.Println("\n--- (b) refused because core cannot compute an ADDRESS SET ---")
	fmt.Println("    Do the refused types already carry an inverse text pair?")
	tm := reflect.TypeFor[encoding.TextMarshaler]()
	tu := reflect.TypeFor[encoding.TextUnmarshaler]()
	for _, t := range []reflect.Type{
		reflect.TypeFor[netip.Addr](), reflect.TypeFor[netip.AddrPort](),
		reflect.TypeFor[netip.Prefix](), reflect.TypeFor[big.Int](),
		reflect.TypeFor[net.IP](), reflect.TypeFor[Node](),
	} {
		fmt.Printf("    %-18s TextMarshaler(val)=%-6v (ptr)=%-6v  TextUnmarshaler(ptr)=%v\n",
			t, t.Implements(tm), reflect.PointerTo(t).Implements(tm), reflect.PointerTo(t).Implements(tu))
	}

	fmt.Println("\n    before registration:")
	tryRT("netip.Addr", reflect.TypeFor[netip.Addr](), reflect.ValueOf(netip.MustParseAddr("192.0.2.1")))
	tryRT("big.Int", reflect.TypeFor[big.Int](), reflect.ValueOf(*big.NewInt(1 << 40)))
	tryRT("Node (recursive)", reflect.TypeFor[Node](), reflect.ValueOf(Node{"a", &Node{"b", nil}}))
	tryRT("net.Addr (interface)", reflect.TypeFor[net.Addr](), reflect.Value{})
	tryRT("map[netip.Addr]string", reflect.TypeFor[map[netip.Addr]string](),
		reflect.ValueOf(map[netip.Addr]string{netip.MustParseAddr("10.0.0.1"): "a"}))

	// Register codecs.
	textCodec[netip.Addr](func() interface {
		encoding.TextMarshaler
		encoding.TextUnmarshaler
	} {
		return new(netip.Addr)
	})
	textCodec[big.Int](func() interface {
		encoding.TextMarshaler
		encoding.TextUnmarshaler
	} {
		return new(big.Int)
	})
	// A recursive type collapsed to one leaf by its own codec.
	register(func(n Node) (Value, error) {
		s := ""
		for p := &n; p != nil; p = p.Next {
			if s != "" {
				s += ">"
			}
			s += p.Name
		}
		return String(s), nil
	}, func(v Value) (Node, error) {
		s, err := v.AsString()
		if err != nil {
			return Node{}, err
		}
		var head *Node
		var tail *Node
		for _, part := range splitOn(s, '>') {
			n := &Node{Name: part}
			if head == nil {
				head, tail = n, n
			} else {
				tail.Next = n
				tail = n
			}
		}
		return *head, nil
	})
	// An INTERFACE type: the codec owns the discriminator inside its own text.
	register(func(a net.Addr) (Value, error) {
		if a == nil {
			return Null(), nil
		}
		return String(a.Network() + "://" + a.String()), nil
	}, func(v Value) (net.Addr, error) {
		if v.Kind() == VNull {
			return nil, nil
		}
		s, err := v.AsString()
		if err != nil {
			return nil, err
		}
		parts := splitOn(s, ':')
		return net.ResolveTCPAddr(parts[0], s[len(parts[0])+3:])
	})

	fmt.Println("\n    after registration:")
	tryRT("netip.Addr", reflect.TypeFor[netip.Addr](), reflect.ValueOf(netip.MustParseAddr("192.0.2.1")))
	tryRT("big.Int", reflect.TypeFor[big.Int](), reflect.ValueOf(*big.NewInt(1 << 40)))
	tryRT("Node (recursive)", reflect.TypeFor[Node](), reflect.ValueOf(Node{"a", &Node{"b", nil}}))
	tryRT("net.Addr (interface)", reflect.TypeFor[net.Addr](),
		reflect.ValueOf(&net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80}).Convert(reflect.TypeFor[net.Addr]()))
	tryRT("map[netip.Addr]string", reflect.TypeFor[map[netip.Addr]string](),
		reflect.ValueOf(map[netip.Addr]string{netip.MustParseAddr("10.0.0.1"): "a"}))
}

func splitOn(s string, sep byte) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	return append(out, cur)
}
