package main

// P1: the interface census.
//
// The chain's arms can only be argued against what real types actually
// implement. Three questions this answers, and each one decides an ask:
//
//   - how often is an arm a HALF pair (encoder without decoder, or the
//     reverse)? That is the ticket's "decoder but no matching encoder".
//   - how often does a type carry MORE than one arm? Precedence is only
//     exercised where that is true, and a fixture where every type has
//     exactly one arm never tests precedence at all.
//   - is any arm ever the ONLY one a type carries? An arm that never
//     uniquely rescues a type is an arm ferry can drop.

import (
	"encoding"
	"encoding/gob"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	xloadtype "github.com/gojekfarm/xtools/xload/type"
	"golang.org/x/text/language"
)

type iface struct {
	short string
	t     reflect.Type
}

var (
	ifTextM  = iface{"TextM", reflect.TypeFor[encoding.TextMarshaler]()}
	ifTextA  = iface{"TextA", reflect.TypeFor[encoding.TextAppender]()}
	ifTextU  = iface{"TextU", reflect.TypeFor[encoding.TextUnmarshaler]()}
	ifJSONM  = iface{"JSONM", reflect.TypeFor[json.Marshaler]()}
	ifJSONU  = iface{"JSONU", reflect.TypeFor[json.Unmarshaler]()}
	ifJSONMT = iface{"JSONMTo", reflect.TypeFor[jsonv2.MarshalerTo]()}
	ifJSONUF = iface{"JSONUFrom", reflect.TypeFor[jsonv2.UnmarshalerFrom]()}
	ifBinM   = iface{"BinM", reflect.TypeFor[encoding.BinaryMarshaler]()}
	ifBinA   = iface{"BinA", reflect.TypeFor[encoding.BinaryAppender]()}
	ifBinU   = iface{"BinU", reflect.TypeFor[encoding.BinaryUnmarshaler]()}
	ifGobE   = iface{"GobE", reflect.TypeFor[gob.GobEncoder]()}
	ifGobD   = iface{"GobD", reflect.TypeFor[gob.GobDecoder]()}
	ifStr    = iface{"Stringer", reflect.TypeFor[fmt.Stringer]()}
)

// impl reports how a type satisfies an interface:
//
//	"-"   neither T nor *T
//	"V"   T does (so *T does too)
//	"P"   only *T does, which needs an addressable value
//
// The V/P split is not bookkeeping. ADR-0005 refused fmt.Stringer partly
// because url.URL does not implement it and *url.URL does, which is survey
// item 5.14's receiver defect. Every arm in the chain has the same exposure.
func impl(t reflect.Type, i iface) string {
	switch {
	case t.Implements(i.t):
		return "V"
	case reflect.PointerTo(t).Implements(i.t):
		return "P"
	}
	return "-"
}

type censusRow struct {
	name string
	t    reflect.Type
}

func censusTypes() []censusRow {
	return []censusRow{
		{"time.Time", reflect.TypeFor[time.Time]()},
		{"time.Duration", reflect.TypeFor[time.Duration]()},
		{"time.Month", reflect.TypeFor[time.Month]()},
		{"netip.Addr", reflect.TypeFor[netip.Addr]()},
		{"netip.AddrPort", reflect.TypeFor[netip.AddrPort]()},
		{"netip.Prefix", reflect.TypeFor[netip.Prefix]()},
		{"net.IP", reflect.TypeFor[net.IP]()},
		{"net.IPNet", reflect.TypeFor[net.IPNet]()},
		{"net.IPMask", reflect.TypeFor[net.IPMask]()},
		{"net.HardwareAddr", reflect.TypeFor[net.HardwareAddr]()},
		{"net.TCPAddr", reflect.TypeFor[net.TCPAddr]()},
		{"url.URL", reflect.TypeFor[url.URL]()},
		{"url.Userinfo", reflect.TypeFor[url.Userinfo]()},
		{"big.Int", reflect.TypeFor[big.Int]()},
		{"big.Float", reflect.TypeFor[big.Float]()},
		{"big.Rat", reflect.TypeFor[big.Rat]()},
		{"regexp.Regexp", reflect.TypeFor[regexp.Regexp]()},
		{"json.RawMessage", reflect.TypeFor[json.RawMessage]()},
		{"slog.Level", reflect.TypeFor[slog.Level]()},
		{"os.FileMode", reflect.TypeFor[os.FileMode]()},
		{"uuid.UUID", reflect.TypeFor[uuid.UUID]()},
		{"decimal.Decimal", reflect.TypeFor[decimal.Decimal]()},
		{"language.Tag", reflect.TypeFor[language.Tag]()},
		{"xloadtype.URL", reflect.TypeFor[xloadtype.URL]()},
		{"xloadtype.Endpoint", reflect.TypeFor[xloadtype.Endpoint]()},
		{"xloadtype.Listener", reflect.TypeFor[xloadtype.Listener]()},
		{"string", reflect.TypeFor[string]()},
		{"int", reflect.TypeFor[int]()},
		{"[]byte", reflect.TypeFor[[]byte]()},
	}
}

// arm is a candidate chain arm: an encode probe and a decode probe.
type arm struct {
	name string
	enc  []iface // any one satisfies the encode half
	dec  iface
}

var arms = []arm{
	{"text", []iface{ifTextA, ifTextM}, ifTextU},
	{"json", []iface{ifJSONMT, ifJSONM}, ifJSONU},
	{"binary", []iface{ifBinA, ifBinM}, ifBinU},
	{"gob", []iface{ifGobE}, ifGobD},
}

// armState returns "pair" / "enc only" / "dec only" / "-".
func armState(t reflect.Type, a arm) string {
	e := false
	for _, i := range a.enc {
		if impl(t, i) != "-" {
			e = true
		}
	}
	d := impl(t, a.dec) != "-"
	switch {
	case e && d:
		return "pair"
	case e:
		return "ENC only"
	case d:
		return "DEC only"
	}
	return "-"
}

func runCensus() {
	fmt.Println("\n=== P1a: which interfaces each type carries (V = value receiver, P = pointer only) ===")
	cols := []iface{ifTextM, ifTextA, ifTextU, ifJSONM, ifJSONU, ifJSONMT, ifJSONUF, ifBinM, ifBinA, ifBinU, ifGobE, ifGobD, ifStr}
	fmt.Printf("  %-20s", "type")
	for _, c := range cols {
		fmt.Printf(" %-9s", c.short)
	}
	fmt.Println()
	fmt.Println("  " + dashes(20+10*len(cols)))
	for _, r := range censusTypes() {
		fmt.Printf("  %-20s", r.name)
		for _, c := range cols {
			fmt.Printf(" %-9s", impl(r.t, c))
		}
		fmt.Println()
	}

	fmt.Println("\n=== P1b: per arm, is it a pair or a half? ===")
	fmt.Printf("  %-20s %-10s %-10s %-10s %-10s  %s\n", "type", "text", "json", "binary", "gob", "arms carried")
	fmt.Println("  " + dashes(90))
	pairCount := map[string]int{}
	halfCount := map[string]int{}
	uniqueRescue := map[string]int{}
	multi := 0
	for _, r := range censusTypes() {
		var states []string
		var pairs []string
		for _, a := range arms {
			s := armState(r.t, a)
			states = append(states, s)
			if s == "pair" {
				pairs = append(pairs, a.name)
				pairCount[a.name]++
			} else if s != "-" {
				halfCount[a.name]++
			}
		}
		if len(pairs) == 1 {
			uniqueRescue[pairs[0]]++
		}
		if len(pairs) > 1 {
			multi++
		}
		fmt.Printf("  %-20s %-10s %-10s %-10s %-10s  %d: %v\n", r.name,
			states[0], states[1], states[2], states[3], len(pairs), pairs)
	}
	fmt.Println()
	for _, a := range arms {
		fmt.Printf("  arm %-8s complete pairs: %2d   half pairs: %2d   sole arm for: %2d types\n",
			a.name, pairCount[a.name], halfCount[a.name], uniqueRescue[a.name])
	}
	fmt.Printf("\n  types carrying MORE than one complete arm: %d of %d\n", multi, len(censusTypes()))
	fmt.Println("  ^ precedence is only exercised by those. A fixture without them tests nothing.")
}
