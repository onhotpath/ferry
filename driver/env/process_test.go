package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onhotpath/ferry"
)

// The process half of a dump exists so that the two halves of the composite stay
// in agreement, and these are the two things that makes true: a variable this
// save wrote is exported, and a variable this save swept is unset.

// TestASaveWithoutSetenvLeavesTheProcessSayingTheOldThing is the sharp edge the
// option answers, asserted rather than described: the file is right, the load is
// wrong, and nothing in between says so.
func TestASaveWithoutSetenvLeavesTheProcessSayingTheOldThing(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["HOST"] = "ambient"

	path := filepath.Join(t.TempDir(), ".env")

	if err := ferry.Dump(t.Context(), host{Host: "written"}, NewDotEnvSink(path)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := read(t, path); got != "HOST=written\n" {
		t.Fatalf("the file holds %q, want the save to have written it", got)
	}

	got, err := ferry.Load[host](t.Context(), New(Environ(e.environ), DotEnv(path)))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Host != "ambient" {
		t.Errorf("loaded %q, want the process's own value: without env.Setenv the save looks as though it did "+
			"nothing", got.Host)
	}
}

// TestASaveWithSetenvBringsTheProcessIntoAgreement is the same save with the
// option, and it is what makes the composite one plane.
func TestASaveWithSetenvBringsTheProcessIntoAgreement(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["HOST"] = "ambient"

	path := filepath.Join(t.TempDir(), ".env")

	if err := ferry.Dump(t.Context(), host{Host: "written"}, NewDotEnvSink(path, Setenv(e))); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	got, err := ferry.Load[host](t.Context(), New(Environ(e.environ), DotEnv(path)))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got.Host != "written" {
		t.Errorf("loaded %q, want what the save wrote", got.Host)
	}
}

// TestAShrinkingSliceUnsetsWhatItLeftBehind is why [Process] has two methods
// rather than one.
//
// Sweeping the file is not enough: the process is the layer above it, so the
// positions this save no longer writes go on being served from there and the next
// load reads a slice that never shrank.
func TestAShrinkingSliceUnsetsWhatItLeftBehind(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	path := filepath.Join(t.TempDir(), ".env")
	sink := NewDotEnvSink(path, Setenv(e))
	src := New(Environ(e.environ), DotEnv(path))

	if err := ferry.Dump(t.Context(), tags{Tags: []string{"a", "b", "c"}}, sink); err != nil {
		t.Fatalf("the first dump: %+v", err)
	}

	if err := ferry.Dump(t.Context(), tags{Tags: []string{"x"}}, sink); err != nil {
		t.Fatalf("the second dump: %+v", err)
	}

	if got := read(t, path); got != "TAGS_0=x\n" {
		t.Errorf("the file holds %q, want one variable", got)
	}

	unset(t, e, "TAGS_1", "TAGS_2")

	got, err := ferry.Load[tags](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if len(got.Tags) != 1 || got.Tags[0] != "x" {
		t.Errorf("loaded %v, want the one element the second save wrote", got.Tags)
	}
}

// unset asserts that the process holds none of these names, which is the half of
// a sweep the file cannot do.
func unset(t *testing.T, e *fakeEnviron, names ...string) {
	t.Helper()

	for _, name := range names {
		if _, held := e.vars[name]; held {
			t.Errorf("the process still exports %s, so the next load reads a slice that never shrank", name)
		}
	}
}

// TestTheProcessHalfRunsOnlyAfterTheFileWasReplaced is what stops a save that
// could not write the file from having already changed the environment.
func TestTheProcessHalfRunsOnlyAfterTheFileWasReplaced(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	path := staged(t, "HOST=old\n")

	set, w := writerOver[host](t, NewDotEnvSink(path, Setenv(e)))
	defer closeWriter(t, w)

	if err := w.Set(t.Context(), leafOf(t, set, ferry.At("host")), ferry.String("mine")); err != nil {
		t.Fatalf("set: %+v", err)
	}

	// Somebody else's edit, which is what the commit refuses over.
	write(t, path, "HOST=theirs\nTHEIRS=1\n")

	if err := committerOf(t, w).Commit(t.Context()); err == nil {
		t.Fatal("the commit succeeded, want the refusal this case is built on")
	}

	if len(e.vars) != 0 {
		t.Errorf("the process holds %v, want nothing: a save that could not write the file must not have "+
			"already changed the environment", e.vars)
	}
}

// TestSetenvWithNoProcessNamesTheRunningOne is the shape of the default, checked
// without touching the environment of the test binary: what the option builds is
// the os-backed implementation.
func TestSetenvWithNoProcessNamesTheRunningOne(t *testing.T) {
	t.Parallel()

	c := sinkDefaults()
	Setenv(nil).applySink(&c)

	if _, ok := c.proc.(osProcess); !ok {
		t.Errorf("env.Setenv(nil) writes through %T, want the running process", c.proc)
	}
}

// TestTheDefaultProcessIsTheRunningOne is the one case in this package that
// touches the environment of the test binary, which is why it does not call
// t.Parallel: testing.T.Setenv forbids it, and it is what puts the value back.
//
// The two methods are one line each and they are the whole of what [Setenv] does
// by default, so what this asserts is that they reach the process rather than
// something else.
func TestTheDefaultProcessIsTheRunningOne(t *testing.T) {
	const name = "FERRY_ENV_DEFAULT_PROCESS_PROBE"

	// Registering the name here is what restores it when the test ends, whatever
	// the calls below leave it as.
	t.Setenv(name, "before")

	p := osProcess{}

	if err := p.Setenv(name, "after"); err != nil {
		t.Fatalf("setenv: %v", err)
	}

	if got, held := os.LookupEnv(name); !held || got != "after" {
		t.Errorf("the process holds %q (set: %t), want the value just written", got, held)
	}

	if err := p.Unsetenv(name); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	if _, held := os.LookupEnv(name); held {
		t.Error("the variable is still set, and unsetting really has to unset: a sweep that left it would " +
			"serve a removed slice element back on the next load")
	}
}

// TestAProcessThatRefusesIsReported is the failure a caller has to hear about:
// the file was replaced and the two halves are not in agreement, which is not a
// save that quietly succeeded.
func TestAProcessThatRefusesIsReported(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".env")

	err := ferry.Dump(t.Context(), host{Host: "new"}, NewDotEnvSink(path, Setenv(refusingProcess{})))
	if err == nil {
		t.Fatal("the dump reported success, want the process half's failure")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal is %+v, want it to answer errors.Is against ferry.ErrPlane", err)
	}

	if got := read(t, path); got != "HOST=new\n" {
		t.Errorf("the file holds %q, want the replacement that did happen: what failed is the second half", got)
	}
}

// refusingProcess is a process environment that takes nothing.
type refusingProcess struct{}

func (refusingProcess) Setenv(string, string) error { return errors.New("no") }
func (refusingProcess) Unsetenv(string) error       { return errors.New("no") }
