package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// sectionReach measures whether Codec ever touches the registrant's own codec,
// by counting calls and by asking the registry whether it was ever compiled
// against.
func sectionReach() {
	var c counts

	reg := ferry.NewRegistry()
	if err := reg.Register(countingCodec(&c)); err != nil {
		fmt.Println("register:", err)

		return
	}

	fmt.Println("-- the registrant's own codec, counted")
	fmt.Printf("  after Register           %s\n", c)

	rec := &capture{}
	ferrytest.Codec(rec, reg)

	fmt.Printf("  after ferrytest.Codec    %s\n", c)
	rec.report("ferrytest.Codec(t, reg)")

	fmt.Println()
	fmt.Println("-- was reg ever handed to a verb? a registry freezes at its first retained compile")
	fmt.Printf("  Register(Feet) after Codec        -> %v\n", errText(reg.Register(feetCodec())))

	proof := ferrytest.Type("Meters", func(a, b Meters) bool { return a == b },
		ferrytest.At(Meters(0), ferry.String("0")),
		ferrytest.At(Meters(1.0/3.0), ferry.String("0.3333333333333333")),
	)

	trip := &capture{}
	ferrytest.RoundTrip(trip, ferrytest.MemPlane(), []ferrytest.Proof{proof}, ferry.WithRegistry(reg))

	fmt.Printf("  Register(Yards) after RoundTrip   -> %v\n", errText(reg.Register(yardsCodec())))
	fmt.Println()
	fmt.Printf("  after ferrytest.RoundTrip %s\n", c)
	trip.report("ferrytest.RoundTrip(t, MemPlane(), proofs, WithRegistry(reg))")

	fmt.Println()
	fmt.Println("-- and the same suite handed no registry at all")

	nilRun := &capture{}
	ferrytest.Codec(nilRun, nil)
	nilRun.report("ferrytest.Codec(t, nil)")

	broken := ferry.NewRegistry()
	if err := broken.Register(driftingCodec(), foldingKey(), lossyMeters()); err != nil {
		fmt.Println("  register:", err)

		return
	}

	badRun := &capture{}
	ferrytest.Codec(badRun, broken)
	badRun.report("ferrytest.Codec(t, a registry of three wrong codecs)")
}

// feetCodec and yardsCodec are two more correct registrations, used only to ask
// whether the registry is still open.
func feetCodec() ferry.Reg {
	return ferry.StringCodec(
		func(f Feet) string { return fmt.Sprintf("%g", float64(f)) },
		func(s string) (Feet, error) { var f float64; _, err := fmt.Sscanf(s, "%g", &f); return Feet(f), err },
	)
}

func yardsCodec() ferry.Reg {
	return ferry.StringCodec(
		func(y Yards) string { return fmt.Sprintf("%g", float64(y)) },
		func(s string) (Yards, error) { var f float64; _, err := fmt.Sscanf(s, "%g", &f); return Yards(f), err },
	)
}

// errText renders an error for a table row.
func errText(err error) string {
	if err == nil {
		return "nil (accepted, so no schema had been compiled against reg)"
	}

	return "refused: " + wrap(oneLine(err.Error()))
}
