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
// suites to the rules it publishes. Nothing takes a T yet - the suites are a
// later ticket - so this asserts only that a caller who is not a test can be
// one.
type capture struct{ lines []string }

func (c *capture) Errorf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (*capture) Helper() {}

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
// that a driver's CI depends on. The seven missing from ADR-0014's twenty are
// the suites and the tables that call the entry point, which does not exist
// yet: RoundTrip, Driver, Codec, Complete, Injective, Record and CoreTypes.
//
// Twenty rather than nineteen since #101, which added Instance: the shape as
// published could not support its own golden artefact case, because nothing
// handed a suite the contents of the plane instance it had just dumped to.
func TestExportedSurface(t *testing.T) {
	want := []string{
		"Artefact", "At", "BitEq", "Case", "Eq", "Instance", "MapEq", "MemPlane",
		"Plane", "Proof", "PtrEq", "SliceEq", "Static", "T", "Type",
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
