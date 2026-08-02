package main

import (
	"fmt"
	"os"
	"reflect"
	"time"
)

// Does the round-trip property notice a WRONG REPRESENTATION? Swap
// time.Duration's codec for json/v2's FormatDurationAsNano shape - bare
// nanoseconds - and ask the harness.
func runAudit2() {
	dir, _ := os.MkdirTemp("", "ferrya2")
	defer os.RemoveAll(dir)
	yp := yamlPlane(dir)

	fmt.Println("\n--- the audit set through the REAL yaml plane ---")
	ok, bad := 0, 0
	for _, pr := range auditSet() {
		if f := pr.run(yp); len(f) > 0 {
			bad++
			fmt.Printf("  FAIL %-22s %v\n", pr.Name(), f)
		} else {
			ok++
		}
	}
	fmt.Printf("  %d/%d round-trip through yaml\n", ok, ok+bad)

	fmt.Println("\n--- does the property catch a wrong REPRESENTATION? ---")
	t := reflect.TypeFor[time.Duration]()
	good := byIdentity[t]
	byIdentity[t] = leafCodec{
		name: "nanoseconds, json/v2 FormatDurationAsNano shape",
		enc:  func(v reflect.Value) (Value, error) { return Int(v.Int()), nil },
		dec: func(val Value, dst reflect.Value) error {
			n, err := val.AsInt()
			if err != nil {
				return err
			}
			dst.SetInt(n)
			return nil
		},
	}
	pr := Type("time.Duration", Eq[time.Duration], time.Second, 90*time.Minute, 0)
	fails := pr.run(identityPlane)
	v, _ := dump(reflect.ValueOf(struct{ D time.Duration }{30 * time.Second}))
	fmt.Printf("  representation now: %s\n", v[Path{}.Name("D")].GoString())
	fmt.Printf("  round-trip failures: %d   <- the property is BLIND to this\n", len(fails))
	byIdentity[t] = good
	v2, _ := dump(reflect.ValueOf(struct{ D time.Duration }{30 * time.Second}))
	fmt.Printf("  restored:           %s\n", v2[Path{}.Name("D")].GoString())
	fmt.Println("  -> so the pinned representation needs a golden check, not the property.")

	fmt.Println("\n--- the golden check, as a second column in the same table ---")
	for _, c := range []struct {
		name string
		val  any
		want string
	}{
		{"time.Duration", 30 * time.Second, `string("30s")`},
		{"time.Time", time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), `string("2026-08-02T12:00:00Z")`},
		{"[]byte", []byte{0, 255, 65}, `bytes("\x00\xffA")`},
		{"float64", 0.1, `number("0.1")`},
		{"uint64 max", uint64(1<<64 - 1), `number("18446744073709551615")`},
		{"int", -5, `number("-5")`},
		{"bool", true, `bool("true")`},
	} {
		rv := reflect.New(reflect.TypeOf(c.val)).Elem()
		rv.Set(reflect.ValueOf(c.val))
		h := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: rv.Type()}})).Elem()
		h.Field(0).Set(rv)
		d, err := dump(h)
		got := "ERR"
		if err == nil {
			got = d[Path{}.Name("V")].GoString()
		}
		mark := "ok "
		if got != c.want {
			mark = "DIFF"
		}
		fmt.Printf("  %s %-14s %-28s want %s\n", mark, c.name, got, c.want)
	}
}
