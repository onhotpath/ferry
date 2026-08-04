package ferry_test

import (
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// This file is the outside of the seam. It is package ferry_test rather than
// ferry, so it reaches nothing but the published verbs and the plane ferrytest
// ships, and it is the only place core's own tests can import ferrytest at all
// without a cycle.
//
// What it adds over the internal tests is the plane: those run against a
// test-local driver, and these run against the memory plane a user actually
// gets, whose Source and Sink stay unexported inside an Instance and which
// implements neither Committer nor Releaser.

// TestCoreTypesOverTheMemoryPlane runs every row of the published type table
// against the engine.
//
// The memory plane is where core's value fidelity is stated, because it is the
// only plane that adds nothing of its own: it stores the boundary Value itself,
// so a failure here is core's and never a driver's.
//
// This test used to run behind a skip list. #72 wrote ferrytest.CoreTypes
// complete - nineteen rows and 57 cases - ahead of the compiler that admits
// their types, and named every row the engine could not yet carry against the
// ticket that would land it. The list was a ratchet rather than a to-do: a row
// that started round-tripping failed the suite as stale, so a widening ticket
// could not land green without deleting its entry, and the ticket that emptied
// the list deleted the list. This one did, and the whole table runs.
func TestCoreTypesOverTheMemoryPlane(t *testing.T) {
	t.Parallel()

	rows := ferrytest.CoreTypes()
	if len(rows) == 0 {
		t.Fatal("the type table is empty, so this test asserts nothing")
	}

	ferrytest.RoundTrip(t, ferrytest.MemPlane(), rows)
}

type memCommon struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

type memDB struct {
	Host string `ferry:"host"`
	Port string `ferry:"port"`
}

type memConf struct {
	memCommon
	Region string `ferry:"region"`
	DB     memDB  `ferry:"db"`
}

func memFilled() memConf {
	return memConf{
		memCommon: memCommon{Name: "svc", Env: "prod"},
		Region:    "eu-west-1",
		DB:        memDB{Host: "db1", Port: "5432"},
	}
}

// TestRoundTripOverTheMemoryPlane is the ticket in one function: a struct of
// string leaves, with a nested struct and a promoted embedded one, dumps to the
// memory plane and loads back equal.
func TestRoundTripOverTheMemoryPlane(t *testing.T) {
	t.Parallel()

	want := memFilled()
	// Fresh per case, because Open mints an empty plane and a shared one is the
	// defect that hides a broken second walk.
	inst := ferrytest.MemPlane().Open()

	if err := ferry.Dump(t.Context(), want, inst.Sink); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got, err := ferry.Load[memConf](t.Context(), inst.Source)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != want {
		t.Errorf("round tripped to %+v, want %+v", got, want)
	}
}

// TestSeededLoadOverAnEmptyMemoryPlane is the same rule through a plane that
// really does report Absent rather than one written to say so.
func TestSeededLoadOverAnEmptyMemoryPlane(t *testing.T) {
	t.Parallel()

	seed := memFilled()

	got, err := ferry.LoadOver(t.Context(), seed, ferrytest.MemPlane().Open().Source)
	if err != nil {
		t.Fatalf("load over: %+v", err)
	}

	if got != seed {
		t.Errorf("an empty plane changed the seed to %+v, want %+v", got, seed)
	}
}

// TestLoadFromStatic is the audience ADR-0014 says is the largest one that
// package has: somebody who is not testing ferry and wants a config struct
// filled from a literal.
func TestLoadFromStatic(t *testing.T) {
	t.Parallel()

	src := ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("name"):       ferry.String("svc"),
		ferry.At("env"):        ferry.String("prod"),
		ferry.At("region"):     ferry.String("eu-west-1"),
		ferry.At("db", "host"): ferry.String("db1"),
		ferry.At("db", "port"): ferry.String("5432"),
	})

	got, err := ferry.Load[memConf](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if want := memFilled(); got != want {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestTheMemoryPlaneRefusesASecondWrite is a driver's own refusal travelling
// the whole way out: it names the address with ErrorAt, core supplies the
// moment and the provenance, and the caller matches with errors.Is.
func TestTheMemoryPlaneRefusesASecondWrite(t *testing.T) {
	t.Parallel()

	inst := ferrytest.MemPlane().Open()

	if err := ferry.Dump(t.Context(), memFilled(), inst.Sink); err != nil {
		t.Fatalf("the first dump: %+v", err)
	}

	err := ferry.Dump(t.Context(), memFilled(), inst.Sink)
	if n := len(ferry.Elements(err)); n != 5 {
		t.Fatalf("the second dump reported %d elements, want one per address:\n%+v", n, err)
	}

	for _, e := range ferry.Elements(err) {
		mustBeDriverRefusal(t, e)
	}
}

func mustBeDriverRefusal(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ferry.ErrPlane) || !errors.Is(err, ferry.ErrDriver) {
		t.Errorf("%v is not a driver's plane refusal", err)
	}

	e, ok := errors.AsType[*ferry.Error](err)
	if !ok || e.Address() == (ferry.Path{}) {
		t.Errorf("%v names no address", err)
	}
}
