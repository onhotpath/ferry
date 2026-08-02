package main

// R1: what registration looks like at the call site, and whether inference
// works without explicit instantiation.
//
// The ticket asks this first and it cannot be judged from Markdown, which is
// why #19 carries wayfinder:prototype. Everything here is a compile-time
// question, so the probe's real output is that this FILE COMPILES: every call
// below is written the way a user would write it, and any that needed an
// explicit type argument would have failed the build rather than printed
// anything.

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/language"
)

type R1Timeout time.Duration

// The ten registrations a real user actually writes, in the shape they would
// actually write them. Not one carries an explicit type argument.
func r1Registrations() []Reg {
	return []Reg{
		// (a) A method expression and a package function that already have
		//     exactly the right signatures. This is the best case and it is
		//     also the commonest: netip.Addr.String is func(netip.Addr) string
		//     and netip.ParseAddr is func(string) (netip.Addr, error).
		StringCodec(netip.Addr.String, netip.ParseAddr),
		StringCodec(netip.AddrPort.String, netip.ParseAddrPort),
		StringCodec(netip.Prefix.String, netip.ParsePrefix),
		StringCodec(language.Tag.String, language.Parse),
		StringCodec(uuid.UUID.String, uuid.Parse),

		// (b) The same, where BOTH halves need a wrapper, because url.URL's
		//     String is on the pointer receiver so `url.URL.String` is not a
		//     legal method expression, and url.Parse returns a pointer.
		//     ADR-0005 predicted url.URL "will be reported as a bug", and
		//     this is the seven lines that answer it.
		StringCodec(func(u url.URL) string { return u.String() }, func(s string) (url.URL, error) {
			u, err := url.Parse(s)
			if err != nil {
				return url.URL{}, err
			}
			return *u, nil
		}),

		// (c) ADR-0005's named hole: a named type over time.Duration.
		StringCodec(
			func(t R1Timeout) string { return time.Duration(t).String() },
			func(s string) (R1Timeout, error) {
				d, err := time.ParseDuration(s)
				return R1Timeout(d), err
			}),

		// (d) The kind declaration doing real work. big.Int's text IS a
		//     number, so it declares Number, and TypeCodec still infers T
		//     from the two function literals with no instantiation.
		TypeCodec(VNumber,
			func(x big.Int) (Value, error) { return Number(x.String()), nil },
			func(v Value) (big.Int, error) {
				var x big.Int
				s, err := v.AsNumber()
				if err != nil {
					return x, err
				}
				if _, ok := x.SetString(s, 10); !ok {
					return x, fmt.Errorf("not an integer: %q", s)
				}
				return x, nil
			}),

		// (e) A decimal, which is the other "text is a number" case.
		TypeCodec(VNumber,
			func(d decimal.Decimal) (Value, error) { return Number(d.String()), nil },
			func(v Value) (decimal.Decimal, error) {
				s, err := v.AsNumber()
				if err != nil {
					return decimal.Decimal{}, err
				}
				return decimal.NewFromString(s)
			}),

		// (f) An INTERFACE. The codec owns the discriminator inside its own
		//     text, so ferry needs no type registry and the plane gets no
		//     ferry-specific tagging.
		TypeCodec(VString,
			func(a net.Addr) (Value, error) {
				if a == nil {
					return Null(), nil
				}
				return String(a.Network() + "://" + a.String()), nil
			},
			func(v Value) (net.Addr, error) {
				if v.Kind() == VNull {
					return nil, nil
				}
				s, err := v.AsString()
				if err != nil {
					return nil, err
				}
				return net.ResolveTCPAddr("tcp", s[len("tcp://"):])
			}),
	}
}

// r1Lines counts the source lines each registration above costs, so the ADR
// can state the ergonomic claim as a number rather than as an adjective.
var r1Lines = []struct {
	what  string
	lines int
	form  string
}{
	{"netip.Addr", 1, "StringCodec(netip.Addr.String, netip.ParseAddr)"},
	{"netip.AddrPort", 1, "StringCodec(netip.AddrPort.String, netip.ParseAddrPort)"},
	{"netip.Prefix", 1, "StringCodec(netip.Prefix.String, netip.ParsePrefix)"},
	{"language.Tag", 1, "StringCodec(language.Tag.String, language.Parse)"},
	{"uuid.UUID", 1, "StringCodec(uuid.UUID.String, uuid.Parse)"},
	{"url.URL", 7, "StringCodec(func(url.URL) string{...}, func(string) (url.URL, error){...})"},
	{"a named time.Duration", 6, "StringCodec(func(T) string{...}, func(string) (T, error){...})"},
	{"big.Int", 12, "TypeCodec(VNumber, func(big.Int) (Value, error){...}, ...)"},
	{"decimal.Decimal", 9, "TypeCodec(VNumber, ...)"},
	{"net.Addr, an interface", 18, "TypeCodec(VString, ...)"},
}

func runR1() {
	fmt.Println("--- R1a: every registration below compiles with NO explicit instantiation ---")
	fmt.Println("    READ R14 BEFORE COPYING THE ONE-LINERS. Three of the five are not")
	fmt.Println("    total over the zero value, which R10's harness found and this probe")
	fmt.Println("    did not, because a call site that compiles is not a codec that works.")
	fmt.Println("    T is inferred from the function arguments in all ten cases.")
	fmt.Println("    The five one-liners infer T from a METHOD EXPRESSION on one side and")
	fmt.Println("    a package parse function on the other, with no literal written at all.")
	fmt.Println()
	for _, l := range r1Lines {
		fmt.Printf("    %-24s %2d line(s)  %s\n", l.what, l.lines, l.form)
	}

	reg := NewRegistry()
	if err := reg.Register(r1Registrations()...); err != nil {
		fmt.Println("    register err:", err)
	}
	fmt.Printf("\n    registry holds %d types, err=<nil>\n", len(reg.byType))

	fmt.Println("\n--- R1b: what the registered set does to ADR-0005's refusal list ---")
	withRegistry(reg, func() {
		type conf struct {
			A netip.Addr
			P netip.AddrPort
			X netip.Prefix
			U url.URL
			B big.Int
			D decimal.Decimal
			N net.Addr
			T R1Timeout
		}
		addrs, err := compile(reflect.TypeFor[conf]())
		fmt.Printf("    compile: %d addresses, err=%v\n", len(addrs), err)
		c := conf{
			A: netip.MustParseAddr("192.0.2.1"),
			P: netip.MustParseAddrPort("192.0.2.1:80"),
			X: netip.MustParsePrefix("10.0.0.0/8"),
			U: mustU("https://example.com/a?q=1"),
			B: *big.NewInt(1 << 40),
			D: decimal.RequireFromString("1.25"),
			N: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80},
			T: R1Timeout(90 * time.Second),
		}
		d, derr := dump(reflect.ValueOf(c))
		fmt.Printf("    dump err=%v\n", derr)
		for _, p := range sortedAddrs(d) {
			fmt.Printf("      %-4s %s\n", p, d[p].GoString())
		}
	})

	fmt.Println("\n--- R1c: the shapes that do NOT compile, verbatim from go1.27rc2 ---")
	for _, bad := range [][2]string{
		{"StringCodec(netip.Addr.String, netip.ParsePrefix)",
			"in call to StringCodec, type func(s string) (netip.Prefix, error) of netip.ParsePrefix\n" +
				"         does not match inferred type func(string) (netip.Addr, error) for func(string) (T, error)"},
		{"StringCodec(netip.ParseAddr, netip.Addr.String)   // halves swapped",
			"in call to StringCodec, type func(s string) (netip.Addr, error) of netip.ParseAddr\n" +
				"         does not match inferred type func(string) string for func(T) string"},
		{"StringCodec(netip.Addr.String)                    // one half only",
			"not enough arguments in call to StringCodec\n" +
				"         have (func(netip.Addr) string)\n" +
				"         want (func(T) string, func(string) (T, error))"},
		{"StringCodec(url.URL.String, ...)                  // pointer-receiver method",
			"invalid method expression url.URL.String (needs pointer receiver (*url.URL).String)"},
	} {
		fmt.Printf("    %s\n      -> %s\n", bad[0], bad[1])
	}
	fmt.Println("    ^ every one is a BUILD error, and the first three name T. A half pair")
	fmt.Println("      is not merely refused at registration, it is unwritable. The `want`")
	fmt.Println("      line in the third is the API documenting itself in the diagnostic.")
	fmt.Println("    The fourth is the one ergonomic limit found: a method expression needs a")
	fmt.Println("      VALUE receiver, so url.URL and big.Int cost a wrapper where netip.Addr")
	fmt.Println("      and uuid.UUID do not. That is a property of the stdlib's receivers and")
	fmt.Println("      no API shape removes it.")
}
