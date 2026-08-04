package perschema_test

import (
	"context"
	"testing"

	"github.com/onhotpath/ferry"
	ps "github.com/onhotpath/ferry/proto/perschema"
)

// The rule under test:
//
//	A Source may carry per-schema configuration only where it is checkable
//	against the AddressSet at Bind. The name-exists half is checkable; the
//	is-it-a-container half is not.
//
// The experiment is the same for every configuration, so the rows compare.
//
// Two handlers, each of which would be correct with a Source configured for it.
// They share one Source carrying handler A's configuration. Handler B's load is
// run twice: once through the shared Source and once through the Source B would
// have built for itself. If the two differ and nothing errored, the shared
// configuration silently changed B's answer.
type experiment struct {
	label string

	// opts is handler A's configuration, which is the one the shared Source
	// carries.
	opts []ps.Option

	// checkable is whether the configuration's whole meaning is a claim an
	// AddressSet can answer, which is what the rule asks.
	checkable bool

	// a and b run the two handlers against a Source.
	a func(ferry.Source, context.Context) string
	b func(ferry.Source, context.Context) string

	// bErr is handler B's error through the shared Source, so loud and silent
	// separate.
	bErr func(ferry.Source, context.Context) error

	ctx context.Context
}

func experiments() []experiment {
	present := ctxWith(gzip(), [2]string{"Trace-Id", "real"}, [2]string{"X-Trace-Id", "aliased"})
	absent := ctxWith(gzip())

	loadA := func(s ferry.Source, ctx context.Context) string {
		return outcome(ferry.Load[Encodings](ctx, s))
	}
	trace := func(s ferry.Source, ctx context.Context) string { return outcome(ferry.Load[Trace](ctx, s)) }
	traceErr := func(s ferry.Source, ctx context.Context) error {
		_, err := ferry.Load[Trace](ctx, s)

		return err
	}

	return []experiment{
		{
			label:     "Repeatable          ",
			opts:      []ps.Option{ps.Repeatable("Accept-Encoding"), ps.CheckNames()},
			checkable: false,
			ctx:       present,
			a:         loadA,
			b:         func(s ferry.Source, ctx context.Context) string { return outcome(ferry.Load[Encoding](ctx, s)) },
			bErr: func(s ferry.Source, ctx context.Context) error {
				_, err := ferry.Load[Encoding](ctx, s)

				return err
			},
		},
		{
			label:     "Repeatable+Audited  ",
			opts:      []ps.Option{ps.Repeatable("Accept-Encoding"), ps.Audited(), ps.CheckNames()},
			checkable: false,
			ctx:       present,
			a:         loadA,
			b:         func(s ferry.Source, ctx context.Context) string { return outcome(ferry.Load[Encoding](ctx, s)) },
			bErr: func(s ferry.Source, ctx context.Context) error {
				_, err := ferry.Load[Encoding](ctx, s)

				return err
			},
		},
		{
			label:     "Alias               ",
			opts:      []ps.Option{ps.Alias("trace-id", "x-trace-id"), ps.CheckNames()},
			checkable: true,
			ctx:       present,
			a:         trace,
			b:         trace,
			bErr:      traceErr,
		},
		{
			label:     "Required            ",
			opts:      []ps.Option{ps.Required("trace-id"), ps.CheckNames()},
			checkable: true,
			ctx:       absent,
			a:         trace,
			b:         trace,
			bErr:      traceErr,
		},
		{
			label:     "Fallback            ",
			opts:      []ps.Option{ps.Fallback("trace-id", "from-the-declaration"), ps.CheckNames()},
			checkable: true,
			ctx:       absent,
			a:         trace,
			b:         trace,
			bErr:      traceErr,
		},
	}
}

// TestQ3TheRule runs every configuration through the same experiment and tables
// the two axes the rule conflates.
func TestQ3TheRule(t *testing.T) {
	t.Logf("%-20s  %-9s  %-9s  %s", "configuration", "checkable", "B differs", "B's error through the shared Source")
	t.Logf("%s", "-------------------------------------------------------------------------------------")

	for _, e := range experiments() {
		shared := ps.NewSource(e.opts...)
		own := ps.NewSource()

		_ = e.a(shared, e.ctx)

		viaShared := e.b(shared, e.ctx)
		viaOwn := e.b(own, e.ctx)
		err := e.bErr(shared, e.ctx)

		loud := "none - SILENT"
		if err != nil {
			loud = class(err) + " - LOUD"
		}

		t.Logf("%-20s  %-9t  %-9t  %s", e.label, e.checkable, viaShared != viaOwn, loud)
	}

	t.Logf("")
	t.Logf("the same rows in full:")

	for _, e := range experiments() {
		shared := ps.NewSource(e.opts...)
		own := ps.NewSource()

		t.Logf("")
		t.Logf("  %s", e.label)
		t.Logf("    A through the shared Source : %s", e.a(shared, e.ctx))
		t.Logf("    B through the shared Source : %s", e.b(shared, e.ctx))
		t.Logf("    B through its own Source    : %s", e.b(own, e.ctx))
	}
}

// TestQ3CheckableAndUnsafe is the first half of the rule's test: a
// configuration that satisfies the rule and is still unsafe.
//
// Alias's whole meaning is a claim about names. The name-exists half is checked
// at Bind and core's own NewKeys checks the renamed key space for injectivity,
// so there is no unchecked half at all - and a second handler that has the name
// and wanted the plane's own key gets the other header's value with no error.
func TestQ3CheckableAndUnsafe(t *testing.T) {
	shared := ps.NewSource(ps.Alias("trace-id", "x-trace-id"), ps.CheckNames())
	ctx := ctxWith([2]string{"Trace-Id", "real"}, [2]string{"X-Trace-Id", "aliased"})

	t.Logf("every check the rule asks for passes:")

	b, err := ferry.Bind[Trace](shared)
	t.Logf("  Bind[Trace]        err = %v", err)

	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	t.Logf("  the name exists in the AddressSet, and NewKeys accepted the renamed key space")
	t.Logf("")
	t.Logf("and handler B, which has the name and wanted the plane's own key:")
	t.Logf("  through the shared Source : %s", outcome(b.Load(ctx)))
	t.Logf("  through its own Source    : %s", outcome(ferry.Load[Trace](ctx, ps.NewSource())))
	t.Logf("  error                     : %v", errOf(ferry.Load[Trace](ctx, shared)))
	t.Logf("")
	t.Logf("so a configuration with no uncheckable half is still silently wrong for a second schema")
}

// TestQ3NotCheckableAndSafe is the second half: a configuration that violates
// the rule and is nonetheless safe.
//
// Repeatable+Audited asserts exactly what Repeatable does - that the Go type
// behind a name is a sequence - and no more of it is checkable at Bind. What
// changes is only that a declaration nothing read as a sequence is reported at
// Close.
func TestQ3NotCheckableAndSafe(t *testing.T) {
	shared := ps.NewSource(ps.Repeatable("Accept-Encoding"), ps.Audited(), ps.CheckNames())
	ctx := ctxWith(gzip(), br())

	t.Logf("nothing more is checkable at Bind than for plain Repeatable:")

	_, err := ferry.Bind[Encoding](shared)
	t.Logf("  Bind[Encoding]  err = %v", err)

	t.Logf("")
	t.Logf("A, Encodings []string, the schema the declaration is correct for:")
	t.Logf("  %s", outcome(ferry.Load[Encodings](ctx, shared)))
	t.Logf("  -> correct, and quiet: something read the name as the sequence it was declared to be")

	t.Logf("")
	t.Logf("B, Encoding string, the schema the declaration is wrong for:")

	got, berr := ferry.Load[Encoding](ctx, shared)
	t.Logf("  value = %+v", got)
	t.Logf("%s", full(berr))

	t.Logf("")
	t.Logf("and the same with one value at the name, which is where plain Repeatable was silent:")

	one := ctxWith(gzip())

	t.Logf("  A, Encodings []string : %s", outcome(ferry.Load[Encodings](one, shared)))

	got2, berr2 := ferry.Load[Encoding](one, shared)
	t.Logf("  B, Encoding  string   : %+v", got2)
	t.Logf("%s", full(berr2))

	t.Logf("")
	t.Logf("and through ferry.Bind, to check the binding changes nothing:")

	bA, _ := ferry.Bind[Encodings](shared)
	bB, _ := ferry.Bind[Encoding](shared)

	t.Logf("  A : %s", outcome(bA.Load(one)))
	t.Logf("  B : %s", outcome(bB.Load(one)))
}

func errOf[T any](_ T, err error) error { return err }
