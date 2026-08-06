package ferry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The four ways a driver can hand core nothing and no error, which ADR-0004
// makes illegal state and this file holds core to refusing.
//
// Every one of them used to be a nil dereference inside core, on a nil a third
// party produced, which is the one shape ADR-0011's "ferry itself never panics"
// exists for: the stack trace names core and the mistake is the driver's.

// nilBindSource returns no open and no error from Bind.
type nilBindSource struct{}

func (nilBindSource) Bind(*AddressSet) (OpenFunc, error) { return nil, nil }

// nilBindSink is the same on the write side.
type nilBindSink struct{}

func (nilBindSink) Bind(*AddressSet) (OpenWriterFunc, error) { return nil, nil }

// nilOpenSource binds and then returns no reader and no error.
type nilOpenSource struct{}

func (nilOpenSource) Bind(*AddressSet) (OpenFunc, error) {
	return func(context.Context) (Reader, error) { return nil, nil }, nil
}

// nilOpenSink is the same on the write side.
type nilOpenSink struct{}

func (nilOpenSink) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return nil, nil }, nil
}

// contractCfg is a schema with one leaf, which is all a nil open has to be
// asked for.
type contractCfg struct {
	Leaf string `ferry:"leaf"`
}

// TestNilAndNilFromADriverIsRefused is ADR-0004's illegal state, refused where
// it happens and named for the driver rather than dereferenced one line later.
func TestNilAndNilFromADriverIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  func() error
		want string
	}{{
		name: "Bind returns no open",
		run:  func() error { _, err := Load[contractCfg](t.Context(), nilBindSource{}); return err },
		want: "the source returned no error and no open from Bind",
	}, {
		name: "BindSink returns no open",
		run:  func() error { return Dump(t.Context(), contractCfg{}, nilBindSink{}) },
		want: "the sink returned no error and no open from Bind",
	}, {
		name: "the open returns no reader",
		run:  func() error { _, err := Load[contractCfg](t.Context(), nilOpenSource{}); return err },
		want: "the source's open returned no error and no reader",
	}, {
		name: "the open returns no writer",
		run:  func() error { return Dump(t.Context(), contractCfg{}, nilOpenSink{}) },
		want: "the sink's open returned no error and no writer",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkDriverNil(t, tc.run(), tc.want)
		})
	}
}

// checkDriverNil holds one refusal to the class, the provenance and the
// sentence, which is the whole of what a driver author gets to act on.
func checkDriverNil(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("a driver returned nil and nil and the call succeeded, want %q", want)
	}

	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to contain %q", err, want)
	}

	if !errors.Is(err, ErrPlane) {
		t.Errorf("%v is not an ErrPlane", err)
	}

	if !errors.Is(err, ErrDriver) {
		t.Errorf("%v is not marked as a driver's own failure", err)
	}
}

// TestNilAndNilIsRefusedThroughAHeldBinding is the same four states reached
// through the binding rather than through the one-shot verbs, because a held
// binding is where a driver's open is called again and again.
func TestNilAndNilIsRefusedThroughAHeldBinding(t *testing.T) {
	t.Parallel()

	if _, err := Bind[contractCfg](nilBindSource{}); err == nil {
		t.Error("Bind accepted a source that returned no open and no error")
	}

	if _, err := BindSink[contractCfg](nilBindSink{}); err == nil {
		t.Error("BindSink accepted a sink that returned no open and no error")
	}

	b, err := Bind[contractCfg](nilOpenSource{})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if _, err := b.Load(t.Context()); err == nil {
		t.Error("a load through an open that returned no reader and no error succeeded")
	}

	sb, err := BindSink[contractCfg](nilOpenSink{})
	if err != nil {
		t.Fatalf("BindSink: %v", err)
	}

	if err := sb.Dump(t.Context(), contractCfg{}); err == nil {
		t.Error("a dump through an open that returned no writer and no error succeeded")
	}
}

// TestANilOptionIsRefusedAndNotDereferenced is the one hole left in the set of
// nils core refuses with a sentence, and it is the one that used to crash.
//
// An Option list built by appending whatever a helper returned is how a nil
// arrives in one, so the refusal names the position: an Option is opaque and a
// list of three has nothing else to tell one member from another.
func TestANilOptionIsRefusedAndNotDereferenced(t *testing.T) {
	t.Parallel()

	var nilOpt Option

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "Compile", run: func() error { return Compile[contractCfg](nilOpt) }},
		{name: "Load", run: func() error { _, err := Load[contractCfg](t.Context(), empty{}, nilOpt); return err }},
		{name: "LoadOver", run: func() error {
			_, err := LoadOver(t.Context(), contractCfg{}, empty{}, nilOpt)

			return err
		}},
		{name: "Dump", run: func() error { return Dump(t.Context(), contractCfg{}, discard{}, nilOpt) }},
		{name: "Bind", run: func() error { _, err := Bind[contractCfg](empty{}, nilOpt); return err }},
		{name: "BindSink", run: func() error { _, err := BindSink[contractCfg](discard{}, nilOpt); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkNilOption(t, tc.run())
		})
	}
}

// checkNilOption holds one entry point's refusal of a nil Option to the
// position and the class.
func checkNilOption(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("a nil Option was accepted")
	}

	if !strings.Contains(err.Error(), "the Option at position 0 is nil") {
		t.Errorf("error = %v, want it to name the position of the nil Option", err)
	}

	if !errors.Is(err, ErrSchema) {
		t.Errorf("%v is not an ErrSchema", err)
	}
}

// TestEveryNilOptionIsReported is the aggregation rule applied to the same
// hole: newConfig reports every Option that was wrong rather than the first.
func TestEveryNilOptionIsReported(t *testing.T) {
	t.Parallel()

	var nilOpt Option

	err := Compile[contractCfg](nilOpt, TagKey("cfg"), nilOpt)
	if err == nil {
		t.Fatal("two nil Options were accepted")
	}

	report := fmt.Sprintf("%+v", err)
	for _, want := range []string{"position 0", "position 2"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not name the Option at %s:\n%s", want, report)
		}
	}
}

// empty and discard are the two planes the Option tests reach for, because what
// is under test is the Option list and neither call should reach a plane at all.

// discard is a sink that takes every write and keeps nothing.
type discard struct{}

func (discard) Bind(*AddressSet) (OpenWriterFunc, error) {
	return func(context.Context) (Writer, error) { return discardWriter{}, nil }, nil
}

type discardWriter struct{}

func (discardWriter) Set(context.Context, LeafAddr, Value) error { return nil }

// empty is a source holding nothing, so every address is absent.
type empty struct{}

func (empty) Bind(*AddressSet) (OpenFunc, error) {
	return func(context.Context) (Reader, error) { return emptyReader{}, nil }, nil
}

type emptyReader struct{}

func (emptyReader) Get(context.Context, LeafAddr) (Value, error) { return Value{}, nil }
