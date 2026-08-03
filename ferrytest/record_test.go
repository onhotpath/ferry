package ferrytest_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The struct the extraction is asked about. It has a nested section, because a
// prefix is the thing a caller most wants confirmed, and a skipped field,
// because a mapped-address report that included one would be wrong in the
// direction nobody checks.
type (
	dbSettings struct {
		Port string `ferry:"port"`
	}

	appSettings struct {
		Host   string     `ferry:"host"`
		DB     dbSettings `ferry:"db"`
		Hidden string     `ferry:"-"`
	}
)

// TestRecordMapsAZeroStruct is ADR-0001's schema-extraction pattern, and the
// "with no plane reachable" half is by construction: Record takes no Source and
// no Sink, so there is no plane for a caller to supply or for ferry to open.
func TestRecordMapsAZeroStruct(t *testing.T) {
	got, err := ferrytest.Record(context.Background(), appSettings{})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	want := map[ferry.Path]ferry.Value{
		ferry.At("host"):       ferry.String(""),
		ferry.At("db", "port"): ferry.String(""),
	}

	if !maps.Equal(got, want) {
		t.Errorf("Record = %v, want %v", got, want)
	}
}

// TestRecordReportsWhatThisValueWrites is why the value matters as well as its
// type: the boundary Value is what a dump of this value would hand a plane, so
// the zero value is how a caller asks about the type and any other value
// answers about itself.
func TestRecordReportsWhatThisValueWrites(t *testing.T) {
	got, err := ferrytest.Record(context.Background(), appSettings{
		Host: "localhost",
		DB:   dbSettings{Port: "5432"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	want := map[ferry.Path]ferry.Value{
		ferry.At("host"):       ferry.String("localhost"),
		ferry.At("db", "port"): ferry.String("5432"),
	}

	if !maps.Equal(got, want) {
		t.Errorf("Record = %v, want %v", got, want)
	}
}

// TestRecordHonoursOptions asserts the Option list reaches the compiler. A
// TagKey the extraction could not see would answer about a schema no load will
// ever build.
func TestRecordHonoursOptions(t *testing.T) {
	type keyed struct {
		Host string `cfg:"host"`
	}

	got, err := ferrytest.Record(context.Background(), keyed{Host: "h"}, ferry.TagKey("cfg"))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	want := map[ferry.Path]ferry.Value{ferry.At("host"): ferry.String("h")}
	if !maps.Equal(got, want) {
		t.Errorf("Record = %v, want %v", got, want)
	}
}

// TestRecordRefusesATypeThatDoesNotCompile asserts the refusal is the
// compiler's own, reached through the entry point rather than restated here.
func TestRecordRefusesATypeThatDoesNotCompile(t *testing.T) {
	type untagged struct {
		Host string
	}

	got, err := ferrytest.Record(context.Background(), untagged{})
	if !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("Record error = %v, want a schema refusal", err)
	}

	if got != nil {
		t.Errorf("Record = %v, want nothing alongside a refusal", got)
	}
}

// TestRecordIsNotSharedBetweenCalls asserts the map is the call's own, because
// a caller comparing two extractions would otherwise be comparing one.
func TestRecordIsNotSharedBetweenCalls(t *testing.T) {
	first, err := ferrytest.Record(context.Background(), appSettings{Host: "one"})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if _, err = ferrytest.Record(context.Background(), appSettings{Host: "two"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if got := first[ferry.At("host")]; got != ferry.String("one") {
		t.Errorf("the first extraction now reads %#v, so the two calls share a map", got)
	}
}

// TestProofNameAndType asserts the two columns a suite reads without a type
// parameter in hand. The name is a label for a report and the type is what
// ADR-0014's completeness check joins on, which is why the join is not by name.
func TestProofNameAndType(t *testing.T) {
	p := ferrytest.Type("a label", ferrytest.Eq[nanoseconds])

	if p.Name() != "a label" {
		t.Errorf("Name = %q, want the label it was built with", p.Name())
	}

	if got, want := p.Type().String(), "ferrytest_test.nanoseconds"; got != want {
		t.Errorf("Type = %s, want %s", got, want)
	}
}
