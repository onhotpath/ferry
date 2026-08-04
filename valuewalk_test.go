package ferry

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// This file is the seam's own side. valueWalk is unexported and reached through
// internal/valuewalk by exactly one caller in this module, so there is no
// exported verb to assert it through and the seam is itself what a test can
// hold: the two methods are [Dump] and [LoadOver] with the root handed over as
// a reflect.Value, and what they add over those two is the refusal of a root a
// type parameter could never have named.

// TestValueWalkDumpsAndLoadsARootHandedOverAsAReflectValue is the seam doing
// what it exists for: one walk in each direction over a root the caller holds
// only as a reflect.Value.
//
// It asserts the same thing the generic verbs are asserted on - what the plane
// was written and what came back - because the point of the seam is that it
// adds no second engine.
func TestValueWalkDumpsAndLoadsARootHandedOverAsAReflectValue(t *testing.T) {
	t.Parallel()

	want := filled()
	sink := newPlane(map[Path]Value{})

	if err := (valueWalk{}).DumpValue(t.Context(), reflect.ValueOf(want), planeSink{p: sink}, nil); err != nil {
		t.Fatalf("DumpValue: %v", err)
	}

	// A fresh destination, because a walk asserted against the value it was
	// seeded from asserts nothing.
	dst := reflect.New(reflect.TypeFor[walkConf]()).Elem()

	if err := (valueWalk{}).LoadValue(t.Context(), dst, planeSource{p: sink}, nil); err != nil {
		t.Fatalf("LoadValue: %v", err)
	}

	got, ok := dst.Interface().(walkConf)
	if !ok {
		t.Fatalf("the destination came back as %T", dst.Interface())
	}

	if got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestValueWalkLoadsOverWhatTheDestinationAlreadyHeld is the other half of
// LoadValue's one argument: the seed travels in dst, so the zero of dst is
// [Load] and a populated dst is [LoadOver].
func TestValueWalkLoadsOverWhatTheDestinationAlreadyHeld(t *testing.T) {
	t.Parallel()

	src := newPlane(map[Path]Value{At("host"): String("db1")})

	over := &walkDB{Host: "seed", Port: "5432"}

	if err := (valueWalk{}).LoadValue(t.Context(), reflect.ValueOf(over).Elem(), planeSource{p: src}, nil); err != nil {
		t.Fatalf("LoadValue: %v", err)
	}

	if over.Host != "db1" {
		t.Errorf("the address the plane held loaded as %q, want %q", over.Host, "db1")
	}

	if over.Port != "5432" {
		t.Errorf("the address the plane did not hold loaded as %q, want the seed %q", over.Port, "5432")
	}
}

// TestValueWalkRefusesARootThatNamesNoType is the refusal a type parameter made
// impossible.
//
// [Dump] and [LoadOver] cannot be handed a root with no type, because T names
// one at the call. A reflect.Value can be the zero reflect.Value, which names
// nothing and so compiles to no schema, and the seam has to say so rather than
// let reflect panic somewhere inside the compiler.
func TestValueWalkRefusesARootThatNamesNoType(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*testing.T) error{
		"dumping": func(sub *testing.T) error {
			sink := planeSink{p: newPlane(map[Path]Value{})}

			return (valueWalk{}).DumpValue(sub.Context(), reflect.Value{}, sink, nil)
		},
		"loading": func(sub *testing.T) error {
			return (valueWalk{}).LoadValue(sub.Context(), reflect.Value{}, planeSource{p: answering()}, nil)
		},
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mustRefuseTheRoot(t, run(t), "names no type")
		})
	}
}

// TestValueWalkRefusesADestinationItCannotWriteTo is [LoadOver]'s
// reflect.ValueOf(&over).Elem() stated as a requirement.
//
// A load writes into its destination, and a reflect.Value that is not
// addressable has nowhere to be written: reflect.ValueOf of a struct is a copy
// the caller cannot see, so a walk into one would report success and change
// nothing.
func TestValueWalkRefusesADestinationItCannotWriteTo(t *testing.T) {
	t.Parallel()

	err := (valueWalk{}).LoadValue(t.Context(), reflect.ValueOf(walkConf{}), planeSource{p: answering()}, nil)

	mustRefuseTheRoot(t, err, "nowhere to write")
}

// mustRefuseTheRoot holds a seam refusal to ADR-0011's class and to the sentence
// the caller reads, which is the whole of what these two arms promise: they are
// reached before any schema exists, so there is no address to name and the text
// is all there is.
func mustRefuseTheRoot(t *testing.T, err error, want string) {
	t.Helper()

	if !errors.Is(err, ErrValue) {
		t.Fatalf("err = %v, want one answering ErrValue", err)
	}

	if report := reportOf(err); !strings.Contains(report, want) {
		t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, want)
	}
}
