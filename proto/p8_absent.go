package main

// P8: is the chain invoked for Absent, for Null, and for the empty string?
//
// Survey item 5.9's last bullet is "decoders never see an empty input"
// (load.go:415-417). ferry must decide the same question, and the interesting
// part is that ferry can give the SAME answer for a different reason, because
// ADR-0004 made absent and empty two observations rather than one.

import (
	"context"
	"fmt"
	"reflect"

	"github.com/gojekfarm/xtools/xload"
)

// counted records every Value its decoder is handed, so "did the chain run"
// is measured rather than reasoned about.
type counted struct{ S string }

var countedSeen []string

func (v counted) MarshalText() ([]byte, error) { return []byte(v.S), nil }
func (v *counted) UnmarshalText(b []byte) error {
	countedSeen = append(countedSeen, fmt.Sprintf("%q", string(b)))
	v.S = "decoded:" + string(b)
	return nil
}

// xloadCounted is the same shape through xload's own Decoder interface.
type xloadCounted struct{ S string }

var xloadSeen []string

func (v *xloadCounted) Decode(s string) error {
	xloadSeen = append(xloadSeen, fmt.Sprintf("%q", s))
	v.S = "decoded:" + s
	return nil
}

func runAbsent() {
	fmt.Println("\n--- P8a: xload, reproduced: what its decoder is handed ---")
	type xconf struct {
		Set     xloadCounted `env:"SET"`
		Empty   xloadCounted `env:"EMPTY"`
		Missing xloadCounted `env:"MISSING"`
	}
	var xc xconf
	xloadSeen = nil
	err := xload.Load(context.Background(), &xc,
		xload.LoaderFunc(func(_ context.Context, key string) (string, error) {
			switch key {
			case "SET":
				return "v", nil
			case "EMPTY":
				return "", nil
			}
			return "", nil
		}))
	fmt.Printf("    decoder saw: %v   err=%v\n", xloadSeen, err)
	fmt.Printf("    result: Set=%q Empty=%q Missing=%q\n", xc.Set.S, xc.Empty.S, xc.Missing.S)
	fmt.Println("    ^ EMPTY and MISSING are one observation, so the decoder is called")
	fmt.Println("      for neither and both fields keep the zero value. The guard is")
	fmt.Println("      `if val == \"\" { return false, nil }` at load.go:415-417, and it is")
	fmt.Println("      forced: xload cannot tell an empty value from a missing key (5.1).")

	fmt.Println("\n--- P8b: ferry, the same three states, which the boundary can tell apart ---")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()
	type fconf struct {
		Set     counted `ferry:"set"`
		Empty   counted `ferry:"empty"`
		Missing counted `ferry:"missing"`
		Nulled  counted `ferry:"nulled"`
	}
	plane := map[Path]Value{
		mkPath("set"):    String("v"),
		mkPath("empty"):  String(""),
		mkPath("nulled"): Null(),
	}
	countedSeen = nil
	var fc fconf
	err = load(plane, reflect.ValueOf(&fc).Elem())
	fmt.Printf("    decoder saw: %v\n", countedSeen)
	fmt.Printf("    result: Set=%q Empty=%q Missing=%q Nulled=%q\n", fc.Set.S, fc.Empty.S, fc.Missing.S, fc.Nulled.S)
	fmt.Printf("    err: %v\n", err)
	fmt.Println("    ^ the decoder sees the empty string, which xload's cannot, and does")
	fmt.Println("      not see the missing address, which xload's cannot distinguish.")
	fmt.Println("      Null reaches the codec as itself and the codec refuses it, because")
	fmt.Println("      this codec declared kind String and String is the only donor.")

	fmt.Println("\n--- P8c: what a codec that WANTS Null looks like ---")
	fmt.Println("    ADR-0005's registered net.Addr codec returns Null for a nil")
	fmt.Println("    interface and accepts Null on the way back. So Null is not core's")
	fmt.Println("    to intercept: whatever kind a codec EMITS it must ACCEPT, and that")
	fmt.Println("    is checkable in the harness rather than by prose.")
	fmt.Println("    Absent is different in kind: it is the absence of an observation,")
	fmt.Println("    it carries no text a codec could rebuild anything from, and what it")
	fmt.Println("    means to a Go field is #8's, not each codec author's.")
}

func mkPath(name string) Path { return Path{}.Name(name) }
