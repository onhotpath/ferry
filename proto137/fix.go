package main

import (
	"context"
	"fmt"
	"reflect"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// sectionFix runs the six cases against a registrant's own registered types
// through the prototype non-generic entry point, and reports which of them the
// reach actually rescues.
func sectionFix() {
	reg := ferry.NewRegistry()

	err := reg.Register(
		nilTolerantAddr(),
		lossyMeters(),
		driftingCodec(),
		foldingKey(),
	)
	if err != nil {
		fmt.Println("register:", err)

		return
	}

	fmt.Println("-- for each type the registry holds, walked from a reflect.Type alone")

	for _, t := range reg.Types() {
		fmt.Printf("\n  %s\n", t)
		fmt.Printf("    case 2, nil/zero encode  %s\n", zeroEncode(reg, t))
		fmt.Printf("    case 3, decode back      %s\n", zeroDecode(reg, t))
		fmt.Printf("    case 5, String donated   %s\n", donatedLoad(reg, t))
		fmt.Printf("    case 4, drift            %s\n", "NOT REACHABLE: needs a non-zero value of "+t.String())
		fmt.Printf("    case 6, two keys         %s\n", "NOT REACHABLE: needs two distinct values of "+t.String())
	}

	fmt.Println()
	fmt.Println("-- and the one thing it fixes about case 1: whether ferry uses the text pair for this type")

	for _, g := range []struct {
		how string
		reg ferry.Reg
	}{
		{"TextCodec[Disagree](KindString)", ferry.TextCodec[Disagree](ferry.KindString)},
		{"StringCodec[Disagree](...)", disagreeAsString()},
	} {
		r := ferry.NewRegistry()
		if err := r.Register(g.reg); err != nil {
			fmt.Println("   ", err)

			continue
		}

		dt := reflect.TypeFor[Disagree]()
		appended, _ := Disagree{}.AppendText(nil)
		fmt.Printf("  %-32s AppendText says %-12q ferry writes %s\n", g.how, appended, zeroEncode(r, dt))
	}
}

// disagreeAsString registers Disagree through a codec of its own, so the text
// pair it carries is never consulted.
func disagreeAsString() ferry.Reg {
	return ferry.StringCodec(
		func(d Disagree) string { return fmt.Sprintf("codec:%d", d.n) },
		func(s string) (Disagree, error) {
			var n int
			_, err := fmt.Sscanf(s, "codec:%d", &n)

			return Disagree{n: n}, err
		},
	)
}

// rootOf builds the annotated one-field struct a leaf has to travel in, at run
// time, which is what reflect.StructOf buys.
func rootOf(t reflect.Type) reflect.Type {
	return reflect.StructOf([]reflect.StructField{{
		Name: "Value",
		Type: t,
		Tag:  reflect.StructTag(`ferry:"value"`),
	}})
}

// zeroEncode dumps the zero value of a runtime type, which is Codec case 2's
// value for an interface registration and the only value core holds for any
// other.
func zeroEncode(reg *ferry.Registry, t reflect.Type) string {
	root := reflect.New(rootOf(t)).Elem()
	rec := newSpy()

	if err := ferry.DumpValue(t0(), root, rec, ferry.WithRegistry(reg)); err != nil {
		return "reached, and reported: " + wrap(oneLine(err.Error()))
	}

	return fmt.Sprintf("reached, wrote %#v", rec.seen[ferry.At("value")])
}

// zeroDecode loads that same encoding back into a fresh runtime-typed
// destination, which is Codec case 3.
func zeroDecode(reg *ferry.Registry, t reflect.Type) string {
	root := reflect.New(rootOf(t)).Elem()
	rec := newSpy()

	if err := ferry.DumpValue(t0(), root, rec, ferry.WithRegistry(reg)); err != nil {
		return "not reached: " + wrap(oneLine(err.Error()))
	}

	dst := reflect.New(rootOf(t)).Elem()
	if err := ferry.LoadValue(t0(), dst, ferrytest.Static(rec.seen), ferry.WithRegistry(reg)); err != nil {
		return "reached, and reported: " + wrap(oneLine(err.Error()))
	}

	return fmt.Sprintf("reached, loaded %#v", dst.Field(0).Interface())
}

// donatedLoad is Codec case 5 against the registrant's own codec: the same
// encoding, reported by a flat plane as a String.
func donatedLoad(reg *ferry.Registry, t reflect.Type) string {
	root := reflect.New(rootOf(t)).Elem()
	rec := newSpy()

	if err := ferry.DumpValue(t0(), root, rec, ferry.WithRegistry(reg)); err != nil {
		return "not reached: " + wrap(oneLine(err.Error()))
	}

	flat := map[ferry.Path]ferry.Value{}

	for addr, v := range rec.seen {
		flat[addr] = asFlat(v)
	}

	dst := reflect.New(rootOf(t)).Elem()
	if err := ferry.LoadValue(t0(), dst, ferrytest.Static(flat), ferry.WithRegistry(reg)); err != nil {
		return "reached, and reported: " + wrap(oneLine(err.Error()))
	}

	return fmt.Sprintf("reached, loaded %#v", dst.Field(0).Interface())
}

// asFlat is what a flat plane reports: everything but a Null comes back as a
// String, which is the donation case 5 is about.
func asFlat(v ferry.Value) ferry.Value {
	switch v.Kind() {
	case ferry.KindNull:
		return v
	case ferry.KindNumber:
		s, _ := v.AsNumber()

		return ferry.String(s)
	case ferry.KindBool:
		b, _ := v.AsBool()

		return ferry.String(fmt.Sprint(b))
	default:
		s, _ := v.AsString()

		return ferry.String(s)
	}
}

// nilTolerantAddr is a correct interface codec, so that case 2 and case 3 have
// something to succeed against.
func nilTolerantAddr() ferry.Reg {
	return ferry.ValueCodec[Addr](ferry.KindString,
		func(a Addr) (ferry.Value, error) {
			if a == nil {
				return ferry.Null(), nil
			}

			return ferry.String(a.Network()), nil
		},
		func(v ferry.Value) (Addr, error) {
			if v.Kind() == ferry.KindNull {
				return nil, nil
			}

			return udp{}, nil
		},
	)
}

// spy is a sink that keeps what it was handed, since ferrytest.Record takes a
// type parameter and this section has no type to give it.
type spy struct{ seen map[ferry.Path]ferry.Value }

func newSpy() *spy { return &spy{seen: map[ferry.Path]ferry.Value{}} }

func (s *spy) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return s, nil }, nil
}

func (s *spy) Set(_ context.Context, at ferry.Path, v ferry.Value) error {
	s.seen[at] = v

	return nil
}
