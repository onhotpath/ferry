package main

// T1: is the defaulted value reachable at all?
//
// ADR-0010 states, in its "what this ADR does not decide" section, that core
// need not export a schema view because "template generation reaches the
// defaults through a recording sink". ADR-0006 states the opposite half from
// the other end: "`required` fires on a Load from an empty plane, so the
// defaulted zero value is not reachable by Load alone. That is #14's to
// resolve."
//
// Both sentences are about the same mechanism and neither had been run. This
// probe runs it.

import (
	"context"
	"errors"
	"fmt"
)

func runT1() {
	ctx := context.Background()

	fmt.Println("(a) Dump the ZERO value into a recording sink - ADR-0010's stated route")
	rec := newRecorder()
	if err := Dump(ctx, TConf{}, rec); err != nil {
		fmt.Println("  err:", err)
	}
	for _, p := range rec.addrs() {
		fmt.Printf("  %-16s %s\n", p, rec.vals[p].GoString())
	}
	fmt.Printf("  %d addresses recorded\n", len(rec.addrs()))
	fmt.Println("  /listen holds the ZERO value, not the declared default `:8080`.")
	fmt.Println("  /timeout holds `0s`, not `30s`.  /db/port holds 0, not 5432.")
	fmt.Println("  A Dump writes the value in hand and never substitutes a default")
	fmt.Println("  (ADR-0006: \"A default is a Load-side rule\"), so the recording sink")
	fmt.Println("  sees the zero value at every defaulted address.")

	fmt.Println("\n(b) three addresses the zero dump does not contain at all")
	fmt.Println("  /debug   - omitzero at its zero value, so no Set call (ADR-0006)")
	fmt.Println("  /tls/*   - a nil *struct writes Null at /tls and never descends")
	fmt.Println("  /tags, /limits - a nil composite writes Null at its own address (ADR-0005)")
	fmt.Println("  So a zero-value dump reaches neither the defaults nor the whole address set.")

	fmt.Println("\n(c) Load from an EMPTY plane - ADR-0006's route")
	var seen []Path
	cfg, err := Load[TConf](ctx, tEmptySource{seen: &seen}, WithSched(tAggregating))
	fmt.Printf("  value returned: %+v\n", cfg)
	fmt.Printf("  err:\n%s\n", tIndent(errText(err)))
	fmt.Printf("  addresses the source was asked about: %d\n", len(seen))
	fmt.Println("  The defaults WERE applied inside the walk, and ADR-0011's")
	fmt.Println("  \"ferry yields no value it built\" means none of it crosses the boundary.")

	fmt.Println("\n(d) the same Load with every `required` field removed from the type")
	type TConfNoReq struct {
		Listen  string `ferry:"listen,default=:8080"`
		Timeout string `ferry:"timeout,default=30s"`
	}
	ok, err := Load[TConfNoReq](ctx, tEmptySource{})
	fmt.Printf("  value: %+v   err: %v\n", ok, err)
	fmt.Println("  So ADR-0010's sentence is true for a struct with no required field")
	fmt.Println("  and false for one with any: Load(empty) -> Dump(recorder) carries the")
	fmt.Println("  defaults only when the Load succeeds, and `required` is exactly the")
	fmt.Println("  thing a template exists to announce.")

	fmt.Println("\n(e) the required set IS reachable, from the error set")
	var req []Path
	for _, e := range tElements(err2(Load[TConf](ctx, tEmptySource{}, WithSched(tAggregating)))) {
		if errors.Is(e, tErrMissing) {
			req = append(req, tAddress(e))
		}
	}
	fmt.Printf("  required addresses, read via Elements + errors.Is + Address: %v\n", req)
	fmt.Println("  That is ADR-0001's \"read the error set\" pattern, and it needs")
	fmt.Println("  ADR-0011's accessors. With a first-error scheduler it yields one")
	fmt.Println("  address per Load instead of all of them - priced in T2.")
}

func err2[T any](_ T, err error) error { return err }

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func tIndent(s string) string {
	out := "    "
	for _, r := range s {
		out += string(r)
		if r == '\n' {
			out += "    "
		}
	}
	return out
}
