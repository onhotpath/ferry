package main

import (
	"fmt"
	"math/big"
	"net/netip"
	"reflect"
	"sync"
	"time"
)

// A struct is admitted by KIND, so any struct is "supported". What happens to
// a struct whose state is entirely unexported? reflect cannot read or set it,
// so it contributes no address - and dumping it writes nothing, silently,
// which is the one thing ADR-0001 rules out by name.
func runAudit3() {
	type WithMutex struct {
		mu   sync.Mutex
		Name string
	}
	for _, t := range []reflect.Type{
		reflect.TypeFor[netip.Addr](),
		reflect.TypeFor[netip.AddrPort](),
		reflect.TypeFor[big.Int](),
		reflect.TypeFor[time.Location](),
		reflect.TypeFor[sync.Mutex](),
		reflect.TypeFor[WithMutex](),
		reflect.TypeFor[struct{}](),
	} {
		addrs, err := compile(t)
		fmt.Printf("  %-20s exported=%d/%d  addresses=%d  compile err=%v\n",
			t, exported(t), t.NumField(), len(addrs), err)
	}

	fmt.Println("\n  the mixed case: one mapped sibling must not hide the loss")
	type Mixed struct {
		A netip.Addr
		B string
	}
	ma, me := compile(reflect.TypeFor[Mixed]())
	fmt.Printf("    struct{A netip.Addr; B string} -> addresses=%v\n    err=%v\n", ma, me)

	fmt.Println("\n  what dumping netip.Addr actually produces:")
	a := netip.MustParseAddr("192.0.2.1")
	v, err := dump(reflect.ValueOf(struct{ A netip.Addr }{a}))
	fmt.Printf("    value in  : %v\n    dumped    : %v addresses, err=%v\n", a, len(v), err)
	var back struct{ A netip.Addr }
	load(v, reflect.ValueOf(&back).Elem())
	fmt.Printf("    loaded back: %v   <- a silent total loss\n", back.A)

	fmt.Println("\n  and the same for a value that LOOKS fine:")
	bi := big.NewInt(1 << 40)
	v2, _ := dump(reflect.ValueOf(struct{ N big.Int }{*bi}))
	fmt.Printf("    big.Int %v -> %d addresses\n", bi, len(v2))
}

func exported(t reflect.Type) int {
	if t.Kind() != reflect.Struct {
		return -1
	}
	n := 0
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			n++
		}
	}
	return n
}
