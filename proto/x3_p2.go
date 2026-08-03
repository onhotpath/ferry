package main

// X3-2. Does registration rescue them?
//
// This is the repo owner's own hypothesis and the most likely resolution.
// ADR-0005 states the mechanism:
//
//	A codec collapses a type to a leaf, and a leaf needs no address set.
//	classify consults the identity table before reflect.Kind, so a registered
//	type is a leaf, mints exactly one address, and is never walked.
//
// If that holds then ADR-0008's field rule never fires for a registered type,
// and ADR-0005's table is stale about the ROUTE rather than wrong about the
// OUTCOME. The test is end to end - register, dump, load, compare - on all
// three planes, because "compiles" is not "round-trips".

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
)

// x3Box is the field the codec has to reach. One tagged field, so any refusal
// is about T and not about the holder.
type x3Box[T any] struct {
	V T `ferry:"v"`
}

// x3Trip is one round trip through the ENTRY POINTS, on a real Source/Sink
// pair. Not dumpTo/loadFrom: #41 item 6's whole point is that a harness that
// does not go through Dump and Load is not measuring the engine.
func x3Trip[T any](reg *Registry, pl Plane, in T) (out T, dumpErr, loadErr error) {
	ctx := context.Background()
	src, sink := pl.Open()
	if err := Dump(ctx, x3Box[T]{in}, sink, WithRegistry(reg)); err != nil {
		return out, err, nil
	}
	box, err := Load[x3Box[T]](ctx, src, WithRegistry(reg))
	return box.V, nil, err
}

// x3Addr reports the address set a registered type mints, which is ADR-0005's
// "mints exactly one address, and is never walked" made checkable.
func x3Addrs[T any](reg *Registry) ([]Path, error) {
	o := defaultOpts()
	o.reg = reg
	s, err := compileOnce(reflect.TypeFor[x3Box[T]](), o)
	if err != nil {
		return nil, err
	}
	return s.addrs, nil
}

// --- the three codecs -------------------------------------------------------

// x3IPNetCodec: see X3-3 for why this is a ValueCodec and not the StringCodec
// the natural text form invites.
func x3IPNetCodec() Reg {
	return ValueCodec[net.IPNet](VString,
		func(n net.IPNet) (Value, error) {
			if n.IP == nil && n.Mask == nil {
				return String(""), nil
			}
			return String(n.String()), nil
		},
		func(v Value) (net.IPNet, error) {
			s, err := v.AsString()
			if err != nil {
				return net.IPNet{}, err
			}
			if s == "" {
				return net.IPNet{}, nil
			}
			_, ipn, err := net.ParseCIDR(s)
			if err != nil {
				return net.IPNet{}, err
			}
			return *ipn, nil
		})
}

func x3TCPAddrCodec() Reg {
	return ValueCodec[net.TCPAddr](VString,
		func(a net.TCPAddr) (Value, error) { return String(a.String()), nil },
		func(v Value) (net.TCPAddr, error) {
			s, err := v.AsString()
			if err != nil {
				return net.TCPAddr{}, err
			}
			a, err := net.ResolveTCPAddr("tcp", s)
			if err != nil {
				return net.TCPAddr{}, err
			}
			return *a, nil
		})
}

// x3NullStringCodec is the one that needs the general constructor for a reason
// unrelated to the zero value: sql.NullString's whole point is a value that is
// present-and-null, and Null is a Value kind rather than a string.
func x3NullStringCodec() Reg {
	return ValueCodec[sql.NullString](VString,
		func(n sql.NullString) (Value, error) {
			if !n.Valid {
				return Null(), nil
			}
			return String(n.String), nil
		},
		func(v Value) (sql.NullString, error) {
			if v.Kind() == VNull {
				return sql.NullString{}, nil
			}
			s, err := v.AsString()
			if err != nil {
				return sql.NullString{}, err
			}
			return sql.NullString{String: s, Valid: true}, nil
		})
}

func runX3_2() {
	fmt.Println("ADR-0005's own mechanism sentence:")
	quoteX3(
		"A codec collapses a type to a leaf, and a leaf needs no address set.",
		"classify consults the identity table before reflect.Kind, so a registered type",
		"is a leaf, mints exactly one address, and is never walked.",
	)

	fmt.Println("--- X3-2a: the registration is accepted, and the field rule stops firing ---")
	reg := NewRegistry()
	for _, g := range []struct {
		label string
		r     Reg
	}{
		{"net.IPNet", x3IPNetCodec()},
		{"net.TCPAddr", x3TCPAddrCodec()},
		{"sql.NullString", x3NullStringCodec()},
	} {
		err := reg.Register(g.r)
		fmt.Printf("    Register(%-15s) -> %v\n", g.label, err)
	}

	ipa, ipe := x3Addrs[net.IPNet](reg)
	tca, tce := x3Addrs[net.TCPAddr](reg)
	nsa, nse := x3Addrs[sql.NullString](reg)
	fmt.Printf("\n    Compile[struct{ V net.IPNet      }] -> %v   addrs=%v\n", ipe, ipa)
	fmt.Printf("    Compile[struct{ V net.TCPAddr    }] -> %v   addrs=%v\n", tce, tca)
	fmt.Printf("    Compile[struct{ V sql.NullString }] -> %v   addrs=%v\n", nse, nsa)
	fmt.Println("\n    One address each, and it is the FIELD's address /v - not /v/IP and")
	fmt.Println("    /v/Mask. resolveLeaf calls identityLookup before it reaches the struct")
	fmt.Println("    arm at all, so parseTag is never called on IP or Mask and ADR-0008's")
	fmt.Println("    field rule has nothing to fire on. The mechanism sentence holds.")

	fmt.Println("\n--- X3-2b: end to end, on three planes ---")
	fmt.Println("    Dump then Load through the entry points. A refusal on the flat plane")
	fmt.Println("    is ADR-0005's kind declaration working, not a failure: flatKinds")
	fmt.Println("    omits VNull and the driver says so loudly.")
	dir, _ := os.MkdirTemp("", "x3")
	defer os.RemoveAll(dir)
	planes := []Plane{memoryPlane(), yamlPlane(dir), flatPlane()}

	_, ipn, _ := net.ParseCIDR("10.0.0.0/8")
	tcp := net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80}

	for _, pl := range planes {
		fmt.Printf("\n    %s\n", pl.Name)
		x3Report(reg, pl, "net.IPNet 10.0.0.0/8", *ipn, func(a, b net.IPNet) bool { return a.String() == b.String() },
			func(v net.IPNet) string { return v.String() })
		x3Report(reg, pl, "net.IPNet zero", net.IPNet{}, func(a, b net.IPNet) bool { return a.String() == b.String() },
			func(v net.IPNet) string { return v.String() })
		x3Report(reg, pl, "net.TCPAddr 192.0.2.1:80", tcp, func(a, b net.TCPAddr) bool { return a.String() == b.String() },
			func(v net.TCPAddr) string { return v.String() })
		x3Report(reg, pl, "net.TCPAddr zero", net.TCPAddr{}, func(a, b net.TCPAddr) bool { return a.String() == b.String() },
			func(v net.TCPAddr) string { return v.String() })
		x3Report(reg, pl, "sql.NullString valid", sql.NullString{String: "x", Valid: true},
			func(a, b sql.NullString) bool { return a == b },
			func(v sql.NullString) string { return fmt.Sprintf("%q/%v", v.String, v.Valid) })
		x3Report(reg, pl, "sql.NullString null", sql.NullString{},
			func(a, b sql.NullString) bool { return a == b },
			func(v sql.NullString) string { return fmt.Sprintf("%q/%v", v.String, v.Valid) })
	}

	fmt.Println("\n--- X3-2c: what the plane actually holds ---")
	fmt.Println("    ADR-0005's table promises /IP and /Mask, two byte blobs. Under a")
	fmt.Println("    registration it is one address holding one string, which is a")
	fmt.Println("    different plane layout for the same Go type.")
	yp := yamlPlane(dir)
	src, sink := yp.Open()
	ctx := context.Background()
	if err := Dump(ctx, x3Box[net.IPNet]{*ipn}, sink, WithRegistry(reg)); err != nil {
		fmt.Println("    dump err:", err)
	}
	if fs, ok := src.(FYAMLSource); ok {
		b, _ := os.ReadFile(fs.Path)
		fmt.Printf("    --- %s ---\n", fs.Path)
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			fmt.Printf("      %s\n", l)
		}
	}
	fmt.Println("    ADR-0005's published row for the SAME type, unregistered:")
	quoteX3("| net.IPNet | **admitted, round-trips** | `/IP` and `/Mask`, two byte blobs |")
	fmt.Println("    Both are true statements about ferry. Neither is true of the other's")
	fmt.Println("    route, and the table has no column that says which route it is on.")
}

// x3Report runs one value and prints the outcome, including whether the plane
// refused a kind it had already declared it could not carry.
func x3Report[T any](reg *Registry, pl Plane, label string, in T, eq func(a, b T) bool, show func(T) string) {
	out, de, le := x3Trip(reg, pl, in)
	switch {
	case de != nil:
		k, refused := refusedKind(de)
		note := "dump refused"
		if refused {
			note = fmt.Sprintf("dump refused: plane cannot carry %v (declared)", k)
		}
		fmt.Printf("      %-26s %-9s %v\n", label, note, x3One(de))
	case le != nil:
		fmt.Printf("      %-26s %-9s %v\n", label, "load err", x3One(le))
	case eq(in, out):
		fmt.Printf("      %-26s %-9s %s\n", label, "round-trip", show(out))
	default:
		fmt.Printf("      %-26s %-9s %s -> %s\n", label, "MISMATCH", show(in), show(out))
	}
}

// x3One shows the FIRST element of a refusal, which is what a reader scanning a
// table wants. It goes through errLines because a schema refusal is a ferry
// aggregate since #41 D8's compiler half, and Error() on one is a summary.
func x3One(err error) string {
	ls := errLines(err)
	switch len(ls) {
	case 0:
		return "<nil>"
	case 1:
		return ls[0]
	}
	return ls[0] + " (+more)"
}
