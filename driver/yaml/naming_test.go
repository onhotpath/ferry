package yaml_test

import (
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// The fixtures this file refuses through: a nested key, a sequence position, and
// a mapping whose key the document leaves empty.
type (
	namedNested struct {
		DB namedPort `ferry:"db"`
	}
	namedPort struct {
		Port int `ferry:"port"`
	}
	namedList struct {
		Ports []int `ferry:"ports"`
	}
	namedMap struct {
		M map[string]string `ferry:"m"`
	}
)

// TestAReportNamesTheKeyInTheDocument is what this driver's own spelling buys:
// a failure reads as the key somebody opens the file and edits, rather than as
// ferry's rendering of the address.
func TestAReportNamesTheKeyInTheDocument(t *testing.T) {
	t.Parallel()

	path := write(t, "db:\n  port: eighty\n")

	_, err := ferry.Load[namedNested](t.Context(), yaml.NewSource(path))
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: db.port: ") {
		t.Errorf("the report does not open with the document's own name for the key: %s", got)
	}
}

// TestAReportNamesASequencePositionInBrackets is the other half of the
// rendering: a position is not a member and is not spelled like one.
func TestAReportNamesASequencePositionInBrackets(t *testing.T) {
	t.Parallel()

	path := write(t, "ports:\n  - eighty\n")

	_, err := ferry.Load[namedList](t.Context(), yaml.NewSource(path))
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: ports[0]: ") {
		t.Errorf("the report does not name the position the way a document does: %s", got)
	}
}

// TestAKeyWithNoNameIsRefusedAtTheMappingThatHoldsIt is where a document's
// empty key lands: the refusal is about the mapping, so the mapping is what the
// report names, and no address carrying the empty member is ever built.
func TestAKeyWithNoNameIsRefusedAtTheMappingThatHoldsIt(t *testing.T) {
	t.Parallel()

	path := write(t, "m:\n  \"\": v\n")

	_, err := ferry.Load[namedMap](t.Context(), yaml.NewSource(path))
	if err == nil {
		t.Fatal("the load succeeded, so there is no report to read")
	}

	if got := err.Error(); !strings.HasPrefix(got, "ferry: m: ") {
		t.Errorf("the report does not name the mapping the empty key is under: %s", got)
	}
}

// TestAMemberWithNoNameHasNoRendering is that guard asserted directly, because
// nothing a load or a dump can do reaches it: the address is refused before it
// is built. Reached through the test-only handle for that reason.
func TestAMemberWithNoNameHasNoRendering(t *testing.T) {
	t.Parallel()

	for _, addr := range []ferry.Path{ferry.At("m").At(""), {}} {
		if got, named := yaml.DocumentName(addr); named {
			t.Errorf("the document named %v %q, and a member with no name has no name in a document either",
				addr, got)
		}
	}
}
