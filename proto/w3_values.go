package main

// W3: do natively typed values fit, or does ferry force a lossy trip through
// strings?
//
// This is #15's second question and the one the ticket was filed for. It runs
// against the fake here and against a real hive on the runner, through the same
// driver and the same ferry Load and Dump.

import (
	"context"
	"fmt"
	"time"
)

type WConf struct {
	Name    string        `ferry:"name"`
	Port    int           `ferry:"port"`
	Big     int64         `ferry:"big"`
	Neg     int           `ferry:"neg"`
	On      bool          `ferry:"on"`
	Blob    []byte        `ferry:"blob"`
	Timeout time.Duration `ferry:"timeout"`
}

func wRoundTrip(ctx context.Context, st wStore, base string, v WConf, numberAs uint32) (WConf, []string, error) {
	if err := Dump(ctx, v, WRegSink{Store: st, Base: base, NumberAs: numberAs}); err != nil {
		return WConf{}, nil, fmt.Errorf("dump: %w", err)
	}
	var shown []string
	if f, ok := st.(*wFake); ok {
		shown = f.dump()
	}
	got, err := Load[WConf](ctx, WRegSource{Store: st, Base: base}, WithSched(tAggregating))
	return got, shown, err
}

func runW3() {
	ctx := context.Background()
	base := `HKCU\Software\ferry-proto-15`
	orig := WConf{
		Name:    "svc",
		Port:    8080,
		Big:     1 << 40,
		Neg:     -1,
		On:      true,
		Blob:    []byte{0x00, 0xff, 0x41},
		Timeout: 30 * time.Second,
	}

	fmt.Println("(a) a full round trip through the driver, NumberAs=REG_DWORD")
	f := newFake()
	got, hive, err := wRoundTrip(ctx, f, base, orig, wDWORD)
	fmt.Println("    what the plane holds:")
	for _, l := range hive {
		fmt.Printf("      %s\n", l)
	}
	fmt.Printf("    load err : %v\n", errShortW(err))
	fmt.Printf("    original : %+v\n", orig)
	fmt.Printf("    loaded   : %+v\n", got)
	fmt.Println("    ONE field killed the whole load: ADR-0011 yields no value ferry built,")
	fmt.Println("    so a single un-loadable address makes every other field zero. Dropping")
	fmt.Println("    the bool from the type, the rest round-trips exactly:")
	fb := newFake()
	origNB := WNoBool{Name: orig.Name, Port: orig.Port, Big: orig.Big, Neg: orig.Neg, Blob: orig.Blob, Timeout: orig.Timeout}
	_ = Dump(ctx, origNB, WRegSink{Store: fb, Base: base, NumberAs: wDWORD})
	gotNB, errNB := Load[WNoBool](ctx, WRegSource{Store: fb, Base: base}, WithSched(tAggregating))
	fmt.Printf("      err=%v  equal=%v\n", errShortW(errNB), fmt.Sprint(origNB) == fmt.Sprint(gotNB))
	fmt.Printf("      %+v\n", gotNB)

	fmt.Println("\n(b) FINDING 1: a Go bool cannot round-trip through this plane")
	fmt.Println("    The Registry has no boolean type. Every convention in the wild is a")
	fmt.Println("    REG_DWORD holding 0 or 1, which is what this driver writes.")
	fmt.Println("    On the way back that is a Number, and ADR-0005 is explicit: \"Every")
	fmt.Println("    leaf accepts its own kind. Every leaf additionally accepts String...")
	fmt.Println("    Nothing else coerces.\" A Go bool accepts Bool and String and refuses")
	fmt.Println("    Number, deliberately, because accepting it would be ferry overriding")
	fmt.Println("    a plane's own type information.")
	fmt.Println("    So the driver has exactly two honest choices and both are bad:")
	for _, c := range []struct {
		what string
		v    wVal
	}{
		{"REG_DWORD 1, the Registry convention", wVal{typ: wDWORD, n: 1}},
		{"REG_SZ \"true\",  ferry's convention  ", wVal{typ: wSZ, s: "true"}},
		{"REG_SZ \"1\",     the other spelling  ", wVal{typ: wSZ, s: "1"}},
	} {
		fx := newFake()
		_ = fx.SetValue(base, "on", c.v)
		bv, _ := wReadOne(ctx, fx, base, "on")
		got, err := Load[WBoolConf](ctx, WRegSource{Store: fx, Base: base}, WithSched(tAggregating))
		fmt.Printf("      %s -> boundary %-14s field On=%-5v  load: %v\n",
			c.what, bv.GoString(), got.On, errShortW(err))
	}
	fmt.Println("    The first is what every other Windows program reads and ferry cannot")
	fmt.Println("    load. The second ferry round-trips and no other program recognises.")
	fmt.Println("    ADR-0001's driver fidelity is \"Load from a plane, Dump back, and the")
	fmt.Println("    plane still means the same thing\", and here the two directions")
	fmt.Println("    disagree about what the plane means in the first place.")

	fmt.Println("\n(c) FINDING 2: Value's Number carries no WIDTH, so a Load-Dump cycle")
	fmt.Println("    rewrites the plane's own type")
	f4 := newFake()
	_ = f4.SetValue(base, "port", wVal{typ: wQWORD, n: 8080})
	_ = f4.SetValue(base, "name", wVal{typ: wSZ, s: "svc"})
	before, _, _ := f4.GetValue(base, "port")
	cfg, err := Load[WConf](ctx, WRegSource{Store: f4, Base: base}, WithSched(tAggregating))
	_ = err
	_ = Dump(ctx, cfg, WRegSink{Store: f4, Base: base, NumberAs: wDWORD})
	after, _, _ := f4.GetValue(base, "port")
	fmt.Printf("      the plane held : %s\n", before)
	fmt.Printf("      ferry read it  : Number(\"8080\")   - the width is gone\n")
	fmt.Printf("      ferry wrote    : %s\n", after)
	fmt.Println("    The VALUE survives exactly, so ADR-0001's value fidelity is intact.")
	fmt.Println("    The plane's own type does not, so driver fidelity is not: another")
	fmt.Println("    program reading that key with RegQueryValueEx and expecting a QWORD")
	fmt.Println("    now gets ERROR_UNSUPPORTED_TYPE, and nothing in ferry could have")
	fmt.Println("    noticed. ADR-0001 puts driver fidelity on the driver and backs it with")
	fmt.Println("    a conformance suite, and this is a case the suite as described would")
	fmt.Println("    not catch: it compares KEYS AND VALUES, and the value is equal.")

	fmt.Println("\n    A driver COULD read before writing and preserve the type. Priced:")
	fmt.Println("      it makes every Set a Get first, which is the round-trip amplification")
	fmt.Println("      survey item 5.13 is about, and it still has nothing to preserve on a")
	fmt.Println("      key that does not exist yet - which is every template and every first")
	fmt.Println("      run. So it narrows the hole and does not close it.")

	fmt.Println("\n(d) FINDING 3: a negative number has no Registry integer type at all")
	f5 := newFake()
	_ = Dump(ctx, WConf{Neg: -1, Port: 1}, WRegSink{Store: f5, Base: base, NumberAs: wDWORD})
	neg, _, _ := f5.GetValue(base, "neg")
	pos, _, _ := f5.GetValue(base, "port")
	fmt.Printf("      Neg=-1 -> %s\n", neg)
	fmt.Printf("      Port=1 -> %s\n", pos)
	fmt.Println("    One Go field, two plane types, decided by the VALUE rather than by the")
	fmt.Println("    type. That is a property no ADR anticipated: ADR-0005's golden column")
	fmt.Println("    pins a representation per Go type, and here the representation is a")
	fmt.Println("    function of the value. It round-trips, because ADR-0005 makes String")
	fmt.Println("    the universal donor, and it means a Registry key's type is unstable")
	fmt.Println("    across ordinary edits.")

	fmt.Println("\n(e) FINDING 4: REG_MULTI_SZ has no representation in the six kinds")
	fmt.Println("    W0 measured it on a real hive: [\"a\" \"b,c\" \"\"] round-trips through")
	fmt.Println("    the Win32 API exactly, INCLUDING the element containing a comma and")
	fmt.Println("    the empty element. It is a real, lossless, native list of strings at")
	fmt.Println("    ONE value name.")
	f6 := newFake()
	_ = f6.SetValue(base, "name", wVal{typ: wMULTI_SZ, ss: []string{"a", "b,c", ""}})
	_, err6 := Load[WNoBool](ctx, WRegSource{Store: f6, Base: base}, WithSched(tAggregating))
	fmt.Printf("      a plain string field reading one: %v\n", errShortW(err6))
	fmt.Println("      The driver hands it over as NUL-joined Bytes, which W6 establishes is")
	fmt.Println("      the only lossless shape available, and a string leaf refuses Bytes")
	fmt.Println("      because ADR-0005 coerces nothing but String. So it is loud, and the")
	fmt.Println("      user needs a named type and a registered codec to read it at all -")
	fmt.Println("      W6 measures the whole route and what it costs.")
	fmt.Println("      An EARLIER version of this driver returned an error here instead, and")
	fmt.Println("      that is how #15 found the walk DISCARDING a Reader's error and")
	fmt.Println("      substituting Absent: the driver saying \"I cannot express this type\"")
	fmt.Println("      was indistinguishable from a missing key and the field silently took")
	fmt.Println("      its zero value. That is survey item 5.11's shape inside ferry's own")
	fmt.Println("      walk, and ADR-0001 rules it out by architecture.")
	fmt.Println("    ADR-0004 closed the value model at six kinds with NO GROUP ARM, on the")
	fmt.Println("    argument that \"under a structured address a composite gets one address")
	fmt.Println("    per element, so nothing ever asks the plane for the value AT /servers\",")
	fmt.Println("    and it named the remaining case as a flat plane holding a whole list in")
	fmt.Println("    one value, arriving as String(\"a,b,c\") for a codec to split.")
	fmt.Println("    REG_MULTI_SZ is not that case. It is not a delimited string: the")
	fmt.Println("    elements are separately delimited by the plane itself, so there is")
	fmt.Println("    nothing for a codec to split and no delimiter to choose.")
	fmt.Println("    The four available answers, and what each costs:")
	fmt.Println("      refuse the type            - a hive containing one is unloadable")
	fmt.Println("      (W6 takes the third, and measures that it needs a driver option too)")
	fmt.Println("      join with a separator      - lossy, and it is 5.10 reintroduced at the")
	fmt.Println("                                   driver: [\"a\" \"b,c\"] and [\"a\" \"b\" \"c\"]")
	fmt.Println("                                   become one string")
	fmt.Println("      join with NUL as Bytes     - lossless, and it makes every consumer")
	fmt.Println("                                   register a codec to read a plain []string")
	fmt.Println("      map it to Index addresses  - what ferry WANTS, and the boundary has no")
	fmt.Println("                                   shape for it: Get is per address, and")
	fmt.Println("                                   MULTI_SZ lives at one")
	fmt.Println("    ADR-0004 calls the missing escape arm \"the weakest call in this ADR\"")
	fmt.Println("    and gives v0 as the whole mitigation. This is the first measured plane")
	fmt.Println("    that needs one.")

	fmt.Println("\n(f) FINDING 5: REG_EXPAND_SZ's type is lost on a round trip")
	f7 := newFake()
	_ = f7.SetValue(base, "name", wVal{typ: wEXPAND_SZ, s: `%SystemRoot%\x`})
	cfg7, _ := Load[WConf](ctx, WRegSource{Store: f7, Base: base}, WithSched(tAggregating))
	_ = Dump(ctx, cfg7, WRegSink{Store: f7, Base: base, NumberAs: wDWORD})
	after7, _, _ := f7.GetValue(base, "name")
	fmt.Printf("      before: REG_EXPAND_SZ(%q)\n", `%SystemRoot%\x`)
	fmt.Printf("      after : %s\n", after7)
	fmt.Println("    The text survives and the semantics do not: REG_EXPAND_SZ tells every")
	fmt.Println("    Windows reader to expand the variable, and REG_SZ tells it not to.")
	fmt.Println("    This is the DWORD/QWORD finding again with a different pair, and it is")
	fmt.Println("    the same cause: Value carries a kind from ferry's six and not the")
	fmt.Println("    plane's own type, so any distinction finer than the six is dropped on")
	fmt.Println("    read and invented on write.")

	fmt.Println("\n(g) what DOES fit, which is most of it")
	fmt.Println("      REG_SZ     <-> String   exact")
	fmt.Println("      REG_BINARY <-> Bytes    exact, including a NUL and a non-UTF-8 byte")
	fmt.Println("      REG_DWORD/QWORD -> Number  exact as a VALUE, lossy as a TYPE")
	fmt.Println("      REG_NONE   <-> Null     exact, and it is the plane's own null")
	fmt.Println("    So the answer to \"do natively typed values fit\" is: the VALUES fit and")
	fmt.Println("    the TYPES do not, and ferry never forces a trip through strings except")
	fmt.Println("    where the plane has no integer type for the number in hand.")
}

// wReadOne loads a single-field view so a probe can see one address's fate.
func wReadOne(ctx context.Context, st wStore, base, name string) (Value, error) {
	r := &wRegReader{store: st, kf: wRegKey{base: base}}
	v, err := r.Get(ctx, path(name))
	if err != nil {
		return Absent, err
	}
	var out WConf
	_ = out
	return v, nil
}
