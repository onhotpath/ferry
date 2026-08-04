package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// absentPlane is MemPlane with one change: every container address answers
// Absent, whatever the plane was handed. It is a driver written to case 3's
// letter, and the question is whether #83's suite notices.
func absentPlane() ferrytest.Plane {
	p := ferrytest.MemPlane()
	inner := p.Open
	p.Name = "memory, answering Absent at every container address"
	p.Open = func() ferrytest.Instance {
		inst := inner()
		inst.Source = allAbsent{inner: inst.Source}

		return inst
	}

	return p
}

// allAbsent reports Absent at any address that has children or that is one of
// the suite's own container addresses.
type allAbsent struct{ inner ferry.Source }

func (s allAbsent) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return allAbsentReader{inner: r}, nil
	}, nil
}

type allAbsentReader struct{ inner ferry.Reader }

// Get answers Absent at the suite's container addresses and passes everything
// else through.
func (r allAbsentReader) Get(ctx context.Context, addr ferry.Path) (ferry.Value, error) {
	for _, name := range []string{"list", "map", "nillist", "emptymap", "section", "value"} {
		if addr == ferry.At(name) {
			v, err := r.inner.Get(ctx, addr)
			if err != nil || v.Kind() != ferry.KindNull {
				return v, err
			}

			return ferry.Value{}, nil
		}
	}

	return r.inner.Get(ctx, addr)
}

func (r allAbsentReader) Children(ctx context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	return r.inner.(ferry.Enumerator).Children(ctx, prefix)
}

// sec7 runs #83's suite against the memory plane and against a driver written
// to case 3's letter, and reports what each says.
func sec7() {
	head("7. What #83's Driver case 3 does today")

	for _, p := range []ferrytest.Plane{ferrytest.MemPlane(), absentPlane()} {
		sub(p.Name)

		c := &capture{}
		ferrytest.Driver(c, p)

		fails := 0

		for _, l := range c.lines {
			if strings.HasPrefix(l, "FAIL") {
				fails++

				fmt.Println("  " + wrapAt(l, 108))
			}
		}

		if fails == 0 {
			fmt.Println("  no case failed")
		}
	}

	sub("what tightening case 3 to exactly Null would report, against the same two")

	for _, p := range []ferrytest.Plane{ferrytest.MemPlane(), absentPlane()} {
		inst := p.Open()

		if err := ferry.Dump(context.Background(), blanks{EmptyMap: map[string]string{}}, inst.Sink); err != nil {
			fmt.Println("  dump:", err)

			continue
		}

		open, err := inst.Source.Bind(ferry.NewAddressSet(ferry.At("nillist"), ferry.At("emptymap")))
		if err != nil {
			fmt.Println("  bind:", err)

			continue
		}

		r, err := open(context.Background())
		if err != nil {
			fmt.Println("  open:", err)

			continue
		}

		for _, a := range []ferry.Path{ferry.At("nillist"), ferry.At("emptymap")} {
			v, _ := r.Get(context.Background(), a)
			verdict := "would pass"

			if v.Kind() != ferry.KindNull {
				verdict = "would FAIL"
			}

			fmt.Printf("  %-52s %-8s %s -> %s\n", p.Name, a.String(), show(v), verdict)
		}
	}
}

// blanks is #83's own fixture, restated here because ferrytest keeps it
// unexported.
type blanks struct {
	NilList  []string          `ferry:"nillist"`
	EmptyMap map[string]string `ferry:"emptymap"`
	Section  *blankSection     `ferry:"section"`
}

type blankSection struct {
	Name string `ferry:"name"`
}
