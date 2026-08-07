package ferry

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAHalfPairDoesNotCompile is the assertion behind ADR-0009's strongest
// claim, and it is the one assertion in this package that has to run the
// compiler.
//
// "A codec is a pair" is enforced by the signature rather than by a check, so
// there is no run-time behaviour to observe: what the rule produces is a build
// error, and a rule nothing asserts is a rule the next refactor drops. Four
// fixtures under internal/testdata name the shapes a registrant can get wrong -
// one half, both halves swapped, two halves over different types, and a
// bytes-keyed map - and each is a package the go command never matches against
// ./..., so the module still builds, vets and lints clean around them.
//
// The fourth is ADR-0017's key eligibility, which is a compile fact for the same
// reason: AsMapKey exists on the return type of the two constructors whose kind
// may address a map key and on nothing else, so a bytes registration declaring
// itself a key does not compile rather than being refused at NewRegistry.
//
// The exact wording of a compiler diagnostic is Go's rather than ferry's, so
// what is asserted is that the build fails and that the message names the
// constructor and the type inference got hold of. That is what makes the
// diagnostic document the API, which ADR-0009 records as the reason a half pair
// needs no refusal of its own.
func TestAHalfPairDoesNotCompile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		pkg  string
		want []string
	}{{
		name: "one half only",
		pkg:  "./internal/testdata/badcodec/halfpair",
		want: []string{"not enough arguments in call to ferry.StringValue", "want (func(T) (string, error)"},
	}, {
		name: "both halves, swapped",
		pkg:  "./internal/testdata/badcodec/swapped",
		want: []string{"in call to ferry.StringValue"},
	}, {
		name: "two halves over two types",
		pkg:  "./internal/testdata/badcodec/mismatched",
		want: []string{"in call to ferry.StringValue"},
	}, {
		name: "a bytes registration declared usable as a map key",
		pkg:  "./internal/testdata/badcodec/byteskey",
		want: []string{"AsMapKey undefined"},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustNotCompile(t, c.pkg, c.want)
		})
	}
}

// mustNotCompile builds one fixture package and holds its refusal to naming the
// constructor and what inference made of the arguments.
func mustNotCompile(t *testing.T, pkg string, want []string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "go", "build", pkg).CombinedOutput()
	if err == nil {
		t.Fatalf("%s compiled, and it is a fixture that must not", pkg)
	}

	for _, w := range want {
		if !strings.Contains(string(out), w) {
			t.Errorf("the compiler said\n\t%s\nand it does not contain\n\t%s", out, w)
		}
	}
}
