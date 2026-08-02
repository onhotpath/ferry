package main

// E12: the seam with #9. ADR-0011 states "when a Load fails, ferry yields no
// value" as a constraint on this ticket's entry point.
//
// The rule is checked against BOTH of this ADR's load verbs, because the
// second one takes a caller-supplied seed and "yields no value" has two
// readings there that ADR-0011 could not have seen.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

type E12Conf struct {
	Host    string `ferry:"host"`
	Port    int    `ferry:"port"`
	Retries int    `ferry:"retries"`
}

// loadOverSeed is the alternative reading: on failure, hand back the value the
// caller supplied rather than the zero value.
func loadOverSeed[T any](ctx context.Context, seed T, vals map[Path]Value, options ...Option) (T, error) {
	out, err := loadFrom(ctx, seed, vals, options...)
	if err != nil {
		return seed, err
	}
	return out, nil
}

func runE12() {
	ctx := context.Background()

	good := map[Path]Value{
		Path{}.Name("host"):    String("db1"),
		Path{}.Name("port"):    Number("5432"),
		Path{}.Name("retries"): Number("3"),
	}
	// /port stops parsing, so the walk fails after /host has been set.
	// TWO fields fail, so an aggregating scheduler has something to aggregate.
	bad := map[Path]Value{
		Path{}.Name("host"):    String("db1"),
		Path{}.Name("port"):    Number("not-a-number"),
		Path{}.Name("retries"): Number("also-bad"),
	}

	fmt.Println("--- E12a: ADR-0011's rule, on Load[T], confirmed ---")
	partial, _ := loadFrom(ctx, E12Conf{}, bad)
	fmt.Printf("  what Load[T] returns on failure   : %+v\n", partial)
	fmt.Println("  (this prototype's walk stops at the first error, so the partial it")
	fmt.Println("   built is smaller than an aggregating walk's - see E12d)")

	fmt.Println("\n--- E12b: the same rule on LoadOver, which is where it bites ---")
	live, err := loadOverSeed(ctx, E12Conf{}, good)
	fmt.Printf("  a good reload first               : %+v err=%v\n", live, err)

	fmt.Println("\n  now the plane goes bad, and the caller writes the ordinary reload:")
	fmt.Println("      cfg, err = ferry.LoadOver(ctx, cfg, src)")

	zeroed, zerr := loadFrom(ctx, live, bad) // returns the ZERO value on error
	fmt.Printf("  yields the ZERO value             : %+v err=%v\n", zeroed, trunc(zerr))
	kept, kerr := loadOverSeed(ctx, live, bad) // returns the SEED on error
	fmt.Printf("  yields the SEED unchanged         : %+v err=%v\n", kept, trunc(kerr))

	fmt.Println("\n  With the error IGNORED, which is the case ADR-0011's rule is designed")
	fmt.Println("  for, the two readings differ in the worst possible way:")
	fmt.Printf("    zero reading : the caller's LIVE config becomes %+v\n", zeroed)
	fmt.Printf("    seed reading : the caller's LIVE config stays   %+v\n", kept)
	fmt.Println("  The zero reading destroys a value ferry never touched, which is the")
	fmt.Println("  outcome ADR-0011 rules out a partial for, arriving through the other")
	fmt.Println("  door and doing more damage.")

	fmt.Println("\n--- E12c: the reading that makes it ONE rule rather than two ---")
	fmt.Println("    ferry yields no value IT BUILT.")
	fmt.Println("  LoadOver returns the seed; Load is LoadOver with the zero value, so")
	fmt.Println("  Load returning the zero value falls out rather than being a rule.")
	l, lerr := loadOverSeed(ctx, E12Conf{}, bad)
	fmt.Printf("  Load[T] under that reading        : %+v err=%v\n", l, trunc(lerr))
	fmt.Println("  which is byte-identical to ADR-0011's measured result.")

	fmt.Println("\n--- E12d: where #9's aggregation lands in a walk written once ---")
	fmt.Println("  ADR-0011 says aggregating means the walk continues past a failed")
	fmt.Println("  field. This prototype's serial scheduler returns on the first error.")
	fmt.Println("  The seam holds: aggregation is a property of the SCHEDULER, so the")
	fmt.Println("  walk is unchanged. Measured, the same walk under both:")

	o := defaultOpts()
	s, _ := schemaFor(rtE12(), o)
	for _, sc := range []struct {
		name string
		f    sched
	}{
		{"first-error scheduler", serial},
		{"aggregating scheduler", aggregate},
	} {
		var out E12Conf
		w := &walker{dir: loadDir(mapReader{bad}, ctx, o), sch: sc.f, ctx: ctx}
		_, werr := w.walk(s.root, rvOf(&out), Path{})
		n := 1
		if werr != nil {
			if u, ok := werr.(interface{ Unwrap() []error }); ok {
				n = len(u.Unwrap())
			}
		}
		fmt.Printf("    %-22s partial=%+v errors=%d\n", sc.name, out, n)
	}
	fmt.Println("  The walk function is byte-identical between the two rows. That is what")
	fmt.Println("  \"a concurrent mode is a scheduler and never a second walk\" buys #9 as")
	fmt.Println("  well as #20, and neither ticket asked for it.")
}

// aggregate is #9's scheduler: run every sibling, collect every error.
func aggregate(tasks []func() error) error {
	var errs []error
	for _, t := range tasks {
		if err := t(); err != nil {
			errs = append(errs, err)
		}
	}
	// NOTE: ADR-0011 requires ferry's own aggregate constructor here rather
	// than errors.Join, or Elements() cannot range the result. This is a
	// prototype and uses Join; the ADR records the obligation.
	return errors.Join(errs...)
}

func rtE12() reflect.Type { return reflect.TypeFor[E12Conf]() }

func rvOf(p any) reflect.Value { return reflect.ValueOf(p).Elem() }
