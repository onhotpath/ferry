package ferrytest_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/ferrytest"
)

// T is satisfied by the testing types with no adapter, which is the whole
// reason it is two methods rather than a struct or *testing.T itself.
var (
	_ ferrytest.T = (*testing.T)(nil)
	_ ferrytest.T = (*testing.B)(nil)
	_ ferrytest.T = (*testing.F)(nil)
)

// capture is what the second and third reasons for T buy: a caller who wants to
// assert that a driver fails a case, and this package's own tests holding its
// suites to the rules it publishes. [ferrytest.RoundTrip] reports through it
// throughout roundtrip_test.go, which is the whole of why T is an interface.
type capture struct {
	lines   []string
	logs    []string
	helpers int
}

func (c *capture) Errorf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

// Logf is not on [ferrytest.T] and is deliberately implemented anyway.
//
// A suite skips a case that cannot be run - case 10 for a plane that does not
// take its plane per request, case 5 for a reader that does not enumerate - and
// ADR-0014 wants
// that skip explicit rather than silent. T is two methods and neither is a log,
// so a suite writes the skip where the reporter can carry one, which *testing.T
// does and which this captures separately from the failures.
func (c *capture) Logf(format string, args ...any) {
	c.logs = append(c.logs, fmt.Sprintf(format, args...))
}

// Helper is counted rather than ignored, because a suite that never calls it
// attributes every failure to a line inside ferrytest and a driver author
// learns nothing from their own CI output.
func (c *capture) Helper() { c.helpers++ }

var _ ferrytest.T = (*capture)(nil)

func TestCaptureIsAT(t *testing.T) {
	var reported ferrytest.T = &capture{}

	reported.Helper()
	reported.Errorf("%s", "a failure a suite reported")

	c, ok := reported.(*capture)
	if !ok {
		t.Fatalf("%T is not the capture it was built as", reported)
	}

	if len(c.lines) != 1 {
		t.Errorf("captured %d reports, want 1", len(c.lines))
	}
}

// TestExportedSurface is ADR-0014's list, held to mechanically.
//
// The surface is fixed by decision rather than left to emerge - which is why
// revive's max-public-structs is switched off in this repository - so a name
// arriving here without an ADR behind it is a change to a published contract
// that a driver's CI depends on.
//
// CoreTypes arrived ahead of the engine that can satisfy it, which is #72's own
// decision rather than an accident of ordering: the table is ADR-0013's
// compatibility promise in executable form, so it is complete from the ticket
// that writes it and the types the engine cannot yet carry are named in a skip
// list in core's own test rather than being absent from the artefact.
//
// The list below is ADR-0014's own, name for name (#175, and #114/#237 for
// `Inside`). Why each of them ships is argued there and is not restated here: a
// test comment explaining why the specification is wrong is a workaround, and
// the specification is now right.
func TestExportedSurface(t *testing.T) {
	want := []string{
		"Artefact", "At", "BitEq", "Case", "CheckErrors", "Codec", "Complete", "CoreTypes", "DiffErrors",
		"Driver", "Eq", "Golden", "Injective", "Inside", "Instance", "MapEq", "MemPlane", "Plane", "Proof",
		"PtrEq", "Record", "RoundTrip", "SliceEq", "Static", "T", "Type", "Want",
	}

	got := exportedNames(t)
	if !slices.Equal(got, want) {
		t.Errorf("ferrytest exports\n\t%v\nwant\n\t%v", got, want)
	}
}

func exportedNames(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	var out []string

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		out = append(out, namesIn(t, filepath.Join(".", e.Name()))...)
	}

	slices.Sort(out)

	return out
}

func namesIn(t *testing.T, path string) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var out []string
	for _, d := range f.Decls {
		out = append(out, declNames(d)...)
	}

	return out
}

func declNames(decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil {
			return nil
		}

		return exportedOnly(d.Name)
	case *ast.GenDecl:
		return genDeclNames(d)
	default:
		return nil
	}
}

func genDeclNames(decl *ast.GenDecl) []string {
	var out []string

	for _, spec := range decl.Specs {
		out = append(out, specNames(spec)...)
	}

	return out
}

func specNames(spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return exportedOnly(s.Name)
	case *ast.ValueSpec:
		return exportedOnly(s.Names...)
	default:
		return nil
	}
}

func exportedOnly(ids ...*ast.Ident) []string {
	var out []string

	for _, id := range ids {
		if id.IsExported() {
			out = append(out, id.Name)
		}
	}

	return out
}
