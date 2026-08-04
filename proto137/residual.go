package main

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// sectionResidual runs every gate a registrant has against every class of wrong
// codec, and names what falls through all of them.
func sectionResidual() {
	fmt.Println("D1  not total over the zero value: netip.Addr through String and ParseAddr")
	d1()

	fmt.Println("\nD2  lossy: Meters formatted to two decimals")
	d2()

	fmt.Println("\nD3  constant: Meters always writes 0.00")
	d3()

	fmt.Println("\nD4  drifting kind: declares String, emits Number away from the zero value")
	d4()

	fmt.Println("\nD5  consistently wrong kind: digit text declared String and never drifting")
	d5()

	fmt.Println("\nD6  a key codec that is not injective, declared .AsMapKey()")
	d6()

	fmt.Println("\nD7  an interface codec that dereferences the nil interface")
	d7()

	fmt.Println("\nD8  lossy, with a proof whose values happen to be lossless")
	d8()
}

func d1() {
	reg := ferry.NewRegistry()
	err := reg.Register(ferry.StringCodec(
		netip.Addr.String,
		func(s string) (netip.Addr, error) { return netip.ParseAddr(s) },
	))
	fmt.Printf("  Register    %s\n", verdictErr(err))
}

func d2() { lossGates(lossyMeters(), Meters(1.0/3.0), "0.3333333333333333") }

func d3() { lossGates(constMeters(), Meters(2.5), "2.5") }

// lossGates runs the four gates that apply to a value codec over Meters.
func lossGates(g ferry.Reg, v Meters, golden string) {
	reg := ferry.NewRegistry()
	fmt.Printf("  Register    %s\n", verdictErr(reg.Register(g)))
	fmt.Printf("  Codec       %s\n", verdictSuite(func(c *capture) { ferrytest.Codec(c, reg) }))
	fmt.Printf("  Complete    %s\n", verdictList(ferrytest.Complete(reg, ferrytest.CoreTypes()...)))
	fmt.Printf("  a real Dump %s\n", verdictDump(reg, Holder[Meters]{Value: v}))
	fmt.Printf("  RoundTrip   %s\n", verdictSuite(func(c *capture) {
		ferrytest.RoundTrip(c, ferrytest.MemPlane(), []ferrytest.Proof{
			ferrytest.Type("Meters", func(a, b Meters) bool { return a == b },
				ferrytest.At(v, ferry.String(golden)),
			),
		}, ferry.WithRegistry(reg))
	}))
}

func d4() {
	reg := ferry.NewRegistry()
	fmt.Printf("  Register    %s\n", verdictErr(reg.Register(driftingCodec())))
	fmt.Printf("  Codec       %s\n", verdictSuite(func(c *capture) { ferrytest.Codec(c, reg) }))
	fmt.Printf("  a real Dump %s\n", verdictDump(reg, Holder[Drift]{Value: "x"}))
}

func d5() {
	reg := ferry.NewRegistry()
	fmt.Printf("  Register    %s\n", verdictErr(reg.Register(digitsAsString())))
	fmt.Printf("  Codec       %s\n", verdictSuite(func(c *capture) { ferrytest.Codec(c, reg) }))
	fmt.Printf("  a real Dump %s\n", verdictDump(reg, Holder[Digits]{Value: "42"}))
	fmt.Printf("  RoundTrip, golden says String %s\n", verdictSuite(func(c *capture) {
		ferrytest.RoundTrip(c, ferrytest.MemPlane(), []ferrytest.Proof{
			ferrytest.Type("Digits", func(a, b Digits) bool { return a == b },
				ferrytest.At(Digits("42"), ferry.String("42")),
			),
		}, ferry.WithRegistry(reg))
	}))
	fmt.Printf("  RoundTrip, golden says Number %s\n", verdictSuite(func(c *capture) {
		ferrytest.RoundTrip(c, ferrytest.MemPlane(), []ferrytest.Proof{
			ferrytest.Type("Digits", func(a, b Digits) bool { return a == b },
				ferrytest.At(Digits("42"), ferry.Number("42")),
			),
		}, ferry.WithRegistry(reg))
	}))
}

func d6() {
	reg := ferry.NewRegistry()
	fmt.Printf("  Register    %s\n", verdictErr(reg.Register(foldingKey())))
	fmt.Printf("  Codec       %s\n", verdictSuite(func(c *capture) { ferrytest.Codec(c, reg) }))
	fmt.Printf("  Injective   %s\n", verdictList(ferrytest.Injective(reg, Folding("Ab"), Folding("AB"))))
	fmt.Printf("  Injective, over one value only %s\n", verdictList(ferrytest.Injective(reg, Folding("Ab"))))
	fmt.Printf("  a real Dump of both keys %s\n",
		verdictDump(reg, Keyed[Folding]{Map: map[Folding]string{"Ab": "", "AB": ""}}))
	fmt.Printf("  a real Dump of one key   %s\n",
		verdictDump(reg, Keyed[Folding]{Map: map[Folding]string{"Ab": ""}}))
}

func d7() {
	reg := ferry.NewRegistry()

	fmt.Printf("  Register    %s\n", verdictPanic(func() error { return reg.Register(nilHostileAddr()) }))
}

func d8() {
	reg := ferry.NewRegistry()
	fmt.Printf("  Register    %s\n", verdictErr(reg.Register(lossyMeters())))
	fmt.Printf("  Codec       %s\n", verdictSuite(func(c *capture) { ferrytest.Codec(c, reg) }))
	fmt.Printf("  Complete    %s\n", verdictList(ferrytest.Complete(reg, append(ferrytest.CoreTypes(), meterProof())...)))
	fmt.Printf("  RoundTrip   %s\n", verdictSuite(func(c *capture) {
		ferrytest.RoundTrip(c, ferrytest.MemPlane(), []ferrytest.Proof{meterProof()}, ferry.WithRegistry(reg))
	}))
	fmt.Printf("  and the value the proof does not carry: Dump(1.0/3.0) -> ")

	got, err := ferrytest.Record(t0(), Holder[Meters]{Value: Meters(1.0 / 3.0)}, ferry.WithRegistry(reg))
	fmt.Printf("%s err=%v\n", show(got), err)
}

// meterProof is the proof a careful registrant might write, over values that a
// two-decimal format happens to represent exactly.
func meterProof() ferrytest.Proof {
	return ferrytest.Type("Meters", func(a, b Meters) bool { return a == b },
		ferrytest.At(Meters(0), ferry.String("0.00")),
		ferrytest.At(Meters(0.5), ferry.String("0.50")),
		ferrytest.At(Meters(0.25), ferry.String("0.25")),
		ferrytest.At(Meters(-1), ferry.String("-1.00")),
	)
}

// verdictErr renders whether an error gate fired.
func verdictErr(err error) string {
	if err == nil {
		return "passes"
	}

	return "CAUGHT: " + wrap(oneLine(err.Error()))
}

// verdictSuite runs a suite into a fresh capture and renders what it said.
func verdictSuite(run func(*capture)) string {
	c := &capture{}
	run(c)

	if len(c.lines) == 0 {
		return "silent"
	}

	return fmt.Sprintf("CAUGHT (%d): %s", len(c.lines), wrap(oneLine(c.lines[0])))
}

// verdictList renders a check that returns data rather than asserting.
func verdictList(out []string) string {
	if len(out) == 0 {
		return "silent"
	}

	return "CAUGHT: " + wrap(oneLine(strings.Join(out, " | ")))
}

// verdictDump runs a real dump of a real value, which is where core's own
// per-encode checks live.
func verdictDump[T any](reg *ferry.Registry, v T) string {
	got, err := ferrytest.Record(t0(), v, ferry.WithRegistry(reg))
	if err != nil {
		return "CAUGHT: " + wrap(oneLine(err.Error()))
	}

	return "silent, wrote " + show(got)
}

// verdictPanic reports a gate that fails by panicking rather than by erroring.
func verdictPanic(run func() error) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("PANICKED: %v", r)
		}
	}()

	return verdictErr(run())
}
