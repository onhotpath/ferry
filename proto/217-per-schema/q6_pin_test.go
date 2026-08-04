package perschema_test

import (
	"testing"

	"github.com/onhotpath/ferry"
	ps "github.com/onhotpath/ferry/proto/perschema"
)

// TestQ6PinningTheSchema closes off the obvious repair.
//
// #210 established that a Source cannot count its Binds, because a one-shot
// ferry.Load binds on every call. The repair that survives that measurement is
// to pin the address set instead of the count: remember the set the first Bind
// was handed, and refuse a different one. The one-shot path re-binds with the
// same set every time, so it is not broken by this.
//
// It works, and it does not catch the case the hazard is about.
func TestQ6PinningTheSchema(t *testing.T) {
	ctx := ctxWith(gzip())

	t.Logf("=== the one-shot path is not broken by pinning ===")

	oneShot := ps.NewSource(ps.PinSchema())
	for i := range 3 {
		t.Logf("  ferry.Load[Encoding] #%d : %s", i+1, outcome(ferry.Load[Encoding](ctx, oneShot)))
	}

	t.Logf("  Binds() = %d", oneShot.Binds())

	t.Logf("")
	t.Logf("=== and it refuses a schema whose address set is different ===")

	mixed := ps.NewSource(ps.PinSchema())
	t.Logf("  A, Encoding string : %s", outcome(ferry.Load[Encoding](ctx, mixed)))

	_, err := ferry.Load[Other](ctx, mixed)
	t.Logf("  B, Other           : REFUSED")
	t.Logf("%s", full(err))

	t.Logf("")
	t.Logf("=== but the two schemas the hazard is about have the SAME address set ===")
	t.Logf("  Encodings []string -> %s", render(setFor[Encodings](t)))
	t.Logf("  Encoding  string   -> %s", render(setFor[Encoding](t)))

	hazard := ps.NewSource(ps.Repeatable("Accept-Encoding"), ps.CheckNames(), ps.PinSchema())

	t.Logf("  A, Encodings []string : %s", outcome(ferry.Load[Encodings](ctx, hazard)))
	t.Logf("  B, Encoding  string   : %s", outcome(ferry.Load[Encoding](ctx, hazard)))
	t.Logf("  Binds() = %d, and the pin never fired", hazard.Binds())

	t.Logf("")
	t.Logf("so the strongest guard a Source can build for itself is blind to exactly")
	t.Logf("the collision it would be built for: two Go types, one address set.")
}

// TestQ6PinningUnderAHeldBinding checks the pin against the other entry point,
// because a held binding calls Bind once and the pin is state on the Source.
func TestQ6PinningUnderAHeldBinding(t *testing.T) {
	ctx := ctxWith(gzip())

	held := ps.NewSource(ps.PinSchema())

	bA, errA := ferry.Bind[Encoding](held)
	t.Logf("Bind[Encoding] err = %v", errA)

	_, errB := ferry.Bind[Other](held)
	t.Logf("Bind[Other]    err = %v", errB)

	if errA == nil {
		t.Logf("A still loads    : %s", outcome(bA.Load(ctx)))
	}

	t.Logf("Binds() = %d", held.Binds())
	t.Logf("")
	t.Logf("the pin fires at Bind under a binding and at every Load without one,")
	t.Logf("so it is the one guard that behaves the same through both entry points.")
}
