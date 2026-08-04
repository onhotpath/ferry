package perschema_test

import (
	"testing"

	"github.com/onhotpath/ferry"
	ps "github.com/onhotpath/ferry/proto/perschema"
)

// TestQ5CoreDistinguishesWhatTheDriverCannot is the whole of the core-help
// question, measured rather than argued.
//
// The driver is handed a byte-identical AddressSet for []string and for string.
// Core then drives the two differently at load. So the bit the driver needs
// exists in core, on the other side of the Bind boundary, and the AddressSet is
// where it stops.
func TestQ5CoreDistinguishesWhatTheDriverCannot(t *testing.T) {
	t.Logf("what the driver is handed at Bind:")
	t.Logf("  Encodings []string -> %s", render(setFor[Encodings](t)))
	t.Logf("  Encoding  string   -> %s", render(setFor[Encoding](t)))
	t.Logf("  identical: %t", render(setFor[Encodings](t)) == render(setFor[Encoding](t)))

	t.Logf("")
	t.Logf("what core then does with it, at load, over the same plane:")

	ctx := ctxWith(gzip(), br())

	var seq []string

	got, err := ferry.Load[Encodings](ctx, ps.NewSource(ps.Trace(&seq)))
	t.Logf("  Encodings []string -> %s", outcome(got, err))

	for _, s := range seq {
		t.Logf("      %s", s)
	}

	seq = nil

	got2, err2 := ferry.Load[Encoding](ctx, ps.NewSource(ps.Trace(&seq)))
	t.Logf("  Encoding  string   -> %s", outcome(got2, err2))

	for _, s := range seq {
		t.Logf("      %s", s)
	}

	t.Logf("")
	t.Logf("core called Children at /accept-encoding for one and not for the other,")
	t.Logf("from the same set, so the bit is core's and it is not in what Bind is given.")
}

// TestQ5WhenTheDriverLearnsIt pins the moment the information becomes available
// to the driver, which is the thing that decides where a refusal can land.
func TestQ5WhenTheDriverLearnsIt(t *testing.T) {
	ctx := ctxWith(gzip())

	for _, c := range []struct {
		label string
		run   func(*[]string) string
	}{
		{"Encodings []string", func(tr *[]string) string {
			return outcome(ferry.Load[Encodings](ctx, ps.NewSource(ps.Repeatable("Accept-Encoding"), ps.Trace(tr))))
		}},
		{"Encoding  string  ", func(tr *[]string) string {
			return outcome(ferry.Load[Encoding](ctx, ps.NewSource(ps.Repeatable("Accept-Encoding"), ps.Trace(tr))))
		}},
	} {
		var tr []string

		out := c.run(&tr)
		t.Logf("%s -> %s", c.label, out)

		for _, s := range tr {
			t.Logf("    %s", s)
		}
	}

	t.Logf("")
	t.Logf("Bind     : identical sets, nothing to tell them apart")
	t.Logf("Get      : one address, no arity, no kind - still nothing")
	t.Logf("Children : called only for the sequence, so THIS is where the driver learns it")
	t.Logf("Close    : the last moment, and the only one at which the driver knows and can still speak")
}

// TestQ5WhatCrossesTheBoundary establishes what a core change would have to see,
// by measuring that a driver's declaration is invisible to core.
func TestQ5WhatCrossesTheBoundary(t *testing.T) {
	t.Logf("Source.Bind(*AddressSet) (OpenFunc, error)")
	t.Logf("  in  : the address set, and nothing else")
	t.Logf("  out : an OpenFunc, or an error")
	t.Logf("")
	t.Logf("so the only thing a driver can say back at Bind is 'no', and core learns")
	t.Logf("nothing about WHY. Two Sources carrying opposite declarations against one")
	t.Logf("schema are, to core, the same call:")

	ctx := ctxWith(gzip())

	for _, c := range []struct {
		label string
		src   ferry.Source
	}{
		{"no declaration              ", ps.NewSource()},
		{"Repeatable(Accept-Encoding) ", ps.NewSource(ps.Repeatable("Accept-Encoding"))},
		{"Alias(accept-encoding, x-ae)", ps.NewSource(ps.Alias("accept-encoding", "x-ae"))},
	} {
		t.Logf("  %s  set core handed it = %s   load -> %s",
			c.label, render(setFor[Encoding](t)), outcome(ferry.Load[Encoding](ctx, c.src)))
	}

	t.Logf("")
	t.Logf("core cannot refuse a mismatch it cannot see. For it to refuse one it would")
	t.Logf("need both halves in one place: the declaration, which only the driver has,")
	t.Logf("and the container bit, which only core has.")
}

// TestQ5TheBitCoreAlreadyHolds shows the bit is not something core would have to
// compute. It is computed, used, and dropped.
func TestQ5TheBitCoreAlreadyHolds(t *testing.T) {
	t.Logf("core's compiler holds leaves and containers as two slices and the address")
	t.Logf("set is their union, so the bit exists and is discarded at one line.")
	t.Logf("")
	t.Logf("measured through the only window on it that is exported - which addresses")
	t.Logf("core puts in the set at all:")

	type Ptr struct {
		Sect *struct {
			Host string `ferry:"host"`
		} `ferry:"sect"`
	}

	type Plain struct {
		Sect struct {
			Host string `ferry:"host"`
		} `ferry:"sect"`
	}

	for _, c := range []struct {
		label string
		set   *ferry.AddressSet
	}{
		{"*struct{Host}  (nillable, so it has a container address)", setFor[Ptr](t)},
		{"struct{Host}   (not nillable, so it has none)           ", setFor[Plain](t)},
		{"[]string       (nillable)                               ", setFor[Encodings](t)},
		{"string                                                  ", setFor[Encoding](t)},
	} {
		t.Logf("  %s -> %s", c.label, render(c.set))
	}

	t.Logf("")
	t.Logf("a container address is in the set when the composite is nillable, and")
	t.Logf("/accept-encoding is in it for both []string and string, so membership is")
	t.Logf("not the bit either.")
}
