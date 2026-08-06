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

	g, err := boundTo[threeLeaves](e)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	want := map[ferry.Path]ferry.Value{
		ferry.At("empty"):   ferry.String(""),
		ferry.At("set"):     ferry.String("v"),
		ferry.At("missing"): {},
	}

	for at, expected := range want {
		got, getErr := g.r.Get(t.Context(), g.leaf(t, at))
		if getErr != nil {
			t.Fatalf("Get(%s): %v", at, getErr)
		}

		if got != expected {
			t.Errorf("Get(%s) = %#v, want %#v", at, got, expected)
		}
	}
}

// threeLeaves is one variable set to empty, one set to a value and one never
// set, which is the whole of what this plane's set-against-unset distinction has
// to say at three addresses.
type threeLeaves struct {
	Empty   string `ferry:"empty"`
	Set     string `ferry:"set"`
	Missing string `ferry:"missing"`
}

// TestOneOpenSeesOneEnvironment pins the snapshot, which is a decision rather
// than an implementation detail: a variable that changed half way through a walk
// would land in some fields and not others, with nothing saying so.
func TestOneOpenSeesOneEnvironment(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["HOST"] = "first"

	g, err := boundTo[oneHost](e)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	e.vars["HOST"] = "second"

	at := ferry.At("host")

	got, err := g.r.Get(t.Context(), g.leaf(t, at))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != ferry.String("first") {
		t.Errorf("Get(%s) = %#v after the environment changed, want the value the open snapshotted", at, got)
	}
}

// oneHost is the smallest schema with a leaf to read.
type oneHost struct {
	Host string `ferry:"host"`
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

	set, err := addrsOf[oneHost]()
	if err != nil {
		t.Fatalf("compiling the fixture: %v", err)
	}

	if _, err = s.Bind(set); !errors.Is(err, ErrOption) {
		t.Errorf("the zero Source bound with %v, want a refusal about its options", err)
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

	// The address comes from a compiled schema this binding was never handed,
	// which is exactly the case: a driver bound to nothing is still asked, and
	// what it answers is an absence rather than a panic.
	set, err := addrsOf[oneHost]()
	if err != nil {
		t.Fatalf("compiling the fixture: %v", err)
	}

	addr, ok := leafIn(set, ferry.At("host"))
	if !ok {
		t.Fatal("the fixture names no leaf at /host")
	}

	got, err := r.Get(t.Context(), addr)
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

// The schemas the presence rule is read through.
//
// A section's address is a place a plane is asked whether the section is there,
// and never a place a value can be, which is the whole of #219: a struct field
// named home makes /home a section, and a process environment holding HOME has
// nothing to say about it.
type (
	homePaths struct {
		Root string `ferry:"root"`
	}

	homeConfig struct {
		Home *homePaths `ferry:"home"`
	}
)

// TestAnAmbientVariableAtASectionIsNotAValue is #219, retired by construction.
//
// HOME is set to something this schema never asked for, and HOME_ROOT holds the
// value it did. Before the address kinds, the driver was asked for a value at
// /home, answered with the ambient HOME, and core refused the whole load; now
// the question is unaskable and the load reads what is actually there.
func TestAnAmbientVariableAtASectionIsNotAValue(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["HOME"] = "/Users/me"
	e.vars["HOME_ROOT"] = "/srv"

	got, err := ferry.Load[homeConfig](t.Context(), New(Environ(e.environ)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Home == nil || got.Home.Root != "/srv" {
		t.Errorf("Home = %+v, want the section HOME_ROOT filled", got.Home)
	}
}

// TestProbeAnswersFromWhatIsUnderTheName is the same rule at the driver's own
// boundary, where a flat plane's one distinction is visible.
//
// A container's own name holds nothing on this plane ever, so its presence is
// the presence of its members: HOME_ROOT makes /home present, and unsetting it
// makes /home absent however much the ambient HOME is set.
func TestProbeAnswersFromWhatIsUnderTheName(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		vars map[string]string
		want ferry.Presence
	}{
		"a member below it is present": {
			vars: map[string]string{"HOME": "/Users/me", "HOME_ROOT": "/srv"},
			want: ferry.PresencePresent,
		},
		"the ambient name alone is absent": {
			vars: map[string]string{"HOME": "/Users/me"},
			want: ferry.PresenceAbsent,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkProbe(t, tc.vars, tc.want)
		})
	}
}

// checkProbe binds one environment and asks about the one section in it.
func checkProbe(t *testing.T, vars map[string]string, want ferry.Presence) {
	t.Helper()

	e := newEnviron()
	maps.Copy(e.vars, vars)

	g, err := boundTo[homeConfig](e)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	prober, ok := g.r.(ferry.Prober)
	if !ok {
		t.Fatal("the reader does not probe, and a section's presence would have nowhere to come from")
	}

	info, err := prober.Probe(t.Context(), g.section(t, ferry.At("home")))
	if err != nil {
		t.Fatalf("Probe(/home): %v", err)
	}

	if info.Presence() != want {
		t.Errorf("Probe(/home) = %v, want %v", info.Presence(), want)
	}
}
