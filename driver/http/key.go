package ferryhttp

import (
	"errors"
	"fmt"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrIllegalName reports an address this driver cannot name in a request at all.
//
// A query parameter name is any byte sequence, so only the two degenerate
// addresses reach it there: one with an empty part, and the empty address
// itself. A header field name is a token, so a name holding a byte no field name
// may hold reaches it as well. A tagged field is refused before anything is
// read, and a map key that mints such a name is refused as it is minted.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrIllegalName = errors.New("http: this cannot be named in a request")

// ErrRepeated reports a name occurring more than once, read into a field that
// takes a single value.
//
// A name occurring more than once is a sequence: ?tags=a&tags=b is two elements,
// not one value that happens to have arrived twice. Reading it into a string
// field would have to discard one of them, so it is refused instead, and the
// refusal names the field. Change the field to a slice, or reject the request.
//
// It arrives while the field is being read, carries that field's address, and
// says how many times the name occurred. A request with two such names reports
// both, one failure per name.
//
// It wraps [ferry.ErrPlane] and stays reachable under ferry's wrapper.
var ErrRepeated = errors.New("http: this name occurs more than once")

// ErrTwoSpellings reports one sequence position spelled two ways in one request.
//
// A sequence reads either from a repeated name or from index-suffixed names, and
// ?tags=a&tags=b&tags.0=z uses both for position 0, so one of the two values
// would be lost. Only an overlap is refused: ?tags=a&tags=b&tags.2=z extends the
// sequence rather than contradicting it, and loads as three elements.
//
// A request claiming several positions twice is one refusal and not several, and
// it names the lowest of them, so the same request always reads the same way.
//
// It wraps [ferry.ErrPlane] and stays reachable under ferry's wrapper.
var ErrTwoSpellings = errors.New("http: this name carries a sequence in two spellings at once")

// flatKey renders one address as a name, joining each part's own text with sep
// and changing none of it.
//
// It is the [ferry.KeyFunc] of the query plane and the first half of the header
// plane's. A query parameter name survives percent-encoding whatever bytes it
// holds, so this transform keeps every part exactly as the tag spelled it, and
// the only names it refuses are the ones no join can rescue.
func flatKey(sep string) ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) { return join(addr, sep) }
}

// join is the transform, without the closure that carries the separator.
func join(addr ferry.Path, sep string) (string, error) {
	var b strings.Builder

	// The rendered address carries one delimiter byte per part and escaping
	// only ever lengthens a part's text, so its length covers the joined name
	// plus a single-byte separator between every pair.
	b.Grow(len(addr.String()))

	first := true

	for seg := range addr.Segments() {
		if seg.Text() == "" {
			return "", illegalName("a part of it is empty, and no join gives an empty part a name")
		}

		if !first {
			b.WriteString(sep)
		}

		first = false

		b.WriteString(seg.Text())
	}

	if first {
		return "", illegalName("there is nothing here to name")
	}

	return b.String(), nil
}

// headerKey is [flatKey] canonicalised the way net/http canonicalises a field
// name, then held to the grammar a field name actually has.
//
// Canonicalising here rather than at every lookup is what makes the name this
// driver computes the name a request carries: net/http stores X-Request-Id under
// that spelling however the wire spelled it, so an address rendering to
// x-request-id has to become X-Request-Id before it can find anything.
//
// The check afterwards is not redundant with it. textproto hands back a name it
// cannot canonicalise unchanged, so a name holding a space or a byte no field
// name may hold arrives here intact, and net/http would refuse to send it.
func headerKey(sep string) ferry.KeyFunc {
	inner := flatKey(sep)

	return func(addr ferry.Path) (string, error) {
		name, err := inner(addr)
		if err != nil {
			return "", err
		}

		canon := textproto.CanonicalMIMEHeaderKey(name)
		if !fieldName(canon) {
			return "", illegalName("a header field name holds only letters, digits and !#$%&'*+-.^_`|~")
		}

		return canon, nil
	}
}

// fieldName reports whether a name is one an HTTP field name may be, which is a
// non-empty run of token bytes and is what net/http's own writer holds an
// outgoing request to.
func fieldName(name string) bool {
	if name == "" {
		return false
	}

	for i := range len(name) {
		if !tokenByte(name[i]) {
			return false
		}
	}

	return true
}

// tokenByte is the token grammar an HTTP field name is written in.
func tokenByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		return true
	default:
		return false
	}
}

// position reads a child's text as a sequence position, and reports false where
// the text is not one position's canonical spelling.
//
// A leading zero keeps a child a name rather than making it a position: an Index
// segment's text is canonical base 10, so "01" is not the rendering of any
// position and reading it as one would silently answer about a different one.
func position(text string) (uint, bool) {
	if text == "" || len(text) > 1 && text[0] == '0' {
		return 0, false
	}

	i, err := strconv.ParseUint(text, base10, 0)
	if err != nil {
		return 0, false
	}

	return uint(i), true
}

// base10 is the only base a position is ever spelled in, which is what makes the
// rendering of an address unique.
const base10 = 10

// illegalName states the class this driver has an opinion about and keeps
// [ErrIllegalName] reachable underneath it.
//
// It names no segment text. ADR-0011 makes "ferry's own message text never
// contains a value the plane supplied" a total rule, and this driver's whole
// plane arrives from a caller nobody vetted; core attaches the address itself,
// which is structure and is what a reader needs in order to act.
func illegalName(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegalName, msg)
}

// repeated states the refusal a name occurring more than once earns, and prints
// the cardinality rather than any of the values.
//
// A count is not text the plane supplied, so ADR-0011's rule permits it, and it
// is the one fact a handler answering 400 wants that the address does not
// already carry.
func repeated(n int) error {
	return fmt.Errorf("%w: %w: it occurs %d times, and this field takes one value",
		ferry.ErrPlane, ErrRepeated, n)
}

// atContainer states the refusal a name earns by holding a value at an address
// the destination takes a container at.
//
// It is the container-side mirror of [repeated] and it prints the same one fact:
// how many times the name occurs, which is structure rather than text the plane
// supplied. Core attaches the address.
func atContainer(n int) error {
	return fmt.Errorf("%w: the request carries this name %d times and the destination takes a container "+
		"there, whose members are the names under it: nothing could hold the value", ferry.ErrValue, n)
}

// twoSpellings states the refusal an overlap between the two sequence spellings
// earns, and names the position both of them spell. Where more than one position
// is claimed twice, [reader.fromNames] hands over the lowest, so the text does
// not move between runs of one request.
//
// The position is structure rather than text the plane supplied, so ADR-0011's
// rule permits it, and it is the one fact this refusal cannot get from the
// address core attaches: core has the container's address at Children and its
// own wins, so a ferry.ErrorAt at the element here would be discarded rather
// than kept.
func twoSpellings(i uint) error {
	return fmt.Errorf("%w: %w: position %d is spelled both by the repetition and by a name of its own, "+
		"so one of the two values would be lost", ferry.ErrPlane, ErrTwoSpellings, i)
}
