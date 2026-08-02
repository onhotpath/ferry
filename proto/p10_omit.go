package main

// P10: is the chain invoked for a zero value, and where does omission sit
// relative to it?
//
// #8 owns whether a zero or defaulted field is dumped at all; #12 owns what
// converts it. Whoever lands second states the composed order, so measure the
// thing that decides it: do "zero in Go" and "empty on the plane" agree?

import (
	"fmt"
	"net/netip"
	"reflect"
	"time"
)

// nonZeroToEmpty is the reverse hazard: a value the user deliberately set,
// whose encoded form is empty.
type nonZeroToEmpty struct{ Explicit bool }

func (v nonZeroToEmpty) MarshalText() ([]byte, error) {
	if v.Explicit {
		return nil, nil // deliberately empty, and deliberately set
	}
	return []byte("unset"), nil
}
func (v *nonZeroToEmpty) UnmarshalText(b []byte) error {
	v.Explicit = len(b) == 0
	return nil
}

func runOmit() {
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	fmt.Println("\n--- P10a: does 'zero in Go' agree with 'empty on the plane'? ---")
	fmt.Printf("    %-20s %-10s %s\n", "type", "Go zero?", "what the zero value encodes to")
	fmt.Println("    " + dashes(76))
	for _, r := range []struct {
		name string
		v    any
	}{
		{"time.Time", time.Time{}},
		{"time.Duration", time.Duration(0)},
		{"netip.Addr", netip.Addr{}},
		{"string", ""},
		{"int", 0},
		{"nonZeroToEmpty(set)", nonZeroToEmpty{Explicit: true}},
	} {
		rv := reflect.ValueOf(r.v)
		val, err := encLeaf(rv)
		z := rv.IsZero()
		fmt.Printf("    %-20s %-10v %s%s\n", r.name, z, val.GoString(), errStr(err))
	}
	fmt.Println("    ^ two rows disagree in opposite directions. time.Time's zero encodes")
	fmt.Println("      to a 20-byte timestamp, so an 'omit if the encoded form is empty'")
	fmt.Println("      rule would never omit it. nonZeroToEmpty is not the Go zero and")
	fmt.Println("      encodes to nothing, so the same rule would drop a value the user")
	fmt.Println("      explicitly set.")

	fmt.Println("\n--- P10b: so the composed order is forced ---")
	fmt.Println("    omitzero is defined in GO terms and is decided from the Go value,")
	fmt.Println("    before the codec runs. omitempty is defined in PLANE terms and needs")
	fmt.Println("    the encoded form, so it can only be decided after. ADR-0005 already")
	fmt.Println("    rejected omitempty ('there is no empty JSON object on a Consul")
	fmt.Println("    plane'), which removes the only rule that would have to run after.")
	fmt.Println()
	fmt.Println("    Order: #11's tag decides omission -> #8's default rule decides the")
	fmt.Println("    value -> #12's chain converts whatever survives. The chain is")
	fmt.Println("    therefore invoked for a zero value exactly when the field is dumped")
	fmt.Println("    at all, and never as the thing that decides whether it is.")
	fmt.Println()
	fmt.Println("    json/v2 makes the same split and says so: omitzero is evaluated")
	fmt.Println("    before the marshaler runs, and omitempty 'may require marshalling")
	fmt.Println("    then unwriting'. ferry does not need the second half.")

	fmt.Println("\n--- P10c: the zero value is what the chain sees most often ---")
	type conf struct {
		A netip.Addr `ferry:"a"`
		B time.Time  `ferry:"b"`
	}
	d, err := dump(reflect.ValueOf(conf{}))
	fmt.Printf("    dump of an all-zero struct: %s err=%v\n", fmtVals(d), err)
	var back conf
	fmt.Printf("    load it back: err=%v  a=%v b=%v\n", load(d, reflect.ValueOf(&back).Elem()),
		back.A, back.B)
	fmt.Println("    ^ so a codec must be total over its type INCLUDING the zero value,")
	fmt.Println("      and that is a value list every proof has to carry. ADR-0005")
	fmt.Println("      already requires the zero value in each core entry's list; this")
	fmt.Println("      extends the same requirement to a registered codec.")
}
