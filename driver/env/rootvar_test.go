package env

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// A schema whose root resolves to a leaf names one address, the root, and this
// driver builds every other name by folding segments together. The root has
// none, so [RootVar] is the whole of what makes it readable and these are the
// three answers a caller gets: the variable's text, absence where it is unset,
// and a refusal at Bind where nothing named it (#337).

// rootPort is the variable the cases below read the root from, spelled once
// because every one of them names it.
const rootPort = "APP_PORT"

// TestARootLeafReadsTheVariableRootVarNames is the option doing its one job,
// seen from where a caller stands: a bare int loads from the variable named for
// it, with no struct and no tag anywhere in the call.
func TestARootLeafReadsTheVariableRootVarNames(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars[rootPort] = "8080"

	got, err := ferry.Load[int](t.Context(), New(Environ(e.environ), RootVar(rootPort)))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 8080 {
		t.Errorf("loaded %d, want the number the root variable holds", got)
	}
}

// TestARootLeafIsAbsentWhereTheVariableIsUnset is the plane reporting absence as
// absence. The root carries no tag, so there is nothing on it to say that a
// value is required or what it defaults to, and an unset variable is a load that
// leaves the destination as it found it.
func TestARootLeafIsAbsentWhereTheVariableIsUnset(t *testing.T) {
	t.Parallel()

	got, err := ferry.Load[int](t.Context(), New(Environ(newEnviron().environ), RootVar(rootPort)))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 0 {
		t.Errorf("loaded %d, want the zero value of a root nothing was set at", got)
	}
}

// TestARootLeafKeepsTheSeedWhereTheVariableIsUnset is the same absence over a
// destination that already holds something, which is where "left as it found
// it" is visible at all: a root with a default is a seed, since there is no tag
// to declare one on.
func TestARootLeafKeepsTheSeedWhereTheVariableIsUnset(t *testing.T) {
	t.Parallel()

	got, err := ferry.LoadOver(t.Context(), 4242, New(Environ(newEnviron().environ), RootVar(rootPort)))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 4242 {
		t.Errorf("loaded %d, want the seed a load over an unset variable never overwrote", got)
	}
}

// TestARootLeafIsRefusedWhereNoRootVarNamesIt is the default answer, and it is a
// refusal rather than a read of something arbitrary: the fold turns every byte
// outside A-Z, 0-9 and _ into _, so no name it produces could ever be the root's
// and there is nothing to guess at.
//
// It lands at Bind, where the caller still has the whole schema in hand and the
// environment has not been read.
func TestARootLeafIsRefusedWhereNoRootVarNamesIt(t *testing.T) {
	t.Parallel()

	read := 0
	environ := func() []string {
		read++

		return []string{"APP_PORT=8080"}
	}

	_, err := ferry.Load[int](t.Context(), New(Environ(environ)))
	if err == nil {
		t.Fatal("the driver loaded a schema whose only address is the root, with no variable to read it from")
	}

	for _, want := range []error{ferry.ErrPlane, ErrIllegalName} {
		if !errors.Is(err, want) {
			t.Errorf("the refusal is %+v, which does not answer errors.Is against %v", err, want)
		}
	}

	if !strings.Contains(err.Error(), "RootVar") {
		t.Errorf("the refusal is %q, want it to name the option that lifts it", err.Error())
	}

	if read != 0 {
		t.Errorf("the environment was read %d times, want none: a schema this plane cannot name is refused "+
			"at Bind, which does no I/O", read)
	}
}

// TestARootLeafRoundTripsThroughTheVariableRootVarNames is both halves reading
// one name, over the stand-in sink this package's tests write through, which is
// what makes the option a plane rule rather than a read-side spelling.
func TestARootLeafRoundTripsThroughTheVariableRootVarNames(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	src := New(Environ(e.environ), RootVar(rootPort))

	if err := ferry.Dump(t.Context(), 8080, standInSink{cfg: src.cfg, env: e}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got := e.vars[rootPort]; got != "8080" {
		t.Errorf("the environment holds %q at %s, want the value dumped at the root", got, rootPort)
	}

	got, err := ferry.Load[int](t.Context(), src)
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != 8080 {
		t.Errorf("loaded %d, want the value dumped at the root", got)
	}
}
