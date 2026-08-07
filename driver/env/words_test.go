package env

import (
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// wordCase is one variable's text and the boolean this plane's words spell it
// as.
type wordCase struct {
	text string
	want bool
}

// toggles is the destination the word cases load into.
type toggles struct {
	Enabled bool   `ferry:"enabled"`
	Label   string `ferry:"label"`
}

// TestBoolWordsFillABoolField is the case ADR-0018 opens with: a plane that
// spells a boolean on and off could not load one at all, because the text went
// to a leaf's own parser and that parser reads true and false.
func TestBoolWordsFillABoolField(t *testing.T) {
	t.Parallel()

	cases := map[string]wordCase{
		"the written truthy word": {text: "on", want: true},
		"the written falsy word":  {text: "off", want: false},
		"a further truthy word":   {text: "true", want: true},
		"a further falsy word":    {text: "false", want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spells(t, tc)
		})
	}
}

// spells is one word, lifted out of its table so that the table stays a table:
// a subtest body counts against the enclosing function's complexity.
func spells(t *testing.T, tc wordCase) {
	t.Helper()

	e := newEnviron()
	e.vars["ENABLED"] = tc.text

	got, err := ferry.Load[toggles](t.Context(),
		New(Environ(e.environ), BoolWords("on", "off", "true", "false")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got.Enabled != tc.want {
		t.Errorf("loaded %t, want %t", got.Enabled, tc.want)
	}
}

// TestBoolWordsLeaveEveryOtherTextAString is the other half of the same rule:
// the words are what makes a value a boolean on this plane, and a variable
// holding none of them is text, exactly as it was before the option existed.
func TestBoolWordsLeaveEveryOtherTextAString(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["ENABLED"] = "on"
	e.vars["LABEL"] = "checkout"

	got, err := ferry.Load[toggles](t.Context(), New(Environ(e.environ), BoolWords("on", "off")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got.Label != "checkout" {
		t.Errorf("loaded %q, want the text the variable held", got.Label)
	}
}

// TestBoolWordsAreExact holds the option to the case rule its doc states: a
// word matches as it was written, so ON is not on and stays text - which a bool
// field then refuses rather than reading as a value nobody wrote.
func TestBoolWordsAreExact(t *testing.T) {
	t.Parallel()

	e := newEnviron()
	e.vars["ENABLED"] = "ON"

	_, err := ferry.Load[toggles](t.Context(), New(Environ(e.environ), BoolWords("on", "off")))
	if !errors.Is(err, ferry.ErrValue) {
		t.Errorf("load = %v, want a value error over the text no word spells", err)
	}
}

// TestBoolWordsRefuseAListWithNoSpelling is where a word list this driver
// cannot build a spelling from is refused: at Bind, before any environment is
// read, which is where every other option of this driver is checked.
func TestBoolWordsRefuseAListWithNoSpelling(t *testing.T) {
	t.Parallel()

	cases := map[string]optionCase{
		"half a pair":       {opt: BoolWords("on", "off", "yes"), err: ErrOption},
		"an empty word":     {opt: BoolWords("on", ""), err: ErrOption},
		"one word twice":    {opt: BoolWords("on", "off", "on", "no"), err: ErrOption},
		"two whole pairs":   {opt: BoolWords("on", "off", "yes", "no")},
		"one pair, plainly": {opt: BoolWords("on", "off")},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkOption(t, tc)
		})
	}
}

// TestBoolWordsObeyTheLaws runs the published proof over the spelling the
// option builds, which is what holds its two halves to each other: four words
// accepted, one written, and the written one inside the accept set.
func TestBoolWordsObeyTheLaws(t *testing.T) {
	t.Parallel()

	c := defaults()
	BoolWords("on", "off", "true", "false").apply(&c)

	if c.wordsErr != nil {
		t.Fatalf("building the spelling: %v", c.wordsErr)
	}

	ferrytest.Spelling(t, c.bools, ferrytest.Eq[bool],
		[]bool{true, false},
		[]string{"ON", "yes", "1", ""},
	)
}

// TestBoolWordsWriteTheFirstPair pins the canonical-out half, which no load can
// see: whichever word the environment spelled it with, a true is written on.
func TestBoolWordsWriteTheFirstPair(t *testing.T) {
	t.Parallel()

	c := defaults()
	BoolWords("on", "off", "true", "false").apply(&c)

	for _, tc := range []struct {
		v    bool
		want string
	}{{v: true, want: "on"}, {v: false, want: "off"}} {
		got, err := c.bools.Render(tc.v)
		if err != nil {
			t.Fatalf("render: %v", err)
		}

		if got != tc.want {
			t.Errorf("a %t is written %q, want %q", tc.v, got, tc.want)
		}
	}
}

// TestBoolWordsRefusalNamesTheTextAndTheWords is law 4 as it was scoped: a
// spelling's parse refusal says which text was refused and what this plane
// would have taken, because a message naming neither cannot be acted on.
func TestBoolWordsRefusalNamesTheTextAndTheWords(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		text string
		want string
	}{
		"a mistyped word":    {text: "onn", want: `"onn" is not one of this plane's boolean words (on, off)`},
		"a folded value":     {text: "on\noff", want: `"on\noff" is not one of this plane's boolean words (on, off)`},
		"an empty variable":  {text: "", want: `"" is not one of this plane's boolean words (on, off)`},
		"invalid UTF-8 text": {text: "\xff", want: `"\xff" is not one of this plane's boolean words (on, off)`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := refusalFor(t, tc.text); got != tc.want {
				t.Errorf("refused with %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBoolWordsRefusalIsBounded is the other half of the same law: the quoting
// is capped, so a variable holding a token or a blob cannot reach a log through
// a refusal.
func TestBoolWordsRefusalIsBounded(t *testing.T) {
	t.Parallel()

	for name, text := range map[string]string{
		"a long single-byte value": strings.Repeat("x", 300),
		"a long multi-byte value":  strings.Repeat("€", 30),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkBounded(t, refusalFor(t, text), text)
		})
	}
}

// checkBounded is the three things a bounded refusal must be, lifted out so
// that the table above stays a table.
func checkBounded(t *testing.T, got, text string) {
	t.Helper()

	if strings.Contains(got, text) {
		t.Errorf("refused with %q, which carries the whole value", got)
	}

	if !strings.Contains(got, "(truncated)") {
		t.Errorf("refused with %q, want it to say the value was cut", got)
	}

	if len(got) > quoteCap*boundedMessage {
		t.Errorf("refused with %d bytes, want a message the cap actually bounds", len(got))
	}
}

// boundedMessage is how many times [quoteCap] the whole refusal may run to: the
// sentence around the quoted text is fixed, so a multiple of the cap is what
// asserts that the value is the only part that varies.
const boundedMessage = 4

// refusalFor is one refusal's message, through the spelling the option builds.
//
// It goes to the spelling rather than through a load because this driver never
// surfaces this refusal: a text no word spells is a String here, so the message
// is the spelling's own statement and this is where it is assertable.
func refusalFor(t *testing.T, text string) string {
	t.Helper()

	c := defaults()
	BoolWords("on", "off").apply(&c)

	if c.wordsErr != nil {
		t.Fatalf("building the spelling: %v", c.wordsErr)
	}

	_, err := c.bools.Parse(text)
	if err == nil {
		t.Fatalf("Parse(%q) was accepted, want a refusal", text)
	}

	return err.Error()
}

// TestADuplicatedVariableResolvesTheWayGetenvDoes is #266: a list carrying one
// name twice loads what a process reading the same environment would see, which
// is the first entry.
func TestADuplicatedVariableResolvesTheWayGetenvDoes(t *testing.T) {
	t.Parallel()

	got, err := ferry.Load[toggles](t.Context(), New(Environ(func() []string {
		return []string{"LABEL=first", "LABEL=second"}
	})))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got.Label != "first" {
		t.Errorf("loaded %q, want the first entry, which is what getenv answers", got.Label)
	}
}
