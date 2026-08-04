package multimap

import (
	"testing"
)

// queryCase is one row of #193's matrix on the query plane: a raw query string,
// the schema it is loaded into, and a label.
type queryCase struct {
	label string
	raw   string
	run   func(s Shape, raw string, opts ...Option) string
}

// queryCases is the floor #193 sets, plus the rows the prototype found it needed.
func queryCases() []queryCase {
	return []queryCase{
		{"?tags=a&tags=b   -> Tags []string", "tags=a&tags=b", loadQuery[Tagged]},
		{"?tags=a          -> Tags []string", "tags=a", loadQuery[Tagged]},
		{"?tags.0=a&tags.1=b -> Tags []string", "tags.0=a&tags.1=b", loadQuery[Tagged]},
		{"(nothing)        -> Tags []string", "", loadQuery[Tagged]},
		{"?q=a&q=b         -> Q string", "q=a&q=b", loadQuery[Scalar]},
		{"?q=a             -> Q string", "q=a", loadQuery[Scalar]},
		{"?x=              -> X string", "x=", loadQuery[Empty]},
		{"?limits.cpu=4&limits.mem=8 -> map[string]int", "limits.cpu=4&limits.mem=8", loadQuery[Mapped]},
		{"?limits=a&limits=b -> map[string]int", "limits=a&limits=b", loadQuery[Mapped]},
		{"?tags=a&tags=b&q=z -> Tags []string + Q string", "tags=a&tags=b&q=z", loadQuery[Both]},
		{"?pair=a&pair=b   -> Pair [2]string", "pair=a&pair=b", loadQuery[Fixed2]},
		{"?pair=a&pair=b&pair=c -> Pair [2]string", "pair=a&pair=b&pair=c", loadQuery[Fixed2]},
		{"?tags=a&tags=b&tags.0=z -> Tags []string", "tags=a&tags=b&tags.0=z", loadQuery[Tagged]},
	}
}

// TestQueryMatrix is the whole of #193's question on the query plane, every
// shape against every case.
func TestQueryMatrix(t *testing.T) {
	for _, s := range Shapes() {
		t.Logf("")
		t.Logf("=== shape %s (query plane) ===", s)

		for _, c := range queryCases() {
			t.Logf("  %-42s  %s", c.label, c.run(s, c.raw, declaredFor(s)...))
		}
	}
}

// declaredFor is the one shape that needs configuring: Declared is told which
// plane keys carry a sequence, and the whole matrix declares the two that do.
func declaredFor(s Shape) []Option {
	if s != Declared {
		return nil
	}

	return []Option{Repeatable("tags", "pair", "Accept-Encoding")}
}

// headerCase is the header plane's row.
type headerCase struct {
	label string
	pairs [][2]string
	run   func(s Shape, pairs [][2]string, opts ...Option) string
}

func headerCases() []headerCase {
	return []headerCase{
		{"Accept-Encoding: gzip / br  -> []string",
			[][2]string{{"Accept-Encoding", "gzip"}, {"Accept-Encoding", "br"}}, loadHeader[AcceptSeq]},
		{"Accept-Encoding: gzip       -> []string",
			[][2]string{{"Accept-Encoding", "gzip"}}, loadHeader[AcceptSeq]},
		{"Accept-Encoding-0/-1        -> []string",
			[][2]string{{"Accept-Encoding-0", "gzip"}, {"Accept-Encoding-1", "br"}}, loadHeader[AcceptSeq]},
		{"(nothing)                   -> []string", nil, loadHeader[AcceptSeq]},
		{"Accept-Encoding: gzip / br  -> string",
			[][2]string{{"Accept-Encoding", "gzip"}, {"Accept-Encoding", "br"}}, loadHeader[AcceptOne]},
		{"Accept-Encoding: gzip       -> string",
			[][2]string{{"Accept-Encoding", "gzip"}}, loadHeader[AcceptOne]},
		{"X: (empty)                  -> X string",
			[][2]string{{"X", ""}}, loadHeader[Empty]},
		{"Limits-Cpu: 4, Limits-Mem: 8 -> map[string]int",
			[][2]string{{"Limits-Cpu", "4"}, {"Limits-Mem", "8"}}, loadHeader[Mapped]},
		{"Limits: a / b               -> map[string]int",
			[][2]string{{"Limits", "a"}, {"Limits", "b"}}, loadHeader[Mapped]},
		{"Pair: a / b                 -> [2]string",
			[][2]string{{"Pair", "a"}, {"Pair", "b"}}, loadHeader[HFixed2]},
		{"Accept-Encoding gzip/br + Q -> []string + string",
			[][2]string{{"Accept-Encoding", "gzip"}, {"Accept-Encoding", "br"}, {"Q", "z"}}, loadHeader[HBoth]},
	}
}

// TestHeaderMatrix is the same question on the header plane, which is the same
// shape and is canonicalised by net/http.
func TestHeaderMatrix(t *testing.T) {
	for _, s := range Shapes() {
		t.Logf("")
		t.Logf("=== shape %s (header plane) ===", s)

		for _, c := range headerCases() {
			t.Logf("  %-44s  %s", c.label, c.run(s, c.pairs, declaredFor(s)...))
		}
	}
}

// TestCallSequence prints the boundary calls the walk actually makes at a
// dynamic container, which is the whole mechanism #193's correction names.
func TestCallSequence(t *testing.T) {
	for _, tc := range []struct {
		label string
		shape Shape
		raw   string
	}{
		{"[]string, two values at one name", Cardinality, "tags=a&tags=b"},
		{"[]string, one value at one name", Cardinality, "tags=a"},
		{"[]string, index-suffixed names", Indexed, "tags.0=a&tags.1=b"},
		{"string, two values at one name", Cardinality, "q=a&q=b"},
	} {
		t.Logf("")
		t.Logf("=== %s (%s) ===", tc.label, tc.shape)

		var trace []string

		out := loadQuery[Tagged](tc.shape, tc.raw, Trace(&trace))
		if tc.label == "string, two values at one name" {
			trace = nil
			out = loadQuery[Scalar](tc.shape, tc.raw, Trace(&trace))
		}

		for _, line := range trace {
			t.Logf("    %s", line)
		}

		t.Logf("    => %s", out)
	}
}
