// Package httpdecisions is a throwaway prototype for issue #210: the four
// questions driver/http still has open once #184 settled the key form and #193
// settled the sequence shape.
//
// It never merges.
//
// #193's seven candidate shapes are gone. `enumerated` won and #209 landed the
// ordering it needed, so the shape is a fixed background here and the only
// shape kept beside it is `indexed`, which several of the four questions are
// stated against. What varies instead are the four axes the questions name:
// whether a Sink is exported at all and what spelling it writes, what a name
// carrying both spellings means, whether a Source may carry per-schema
// configuration, and where the scalar refusal lands.
package httpdecisions

import (
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrIllegalName reports an address a plane in this package cannot name.
var ErrIllegalName = errors.New("http: address has no key on this plane")

// ErrRepeated reports a name the plane holds more than one value at, read at an
// address that can only take one.
var ErrRepeated = errors.New("http: the plane holds more than one value at this name")

// ErrTwoSpellings reports a name that carries a sequence in the plane's own
// repetition and in index-suffixed names at the same time.
var ErrTwoSpellings = errors.New("http: this name carries a sequence in two spellings at once")

func illegal(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegalName, msg)
}

func repeated(n int) error {
	return fmt.Errorf("%w: %w: it holds %d, and this address takes one", ferry.ErrPlane, ErrRepeated, n)
}

// Shape is where a sequence position lives in the plane's key space.
type Shape int

const (
	// Enumerated is #193's answer, and the one driver/http ships. Positions
	// live in the multimap's second dimension; Children at a name holding n
	// values answers n positions for any n including one; Get at a name holding
	// more than one answers Absent.
	Enumerated Shape = iota

	// Indexed is the losing shape, kept because questions 1 and 2 are stated
	// against it: a sequence is spelled tags.0=a&tags.1=b and a repeated name is
	// refused at Get.
	Indexed
)

func (s Shape) String() string {
	if s == Indexed {
		return "indexed"
	}

	return "enumerated"
}

// positionsBehindName reports whether this shape puts a sequence position in the
// multimap's second dimension rather than in the name.
func (s Shape) positionsBehindName() bool { return s == Enumerated }

// answer is what the driver says at a name it cannot tell a container address
// from a leaf address at, which is every name.
type answer int

const (
	answerScalar answer = iota
	answerAbsent
	answerRefuse
)

// atName is the whole of a shape's Get policy at a name holding n values.
//
// declared is question 3's axis: a name the Source was told carries a sequence
// answers Absent whatever its cardinality.
func (s Shape) atName(n int, declared bool) answer {
	if declared {
		return answerAbsent
	}

	if n == 1 {
		return answerScalar
	}

	if s == Indexed {
		return answerRefuse
	}

	return answerAbsent
}

// enumerates reports whether Children mints one position per value held at the
// prefix's own name.
func (s Shape) enumerates(n int) bool {
	if s == Indexed {
		return false
	}

	return n > 0
}

// Clash is question 2: what a name carrying both spellings means.
//
// ?tags=a&tags=b&tags.0=z addresses /tags#0 twice, once out of the second
// dimension and once out of a name of its own.
type Clash int

const (
	// ClashRefuse fails Children, which is what #193's prototype did.
	ClashRefuse Clash = iota

	// ClashIndexWins takes the value at the address's own key, which is what
	// falls out of doing nothing: Get renders /tags#0 to "tags.0", finds it, and
	// never reaches the second dimension.
	ClashIndexWins

	// ClashRepeatedWins takes the value out of the second dimension, so the
	// index-suffixed name is the one that loses.
	ClashRepeatedWins

	// ClashRepeatedWinsAudited is ClashRepeatedWins with the losing spelling
	// reported at Close, so nothing is lost silently.
	ClashRepeatedWinsAudited

	// ClashFirstSpellingWins is the option the question names and the plane
	// cannot express, which is the finding rather than a shape: url.Values and
	// http.Header are maps, and the wire order of two different names is not in
	// them.
	ClashFirstSpellingWins
)

func (c Clash) String() string {
	switch c {
	case ClashRefuse:
		return "refuse-in-children"
	case ClashIndexWins:
		return "index-spelling-wins"
	case ClashRepeatedWins:
		return "repeated-spelling-wins"
	case ClashRepeatedWinsAudited:
		return "repeated-wins-audited"
	case ClashFirstSpellingWins:
		return "first-spelling-wins"
	default:
		return "Clash(?)"
	}
}

// Clashes is every policy, in the order the report tables them.
func Clashes() []Clash {
	return []Clash{ClashRefuse, ClashIndexWins, ClashRepeatedWins, ClashRepeatedWinsAudited, ClashFirstSpellingWins}
}

// Refusal is question 4: where the scalar refusal lands.
type Refusal int

const (
	// RefuseAtCloseInText is what #193 built: a Releaser reports at Close, with
	// the address in the message text.
	RefuseAtCloseInText Refusal = iota

	// RefuseAtCloseWithErrorAt is the same moment with the address attached
	// through ferry.ErrorAt instead of spelled into the text.
	RefuseAtCloseWithErrorAt

	// RefuseAtCloseHybrid is close/ErrorAt with every other offending address
	// named in the text of that one error, because core keeps one address off a
	// joined Close error and discards the rest.
	RefuseAtCloseHybrid

	// RefuseAtGet is what #208 would have to allow: the refusal at the moment
	// and the address a reader wants, which conformance case 3 forbids.
	RefuseAtGet

	// RefuseNever is the silent first-wins every other Go library does, kept as
	// the baseline the other three are priced against.
	RefuseNever
)

func (r Refusal) String() string {
	switch r {
	case RefuseAtCloseInText:
		return "close/in-text"
	case RefuseAtCloseWithErrorAt:
		return "close/ErrorAt"
	case RefuseAtCloseHybrid:
		return "close/ErrorAt+text"
	case RefuseAtGet:
		return "get/refuse"
	case RefuseNever:
		return "never (first wins)"
	default:
		return "Refusal(?)"
	}
}

// Refusals is every option, in the order the report tables them.
func Refusals() []Refusal {
	return []Refusal{RefuseAtCloseInText, RefuseAtCloseWithErrorAt, RefuseAtCloseHybrid, RefuseAtGet, RefuseNever}
}

// SinkSpelling is question 1's sub-question: which spelling a sink emits for a
// []string.
type SinkSpelling int

const (
	// SinkRepeated writes tags=a&tags=b, collapsing /tags#0 and /tags#1 onto the
	// plane key "tags" and relying on the second dimension's order.
	SinkRepeated SinkSpelling = iota

	// SinkIndexed writes tags.0=a&tags.1=b, one plane key per element, which is
	// what ferry.NewKeys was shown and therefore what it checked.
	SinkIndexed
)

func (s SinkSpelling) String() string {
	if s == SinkIndexed {
		return "index-suffixed"
	}

	return "repeated"
}

// SetSemantics is the question a sink cannot avoid answering: what Writer.Set
// does at a key the plane already holds values at.
type SetSemantics int

const (
	// SetAsIn193 is what #193's writer did, and it is here because nobody
	// decided it: a plain name is overwritten with one value, and an element
	// address grows the key to its index and assigns. So the same writer
	// answers "replace" at a scalar and "positional" at a sequence, and a value
	// the plane already held at a higher index survives the dump.
	SetAsIn193 SetSemantics = iota

	// SetAppend is url.Values.Add: every write appends to what the key already
	// holds.
	SetAppend

	// SetReplace clears the key the first time this dump writes it, so what the
	// dump wrote is the whole of what the key holds.
	SetReplace
)

func (s SetSemantics) String() string {
	switch s {
	case SetAppend:
		return "append"
	case SetReplace:
		return "replace"
	default:
		return "as-in-193"
	}
}

// SetSemanticsAll is every option, in the order the report tables them.
func SetSemanticsAll() []SetSemantics { return []SetSemantics{SetAsIn193, SetAppend, SetReplace} }
