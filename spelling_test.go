package ferry_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// A spelling is a seam, in the sense CLAUDE.md gives the word: it is a contract
// core publishes for a driver to implement and core itself never calls, so
// there is no Load or Dump to assert it through and the tests below hold it
// directly. The laws themselves go through the published proof,
// [ferrytest.Spelling], which is the one ADR-0018 obliges (ADR-0014, ADR-0018).

// onOff is the worked case ADR-0018 states law 2 with: four accepted words, one
// written form, and the written form inside the accept set so that the round
// trip closes.
func onOff() ferry.Spelling[bool, string] {
	words := map[string]bool{"on": true, "off": false, "true": true, "false": false}

	return ferry.SpellingFunc(
		func(text string) (bool, error) {
			b, ok := words[text]
			if !ok {
				return false, errors.New("no word of this plane spells a bool that way")
			}

			return b, nil
		},
		func(v bool) (string, error) {
			if v {
				return "on", nil
			}

			return "off", nil
		},
	)
}

func TestSpellingFuncObeysTheLaws(t *testing.T) {
	t.Parallel()

	ferrytest.Spelling(t, onOff(), ferrytest.Eq[bool],
		[]bool{true, false},
		[]string{"yes", "ON", "1", ""},
	)
}

func TestSpellingFuncRefusesWithoutBothHalves(t *testing.T) {
	t.Parallel()

	parse := func(string) (bool, error) { return true, nil }
	render := func(bool) (string, error) { return "on", nil }

	for _, tc := range []struct {
		name string
		sp   ferry.Spelling[bool, string]
		want string
	}{
		{"neither", ferry.SpellingFunc[bool, string](nil, nil), "either half"},
		{"no parse", ferry.SpellingFunc(nil, render), "the half that reads the plane"},
		{"no render", ferry.SpellingFunc[bool, string](parse, nil), "the half that writes the plane"},
		{"no spelling", ferry.With[bool, string](nil), "the spelling it stacks under"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, parseErr := tc.sp.Parse("on")
			_, renderErr := tc.sp.Render(true)

			checkMissingHalf(t, parseErr, tc.want)
			checkMissingHalf(t, renderErr, tc.want)
		})
	}
}

// checkMissingHalf is the assertion both halves get: the class, and the words
// that say which half to supply.
func checkMissingHalf(t *testing.T, err error, want string) {
	t.Helper()

	if !errors.Is(err, ferry.ErrSchema) {
		t.Errorf("refused with %v, want a schema error", err)
	}

	if !strings.Contains(err.Error(), want) {
		t.Errorf("refused with %q, want it to name %q", err, want)
	}
}

// suffix is a payload step whose two directions are visible in the payload
// itself, so the order [ferry.With] composes them in is assertable without a
// transform that records anything: a recording transform would be impure, which
// is the one thing every step here has to be.
type suffix struct{ s string }

func (t suffix) Apply(v string) (string, error) { return v + t.s, nil }

func (t suffix) Invert(v string) (string, error) {
	cut, ok := strings.CutSuffix(v, t.s)
	if !ok {
		return "", fmt.Errorf("the payload does not end in %q", t.s)
	}

	return cut, nil
}

// quoted is a spelling with no transform of its own, so what a composition
// writes is readable at a glance.
type quoted struct{}

func (quoted) Render(v string) (string, error) { return "<" + v + ">", nil }

func (quoted) Parse(c string) (string, error) {
	if !strings.HasPrefix(c, "<") || !strings.HasSuffix(c, ">") {
		return "", errors.New("the carrier is not one this plane wrote")
	}

	return c[1 : len(c)-1], nil
}

func TestWithComposesOutermostFirst(t *testing.T) {
	t.Parallel()

	sp := ferry.With[string, string](quoted{}, suffix{s: "-outer"}, suffix{s: "-inner"})

	got, err := sp.Render("v")
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// The step written last is closest to the payload, so it runs first on the
	// way out: quoted(outer(inner(v))).
	if want := "<v-inner-outer>"; got != want {
		t.Errorf("Render wrote %q, want %q", got, want)
	}

	ferrytest.Spelling(t, sp, ferrytest.Eq[string], []string{"v", ""}, []string{"v", "<v>"})
}

func TestWithoutStepsIsTheSpellingItself(t *testing.T) {
	t.Parallel()

	sp := quoted{}
	if got := ferry.With[string, string](sp); got != ferry.Spelling[string, string](sp) {
		t.Errorf("With returned %#v, want the spelling it was given", got)
	}
}

func TestWithReportsAStepThatRefuses(t *testing.T) {
	t.Parallel()

	sp := ferry.With[string, string](quoted{}, budget{n: 3}, suffix{s: "!"})

	if _, err := sp.Render("wide"); !errors.Is(err, errBudget) {
		t.Errorf("Render answered %v, want the outbound refusal", err)
	}

	if _, err := sp.Parse("<v>"); err == nil {
		t.Error("Parse accepted a carrier the inner step cannot undo")
	}
}

// errBudget is what a size refusal on the way out is matched by, so the test
// asserts the refusal rather than its wording.
var errBudget = errors.New("payload is wider than this plane's budget")

// budget refuses on the way out, which is the half of [ferry.Transform] that is
// easy to forget: a knowable-before-I/O failure lands in the encode phase.
type budget struct{ n int }

func (b budget) Apply(v string) (string, error) {
	if len(v) > b.n {
		return "", errBudget
	}

	return v, nil
}

func (budget) Invert(v string) (string, error) { return v, nil }
