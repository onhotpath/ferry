package perschema_test

import (
	"context"
	"testing"

	"github.com/onhotpath/ferry"
	ps "github.com/onhotpath/ferry/proto/perschema"
)

// TestQ2NotCheckable is the Source carrying per-schema configuration that is
// NOT checkable against the AddressSet: Repeatable asserts that the Go type
// behind a name is a sequence, and an AddressSet has no Go type in it.
func TestQ2NotCheckable(t *testing.T) {
	decl := func() *ps.Source { return ps.NewSource(ps.Repeatable("Accept-Encoding"), ps.CheckNames()) }

	t.Logf("=== single-schema use, the schema the declaration is correct for ===")
	t.Logf("  Encodings []string <- gzip, br : %s",
		outcome(ferry.Load[Encodings](ctxWith(gzip(), br()), decl())))
	t.Logf("  Encodings []string <- gzip     : %s",
		outcome(ferry.Load[Encodings](ctxWith(gzip()), decl())))

	t.Logf("")
	t.Logf("=== single-schema use, the schema the declaration is wrong for ===")
	t.Logf("  Encoding string <- gzip        : %s",
		outcome(ferry.Load[Encoding](ctxWith(gzip()), decl())))
	t.Logf("  (a Source with no declaration  : %s)",
		outcome(ferry.Load[Encoding](ctxWith(gzip()), ps.NewSource())))

	t.Logf("")
	t.Logf("=== shared source, two schemas, through ferry.Load ===")

	shared := decl()
	ctx := ctxWith(gzip())

	t.Logf("  A, Encodings []string : %s", outcome(ferry.Load[Encodings](ctx, shared)))
	t.Logf("  B, Encoding  string   : %s", outcome(ferry.Load[Encoding](ctx, shared)))

	t.Logf("")
	t.Logf("=== shared source, two schemas, through ferry.Bind ===")

	held := decl()

	bA, errA := ferry.Bind[Encodings](held)
	bB, errB := ferry.Bind[Encoding](held)

	if errA != nil || errB != nil {
		t.Logf("  bind A err = %v", errA)
		t.Logf("  bind B err = %v", errB)
		t.Fatalf("neither Bind was expected to refuse")
	}

	t.Logf("  A, Encodings []string : %s", outcome(bA.Load(ctx)))
	t.Logf("  B, Encoding  string   : %s", outcome(bB.Load(ctx)))

	t.Logf("")
	t.Logf("=== what the driver detects at Bind ===")

	_, typo := ferry.Bind[Encodings](ps.NewSource(ps.Repeatable("Accept-Encodings"), ps.CheckNames()))
	t.Logf("  a declared name this schema does not have (a typo): REFUSED")
	t.Logf("%s", full(typo))

	_, ok := ferry.Bind[Encoding](decl())
	t.Logf("  a declared name this schema has, whose Go type is not a sequence:")
	t.Logf("%s", full(ok))

	t.Logf("")
	t.Logf("=== what a caller sees when it goes wrong ===")

	_, loadErr := ferry.Load[Encoding](ctx, decl())
	t.Logf("  the load that silently zeroed Encoding:")
	t.Logf("%s", full(loadErr))
}

// TestQ2Checkable is the Source carrying per-schema configuration that IS
// checkable against the AddressSet: Alias asserts only that a name exists, and
// it renames a plane key, which core's own NewKeys then checks for injectivity.
func TestQ2Checkable(t *testing.T) {
	decl := func() *ps.Source { return ps.NewSource(ps.Alias("trace-id", "x-trace-id"), ps.CheckNames()) }

	real := [2]string{"Trace-Id", "from-the-real-header"}
	aliased := [2]string{"X-Trace-Id", "from-the-aliased-header"}

	t.Logf("=== single-schema use, the schema the declaration is correct for ===")
	t.Logf("  Trace{ID trace-id} <- both headers : %s",
		outcome(ferry.Load[Trace](ctxWith(real, aliased), decl())))
	t.Logf("  (a Source with no declaration      : %s)",
		outcome(ferry.Load[Trace](ctxWith(real, aliased), ps.NewSource())))

	t.Logf("")
	t.Logf("=== shared source, a second schema that does not have the name ===")

	shared := decl()
	ctx := ctxWith(real, aliased)

	t.Logf("  A, Trace{ID trace-id}   : %s", outcome(ferry.Load[Trace](ctx, shared)))

	_, err := ferry.Load[Other](ctx, shared)
	t.Logf("  B, Other{Agent user-agent}: REFUSED at Bind")
	t.Logf("%s", full(err))

	t.Logf("")
	t.Logf("=== shared source, a second schema that has the name AND the alias target ===")

	_, err = ferry.Load[TraceAndLegacy](ctx, shared)
	t.Logf("  C, TraceAndLegacy: REFUSED")
	t.Logf("%s", full(err))

	t.Logf("")
	t.Logf("=== shared source, a second schema that has the name and wants the real header ===")

	t.Logf("  D, Trace{ID trace-id}, wanting Trace-Id and not X-Trace-Id:")
	t.Logf("    with the shared source : %s", outcome(ferry.Load[Trace](ctx, shared)))
	t.Logf("    with its own source    : %s", outcome(ferry.Load[Trace](ctx, ps.NewSource())))
	t.Logf("    -> the check passed, the value is the other header's, and nothing said so")

	t.Logf("")
	t.Logf("=== through ferry.Bind rather than ferry.Load, same four ===")

	held := decl()

	for _, c := range []struct {
		label string
		run   func(ferry.Source) string
	}{
		{"A, Trace           ", func(s ferry.Source) string { return bindLoad[Trace](s, ctx) }},
		{"B, Other           ", func(s ferry.Source) string { return bindLoad[Other](s, ctx) }},
		{"C, TraceAndLegacy  ", func(s ferry.Source) string { return bindLoad[TraceAndLegacy](s, ctx) }},
		{"D, Trace again     ", func(s ferry.Source) string { return bindLoad[Trace](s, ctx) }},
	} {
		t.Logf("  %s: %s", c.label, c.run(held))
	}

	t.Logf("  Binds() = %d", held.Binds())
}

// bindLoad is ferry.Bind followed by one Load, so a refusal at either moment
// prints as one line.
func bindLoad[T any](s ferry.Source, ctx context.Context) string {
	b, err := ferry.Bind[T](s)
	if err != nil {
		return "REFUSED at Bind: " + class(err) + ": " + err.Error()
	}

	return outcome(b.Load(ctx))
}
