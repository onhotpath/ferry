package perschema_test

import (
	"context"
	"testing"

	"github.com/onhotpath/ferry"
	ps "github.com/onhotpath/ferry/proto/perschema"
)

// TestQ1Reproduce re-measures #210's three established results on this base -
// origin/main at fc8707c merged with origin/feat/202-caller-held-binding - in a
// lean driver of this package's own rather than in #210's.
//
// #210's own code is copied verbatim into ./repro210 and produces the same three
// results there; this is the same three against a second implementation, so a
// result that survives is a property of core rather than of one driver.
func TestQ1Reproduce(t *testing.T) {
	t.Logf("=== 1. the baseline: one Source per schema, no declaration ===")
	t.Logf("  Encodings []string  <- gzip, br : %s",
		outcome(ferry.Load[Encodings](ctxWith(gzip(), br()), ps.NewSource())))
	t.Logf("  Encoding  string    <- gzip     : %s",
		outcome(ferry.Load[Encoding](ctxWith(gzip()), ps.NewSource())))
	t.Logf("  Encodings []string  <- gzip     : %s",
		outcome(ferry.Load[Encodings](ctxWith(gzip()), ps.NewSource())))

	t.Logf("")
	t.Logf("=== 2. one Source carrying Repeatable, two schemas, through ferry.Load ===")

	shared := ps.NewSource(ps.Repeatable("Accept-Encoding"))
	ctx := ctxWith(gzip())

	t.Logf("  handler A, Encodings []string: %s", outcome(ferry.Load[Encodings](ctx, shared)))
	t.Logf("  handler B, Encoding  string  : %s", outcome(ferry.Load[Encoding](ctx, shared)))
	t.Logf("  Source.Binds() after two loads = %d", shared.Binds())

	t.Logf("")
	t.Logf("=== 3. the same, through ferry.Bind ===")

	held := ps.NewSource(ps.Repeatable("Accept-Encoding"))

	bA, err := ferry.Bind[Encodings](held)
	if err != nil {
		t.Fatalf("bind A: %v", err)
	}

	bB, err := ferry.Bind[Encoding](held)
	if err != nil {
		t.Fatalf("bind B: %v", err)
	}

	t.Logf("  binding A, Encodings []string: %s", outcome(bA.Load(ctx)))
	t.Logf("  binding B, Encoding  string  : %s", outcome(bB.Load(ctx)))
	t.Logf("  Source.Binds() after two binds and two loads = %d", held.Binds())
}

// TestQ1BindsCount is the third established result: a Source cannot enforce
// one-schema-per-source for itself, because it is re-bound on every ferry.Load.
func TestQ1BindsCount(t *testing.T) {
	ctx := ctxWith(gzip())

	oneShot := ps.NewSource()
	for range 3 {
		if _, err := ferry.Load[Encoding](ctx, oneShot); err != nil {
			t.Fatalf("load: %v", err)
		}
	}

	t.Logf("three ferry.Load calls over one Source, one schema : Binds() = %d", oneShot.Binds())

	bound := ps.NewSource()

	b, err := ferry.Bind[Encoding](bound)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	for range 3 {
		if _, err := b.Load(ctx); err != nil {
			t.Fatalf("load: %v", err)
		}
	}

	t.Logf("one ferry.Bind and three Binding.Load calls        : Binds() = %d", bound.Binds())
	t.Logf("so a guard refusing a second Bind would refuse the second ferry.Load of one schema")
}

// TestQ1BitIdentical states the second result as a comparison rather than as two
// lines a reader has to compare by eye.
func TestQ1BitIdentical(t *testing.T) {
	h := ps.Header(gzip())

	for _, c := range []struct {
		label string
		run   func(ferry.Source) (string, string)
	}{
		{"Encodings []string", func(s ferry.Source) (string, string) { return loadBoth[Encodings](s, h) }},
		{"Encoding  string  ", func(s ferry.Source) (string, string) { return loadBoth[Encoding](s, h) }},
	} {
		src := ps.NewSource(ps.Repeatable("Accept-Encoding"))
		one, held := c.run(src)

		t.Logf("%s  ferry.Load -> %-24s  ferry.Bind -> %-24s  identical=%t",
			c.label, one, held, one == held)
	}
}

// TestQ1SharedContextIsNotTheCause rules out the obvious alternative reading:
// that the two loads interfere through the context rather than through the
// Source.
func TestQ1SharedContextIsNotTheCause(t *testing.T) {
	shared := ps.NewSource(ps.Repeatable("Accept-Encoding"))

	a := ps.WithHeaders(context.Background(), ps.Header(gzip()))
	b := ps.WithHeaders(context.Background(), ps.Header(gzip()))

	t.Logf("separate contexts, one Source:")
	t.Logf("  Encodings []string: %s", outcome(ferry.Load[Encodings](a, shared)))
	t.Logf("  Encoding  string  : %s", outcome(ferry.Load[Encoding](b, shared)))
	t.Logf("separate Sources, one context:")
	t.Logf("  Encodings []string: %s",
		outcome(ferry.Load[Encodings](a, ps.NewSource(ps.Repeatable("Accept-Encoding")))))
	t.Logf("  Encoding  string  : %s",
		outcome(ferry.Load[Encoding](a, ps.NewSource(ps.Repeatable("Accept-Encoding")))))
}
