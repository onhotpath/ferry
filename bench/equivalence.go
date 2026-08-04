package bench

import (
	"fmt"
	"reflect"
	"strings"
)

// Mismatch is one implementation that did not produce the scenario's expected
// value, or failed outright.
type Mismatch struct {
	// Scenario and Impl name the cell of the table that is wrong.
	Scenario string
	Impl     string

	// Err is the failure, if the implementation returned one.
	Err error

	// Got and Want are the values, when there was a value to compare.
	Got  any
	Want any
}

// Error renders the mismatch, with the two values in full.
//
// In full is the point: a benchmark whose columns are not doing the same work
// is worse than no benchmark, because it will be believed and then found out,
// so the failure has to say exactly which field diverged rather than "not
// equal".
func (m *Mismatch) Error() string {
	if m.Err != nil {
		return fmt.Sprintf("%s/%s: failed: %v", m.Scenario, m.Impl, m.Err)
	}

	return fmt.Sprintf("%s/%s: produced a different value\n  got:  %#v\n  want: %#v",
		m.Scenario, m.Impl, m.Got, m.Want)
}

// CheckEquivalence runs every implementation of every scenario exactly once,
// outside any timed loop, and reports every one whose result is not identical
// to the scenario's expected value.
//
// This is the gate the whole harness hangs off. Nothing is benchmarked until
// it passes, because the only way a comparison of this kind is worth
// publishing is if every column ended at the same place from the same source.
// A library that cannot express the struct fails here and is reported in
// [Absences] rather than quietly handed an easier one.
func CheckEquivalence(f *Fixture) []Mismatch {
	var bad []Mismatch

	for i := range Scenarios() {
		sc := Scenarios()[i]
		if sc.Setup != nil {
			sc.Setup(f)
		}

		bad = append(bad, checkScenario(f, &sc)...)
	}

	return bad
}

func checkScenario(f *Fixture, sc *Scenario) []Mismatch {
	var bad []Mismatch

	for i := range sc.Impls {
		if m := checkImpl(f, sc, &sc.Impls[i]); m != nil {
			bad = append(bad, *m)
		}
	}

	return bad
}

// checkImpl returns nil when the implementation produced the expected value.
func checkImpl(f *Fixture, sc *Scenario, impl *Impl) *Mismatch {
	load, err := impl.New(f)
	if err != nil {
		return &Mismatch{Scenario: sc.Name, Impl: impl.Name, Err: err}
	}

	// A fresh destination per implementation, never one shared across the
	// loop: a shared one lets the previous library's value stand in for a
	// field this one failed to write.
	dst := sc.NewDst()
	if err := load(dst); err != nil {
		return &Mismatch{Scenario: sc.Name, Impl: impl.Name, Err: err}
	}

	got := reflect.ValueOf(dst).Elem().Interface()
	if !reflect.DeepEqual(got, sc.Want) {
		return &Mismatch{Scenario: sc.Name, Impl: impl.Name, Got: got, Want: sc.Want}
	}

	return nil
}

// FormatMismatches renders a set of mismatches as one message.
func FormatMismatches(bad []Mismatch) string {
	lines := make([]string, 0, len(bad)+1)
	lines = append(lines, fmt.Sprintf(
		"%d implementation(s) did not produce the expected value; refusing to benchmark:", len(bad)))

	for i := range bad {
		lines = append(lines, "  "+strings.ReplaceAll(bad[i].Error(), "\n", "\n  "))
	}

	return strings.Join(lines, "\n")
}
