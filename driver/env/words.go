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

// gate is where this plane's boolean words apply, which under the kind seam is
// exactly where the schema wants a bool (proto: #309).
//
// It is one interface here only because the #309 surface exploration compares
// four spellings of the same question against one exhibit. A shipping driver
// carries whichever one wins and no interface at all.
//
// Every implementation is built once, at Bind, and never written to afterwards,
// so one binding is still readable from many goroutines with no
// synchronisation.
type gate interface {
	holds(addr ferry.LeafAddr) bool
}

// planeWide is K1, which is what ships: the words decide for the whole plane and
// no address is consulted.
type planeWide struct{}

func (planeWide) holds(ferry.LeafAddr) bool { return true }

// setGate is what candidates A and B both build: a table of the declared leaves
// the schema wants a bool at, and the composites whose minted members it wants
// a bool at.
//
// The second half is there because a minted address is in no address set, so it
// cannot be a key in the table and the composite that will mint it has to answer
// for it. That is the prefix scan at the bottom of this file, and it is the cost
// of keeping the answer on the set rather than on the address.
type setGate struct {
	at    map[ferry.LeafAddr]bool
	under []ferry.Path
}

// candidateA is the two-method spelling: KindAt at a leaf, ElemKind at a
// composite, each asked in its own arm.
// It does not fit in one function under this repository's complexity limit,
// which is a measurement rather than a style note: two questions asked in two
// arms is one branch more than the limit allows, so candidate A costs a driver
// a helper that candidate B does not.
func candidateA(addrs *ferry.AddressSet) gate {
	g := &setGate{at: make(map[ferry.LeafAddr]bool, addrs.Len())}

	for m := range addrs.Seq() {
		g.admit(addrs, m)
	}

	return g
}

// admit records one address if the schema wants a bool there.
func (g *setGate) admit(addrs *ferry.AddressSet, m ferry.Member) {
	switch a := m.(type) {
	case ferry.LeafAddr:
		if k, ok := addrs.KindAt(a); ok && k == ferry.KindBool {
			g.at[a] = true
		}
	case ferry.CompositeAddr:
		if k, ok := addrs.ElemKind(a); ok && k == ferry.KindBool {
			g.under = append(g.under, a.Path())
		}
	default:
		// A section holds no value, so this plane's words say nothing there.
	}
}

// candidateB is the one-method spelling: one question over the sealed sum, and
// the type switch stays because what the driver stores still differs by kind.
func candidateB(addrs *ferry.AddressSet) gate {
	g := &setGate{at: make(map[ferry.LeafAddr]bool, addrs.Len())}

	for m := range addrs.Seq() {
		if k, ok := addrs.Kind(m); !ok || k != ferry.KindBool {
			continue
		}

		switch a := m.(type) {
		case ferry.LeafAddr:
			g.at[a] = true
		case ferry.CompositeAddr:
			g.under = append(g.under, a.Path())
		default:
			// Unreachable: a section answers false above.
		}
	}

	return g
}

// holds reports whether the schema wants a bool at this address.
func (g *setGate) holds(addr ferry.LeafAddr) bool {
	if g.at[addr] {
		return true
	}

	for _, composite := range g.under {
		if under(composite, addr.Path()) {
			return true
		}
	}

	return false
}

// addrGate is candidate C, and it is the whole driver-side implementation: the
// address a driver was handed already carries the schema's answer, so there is
// no table, no Bind pass, and no prefix scan for the addresses a value minted.
type addrGate struct{}

func (addrGate) holds(addr ferry.LeafAddr) bool { return addr.Kind() == ferry.KindBool }

// candidateC needs the address set for nothing at all, which is the measurement.
func candidateC(*ferry.AddressSet) gate { return addrGate{} }

// observe is what one variable's text is at the boundary.
//
// It is a String unless a declared word says it is a bool, which is how a plane
// with no type information of its own carries a kind at all: the words are the
// type information, and they are the operator's own rather than a guess this
// driver makes (ADR-0018).
//
// The gate is the prototyped half: the words are applied where the schema wants
// a bool and nowhere else, so a variable holding a declared word is still text
// at a string field (proto: #309, K2).
func (r *reader) observe(addr ferry.LeafAddr, text string) ferry.Value {
	if !r.gate.holds(addr) {
		return ferry.String(text)
	}

	return r.cfg.observe(text)
}

func (c config) observe(text string) ferry.Value {
	if c.bools == nil {
		return ferry.String(text)
	}

	b, err := c.bools.Parse(text)
	if err != nil {
		return ferry.String(text)
	}

	return ferry.Bool(b)
}
