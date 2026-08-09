package env

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/onhotpath/ferry"
)

// BoolWords says which words this environment spells a boolean with, so that
// ENABLED=on fills a bool field.
//
//	src := env.New(env.BoolWords("on", "off", "true", "false"))
//
// The first two words are the ones a true and a false are written as, and the
// rest are further pairs the same way round: a truthy word and then a falsy
// one. The line above accepts four words and writes on. Without this option a
// bool field takes true and false, which is what a leaf's own parser reads, and
// nothing else.
//
// A word matches exactly. ON is not on, because a variable's value is data and
// this driver folds none of it, and a plane whose operators write both spells
// both by naming both.
//
// The sharp edge is that this is a fact about the whole environment and not
// about one field, which is what makes it declarable at all. A variable holding
// one of these words arrives as a boolean wherever it is read, so a string
// field over FEATURE=on is then a value the field cannot take rather than the
// text "on": choose words your text values do not use.
//
// Bind refuses a list that is not whole pairs, a word that is empty, and a word
// given twice, because each of those is a spelling that cannot be read back the
// way it was written.
func BoolWords(truthy, falsy string, also ...string) Option {
	return optionFunc(func(c *config) {
		words := make([]string, 0, len(also)+2)
		words = append(words, truthy, falsy)
		words = append(words, also...)

		c.bools, c.wordsErr = boolSpelling(words)
	})
}

// boolSpelling builds this plane's boolean spelling out of the words it was
// given, and refuses a list it cannot build one from.
//
// The written form is the first pair and the accept set is every word, which is
// ADR-0018's law 2: wider in, canonical out, with the write form inside the
// accept set so that what this plane writes is something it reads.
//
// The closures are over a table built here and handed to nobody, which is the
// whole of what keeps them pure: the option takes words rather than functions,
// so there is no handle for a caller to keep and change later (ADR-0018).
func boolSpelling(words []string) (ferry.Spelling[bool, string], error) {
	table, err := boolTable(words)
	if err != nil {
		return nil, err
	}

	truthy, falsy := words[0], words[1]

	return ferry.SpellingFunc(
		func(text string) (bool, error) {
			b, ok := table[text]
			if !ok {
				return false, notAWord(text, words)
			}

			return b, nil
		},
		func(v bool) (string, error) {
			if v {
				return truthy, nil
			}

			return falsy, nil
		},
	), nil
}

// notAWord is what the spelling refuses text with, and it quotes the text.
//
// A spelling's parse refusal is the one message class ADR-0011's rule against
// naming a plane-supplied value does not cover, because the whole content of
// the mistake is which word was written: "onn is not one of these" is the
// message, and "some word is not one of these" is not one. The quoting is
// bounded and escaped by [quoted], so the exception cannot become a leak of a
// blob or of a multi-line value (ADR-0018 law 4, ADR-0011).
//
// It never reaches a caller through this driver. On a plane whose values are
// all text a variable holding no declared word is not a broken boolean, it is a
// string, and [config.observe] answers with one; the message is what a caller
// holding the spelling itself sees.
func notAWord(text string, words []string) error {
	return fmt.Errorf("%s is not one of this plane's boolean words (%s)", quoted(text), strings.Join(words, ", "))
}

// quoteCap is how much of a value a refusal may quote, in bytes.
//
// It is chosen against the two things it has to be between. The whole of what a
// refusal has to show is a word or a number somebody mistyped, and a generous
// one of those - a long boolean word, a float with a full mantissa and an
// exponent - is well inside 64 bytes. The values that must not appear are
// tokens, keys, connection strings and PEM blocks, and the shortest of those is
// already over it: an AWS access key ID is 20 bytes and its secret is 40, a
// JWT's header alone is longer, and a PEM line is 64 before its header.
// So a mistyped word is shown whole, and nothing a secret store holds is
// (ADR-0018 law 4, ADR-0011).
const quoteCap = 64

// quoted renders text for a refusal: escaped to one line, and cut to
// [quoteCap].
//
// strconv.Quote is what makes the bound hold in the second dimension: a value
// carrying newlines, control bytes or invalid UTF-8 becomes one printable line,
// so a message stays one line whatever the plane held (ADR-0018 law 4).
//
// It is duplicated in driver/yaml rather than shared. ADR-0018's rule of three
// is the trigger for a common home, and two callers is not three.
func quoted(text string) string {
	if len(text) <= quoteCap {
		return strconv.Quote(text)
	}

	cut := quoteCap
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return strconv.Quote(text[:cut]) + " (truncated)"
}

// boolTable is the accept set, and every refusal it can make.
func boolTable(words []string) (map[string]bool, error) {
	if len(words)%2 != 0 {
		return nil, optionError("env.BoolWords takes its words in pairs, a truthy one and then a falsy one, " +
			"and this list ends half way through a pair")
	}

	table := make(map[string]bool, len(words))

	for i, w := range words {
		if w == "" {
			return nil, optionError("env.BoolWords was given an empty word, and no variable can hold one: " +
				"an unset variable is absence and a set one is text")
		}

		if _, taken := table[w]; taken {
			return nil, optionError("env.BoolWords was given one word twice: a word means one of true and " +
				"false, and a word meaning both is a value that cannot be read back")
		}

		table[w] = i%2 == 0
	}

	return table, nil
}

// observe is what one variable's text is at the boundary.
//
// It is a String unless a declared word says it is a bool, which is how a plane
// with no type information of its own carries a kind at all: the words are the
// type information, and they are the operator's own rather than a guess this
// driver makes (ADR-0018).
func (c *config) observe(text string) ferry.Value {
	if c.bools == nil {
		return ferry.String(text)
	}

	b, err := c.bools.Parse(text)
	if err != nil {
		return ferry.String(text)
	}

	return ferry.Bool(b)
}
