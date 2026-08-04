package ferry_test

import (
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// This file is the outside of the seam. It is package ferry_test rather than
// ferry, so it reaches nothing but the four verbs and the plane ferrytest
// ships, and it is the only place core's own tests can import ferrytest at all
// without a cycle.
//
// What it adds over the internal tests is the plane: those run against a
// test-local driver, and these run against the memory plane a user actually
// gets, whose Source and Sink stay unexported inside an Instance and which
// implements neither Committer nor Releaser.

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
