package main

// B11: auditing ADR-0001's justification for exporting no schema view.
//
// ADR-0001: "Dump a zero-valued or defaulted struct into a sink that records
// what it sees, and you have every mapped key AND ITS GO TYPE without touching
// a plane. Core therefore exports no schema view."
//
// That sentence is load-bearing three times over: it is why schema extraction
// is Enabled rather than In core, it is why ADR-0010 declined to reopen the
// schema view, and it is why ADR-0012's Compile[T]() error keeps its signature.
// It has never been run.

import (
	"context"
	"fmt"
	"net/netip"
	"time"
)

type B11Conf struct {
	Name    string        `ferry:"name"`
	Port    int           `ferry:"port,default=8080"`
	Timeout time.Duration `ferry:"timeout,default=30s"`
	Ratio   float64       `ferry:"ratio"`
	On      bool          `ferry:"on"`
	Listen  netip.Addr    `ferry:"listen"`
	Secret  []byte        `ferry:"secret"`
	Started time.Time     `ferry:"started"`
}

func runB11() {
	ctx := context.Background()

	fmt.Println("--- B11a: what a recording sink actually receives ---")
	rec, err := dumpTo(ctx, B11Conf{})
	if err != nil {
		fmt.Println("  dump:", err)
		return
	}
	fmt.Printf("  %-12s %-16s %-22s %s\n", "address", "Go type", "what the sink sees", "recoverable?")
	want := map[string]string{
		"/name": "string", "/port": "int", "/timeout": "time.Duration",
		"/ratio": "float64", "/on": "bool", "/listen": "netip.Addr",
		"/secret": "[]byte", "/started": "time.Time",
	}
	for _, p := range sortedAddrs(rec) {
		v := rec[p]
		fmt.Printf("  %-12s %-16s %-22s %s\n", p, want[p.String()], v.GoString(), b11Verdict(want[p.String()], v))
	}

	fmt.Println("\n--- B11b: the distinct Go types, and the kinds they collapse to ---")
	kinds := map[string][]string{}
	for _, p := range sortedAddrs(rec) {
		k := rec[p].Kind().String()
		kinds[k] = append(kinds[k], want[p.String()])
	}
	for _, k := range []string{"string", "number", "bool", "bytes"} {
		if ts, ok := kinds[k]; ok {
			fmt.Printf("  %-8s <- %v\n", k, ts)
		}
	}
	fmt.Println("  8 Go types, 4 boundary kinds. The sink cannot tell time.Duration")
	fmt.Println("  from netip.Addr from string: ADR-0007's chain collapses all three")
	fmt.Println("  to a leaf BEFORE the sink is called, which is what a codec IS.")

	fmt.Println("\n--- B11c: and the defaults, which is the half that does hold ---")
	fmt.Println("  A defaulted zero struct carries its declared defaults, because")
	fmt.Println("  ADR-0006 makes a defaulted field dumped:")
	for _, p := range sortedAddrs(rec) {
		if s := p.String(); s == "/port" || s == "/timeout" {
			fmt.Printf("    %-10s %s\n", p, rec[p].GoString())
		}
	}

	fmt.Println("\n--- B11d: and the required markers, which hold via the OTHER half ---")
	type B11Req struct {
		Host string `ferry:"host,required"`
		Opt  string `ferry:"opt"`
	}
	_, lerr := loadFrom(ctx, B11Req{}, map[Path]Value{})
	fmt.Printf("    Load against an empty plane -> %v\n", lerr)
	fmt.Println("    so \"deployment validation is Load plus reading the error set\" holds,")
	fmt.Println("    and ADR-0011's Address() is what makes the set machine-readable.")
}

func b11Verdict(goType string, v Value) string {
	switch v.Kind() {
	case VString:
		if goType == "string" {
			return "yes, by luck"
		}
		return "NO"
	case VNumber:
		if goType == "int" || goType == "float64" {
			return "int or float, not which"
		}
		return "NO"
	case VBool, VBytes:
		return "kind only"
	}
	return "-"
}

// runB12 chases what B11c turned up: the declared defaults are NOT in a dump of
// a zero struct, because ADR-0006 applies a declared default on LOAD when the
// plane reports Absent, and a Dump has the Go value in hand.
//
// ADR-0001's sentence says "a zero-valued OR DEFAULTED struct", so the intended
// route must be Load-from-an-empty-plane first. That route has a precondition
// nobody has stated.
func runB12() {
	ctx := context.Background()
	empty := map[Path]Value{}

	fmt.Println("--- B12a: the two-step route, on a struct with no required field ---")
	type Tmpl struct {
		Host string `ferry:"host,default=localhost"`
		Port int    `ferry:"port,default=8080"`
	}
	seeded, err := loadFrom(ctx, Tmpl{}, empty)
	fmt.Printf("    Load from an EMPTY plane      -> %+v err=%v\n", seeded, err)
	rec, _ := dumpTo(ctx, seeded)
	fmt.Print("    then Dump into a recorder     -> ")
	for _, p := range sortedAddrs(rec) {
		fmt.Printf("%s=%s ", p, rec[p].GoString())
	}
	fmt.Println("\n    Works. This is the route ADR-0001 means by \"defaulted struct\".")

	fmt.Println("\n--- B12b: and the same struct with one required field ---")
	type TmplReq struct {
		Host string `ferry:"host,required"`
		Port int    `ferry:"port,default=8080"`
	}
	got, err2 := loadFrom(ctx, TmplReq{}, empty)
	fmt.Printf("    Load from an EMPTY plane      -> %+v\n                                     err=%v\n", got, err2)
	rec2, _ := dumpTo(ctx, got)
	fmt.Print("    then Dump into a recorder     -> ")
	for _, p := range sortedAddrs(rec2) {
		fmt.Printf("%s=%s ", p, rec2[p].GoString())
	}
	fmt.Println()
	fmt.Println("    The Load FAILS, so ADR-0011's \"ferry yields no value it built\" and")
	fmt.Println("    ADR-0010's \"LoadOver returns the seed\" both fire, and the value the")
	fmt.Println("    template generator needs is the one ferry is refusing to hand over.")
	fmt.Println("    /port's declared default 8080 never reaches the recorder.")

	fmt.Println("\n--- B12c: the struct that most needs a starter file is exactly this one ---")
	fmt.Println("    A field is `required` because the user MUST supply it, which is")
	fmt.Println("    precisely the field a starter config exists to prompt for.")
	fmt.Println("    So template generation - ADR-0001's first feature intended to ship,")
	fmt.Println("    bucketed Enabled on \"needs no new core surface\" - cannot template")
	fmt.Println("    a struct with a required field through the route ADR-0001 names.")
	fmt.Println()
	fmt.Println("    Not caused by any one ADR. It is the composition of three:")
	fmt.Println("      ADR-0006  a declared default is applied on LOAD, at Absent")
	fmt.Println("      ADR-0006  required is a presence test, and Absent fails it")
	fmt.Println("      ADR-0011  ferry yields no value it built when a Load fails")
	fmt.Println("    Each is right on its own and no ADR owns the composition.")
}
