package env

import (
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
)

// TestOptionsAreCheckedBeforeAnythingElse holds the driver to refusing a
// configuration it cannot serve, rather than guessing at one.
//
// The separator rows are the whole of what a separator may be: it lands inside
// every name this driver produces, so a separator holding a byte an environment
// variable name cannot hold would make every name illegal at once, and reporting
// that per address would name every address in the schema for one mistake in the
// call.
func TestOptionsAreCheckedBeforeAnythingElse(t *testing.T) {
	t.Parallel()

	cases := map[string]optionCase{
		"no separator at all":       {opt: Separator(""), err: ErrOption},
		"a hyphen separator":        {opt: Separator("-"), err: ErrOption},
		"a dot separator":           {opt: Separator("."), err: ErrOption},
		"a space separator":         {opt: Separator(" "), err: ErrOption},
		"a canonical form there is": {opt: Canonical(Form(7)), err: ErrOption},
		"no environment to read":    {opt: Environ(nil), err: ErrOption},
		"the wider join":            {opt: Separator("__")},
		"a digit in the separator":  {opt: Separator("_0_")},
		"the upper form":            {opt: Canonical(Upper)},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkOption(t, tc)
		})
	}
}

// optionCase is one option and what binding under it must do: nothing, or a
// refusal carrying the sentinel below.
type optionCase struct {
	opt Option
	err error
}

// checkOption binds one source under one option.
func checkOption(t *testing.T, tc optionCase) {
	t.Helper()

	src := New(Environ(newEnviron().environ), tc.opt)

	_, err := src.Bind(ferry.NewAddressSet(ferry.At("leaf")))
	if !errors.Is(err, tc.err) {
		t.Fatalf("Bind = %v, want %v", err, tc.err)
	}

	if tc.err != nil && !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("the refusal %v is not the plane's own class", err)
	}
}

// TestDefaults pins what a Source built with no options does, because every one
// of the three is a decision a caller inherits by saying nothing.
func TestDefaults(t *testing.T) {
	t.Parallel()

	c := New().cfg

	if c.sep != DefaultSeparator {
		t.Errorf("the default separator is %q, want %q", c.sep, DefaultSeparator)
	}

	if c.canon != Lower {
		t.Errorf("the default canonical form is %v, want Lower", c.canon)
	}

	if c.environ == nil {
		t.Error("a Source built with no options has no environment to read")
	}
}

// TestDefaultReadsTheProcessEnvironment is the one test here that touches the
// real environment, and it is why every other one does not.
//
// t.Setenv forbids t.Parallel, so this test cannot run beside the rest, and a
// package whose every test mutated the process environment would be a package
// whose tests could not run concurrently at all. That hazard is what [Environ]
// exists to remove. It is still worth one test: without it, nothing would catch
// a default that was wired to an empty environment.
func TestDefaultReadsTheProcessEnvironment(t *testing.T) {
	const name = "FERRY_ENV_DRIVER_PROBE"

	t.Setenv(name, "v")

	addr := ferry.At(name)

	open, err := New().Bind(ferry.NewAddressSet(addr))
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	r, err := open(t.Context())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	got, err := r.Get(t.Context(), addr)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != ferry.String("v") {
		t.Errorf("Get(%s) = %#v, want the value the process environment holds", addr, got)
	}
}
