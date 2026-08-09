package env

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
)

// One environment read two ways.
//
// The words are consulted where the schema wants a bool and nowhere else, so a
// variable holding a declared word is a boolean at a bool field and text at a
// string field. Every test here is that one fact, at a different address kind
// (ADR-0016, ADR-0018).

// gateBool wants a bool at FEATURE.
type gateBool struct {
	Feature bool `ferry:"feature"`
}

// gateText wants the text at FEATURE.
type gateText struct {
	Feature string `ferry:"feature"`
}

// oneEnvironment is the environment both readers are pointed at, and it is one
// closure so that the two loads cannot be reading two different planes.
func oneEnvironment() Option {
	return Environ(func() []string { return []string{"FEATURE=on"} })
}

// TestTwoReadersOfOneEnvironment is the exhibit: one variable, two schemas, and
// each of them getting what it asked for.
func TestTwoReadersOfOneEnvironment(t *testing.T) {
	t.Parallel()

	words := []Option{BoolWords("on", "off"), oneEnvironment()}

	a, err := ferry.Load[gateBool](t.Context(), New(words...))
	if err != nil {
		t.Fatalf("the bool reader: %v", err)
	}

	if !a.Feature {
		t.Errorf("the bool reader loaded %v, want true", a.Feature)
	}

	b, err := ferry.Load[gateText](t.Context(), New(words...))
	if err != nil {
		t.Fatalf("the string reader: %v", err)
	}

	if b.Feature != "on" {
		t.Errorf("the string reader loaded %q, want %q", b.Feature, "on")
	}
}

// gateMixed is the collision the retired sharp edge was about: one schema
// holding a bool and a string whose legal value is a declared word.
type gateMixed struct {
	Feature bool   `ferry:"feature"`
	Mode    string `ferry:"mode"`
}

// TestTheCollisionInsideOneSchema is why the change is worth making: the old
// remedy was to choose words your text values do not use, and that is not
// available to a schema author who does not own the Source.
func TestTheCollisionInsideOneSchema(t *testing.T) {
	t.Parallel()

	environ := Environ(func() []string { return []string{"FEATURE=on", "MODE=on"} })

	got, err := ferry.Load[gateMixed](t.Context(), New(BoolWords("on", "off"), environ))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !got.Feature || got.Mode != "on" {
		t.Errorf("loaded %+v, want {true on}", got)
	}
}

// gateBoolFlags and gateTextFlags are one variable under two mappings, whose
// member addresses the value mints and no Bind ever sees.
type gateBoolFlags struct {
	Flags map[string]bool `ferry:"flags"`
}

type gateTextFlags struct {
	Flags map[string]string `ferry:"flags"`
}

// TestAMintedAddressIsGatedByItsOwnKind pins the walk: an address the value
// minted carries the schema's answer, so the words reach a bool element and
// leave a string element alone.
func TestAMintedAddressIsGatedByItsOwnKind(t *testing.T) {
	t.Parallel()

	environ := Environ(func() []string { return []string{"FLAGS_BETA=on"} })
	words := []Option{BoolWords("on", "off"), environ}

	flags, err := ferry.Load[gateBoolFlags](t.Context(), New(words...))
	if err != nil {
		t.Fatalf("the bool mapping: %v", err)
	}

	if !flags.Flags["beta"] {
		t.Errorf("the bool mapping loaded %+v, want beta true", flags.Flags)
	}

	text, err := ferry.Load[gateTextFlags](t.Context(), New(words...))
	if err != nil {
		t.Fatalf("the string mapping: %v", err)
	}

	if text.Flags["beta"] != "on" {
		t.Errorf("the string mapping loaded %+v, want beta on", text.Flags)
	}
}

// wordSink is a flat text plane spelling booleans with this plane's own words,
// written so that the write half of the exhibit is exhibitable at all: there is
// no env.Sink and this package will not grow one.
//
// It is the whole of the write side, and that is the point: a Value carries its
// own kind, so a Bool goes through the words and everything else is text, and
// there is nothing here for an address to decide.
type wordSink struct {
	cfg config
	out map[string]string
}

func (s *wordSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, err := ferry.NewKeys(addrs, driverName, s.cfg.key)
	if err != nil {
		return nil, err
	}

	name := keys.Open()

	return func(context.Context) (ferry.Writer, error) {
		return wordWriter{cfg: s.cfg, keys: name, out: s.out}, nil
	}, nil
}

type wordWriter struct {
	cfg  config
	keys ferry.KeyFunc
	out  map[string]string
}

func (w wordWriter) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	key, err := w.keys(addr.Path())
	if err != nil {
		return err
	}

	text, err := w.render(v)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	w.out[key] = text

	return nil
}

// render is the write half of the spelling: a Bool goes through the words and
// everything else is the text the plane carries.
func (w wordWriter) render(v ferry.Value) (string, error) {
	if v.Kind() != ferry.KindBool || w.cfg.bools == nil {
		return v.AsString()
	}

	b, err := v.AsBool()
	if err != nil {
		return "", err
	}

	return w.cfg.bools.Render(b)
}

// dumpThrough writes one value through the flat sink and hands back what landed
// in the plane.
func dumpThrough[T any](t *testing.T, value T, opts ...Option) map[string]string {
	t.Helper()

	out := map[string]string{}

	c := defaults()
	for _, o := range opts {
		o.apply(&c)
	}

	if err := ferry.Dump(t.Context(), value, &wordSink{cfg: c, out: out}); err != nil {
		t.Fatalf("dump: %v", err)
	}

	return out
}

// TestTheRoundTripCloses is the strongest thing the change buys: ferry wrote
// this plane through a sink of its own and then read it back through its own
// source, for the same field.
func TestTheRoundTripCloses(t *testing.T) {
	t.Parallel()

	words := BoolWords("on", "off")

	written := dumpThrough(t, gateMixed{Feature: true, Mode: "on"}, words)
	if written["FEATURE"] != "on" || written["MODE"] != "on" {
		t.Fatalf("the sink wrote %v, want FEATURE and MODE both on", written)
	}

	environ := Environ(func() []string { return environSlice(written) })

	back, err := ferry.Load[gateMixed](t.Context(), New(words, environ))
	if err != nil {
		t.Fatalf("the round trip: %v", err)
	}

	if !back.Feature || back.Mode != "on" {
		t.Errorf("the round trip read back %+v, want {true on}", back)
	}
}

// environSlice renders a written plane the way os.Environ does.
func environSlice(vars map[string]string) []string {
	out := make([]string, 0, len(vars))
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		out = append(out, k+"="+vars[k])
	}

	return out
}

// BenchmarkLoad is one load through a binding, so that the claim the gate costs
// nothing on the read path stays measured rather than remembered.
func BenchmarkLoad(b *testing.B) {
	binding, err := ferry.Bind[gateBool](New(BoolWords("on", "off"), oneEnvironment()))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := binding.LoadOver(b.Context(), gateBool{}); err != nil {
			b.Fatal(err)
		}
	}
}
