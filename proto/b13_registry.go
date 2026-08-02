package main

// B13: #15's question, asked of the value model rather than of a driver.
//
// #15 asks whether the interfaces hold against "a genuinely non-file, typed,
// hierarchical plane", and names the Windows Registry. Two of its three
// sub-questions can be answered from the six kinds alone, before anyone writes
// the driver, because they are questions about whether a plane's own type
// system fits inside ADR-0004's closed set.

import "fmt"

// The Registry's value types, and what ADR-0004's six kinds can carry.
type b13RegType struct {
	name     string
	sample   string
	ferryVal func() Value
	distinct bool // does ferry's kind distinguish it from its neighbour?
	note     string
}

func runB13() {
	fmt.Println("--- B13a: the Registry's own type system against ADR-0004's six kinds ---")
	rows := []b13RegType{
		{"REG_SZ", `"C:\\svc"`, func() Value { return String(`C:\svc`) }, false,
			"String"},
		{"REG_EXPAND_SZ", `"%ProgramFiles%\\svc"`, func() Value { return String(`%ProgramFiles%\svc`) }, false,
			"String - SAME KIND as REG_SZ, and the difference is whether the plane expands it"},
		{"REG_DWORD", "8080", func() Value { return Number("8080") }, false,
			"Number"},
		{"REG_QWORD", "8080", func() Value { return Number("8080") }, false,
			"Number - SAME KIND as REG_DWORD, and the difference is the width on disk"},
		{"REG_BINARY", "00 ff 41", func() Value { return Bytes([]byte{0, 0xff, 'A'}) }, true,
			"Bytes"},
		{"REG_MULTI_SZ", `["a","b"]`, func() Value { return String("a\x00b") }, true,
			"no kind at all: a LIST inside one value, and ADR-0004 removed the group arm"},
	}
	fmt.Printf("  %-14s %-24s %-12s %s\n", "Registry type", "sample", "ferry kind", "note")
	for _, r := range rows {
		fmt.Printf("  %-14s %-24s %-12s %s\n", r.name, r.sample, r.ferryVal().Kind().String(), r.note)
	}

	fmt.Println("\n--- B13b: what that costs on the DUMP half, which is #15's third question ---")
	fmt.Println("    A Value is {kind, text}. It carries no plane-side type, so a sink")
	fmt.Println("    handed Number(\"8080\") cannot know whether the value it is replacing")
	fmt.Println("    was a DWORD or a QWORD, and a sink handed String(...) cannot know")
	fmt.Println("    whether it was REG_SZ or REG_EXPAND_SZ.")
	fmt.Println()
	fmt.Println("    Measured as a round trip, with the plane's type carried alongside:")
	for _, r := range rows[:4] {
		v := r.ferryVal()
		fmt.Printf("    %-14s -> %-18s -> writing back, the sink must choose: %s\n",
			r.name, v.GoString(), b13Choice(v))
	}
	fmt.Println()
	fmt.Println("    Three ways a driver can answer, none of them free:")
	fmt.Println("      (i)   read the existing value's type first - a read before every")
	fmt.Println("            write, and wrong for a key that does not exist yet")
	fmt.Println("      (ii)  pick one per kind - DWORD always, REG_SZ always - which is")
	fmt.Println("            lossy against a plane the driver did not create")
	fmt.Println("      (iii) carry the type in the ADDRESS rather than the value, which")
	fmt.Println("            ADR-0003 forbids: a segment is a Name or an Index")
	fmt.Println()
	fmt.Println("    ADR-0001 calls this driver fidelity and makes it the driver's")
	fmt.Println("    obligation. What B13a shows is that for this plane it is not")
	fmt.Println("    dischargeable from the Value alone, which no ADR has said.")

	fmt.Println("\n--- B13c: and REG_MULTI_SZ, which is the one with no answer at all ---")
	fmt.Println("    ADR-0004 removed the group arm on the argument that under a")
	fmt.Println("    structured address a composite gets one address per element, so")
	fmt.Println("    \"nothing ever asks the plane for the value AT /servers\".")
	fmt.Println("    A REG_MULTI_SZ is a list living at ONE address, in one value, in a")
	fmt.Println("    plane that is otherwise hierarchical - so the flat-plane escape")
	fmt.Println("    ADR-0004 offers (TAGS=a,b,c arrives as a scalar, a codec splits it)")
	fmt.Println("    applies to a plane ADR-0004 classified as NOT flat.")
	fmt.Println("    That is not a contradiction, and it is a case the ADR's own")
	fmt.Println("    plane taxonomy has no row for: hierarchical AND holding composites")
	fmt.Println("    inside single values.")
}

func b13Choice(v Value) string {
	switch v.Kind() {
	case VNumber:
		return "DWORD or QWORD, and the Value does not say"
	case VString:
		return "REG_SZ or REG_EXPAND_SZ, and the Value does not say"
	}
	return "unambiguous"
}
