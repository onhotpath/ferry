package main

// E10: auditing my own call that Compile[T] freezes the registry.
//
// The draft weighs two readings and picks freezing, on the grounds that a
// non-freezing Compile "reports a failure that never happens". Both readings
// are loud, and the draft never asked the next question: loud TO WHOM, and
// what happens to the caller who does not look.

import (
	"context"
	"fmt"
	"reflect"
)

type E10Opaque struct{ v string }

func (o E10Opaque) text() string { return o.v }

type E10Conf struct {
	Listen E10Opaque `ferry:"listen"`
	Name   string    `ferry:"name"`
}

func e10Codec() Reg {
	return StringCodec(E10Opaque.text, func(s string) (E10Opaque, error) { return E10Opaque{s}, nil })
}

func runE10() {
	ctx := context.Background()

	fmt.Println("--- E10a: the freeze, when the failed Register's error IS checked ---")
	fmt.Println("  (with Compile made to freeze, which is what the draft decided)")
	reg := NewRegistry()
	freezingCompile[E10Conf](reg) // an early Compile, e.g. from a package-level var
	if err := reg.Register(e10Codec()); err != nil {
		fmt.Printf("  Register -> %v\n", err)
	}
	fmt.Println("  Loud, and the user acts on it. This is the case the draft weighed.")

	fmt.Println("\n--- E10b: and when it is NOT checked, which the draft never asked ---")
	fmt.Println("  The first fixture for this was WRONG and is recorded rather than replaced:")
	reg2 := NewRegistry()
	freezingCompile[E10Conf](reg2)
	reg2.Register(e10Codec()) // error dropped, as an init() commonly does
	out, err := dumpTo(ctx, E10Conf{E10Opaque{"L"}, "n"}, WithRegistry(reg2))
	fmt.Printf("    a type that only compiles WITH the codec -> err=%v\n", trunc(err))
	fmt.Println("    Loud. A type that needs the registration to compile at all cannot")
	fmt.Println("    go wrong quietly, so this fixture measures nothing.")
	_ = out

	fmt.Println("\n  The case that matters is a type that compiles BOTH ways with DIFFERENT")
	fmt.Println("  representations, which ADR-0007's chain-before-kind makes ordinary:")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	regA := NewRegistry()
	outA, errA := dumpTo(ctx, E10Wrap{E10Level{2}}, WithRegistry(regA))
	fmt.Printf("    unregistered, the chain claims it        -> %v err=%v\n", fmtAddrs(outA), trunc(errA))

	regB := NewRegistry()
	mustReg(regB, e10LevelCodec())
	outB, _ := dumpTo(ctx, E10Wrap{E10Level{2}}, WithRegistry(regB))
	fmt.Printf("    registered, the table claims it          -> %v\n", fmtAddrs(outB))

	regC := NewRegistry()
	_ = Compile[E10Wrap](WithRegistry(regC)) // an early Compile freezes it
	regC.Register(e10LevelCodec())           // error dropped
	outC, errC := dumpTo(ctx, E10Wrap{E10Level{2}}, WithRegistry(regC))
	fmt.Printf("    Compile first, Register error DROPPED    -> %v err=%v\n", fmtAddrs(outC), trunc(errC))
	fmt.Println("    No error, and the plane holds the representation the user replaced.")
	fmt.Println("    That is ADR-0009's staleness defect reached through a Compile.")

	fmt.Println("\n--- E10c: the same two, if Compile does NOT freeze and does NOT cache ---")
	reg4 := NewRegistry()
	fmt.Printf("  Compile before the registration          -> %v\n", trunc(Compile[E10Wrap](WithRegistry(reg4))))
	reg4.Register(e10LevelCodec()) // error dropped, exactly as in E10b
	out4, _ := dumpTo(ctx, E10Wrap{E10Level{2}}, WithRegistry(reg4))
	fmt.Printf("  Register error DROPPED, then Dump        -> %v\n", fmtAddrs(out4))
	fmt.Println("  The registration lands, so the plane holds what the user asked for.")
	fmt.Println("  Compile takes NEITHER the cache nor the freeze, and it is one omission:")
	fmt.Println("  ADR-0009's obligation is that a registry's answer for a type must never")
	fmt.Println("  change once that type has been RESOLVED against it, and a resolution")
	fmt.Println("  that is discarded retains nothing that could go stale.")

	fmt.Println("\n--- E10d: so what each reading actually costs ---")
	fmt.Println("  (the middle row is the finding, and the draft picked the wrong column)")
	fmt.Println("                                   freezing            not freezing")
	fmt.Println("  Compile at init, error checked    a loud Register     a wrong Compile")
	fmt.Println("                                    failure             error, at the")
	fmt.Println("                                                        Compile call")
	fmt.Println("  Compile at init, error DROPPED    the codec never     nothing: the")
	fmt.Println("                                    lands, and the      registration")
	fmt.Println("                                    plane silently      lands")
	fmt.Println("                                    gets a different")
	fmt.Println("                                    representation")
	fmt.Println("  Compile from a test               identical           identical")
	fmt.Println()
	fmt.Println("  The bottom row is the one that matters for the common case, and the")
	fmt.Println("  middle row is the one the draft never looked at.")
}

// freezingCompile is the draft's Compile: it goes through the cache, so it
// records a use of the registry. Kept as a probe fixture so the two readings
// can be run side by side rather than argued.
func freezingCompile[T any](r *Registry) error {
	o := defaultOpts()
	o.reg = r
	_, err := schemaFor(reflect.TypeFor[T](), o)
	return err
}

// E10Level is a type the CHAIN claims (it has a text pair) and which a
// registration would represent differently. ADR-0009 measured the same shape
// on slog.Level: unregistered string("WARN"), registered number("4").
type E10Level struct{ n int }

func (l E10Level) MarshalText() ([]byte, error) { return []byte(e10Names[l.n]), nil }

func (l *E10Level) UnmarshalText(b []byte) error {
	for i, n := range e10Names {
		if n == string(b) {
			l.n = i
			return nil
		}
	}
	return fmt.Errorf("bad level %q", b)
}

var e10Names = []string{"DEBUG", "INFO", "WARN", "ERROR"}

type E10Wrap struct {
	Level E10Level `ferry:"level"`
}

func e10LevelCodec() Reg {
	return ValueCodec(VNumber,
		func(l E10Level) (Value, error) { return Int(int64(l.n)), nil },
		func(v Value) (E10Level, error) {
			s, err := v.AsNumber()
			if err != nil {
				return E10Level{}, err
			}
			var n int
			fmt.Sscanf(s, "%d", &n)
			return E10Level{n}, nil
		})
}

func fmtAddrs(m map[Path]Value) string {
	out := ""
	for _, p := range sortedAddrs(m) {
		out += fmt.Sprintf("%q=%s ", p.String(), m[p].GoString())
	}
	return out
}
