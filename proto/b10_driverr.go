package main

import (
	"context"
	"errors"
	"fmt"
)

type bFailReader struct{}

func (bFailReader) Get(context.Context, Path) (Value, error) {
	return Absent, errors.New("kv: 503 from the backend")
}

type bFailSource struct{}

func (bFailSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return bFailReader{}, nil }, nil
}

// runB10 is not about this ticket. It is here because auditing B7a turned up
// one place where this prototype quietly does not do what an Accepted ADR
// says, and the obvious next question is whether there are others.
//
// There is at least one more, and it is in the walk rather than in an entry
// point: loadDir's get() discards the error Reader.Get returns and substitutes
// Absent.
//
//	get := func(at Path) Value {
//	    v, err := r.Get(ctx, at)
//	    if err != nil {
//	        v = Value{}          // <- the driver's failure, deleted
//	    }
//	    ...
//	}
//
// So a plane that is failing every read is indistinguishable from a plane that
// is empty, which is 5.1's defect reintroduced one layer above the boundary
// that was fixed to prevent it.
func runB10() {
	ctx := context.Background()
	type Plain struct {
		Name string `ferry:"name"`
		Port int    `ferry:"port"`
	}
	cfg, err := Load[Plain](ctx, bFailSource{})
	fmt.Println("--- a source whose every Get returns a backend failure ---")
	fmt.Printf("  no required field -> cfg=%+v\n                       err=%v\n", cfg, err)
	fmt.Println("  A total backend outage loads as an all-zero struct with a nil error.")
	fmt.Println("  ADR-0001 rules out ignoring anything silently; ADR-0011 gives this")
	fmt.Println("  ErrPlane with ErrDriver provenance; ADR-0011 also rules that ferry")
	fmt.Println("  yields no value it built, and this is a value built out of nothing.")
	type Req struct {
		Host string `ferry:"host,required"`
	}
	c2, e2 := Load[Req](ctx, bFailSource{})
	fmt.Printf("\n  with required     -> %+v\n                       err=%v\n", c2, e2)
	fmt.Println("  Worse than silent: the driver's 503 is REPORTED AS A MISSING KEY, so")
	fmt.Println("  ADR-0011's ErrMissing/ErrPlane split - which exists because \"these six")
	fmt.Println("  keys are unset\" and \"the backend is down\" are different messages for")
	fmt.Println("  different people - is inverted rather than merely absent.")
	fmt.Println()
	fmt.Println("  Not this ticket's to fix. It belongs to whoever implements the walk,")
	fmt.Println("  and it is recorded because it is the third thing this session found")
	fmt.Println("  where prototype code and an Accepted ADR disagree and nothing caught it.")
}
