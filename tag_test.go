package ferry

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/internal/testdata/badtags"
)

// The fixtures live under internal/testdata for a reason that is itself the
// point of this file: two of the three failure modes make `go vet` report the
// source file that declares them, so they cannot sit in a package the module
// vets, and the third is the one vet does not check at all.
//
// Every assertion still goes through Compile[T], which is the seam. Nothing
// here calls the scanner.

// TestCoreCallsNeitherGetNorLookup holds the decision mechanically, because it
// is one line to undo and the undoing is invisible: Get answers with a silent
// empty string, so a core that called it would pass every test in this file
// except the ones whose fixtures it can no longer see.
func TestCoreCallsNeitherGetNorLookup(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parsing core: %v", err)
	}

	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			checkNoLookup(t, fset, name, file)
		}
	}
}

func checkNoLookup(t *testing.T, fset *token.FileSet, name string, file *ast.File) {
	t.Helper()

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		if fn := render(fset, call.Fun); strings.HasSuffix(fn, ".Tag.Get") || strings.HasSuffix(fn, ".Tag.Lookup") {
			t.Errorf("%s calls %s, and core scans the raw struct tag itself (ADR-0008)", name, fn)
		}

		return true
	})
}

func render(fset *token.FileSet, node ast.Node) string {
	var b strings.Builder

	if err := printer.Fprint(&b, fset, node); err != nil {
		return ""
	}

	return b.String()
}

func TestScannerDiagnosesWhatLookupHides(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "a bare double quote truncates the value and destroys the tags beside it",
		run:  Compile[badtags.BareQuote],
		want: []string{
			"/Origins: struct tag is not in the conventional `key:\"value\"` form",
			`at "value\"]\" json:\"origins\""`,
			"the usual cause is a bare double quote inside a ferry tag",
		},
		elements: 1,
	}, {
		name: "an escape Go does not define makes the tag invisible rather than wrong",
		run:  Compile[badtags.BadEscape],
		want: []string{
			`/Host: ferry tag value "a\,b" is not a valid Go quoted string`,
			"a struct tag value is unquoted by strconv.Unquote",
			"may not contain an escape Go does not define",
		},
		elements: 1,
	}, {
		name:     "a value with no closing quote",
		run:      Compile[badtags.Unterminated],
		want:     []string{`/Host: struct tag key "ferry" has an unterminated quoted value`},
		elements: 1,
	}, {
		name: "a field carrying two ferry tags, which go vet does not check",
		run:  Compile[badtags.Duplicate],
		want: []string{
			`/Host: the field carries two ferry tags, "first" and "second"`,
			"reflect.StructTag.Get returns the first",
		},
		elements: 1,
	}})
}

// TestScannerKeepsTheCauseReachable is why the diagnosis is built from an error
// rather than from a string: strconv's own sentinel stays matchable through
// ferry's wrapper, so a caller who wants to know which malformation it was can
// ask without reading the message.
func TestScannerKeepsTheCauseReachable(t *testing.T) {
	t.Parallel()

	err := Compile[badtags.BadEscape]()
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Errorf("%v does not carry strconv.ErrSyntax", err)
	}
}

// TestScannerRefusalIsScoped is the other half of the rule. A malformed tag is
// not always ferry's, and a field whose ferry tag was read cleanly is go vet's
// problem rather than ferry's.
func TestScannerRefusalIsScoped(t *testing.T) {
	t.Parallel()

	scoped := []struct {
		name string
		run  func(...Option) error
	}{
		{"another library's undefined escape", Compile[badtags.ForeignBroken]},
		{"another library's bare double quote", Compile[badtags.ForeignBareQuote]},
		{"text that is not a key:\"value\" pair at all", Compile[badtags.TrailingWord]},
	}

	for _, c := range scoped {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.run(); err != nil {
				t.Fatalf("refused a tag that is not ferry's: %+v", err)
			}
		})
	}
}

// TestScannerMatchesTheKeyExactly holds the key boundary. A key that merely
// ends in the one ferry was told to read is another library's, so its malformed
// tag is ignored and the field is left to the field rule - which is what the
// same field tagged json has always got (#261).
func TestScannerMatchesTheKeyExactly(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "a key ending in ferry's own is not ferry's",
		run:      Compile[badtags.ForeignKeySuffix],
		want:     []string{"field Host carries no ferry tag"},
		elements: 1,
	}, {
		name:     "and one that ends the tag, with no second occurrence to find",
		run:      Compile[badtags.ForeignKeySuffixAtEnd],
		want:     []string{"field Host carries no ferry tag"},
		elements: 1,
	}, {
		name:     "and json, which is the same field and the same answer",
		run:      Compile[badtags.ForeignKeyJSON],
		want:     []string{"field Host carries no ferry tag"},
		elements: 1,
	}, {
		name:     "a key ending in the one TagKey named",
		run:      Compile[badtags.ForeignEnvSuffix],
		opts:     []Option{TagKey("env")},
		want:     []string{"field Host carries no env tag"},
		elements: 1,
	}, {
		name:     "and a shorter one, where the collision is commoner still",
		run:      Compile[badtags.ForeignDBSuffix],
		opts:     []Option{TagKey("db")},
		want:     []string{"field Host carries no db tag"},
		elements: 1,
	}, {
		name:     "ferry's own key, malformed, still refuses loudly",
		run:      Compile[badtags.Unterminated],
		want:     []string{`/Host: struct tag key "ferry" has an unterminated quoted value`},
		elements: 1,
	}})
}

// TestScannerLeavesAForeignMalformedTagAlone is the same rule where nothing is
// left to refuse: a malformed tag under a key ferry does not own, and a
// malformed tag on a field reflect can never set, both compile clean.
func TestScannerLeavesAForeignMalformedTagAlone(t *testing.T) {
	t.Parallel()

	clean := []struct {
		name string
		run  func(...Option) error
	}{
		{"an embedded field under a key ending in ferry's own", Compile[badtags.ForeignKeyPromoted]},
		{"an unexported field under another library's key", Compile[badtags.UnexportedForeign]},
		{"an unexported field under ferry's own key", Compile[badtags.UnexportedMalformed]},
	}

	for _, c := range clean {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.run(); err != nil {
				t.Fatalf("refused a tag that is not ferry's: %+v", err)
			}
		})
	}
}

// TestScannerDistinguishesNoTagFromAnUnreadableOne is the distinction
// reflect.StructTag.Lookup cannot make, and it is what the found bit is for. A
// field carrying no ferry tag gets the field rule, and a field whose tag could
// not be read gets the scanner's own diagnosis; both refuse, and they refuse
// different things.
func TestScannerDistinguishesNoTagFromAnUnreadableOne(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "no tag at all",
		run:      Compile[untagged],
		want:     []string{"field Host carries no ferry tag"},
		elements: 1,
	}, {
		name:     "a tag that could not be read",
		run:      Compile[badtags.BadEscape],
		want:     []string{"is not a valid Go quoted string"},
		elements: 1,
	}})
}
