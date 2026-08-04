package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// sectionDecisive registers a codec that is genuinely wrong and survives
// registration, then asks Codec and a Proof about it side by side.
func sectionDecisive() {
	reg := ferry.NewRegistry()

	fmt.Println("-- Register, which runs the codec against the zero value")
	fmt.Printf("  Register(lossyMeters()) -> %v\n", errOrNil(reg.Register(lossyMeters())))
	fmt.Printf("  reg.Types()             -> %v\n", reg.Types())

	fmt.Println()
	fmt.Println("-- what the codec actually does, away from the zero value")

	one, err := ferrytest.Record(t0(), Holder[Meters]{Value: Meters(1.0 / 3.0)}, ferry.WithRegistry(reg))
	fmt.Printf("  Dump(Meters(1.0/3.0))   -> %s err=%v\n", show(one), err)

	back, err := ferry.Load[Holder[Meters]](t0(),
		ferrytest.Static(map[ferry.Path]ferry.Value{ferry.At("value"): ferry.String("0.33")}),
		ferry.WithRegistry(reg))
	fmt.Printf("  Load back               -> %v err=%v  (in 0.3333333333333333)\n", back.Value, err)

	fmt.Println()
	fmt.Println("-- ferrytest.Codec(t, reg), the suite handed exactly this registry")

	c := &capture{}
	ferrytest.Codec(c, reg)
	c.report("Codec")

	fmt.Println()
	fmt.Println("-- a Proof through RoundTrip, over the same registry and the same codec")

	proof := ferrytest.Type("Meters", func(a, b Meters) bool { return a == b },
		ferrytest.At(Meters(0), ferry.String("0")),
		ferrytest.At(Meters(1.0/3.0), ferry.String("0.3333333333333333")),
	)

	r := &capture{}
	ferrytest.RoundTrip(r, ferrytest.MemPlane(), []ferrytest.Proof{proof}, ferry.WithRegistry(reg))
	r.report("RoundTrip")
}

// errOrNil renders a registration outcome.
func errOrNil(err error) string {
	if err == nil {
		return "nil (accepted)"
	}

	return err.Error()
}
