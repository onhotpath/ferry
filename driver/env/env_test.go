package env

import (
	"errors"
	"maps"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestRequiredTellsAbsentFromEmpty is the distinction this plane exists to keep,
// end to end through a required field.
//
// FOO= is a zero-length string and not a null (ADR-0004), and required is a
// presence test and nothing else: it is satisfied by any observation other than
// Absent (ADR-0006). xload implements it as val == "" && required, so FOO=
// cannot satisfy it there, and this is that fixed.
func TestRequiredTellsAbsentFromEmpty(t *testing.T) {
	t.Parallel()

	t.Run("unset is a refusal naming the address", func(t *testing.T) {
		t.Parallel()
		checkUnsetIsRefused(t)
	})

	t.Run("set to empty satisfies it", func(t *testing.T) {
		t.Parallel()
		checkEmptySatisfies(t)
	})
}

// requiredToken is the schema both halves of that distinction are read through.
type requiredToken struct {
	Token string `ferry:"token,required"`
}

// checkUnsetIsRefused is the Absent half: the plane never spoke about the
// address, so required is not satisfied and the refusal names the address.
func checkUnsetIsRefused(t *testing.T) {
	t.Helper()

	_, err := ferry.Load[requiredToken](t.Context(), New(Environ(newEnviron().environ)))
	if !errors.Is(err, ferry.ErrMissing) {
		t.Fatalf("an unset required variable failed with %v, want a missing refusal", err)
	}

	var e *ferry.Error
	if errors.As(err, &e) && e.Address() != ferry.At("token") {
		t.Errorf("the refusal names %s, want /token", e.Address())
	}
}

// checkEmptySatisfies is the String("") half: TOKEN= is the plane speaking, so
// required is satisfied and the field holds what the plane said.
func checkEmptySatisfies(t *testing.T) {
	t.Helper()

	e := newEnviron()
	e.vars["TOKEN"] = ""

	got, err := ferry.Load[requiredToken](t.Context(), New(Environ(e.environ)))
	if err != nil {
		t.Fatalf("TOKEN= failed with %v, and a zero-length string is an observation", err)
	}

	if got.Token != "" {
		t.Errorf("Token = %q, want the empty string", got.Token)
	}
}

// TestGetAnswersStringOrAbsent is the same distinction at the driver's own
// boundary, where the kind is visible and a load only shows its consequence.
func TestGetAnswersStringOrAbsent(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["EMPTY"] = ""
	e.vars["SET"] = "v"

	addrs := []ferry.Path{ferry.At("empty"), ferry.At("set"), ferry.At("missing")}

	r, err := bound(e, addrs)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	want := map[ferry.Path]ferry.Value{
		ferry.At("empty"):   ferry.String(""),
		ferry.At("set"):     ferry.String("v"),
		ferry.At("missing"): {},
	}

	for addr, expected := range want {
		got, getErr := r.Get(t.Context(), addr)
		if getErr != nil {
			t.Fatalf("Get(%s): %v", addr, getErr)
		}

		if got != expected {
			t.Errorf("Get(%s) = %#v, want %#v", addr, got, expected)
		}
	}
}

// TestOneOpenSeesOneEnvironment pins the snapshot, which is a decision rather
// than an implementation detail: a variable that changed half way through a walk
// would land in some fields and not others, with nothing saying so.
func TestOneOpenSeesOneEnvironment(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["HOST"] = "first"

	addr := ferry.At("host")

	r, err := bound(e, []ferry.Path{addr})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	e.vars["HOST"] = "second"

	got, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != ferry.String("first") {
		t.Errorf("Get(%s) = %#v after the environment changed, want the value the open snapshotted", addr, got)
	}
}

// TestEnvironEntriesThatNameNothingAreSkipped covers the two shapes an environ
// slice holds that are not variables: an entry with no "=" at all, and Windows's
// own "=C:=C:\\" drive-current-directory entries, whose name is empty.
func TestEnvironEntriesThatNameNothingAreSkipped(t *testing.T) {
	t.Parallel()

	environ := func() []string { return []string{"NOTAVARIABLE", `=C:=C:\`, "HOST=h"} }

	got := environMap(environ())

	want := map[string]string{"HOST": "h"}
	if !maps.Equal(got, want) {
		t.Errorf("environMap = %v, want %v", got, want)
	}
}

// TestZeroSourceRefusesAtBind holds the zero value to refusing rather than
// guessing: it has no environment to read and no separator to join with, and a
// driver that filled either in silently would be choosing for the caller.
func TestZeroSourceRefusesAtBind(t *testing.T) {
	t.Parallel()

	var s Source

	_, err := s.Bind(ferry.NewAddressSet(ferry.At("leaf")))
	if !errors.Is(err, ErrOption) {
		t.Errorf("the zero Source bound with %v, want a refusal about its options", err)
	}
}

// TestGetRefusesAnAddressThePlaneCannotName holds a read to the same legality
// rule Bind applies to the static set.
//
// The address below is one only a value can mint - a map holding the empty key -
// so it is in no compiled set and reaches the driver for the first time here. A
// driver that answered Absent for it would report that the plane does not hold
// an address it cannot even name.
func TestGetRefusesAnAddressThePlaneCannotName(t *testing.T) {
	t.Parallel()

	r, err := bound(newEnviron(), []ferry.Path{ferry.At("labels")})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if _, err = r.Get(t.Context(), ferry.At("labels", "")); !errors.Is(err, ErrIllegalName) {
		t.Errorf("Get at an unnameable address failed with %v, want the legality refusal", err)
	}
}

// TestBindTakesNoAddressSetAtAll pins the degenerate call rather than leaving it
// to panic: a driver is handed whatever core hands it, and a schema that
// determined no address is a schema with nothing to check.
func TestBindTakesNoAddressSetAtAll(t *testing.T) {
	t.Parallel()

	open, err := New(Environ(newEnviron().environ)).Bind(nil)
	if err != nil {
		t.Fatalf("Bind(nil): %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	got, err := r.Get(t.Context(), ferry.At("anything"))
	if err != nil || got.Kind() != ferry.KindAbsent {
		t.Errorf("Get = %#v, %v, want an absence: nothing is bound and nothing is set", got, err)
	}
}

// TestLoadFillsAStruct is the ordinary thing a user does, over the shapes this
// plane can hold: a leaf, a nested struct, a sequence and a mapping.
//
// The sequence and the mapping are the tiers that only exist because the reader
// enumerates: their addresses come from the environment rather than from the
// type, so a source that could not list would reach neither.
func TestLoadFillsAStruct(t *testing.T) {
	t.Parallel()

	type db struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	type config struct {
		Name   string            `ferry:"name"`
		DB     db                `ferry:"db"`
		Tags   []string          `ferry:"tags"`
		Limits map[string]string `ferry:"limits"`
	}

	e := newEnviron()
	maps.Copy(e.vars, map[string]string{
		"NAME":       "acme",
		"DB_HOST":    "localhost",
		"DB_PORT":    "5432",
		"TAGS_0":     "a",
		"TAGS_1":     "b",
		"LIMITS_RPS": "10",
	})

	got, err := ferry.Load[config](t.Context(), New(Environ(e.environ)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := config{
		Name:   "acme",
		DB:     db{Host: "localhost", Port: 5432},
		Tags:   []string{"a", "b"},
		Limits: map[string]string{"rps": "10"},
	}

	if got.Name != want.Name || got.DB != want.DB {
		t.Errorf("Load = %+v, want %+v", got, want)
	}

	if len(got.Tags) != len(want.Tags) || got.Tags[0] != want.Tags[0] || got.Tags[1] != want.Tags[1] {
		t.Errorf("Tags = %v, want %v", got.Tags, want.Tags)
	}

	if !maps.Equal(got.Limits, want.Limits) {
		t.Errorf("Limits = %v, want %v", got.Limits, want.Limits)
	}
}
