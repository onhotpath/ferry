package main

// P17: how common is the normalisation hazard, really?
//
// Before-kind breaks a type whose text form drops a distinction the Go value
// retains. P4 showed it with a synthetic type and with net.IP. The question
// the ADR has to answer is how many REAL types are in that class, because
// "a class of hazard" and "exactly one instance" are different decisions.
//
// The test: build a value whose Go representation is not the canonical one,
// run it through the text pair, and see whether it comes back identical.

import (
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"reflect"
	"regexp"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/language"
)

func runNormalise() {
	fmt.Println("\n--- P17: does the Go value retain anything the text form drops? ---")
	fmt.Println("    Each row is a value deliberately built in a non-canonical form.")
	fmt.Printf("    %-18s %-30s %-24s %s\n", "type", "value built as", "text form", "comes back identical")
	fmt.Println("    " + dashes(104))

	type row struct {
		name  string
		built string
		t     reflect.Type
		v     any
	}
	rows := []row{
		{"net.IP", "net.IP{192,0,2,1} (4 bytes)", reflect.TypeFor[net.IP](), net.IP{192, 0, 2, 1}},
		{"net.IP", "net.ParseIP (16 bytes)", reflect.TypeFor[net.IP](), net.ParseIP("192.0.2.1")},
		{"netip.Addr", "v4-in-v6 ::ffff:192.0.2.1", reflect.TypeFor[netip.Addr](), netip.MustParseAddr("::ffff:192.0.2.1")},
		{"netip.Addr", "with a zone, fe80::1%eth0", reflect.TypeFor[netip.Addr](), netip.MustParseAddr("fe80::1%eth0")},
		{"netip.Prefix", "10.0.0.5/8, host bits set", reflect.TypeFor[netip.Prefix](), netip.MustParsePrefix("10.0.0.5/8")},
		{"big.Int", "negative zero via SetString", reflect.TypeFor[big.Int](), mustBig("-0")},
		{"uuid.UUID", "parsed from UPPER CASE", reflect.TypeFor[uuid.UUID](), uuid.MustParse("0E37DF36-F698-11E6-8DD4-CB9CED3DF976")},
		{"uuid.UUID", "parsed from urn: form", reflect.TypeFor[uuid.UUID](), uuid.MustParse("urn:uuid:0e37df36-f698-11e6-8dd4-cb9ced3df976")},
		{"language.Tag", "parsed from EN-gb", reflect.TypeFor[language.Tag](), language.MustParse("EN-gb")},
		{"language.Tag", "parsed from en-Latn-GB", reflect.TypeFor[language.Tag](), language.MustParse("en-Latn-GB")},
		{"decimal.Decimal", "1.2500, trailing zeros", reflect.TypeFor[decimal.Decimal](), decimal.RequireFromString("1.2500")},
		{"decimal.Decimal", "1.25e2, exponent form", reflect.TypeFor[decimal.Decimal](), decimal.RequireFromString("1.25e2")},
		{"regexp.Regexp", "the zero value", reflect.TypeFor[regexp.Regexp](), regexp.Regexp{}},
		{"regexp.Regexp", "compiled ^a.*z$", reflect.TypeFor[regexp.Regexp](), *regexp.MustCompile(`^a.*z$`)},
	}

	broken := 0
	for _, r := range rows {
		c, ok, _ := textCodecFor(r.t)
		if !ok {
			fmt.Printf("    %-18s %-30s (no text pair)\n", r.name, r.built)
			continue
		}
		rv := reflect.ValueOf(r.v)
		val, err := c.enc(rv)
		if err != nil {
			fmt.Printf("    %-18s %-30s ENCODE ERR %v\n", r.name, r.built, err)
			continue
		}
		back := reflect.New(r.t).Elem()
		if err := c.dec(val, back); err != nil {
			fmt.Printf("    %-18s %-30s %-24s DECODE ERR %v\n", r.name, r.built, val.GoString(), shorten2(err.Error(), 30))
			continue
		}
		same := reflect.DeepEqual(r.v, back.Interface())
		verdict := "yes"
		if !same {
			verdict = "NO"
			broken++
		}
		fmt.Printf("    %-18s %-30s %-24s %s\n", r.name, r.built, shorten2(val.GoString(), 24), verdict)
	}
	fmt.Printf("\n    %d of %d values do not come back identical under DeepEqual.\n", broken, len(rows))
	fmt.Println("    Read the NO rows carefully: the question is not whether the text")
	fmt.Println("    form is canonical, it is whether the Go VALUE retained something")
	fmt.Println("    the canonical text does not carry. A type that canonicalises on")
	fmt.Println("    construction, as uuid and language.Tag do, is not in the hazard")
	fmt.Println("    class at all, because there is no non-canonical value to lose.")
}

func mustBig(s string) big.Int {
	var x big.Int
	x.SetString(s, 10)
	return x
}
