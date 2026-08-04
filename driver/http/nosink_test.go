package ferryhttp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
)

// The read half is implemented, and this is the compile-time half of the claim
// that the write half is not. It fails to build the moment [Source] stops
// satisfying ferry.Source.
var _ ferry.Source = (*Source)(nil)

// TestDumpingToAnHTTPRequestDoesNotCompile is the acceptance criterion stated as
// something a test can actually execute.
//
// ferry.Dump's third parameter is a ferry.Sink, which is an interface, so
// ferry.Dump(ctx, v, ferryhttp.NewQuerySource()) compiles exactly when *Source
// implements that interface and not otherwise. The assertion below is therefore
// the same question the compiler answers, asked at run time: it is false today,
// and it becomes true on the day somebody gives *Source a Bind returning a
// ferry.OpenWriterFunc - which is the day the call site would start compiling.
func TestDumpingToAnHTTPRequestDoesNotCompile(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]*Source{"query": NewQuerySource(), "header": NewHeaderSource()} {
		if _, ok := any(src).(ferry.Sink); ok {
			t.Errorf("*Source satisfies ferry.Sink, so ferry.Dump through the %s plane now compiles: what a "+
				"write into a request the caller already built should do at a parameter that is already set "+
				"is undecided, and shipping a sink answers it silently (#210)", name)
		}
	}
}

// TestPackageDeclaresNoSink is the second half, and it covers what the first
// cannot: a sink shipped here as a type of its own rather than as a method on
// [Source].
//
// A ferry sink is a Bind returning a ferry.OpenWriterFunc, and there is no way
// to write one without naming that type, so the package's own source is scanned
// for it. Comments are not scanned - the selector has to appear as code - which
// is what lets this file and the package documentation say the words.
func TestPackageDeclaresNoSink(t *testing.T) {
	t.Parallel()

	files := shippedFiles(t)

	// The control. This package's read half is spelled with the same selector
	// shape its write half would be, so a scan that cannot see ferry.OpenFunc
	// could not see ferry.OpenWriterFunc either, and would pass by finding
	// nothing anywhere.
	if n := selectors(t, files, "OpenFunc"); n == 0 {
		t.Fatalf("the scan found no ferry.OpenFunc across %d files whose Bind returns one, so it is looking "+
			"in the wrong place and asserts nothing", len(files))
	}

	if n := selectors(t, files, "Sink", "OpenWriterFunc"); n != 0 {
		t.Errorf("this package's own source names ferry.Sink or ferry.OpenWriterFunc %d times, so it ships a "+
			"write half", n)
	}
}

// TestOneBindingServesManyGoroutines is the property net/http imposes rather
// than one this driver chose: a handler runs in a goroutine of its own, so a
// source built at start-up is read from all of them at once.
//
// It goes through ferry.Load rather than reaching for a binding, because that is
// the seam, and each goroutine supplies a request of its own through the
// context, which is what a per-request plane means. Under -race it is also the
// assertion that nothing the bind computed is written to afterwards.
func TestOneBindingServesManyGoroutines(t *testing.T) {
	t.Parallel()

	const requests = 32

	src := NewQuerySource()

	var wg sync.WaitGroup

	failures := make([]error, requests)

	for i := range requests {
		wg.Go(func() { failures[i] = loadOne(t, src, strconv.Itoa(i)) })
	}

	wg.Wait()

	for _, err := range failures {
		if err != nil {
			t.Errorf("one source shared across %d concurrent loads: %v", requests, err)
		}
	}
}

// oneQ is the schema the concurrent loads read, and it is at file scope so that
// the goroutine helper below is not generic over a type declared inside a test.
type oneQ struct {
	Q string `ferry:"q"`
}

// loadOne is one goroutine's whole request: its own query, its own load, its own
// answer.
func loadOne(t *testing.T, src *Source, want string) error {
	t.Helper()

	got, err := ferry.Load[oneQ](queryCtx(t, "q="+want), src)
	if err != nil {
		return err
	}

	if got.Q != want {
		return fmt.Errorf("a load of q=%s read %q", want, got.Q)
	}

	return nil
}

// shippedFiles is this package's own source, without its tests: what a consumer
// compiles when they import it.
func shippedFiles(t *testing.T) []string {
	t.Helper()

	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing this package's files: %v", err)
	}

	out := make([]string, 0, len(all))

	for _, path := range all {
		if !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
	}

	return out
}

// selectors counts the qualified references to the named identifiers across a
// set of files, whatever package they are qualified by.
func selectors(t *testing.T, files []string, names ...string) int {
	t.Helper()

	fset := token.NewFileSet()
	found := 0

	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		found += countSelectors(f, names)
	}

	return found
}

// countSelectors is the walk one parsed file at a time, so that the loop above
// stays a loop and this stays a matcher.
func countSelectors(f *ast.File, names []string) int {
	found := 0

	ast.Inspect(f, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok && slices.Contains(names, sel.Sel.Name) {
			found++
		}

		return true
	})

	return found
}
