package env

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// This file is the #309 prototype's whole exhibit: one environment, two
// schemas, and what each of the two paths does with it.
//
// K1 is what ships. The words decide plane-wide, so FEATURE=on arrives as a
// Bool wherever it is read and a string field over it is refused.
//
// K2 is what is prototyped here. The address set carries the schema's kind at
// every address, the driver reads it at Bind, and the words apply only where
// the schema wants a bool.

// ungated builds a K1 source out of the K2 driver, so both paths are runnable
// from one branch. It is not shipped surface.
func ungated() Option { return optionFunc(func(c *config) { c.ungated = true }) }

// oneEnvironment is the one environment both readers are pointed at.
func oneEnvironment() Option {
	return Environ(func() []string { return []string{"FEATURE=on"} })
}

// readerA wants a bool at FEATURE.
type readerA struct {
	Feature bool `ferry:"feature"`
}

// readerB wants the text at FEATURE.
type readerB struct {
	Feature string `ferry:"feature"`
}

func TestTwoReadersOfOneEnvironmentUnderK1(t *testing.T) {
	t.Parallel()

	words := []Option{BoolWords("on", "off"), oneEnvironment(), ungated()}

	a, err := ferry.Load[readerA](context.Background(), New(words...))
	if err != nil {
		t.Fatalf("K1 reader A: %v", err)
	}

	if !a.Feature {
		t.Fatalf("K1 reader A: got %v, want true", a.Feature)
	}

	b, err := ferry.Load[readerB](context.Background(), New(words...))
	if err == nil {
		t.Fatalf("K1 reader B: loaded %q, want a refusal", b.Feature)
	}

	if !errors.Is(err, ferry.ErrPlane) && !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("K1 reader B refused with %v", err)
	}

	t.Logf("K1 reader B: %v", err)
}

func TestTwoReadersOfOneEnvironmentUnderK2(t *testing.T) {
	t.Parallel()

	words := []Option{BoolWords("on", "off"), oneEnvironment()}

	a, err := ferry.Load[readerA](context.Background(), New(words...))
	if err != nil {
		t.Fatalf("K2 reader A: %v", err)
	}

	if !a.Feature {
		t.Fatalf("K2 reader A: got %v, want true", a.Feature)
	}

	b, err := ferry.Load[readerB](context.Background(), New(words...))
	if err != nil {
		t.Fatalf("K2 reader B: %v", err)
	}

	if b.Feature != "on" {
		t.Fatalf("K2 reader B: got %q, want %q", b.Feature, "on")
	}
}

// mixed is the collision the shipped sharp edge is about: one schema holding a
// bool and a string whose legal value is a declared word.
type mixed struct {
	Feature bool   `ferry:"feature"`
	Mode    string `ferry:"mode"`
}

func mixedEnvironment() Option {
	return Environ(func() []string { return []string{"FEATURE=on", "MODE=on"} })
}

func TestTheCollisionInsideOneSchema(t *testing.T) {
	t.Parallel()

	k1, err := ferry.Load[mixed](context.Background(),
		New(BoolWords("on", "off"), mixedEnvironment(), ungated()))
	if err == nil {
		t.Fatalf("K1 mixed: loaded %+v, want a refusal", k1)
	}

	t.Logf("K1 mixed: %v", err)

	k2, err := ferry.Load[mixed](context.Background(),
		New(BoolWords("on", "off"), mixedEnvironment()))
	if err != nil {
		t.Fatalf("K2 mixed: %v", err)
	}

	if !k2.Feature || k2.Mode != "on" {
		t.Fatalf("K2 mixed: got %+v, want {true on}", k2)
	}
}

// minted is the address a value mints rather than the type: a map of bools,
// whose members are in no address set.
type minted struct {
	Flags map[string]bool `ferry:"flags"`
}

func TestAMintedAddressIsGatedByItsComposite(t *testing.T) {
	t.Parallel()

	environ := Environ(func() []string { return []string{"FLAGS_BETA=on"} })

	got, err := ferry.Load[minted](context.Background(), New(BoolWords("on", "off"), environ))
	if err != nil {
		t.Fatalf("K2 minted: %v", err)
	}

	if !got.Flags["beta"] {
		t.Fatalf("K2 minted: got %+v, want beta true", got.Flags)
	}
}

// sink is a flat text plane with this package's own boolean spelling, written
// so the dump direction is exhibitable: there is no env.Sink, and there is no
// second first-party plane that spells booleans with words.
//
// It is deliberately the whole of the write side: a Value already carries its
// kind, so the gate has nothing to decide here.
type sink struct {
	cfg config
	out map[string]string
}

func (s *sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, err := ferry.NewKeys(addrs, driverName, s.cfg.key)
	if err != nil {
		return nil, err
	}

	name := keys.Open()

	return func(context.Context) (ferry.Writer, error) {
		return &writer{cfg: s.cfg, keys: name, out: s.out}, nil
	}, nil
}

type writer struct {
	cfg  config
	keys ferry.KeyFunc
	out  map[string]string
}

func (w *writer) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	key, err := w.keys(addr.Path())
	if err != nil {
		return err
	}

	text, err := w.render(v)
	if err != nil {
		return err
	}

	w.out[key] = text

	return nil
}

// render is the write half of the spelling: a Bool goes through the words and
// everything else is the text the plane carries.
func (w *writer) render(v ferry.Value) (string, error) {
	if v.Kind() != ferry.KindBool || w.cfg.bools == nil {
		return v.AsString()
	}

	b, err := v.AsBool()
	if err != nil {
		return "", err
	}

	return w.cfg.bools.Render(b)
}

func dumpThrough[T any](t *testing.T, value T, opts ...Option) map[string]string {
	t.Helper()

	out := map[string]string{}

	c := defaults()
	for _, o := range opts {
		o.apply(&c)
	}

	if err := ferry.Dump(context.Background(), value, &sink{cfg: c, out: out}); err != nil {
		t.Fatalf("dump: %v", err)
	}

	return out
}

func TestTheDumpDirectionIsTheSameUnderBothPaths(t *testing.T) {
	t.Parallel()

	words := BoolWords("on", "off")

	if got := dumpThrough(t, readerA{Feature: true}, words); got["FEATURE"] != "on" {
		t.Fatalf("reader A dumped %v", got)
	}

	if got := dumpThrough(t, readerB{Feature: "on"}, words); got["FEATURE"] != "on" {
		t.Fatalf("reader B dumped %v", got)
	}

	if got := dumpThrough(t, mixed{Feature: true, Mode: "on"}, words); got["FEATURE"] != "on" ||
		got["MODE"] != "on" {
		t.Fatalf("mixed dumped %v", got)
	}
}

// TestTheRoundTripClosesOnlyUnderK2 is the whole argument in one test: both
// paths write the same plane, and only one of them reads it back.
func TestTheRoundTripClosesOnlyUnderK2(t *testing.T) {
	t.Parallel()

	written := dumpThrough(t, readerB{Feature: "on"}, BoolWords("on", "off"))
	environ := Environ(func() []string { return environSlice(written) })

	if _, err := ferry.Load[readerB](context.Background(),
		New(BoolWords("on", "off"), environ, ungated())); err == nil {
		t.Fatal("K1: the round trip closed, and the shipped sharp edge says it does not")
	}

	back, err := ferry.Load[readerB](context.Background(), New(BoolWords("on", "off"), environ))
	if err != nil {
		t.Fatalf("K2 round trip: %v", err)
	}

	if back.Feature != "on" {
		t.Fatalf("K2 round trip: got %q, want %q", back.Feature, "on")
	}
}

// textFlags is the same map one kind over: the words must not touch it.
type textFlags struct {
	Flags map[string]string `ferry:"flags"`
}

func TestAMintedAddressAtAStringElementIsNotGated(t *testing.T) {
	t.Parallel()

	environ := Environ(func() []string { return []string{"FLAGS_BETA=on"} })

	got, err := ferry.Load[textFlags](context.Background(), New(BoolWords("on", "off"), environ))
	if err != nil {
		t.Fatalf("K2 minted text: %v", err)
	}

	if got.Flags["beta"] != "on" {
		t.Fatalf("K2 minted text: got %+v, want beta on", got.Flags)
	}
}

// Example_k1TheShippedPath is what a user writes today and what they get: the
// words are a fact about the whole plane, so the string field is refused.
func Example_k1TheShippedPath() {
	type Config struct {
		Feature bool   `ferry:"feature"`
		Mode    string `ferry:"mode"` // its legal values include "on"
	}

	src := New(
		BoolWords("on", "off"),
		Environ(func() []string { return []string{"FEATURE=on", "MODE=on"} }),
		ungated(), // K1: what ships
	)

	cfg, err := ferry.Load[Config](context.Background(), src)
	fmt.Println(cfg.Feature, cfg.Mode, err)
	// Output: false  ferry: MODE: what is set here is bool, and string cannot take one
}

// Example_k2TheKindGate is the same program under the prototyped gate: the
// words apply where the schema wants a bool and nowhere else.
func Example_k2TheKindGate() {
	type Config struct {
		Feature bool   `ferry:"feature"`
		Mode    string `ferry:"mode"`
	}

	src := New(
		BoolWords("on", "off"),
		Environ(func() []string { return []string{"FEATURE=on", "MODE=on"} }),
	)

	cfg, err := ferry.Load[Config](context.Background(), src)
	fmt.Println(cfg.Feature, cfg.Mode, err)
	// Output: true on <nil>
}

// BenchmarkLoad is what the gate costs on the read path: the same load with the
// address consulted and without it (proto: #309).
func BenchmarkLoad(b *testing.B) {
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"k1_plane_wide", []Option{BoolWords("on", "off"), oneEnvironment(), ungated()}},
		{"k2_kind_gated", []Option{BoolWords("on", "off"), oneEnvironment()}},
	} {
		b.Run(tc.name, func(b *testing.B) { benchLoad(b, tc.opts) })
	}
}

// benchLoad is one load, repeated, through a binding made once.
func benchLoad(b *testing.B, opts []Option) {
	b.Helper()

	binding, err := ferry.Bind[readerA](New(opts...))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := binding.LoadOver(context.Background(), readerA{}); err != nil {
			b.Fatal(err)
		}
	}
}

func environSlice(vars map[string]string) []string {
	out := make([]string, 0, len(vars))
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		out = append(out, k+"="+vars[k])
	}

	return out
}
