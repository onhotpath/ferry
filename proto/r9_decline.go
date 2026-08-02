package main

// R9: decline and fall through.
//
// The ticket asks for "the decline-and-fall-through mechanism, if any", and
// cites json/v2's SkipFunc. Both the ticket body and the research doc are
// wrong about how v2 spells it, so the mechanism is re-measured from source
// and by execution before ferry decides anything.
//
// Verified on go1.27rc2:
//   - grep -rn SkipFunc $GOROOT/src/encoding/json/  returns NOTHING.
//     The mechanism survives as an unexported `maySkip bool` at
//     arshal_funcs.go:95.
//   - The decline signal is errors.ErrUnsupported. JoinMarshalers' godoc:
//     "If a function returns [errors.ErrUnsupported], then the next
//     applicable function is called, otherwise the default marshaling
//     behavior is used."
//   - MarshalToFunc (the streaming shape) MAY decline: "It may return
//     [errors.ErrUnsupported] ... However, no mutable method calls may be
//     called on the encoder if [errors.ErrUnsupported] is returned."
//   - MarshalFunc (the func(T) ([]byte, error) shape) MAY NOT: "It may not
//     return [errors.ErrUnsupported]."
//
// Run, on go1.27rc2, in a module importing encoding/json/v2:
//
//   MarshalToFunc  {A:1}  -> "first:1"   err=<nil>
//   MarshalToFunc  {A:-1} -> "second:-1" err=<nil>      declined, fell through
//   MarshalFunc    {A:1}  -> "buf:1"     err=<nil>
//   MarshalFunc    {A:-1} ->             err=json: cannot marshal from Go
//                                        main.T: marshal function of type
//                                        func(T) ([]byte, error) may not
//                                        return errors.ErrUnsupported
//   all decline           -> {"A":-1}    err=<nil>      default behaviour
//
// So v2 permits declining exactly where declining is observably free, and
// refuses it on the shape that has already produced a buffer. ferry's codec
// produces a Value rather than writing to a stream, so "nothing has been
// written yet" is trivially true and v2's own constraint does not bind it.
// The question is therefore open on ferry's terms, and this probe asks it on
// ferry's terms.

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
)

type R9Backend struct {
	Host string
	Port int
}

// declining is the same registration with an extra return value: the codec
// may say "not mine" per VALUE, which is what a SkipFunc-shaped mechanism
// means once it is ported to a non-streaming boundary.
type decliningCodec struct {
	kind VKind
	enc  func(reflect.Value) (Value, error) // may return errors.ErrUnsupported
}

func runR9() {
	fmt.Println("--- R9a: a value-dependent claim makes the ADDRESS SET value-dependent ---")
	fmt.Println("    The one property every ADR since #4 leans on: the address set is")
	fmt.Println("    computable from reflect.TypeFor[T]() alone, with no value in hand.")
	fmt.Println("    A codec that may decline at runtime breaks it, and it is not subtle.")

	dc := decliningCodec{kind: VString, enc: func(v reflect.Value) (Value, error) {
		b := v.Interface().(R9Backend)
		if b.Port == 0 {
			return Value{}, errors.ErrUnsupported // "not mine"
		}
		return String(fmt.Sprintf("%s:%d", b.Host, b.Port)), nil
	}}

	type conf struct{ B R9Backend }
	// What compile can say, having no value:
	claimed := NewRegistry()
	_ = claimed.Register(StringCodec(
		func(b R9Backend) string { return fmt.Sprintf("%s:%d", b.Host, b.Port) },
		func(s string) (R9Backend, error) {
			var b R9Backend
			h, p, ok := strings.Cut(s, ":")
			if !ok {
				return b, fmt.Errorf("bad backend %q", s)
			}
			n, err := strconv.Atoi(p)
			b.Host, b.Port = h, n
			return b, err
		}))
	withRegistry(claimed, func() {
		a, _ := compile(reflect.TypeFor[conf]())
		fmt.Printf("    compile, codec claims the type   -> %v\n", a)
	})
	unclaimed, _ := compile(reflect.TypeFor[conf]())
	fmt.Printf("    compile, codec declines the type -> %v\n", unclaimed)

	fmt.Println("\n    and at Dump, with the same schema, the two values disagree:")
	for _, b := range []R9Backend{{"h", 80}, {"h", 0}} {
		v := reflect.ValueOf(conf{b}).Field(0)
		out, err := dc.enc(v)
		if errors.Is(err, errors.ErrUnsupported) {
			sub, _ := dump(v)
			fmt.Printf("      %+v declined -> mints %v\n", b, sortedAddrs(sub))
			continue
		}
		fmt.Printf("      %+v claimed  -> mints [/B] = %s\n", b, out.GoString())
	}
	fmt.Println("    ^ one type, one compiled schema, two address sets, chosen by a field")
	fmt.Println("      value. ADR-0004 hands the STATIC set to Bind before any I/O, so the")
	fmt.Println("      driver has precomputed a key table that is right for one of these")
	fmt.Println("      and wrong for the other, and ADR-0003's prefix-free check ran over a")
	fmt.Println("      set the walk is about to leave.")

	fmt.Println("\n--- R9b: and the LOAD direction has no answer at all ---")
	fmt.Println("    Decline on Dump at least has a value to decline about. On Load the")
	fmt.Println("    codec is handed a Value at an address the schema already committed to.")
	withRegistry(claimed, func() {
		var out conf
		err := load(map[Path]Value{Path{}.Name("B"): String("h:80")}, reflect.ValueOf(&out).Elem())
		fmt.Printf("    plane holds /B=string(\"h:80\"), codec claims  -> %+v err=%v\n", out.B, err)
	})
	var out conf
	err := load(map[Path]Value{
		Path{}.Name("B").Name("Host"): String("h"),
		Path{}.Name("B").Name("Port"): Number("80"),
	}, reflect.ValueOf(&out).Elem())
	fmt.Printf("    plane holds /B/Host and /B/Port, kind admits -> %+v err=%v\n", out.B, err)
	fmt.Println("    ^ these are two different PLANE LAYOUTS, not two code paths. A codec")
	fmt.Println("      that declines on Load would have to ask the plane for addresses the")
	fmt.Println("      source was never bound to, which ADR-0004's contract does not allow")
	fmt.Println("      and which needs an Enumerator the plane may not implement.")
	fmt.Println("      v2 can fall through on decode because it holds the whole document.")
	fmt.Println("      ferry holds one Value at one address. That is the structural")
	fmt.Println("      difference, and it is the reason the mechanism does not port.")

	fmt.Println("\n--- R9c: the decline that IS sound is a decline about the TYPE ---")
	fmt.Println("    A predicate over reflect.Type keeps the address set computable, and")
	fmt.Println("    it is exactly R5's predicate arm, refused there for its own reasons.")
	fmt.Println("    So the two sound-looking forms of `not mine` are one form, and it is")
	fmt.Println("    already decided.")

	fmt.Println("\n--- R9d: what ferry gives up by refusing, priced ---")
	fmt.Println("    v2's decline exists because WithMarshalers is a LIST, so a caller")
	fmt.Println("    composes several and needs a way to pass. ferry's step one is a MAP")
	fmt.Println("    keyed by reflect.Type, and ADR-0007 already made a duplicate a loud")
	fmt.Println("    error, so there is never a second entry to fall through to.")
	fmt.Println("    Measured: the only thing decline could reach is step two and step")
	fmt.Println("    three of the chain, and both are reachable by not registering.")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()
	type withIP struct{ IP net.IP }
	d, _ := dump(reflect.ValueOf(withIP{net.ParseIP("192.0.2.1")}))
	fmt.Printf("    net.IP unregistered, chain step 2 claims it -> %s\n",
		d[Path{}.Name("IP")].GoString())
	chainOrder = nil
	d2, _ := dump(reflect.ValueOf(withIP{net.ParseIP("192.0.2.1")}))
	fmt.Printf("    net.IP with no chain, step 3 claims it     -> %s\n",
		d2[Path{}.Name("IP")].GoString())
	fmt.Println("    ^ `fall through to the next step` is spelled `do not register this")
	fmt.Println("      type`, and it is a decision made once at the call site rather than")
	fmt.Println("      per value at Dump. Nothing is lost that ferry's chain could reach.")
}
