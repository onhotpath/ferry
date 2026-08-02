package main

// What the time.Time losses actually cost, and what the choice of equality
// relation actually buys.

import (
	"fmt"
	"reflect"
	"time"
)

func rtTime(t time.Time) time.Time {
	d, _ := dump(reflect.ValueOf(struct{ V time.Time }{t}))
	var b struct{ V time.Time }
	load(d, reflect.ValueOf(&b).Elem())
	return b.V
}

func runTimeCost() {
	ny, _ := time.LoadLocation("America/New_York")

	fmt.Println("\n--- what is preserved, and what is not ---")
	orig := time.Date(2026, 1, 15, 12, 0, 0, 123456789, ny) // EST, winter
	back := rtTime(orig)
	fmt.Printf("  before : %v   loc=%q  offset=%s\n", orig, orig.Location(), offs(orig))
	fmt.Printf("  after  : %v   loc=%q  offset=%s\n", back, back.Location(), offs(back))
	fmt.Printf("  instant preserved (.Equal) : %v\n", orig.Equal(back))
	fmt.Printf("  nanoseconds preserved      : %v (%d -> %d)\n", orig.Nanosecond() == back.Nanosecond(), orig.Nanosecond(), back.Nanosecond())
	fmt.Printf("  wall clock reading         : %v\n", orig.Format("2006-01-02 15:04:05") == back.Format("2006-01-02 15:04:05"))
	fmt.Printf("  zone identity              : %v\n", orig.Location().String() == back.Location().String())

	fmt.Println("\n--- the concrete harm: arithmetic across a DST boundary ---")
	// Add six months, crossing into EDT.
	fmt.Printf("  orig.AddDate(0,6,0) : %v\n", orig.AddDate(0, 6, 0))
	fmt.Printf("  back.AddDate(0,6,0) : %v\n", back.AddDate(0, 6, 0))
	fmt.Printf("  same instant?       : %v\n", orig.AddDate(0, 6, 0).Equal(back.AddDate(0, 6, 0)))
	fmt.Println("  ^ the loaded value has a FIXED offset, so it does not know DST exists.")
	fmt.Println("    A stored timestamp is unaffected. A stored 'when to run next' is.")

	fmt.Println("\n--- and UTC, which is the case that costs nothing ---")
	u := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	bu := rtTime(u)
	fmt.Printf("  UTC round trip: == %v   loc %q -> %q\n", u == bu, u.Location(), bu.Location())

	fmt.Println("\n--- THE TRADE-OFF: the relation is also the specification of what may be lost ---")
	t := reflect.TypeFor[time.Time]()
	good := byIdentity[t]
	// A codec that throws the zone away entirely, keeping only the instant.
	byIdentity[t] = leafCodec{
		name: "instant only, zone discarded",
		enc: func(v reflect.Value) (Value, error) {
			return Number(fmt.Sprint(v.Interface().(time.Time).UnixNano())), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			n, err := val.AsInt()
			if err != nil {
				return err
			}
			dst.Set(reflect.ValueOf(time.Unix(0, n).UTC()))
			return nil
		},
	}
	d, _ := dump(reflect.ValueOf(struct{ V time.Time }{orig}))
	fmt.Printf("  representation now : %s\n", d[Path{}.Name("V")].GoString())
	for _, c := range []struct {
		label string
		vals  []time.Time
	}{
		{"the zoned value alone   ", []time.Time{time.Date(2026, 8, 2, 12, 0, 0, 0, ny)}},
		{"the whole core value list", []time.Time{
			time.Time{}, time.Unix(0, 0).UTC(),
			time.Date(2026, 8, 2, 12, 0, 0, 0, ny),
			time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)}},
	} {
		f := failsOn(Type("time.Time", time.Time.Equal, c.vals...), memoryPlane())
		fmt.Printf("  %s -> %d failures", c.label, len(f))
		if len(f) > 0 {
			fmt.Printf("  first: %s", shorten(f[0]))
		}
		fmt.Println()
	}
	fmt.Println("  ^ the zoned value alone passes: .Equal compares instants, so it")
	fmt.Println("    CANNOT see the zone being discarded. The whole list catches this")
	fmt.Println("    codec only because time.Time{} overflows UnixNano - an unrelated")
	fmt.Println("    reason. The relation chose what the harness may miss; the value")
	fmt.Println("    list happened to cover it here, and the golden column is the only")
	fmt.Println("    thing that pins the representation on purpose.")
	byIdentity[t] = good

	fmt.Println("\n--- values in the type with no text form at all ---")
	for _, c := range []time.Time{
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		_, err := dump(reflect.ValueOf(struct{ V time.Time }{c}))
		fmt.Printf("  %-34v dump err = %v\n", c.Format("2006-01-02"), err)
	}
}

func offs(t time.Time) string {
	_, o := t.Zone()
	return fmt.Sprintf("%+d", o/3600)
}
