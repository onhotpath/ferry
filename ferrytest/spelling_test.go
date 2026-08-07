package ferrytest_test

import (
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// pair is a spelling assembled out of two closures, so a broken law is one line
// of the table below rather than a type per case.
type pair struct {
	parse  func(string) (string, error)
	render func(string) (string, error)
}

func (p pair) Parse(c string) (string, error)  { return p.parse(c) }
func (p pair) Render(v string) (string, error) { return p.render(v) }

// errNoReading is what every refusing half below answers with.
var errNoReading = errors.New("this plane has no reading for that")

// bracketed is the spelling that obeys every law, and the cases break one of
// its halves at a time.
func bracketed() pair {
	return pair{
		parse: func(c string) (string, error) {
			if len(c) < 2 || c[0] != '<' || c[len(c)-1] != '>' {
				return "", errNoReading
			}

			return c[1 : len(c)-1], nil
		},
		render: func(v string) (string, error) { return "<" + v + ">", nil },
	}
}

// counting makes an impure half: the same input answers differently the second
// time, which is the one thing the probe can see.
func counting(answers ...string) func(string) (string, error) {
	var n int

	return func(string) (string, error) {
		n++
		if n > len(answers) {
			n = len(answers)
		}

		return answers[n-1], nil
	}
}

// refusesThenAnswers is the impurity the refusal probe is for: a carrier this
// plane rejects once and accepts once.
func refusesThenAnswers() func(string) (string, error) {
	var n int

	return func(c string) (string, error) {
		n++
		if n == 1 {
			return "", errNoReading
		}

		return c, nil
	}
}

func TestSpellingReportsABrokenLaw(t *testing.T) {
	t.Parallel()

	refusing := func(string) (string, error) { return "", errNoReading }

	for _, tc := range []struct {
		name     string
		sp       ferry.Spelling[string, string]
		payloads []string
		refused  []string
		want     int
	}{
		{"whole", bracketed(), []string{"v", ""}, []string{"v", ""}, 0},
		{"render refuses", pair{parse: bracketed().parse, render: refusing}, []string{"v"}, nil, 1},
		{"render varies", pair{parse: bracketed().parse, render: counting("<a>", "<b>")}, []string{"v"}, nil, 1},
		{"parse refuses its own write", pair{parse: refusing, render: bracketed().render}, []string{"v"}, nil, 1},
		{"parse answers another", pair{parse: counting("other"), render: bracketed().render}, []string{"v"}, nil, 1},
		{"parse varies", pair{parse: counting("v", "other"), render: bracketed().render}, []string{"v"}, nil, 1},
		{"a refusal is answered", bracketed(), nil, []string{"<v>"}, 1},
		{"a refusal varies", pair{parse: refusesThenAnswers(), render: bracketed().render}, nil, []string{"v"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := &capture{}
			ferrytest.Spelling(c, tc.sp, ferrytest.Eq[string], tc.payloads, tc.refused)

			if len(c.lines) != tc.want {
				t.Errorf("reported %d failures, want %d: %v", len(c.lines), tc.want, c.lines)
			}
		})
	}
}

func TestSpellingReportsNoSpellingAtAll(t *testing.T) {
	t.Parallel()

	c := &capture{}
	ferrytest.Spelling[string, string](c, nil, ferrytest.Eq[string], []string{"v"}, []string{"x"})

	if len(c.lines) != 1 {
		t.Errorf("reported %v, want the one line saying there is nothing to prove", c.lines)
	}
}
