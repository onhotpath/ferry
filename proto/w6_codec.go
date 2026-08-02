package main

// W6: can the REG_MULTI_SZ hole be closed with a registered codec and a named
// type, without touching ADR-0004's value model?
//
// This is the obvious next idea and it deserves running rather than reasoning.
// ADR-0005 states the mechanism that makes it plausible: "A codec collapses a
// type to a leaf, and a leaf needs no address set." So a named []string with a
// codec stops minting /deps#0 and /deps#1 and becomes ONE address, which is
// the shape the plane actually has.

import (
	"context"
	"fmt"
	"strings"
)

// WMultiSZ is the special type. In Go it is a []string; to ferry, once
// registered, it is a leaf.
type WMultiSZ []string

// The encoding is NUL-joined bytes. The format is NUL-separated at the Win32
// level, so an element cannot contain a NUL, which is what makes the join
// lossless rather than a delimiter choice.
func wMultiEnc(m WMultiSZ) (Value, error) {
	return Bytes([]byte(strings.Join(m, "\x00"))), nil
}

func wMultiDec(v Value) (WMultiSZ, error) {
	b, err := v.AsBytes()
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return WMultiSZ{}, nil
	}
	return WMultiSZ(strings.Split(string(b), "\x00")), nil
}

type WSvcPlain struct {
	Deps []string `ferry:"DependOnService"`
}

type WSvcCodec struct {
	Deps WMultiSZ `ferry:"DependOnService"`
}

func runW6() {
	ctx := context.Background()
	base := `Services\Acme`
	want := WMultiSZ{"RpcSs", "Tcpip", ""}

	fmt.Println("(a) the problem restated as two facts")
	fmt.Println("    A REG_MULTI_SZ is a LIST OF STRINGS at ONE value name.")
	fmt.Println("    ferry expresses a list as MANY ADDRESSES, and its boundary Value has")
	fmt.Println("    six kinds of which every one holds a single scalar.")
	fmt.Println("    So the driver has one Value slot and N strings to put in it.")

	fmt.Println("\n(b) what a plain []string does today, which is the write-side damage")
	h := newFake()
	_ = Dump(ctx, WSvcPlain{Deps: []string{"RpcSs", "Tcpip"}}, WRegSink{Store: h, Base: base})
	for _, l := range h.dump() {
		fmt.Printf("      %s\n", l)
	}
	fmt.Println("    A SUBKEY named DependOnService holding values named 0 and 1.")
	fmt.Println("    Windows expects one REG_MULTI_SZ value of that name, so the service")
	fmt.Println("    control manager does not see a dependency list at all.")

	fmt.Println("\n(c) the codec: does it collapse the type to one address?")
	reg := NewRegistry()
	if err := reg.Register(ValueCodec(VBytes, wMultiEnc, wMultiDec)); err != nil {
		fmt.Println("    register:", err)
		return
	}
	h2 := newFake()
	if err := Dump(ctx, WSvcCodec{Deps: want}, WRegSink{Store: h2, Base: base}, WithRegistry(reg)); err != nil {
		fmt.Println("    dump:", err)
	}
	for _, l := range h2.dump() {
		fmt.Printf("      %s\n", l)
	}
	back, err := Load[WSvcCodec](ctx, WRegSource{Store: h2, Base: base}, WithRegistry(reg), WithSched(tAggregating))
	fmt.Printf("    loaded back: %q err=%v\n", back.Deps, errShortW(err))
	fmt.Printf("    round trip equal: %v\n", fmt.Sprint(back.Deps) == fmt.Sprint(want))
	fmt.Println("    YES for ferry: one address, and the list round-trips exactly,")
	fmt.Println("    including the empty trailing element.")
	fmt.Println("    NO for the plane: the type is REG_BINARY, because the codec declares")
	fmt.Println("    Bytes and Bytes is what the driver writes. Every other program on the")
	fmt.Println("    machine reading that value gets ERROR_UNSUPPORTED_TYPE.")

	fmt.Println("\n(d) why the codec CANNOT close it on its own")
	fmt.Println("    The codec's whole output is a Value, which is a kind and text. The")
	fmt.Println("    kinds are Absent, Null, Bool, Number, String, Bytes. There is no kind")
	fmt.Println("    that means MULTI_SZ, so there is no way for the codec to tell the")
	fmt.Println("    driver what it produced.")
	fmt.Println("    And the encoding cannot be self-describing: NUL-joined bytes are")
	fmt.Println("    indistinguishable from a user's genuine binary blob that contains a")
	fmt.Println("    NUL, so a driver that guessed would corrupt the second case.")
	fmt.Println("    Declaring String instead does not help: a REG_SZ is NUL-terminated,")
	fmt.Println("    so it cannot carry an embedded NUL at the Win32 level at all.")

	fmt.Println("\n(e) so the missing half is a DRIVER option, and this is the measurement")
	fmt.Println("    the earlier write only guessed at")
	h3 := newFake()
	isMulti := func(p Path) bool { return strings.HasSuffix(p.String(), "/DependOnService") }
	if err := Dump(ctx, WSvcCodec{Deps: want},
		WRegSink{Store: h3, Base: base, MultiSZ: isMulti}, WithRegistry(reg)); err != nil {
		fmt.Println("    dump:", err)
	}
	for _, l := range h3.dump() {
		fmt.Printf("      %s\n", l)
	}
	back3, err3 := Load[WSvcCodec](ctx, WRegSource{Store: h3, Base: base}, WithRegistry(reg), WithSched(tAggregating))
	fmt.Printf("    loaded back: %q err=%v\n", back3.Deps, errShortW(err3))
	fmt.Printf("    round trip equal: %v\n", fmt.Sprint(back3.Deps) == fmt.Sprint(want))
	fmt.Println("    Now both halves hold: ferry sees one address and the plane holds a")
	fmt.Println("    REG_MULTI_SZ with the right elements.")
	fmt.Println("    ADR-0003 already makes the separator a driver option on exactly this")
	fmt.Println("    reasoning - flattening is the driver's - so this is the same shape and")
	fmt.Println("    needs no amendment to ADR-0004's value model.")

	fmt.Println("\n(f) what it costs the user, stated plainly")
	fmt.Println("    Three things, for what is in Go a []string:")
	fmt.Println("      1. define a named type          type MultiSZ []string")
	fmt.Println("      2. register a codec             ValueCodec(Bytes, enc, dec)")
	fmt.Println("      3. configure the driver         MultiSZ: <which addresses>")
	fmt.Println("    And the failure mode of doing none of them is silent on the plane:")
	fmt.Println("    a plain []string compiles, dumps, and round-trips through ferry")
	fmt.Println("    perfectly, while writing a shape no Windows consumer recognises.")
	fmt.Println("    Steps 2 and 3 also spell the same fact twice - once as a type and once")
	fmt.Println("    as an address predicate - which is the drift ADR-0006 measured against")
	fmt.Println("    a Static defaults source and #10 measured against a redaction table.")
}
