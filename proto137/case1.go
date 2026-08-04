package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// sectionCase1 pins what case 1 catches and what it is silent about, and shows
// the one shape where it reports about methods ferry never calls.
func sectionCase1() {
	fmt.Println("-- a type whose two text spellings disagree, registered through TextCodec")

	reg := ferry.NewRegistry()
	fmt.Printf("  Register(TextCodec[Disagree](KindString)) -> %v\n",
		errOrNil(reg.Register(ferry.TextCodec[Disagree](ferry.KindString))))

	c := &capture{}
	ferrytest.Codec(c, reg)
	c.report("Codec")

	got, err := ferrytest.Record(t0(), Holder[Disagree]{Value: Disagree{n: 7}}, ferry.WithRegistry(reg))
	fmt.Printf("  and what ferry actually writes: %s err=%v\n", show(got), err)

	fmt.Println()
	fmt.Println("-- the same type, registered through StringCodec, so ferry never consults the text pair")

	reg2 := ferry.NewRegistry()
	fmt.Printf("  Register(StringCodec[Disagree](...))      -> %v\n", errOrNil(reg2.Register(ferry.StringCodec(
		func(d Disagree) string { return fmt.Sprintf("codec:%d", d.n) },
		func(s string) (Disagree, error) {
			var n int
			_, err := fmt.Sscanf(s, "codec:%d", &n)

			return Disagree{n: n}, err
		},
	))))

	c2 := &capture{}
	ferrytest.Codec(c2, reg2)
	c2.report("Codec")

	got2, err2 := ferrytest.Record(t0(), Holder[Disagree]{Value: Disagree{n: 7}}, ferry.WithRegistry(reg2))
	fmt.Printf("  and what ferry actually writes: %s err=%v\n", show(got2), err2)

	fmt.Println()
	fmt.Println("-- a registered type that declares no text pair at all")

	reg3 := ferry.NewRegistry()
	fmt.Printf("  Register(lossyMeters())                   -> %v\n", errOrNil(reg3.Register(lossyMeters())))

	c3 := &capture{}
	ferrytest.Codec(c3, reg3)
	c3.report("Codec")
}
