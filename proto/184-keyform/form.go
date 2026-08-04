// Package keyform is a throwaway prototype for issue #184: it builds both
// candidate key functions for an HTTP query-parameter and header driver, for
// real, and runs them through ferry.NewKeys, ferry.Load and ferry.Dump.
//
// It never merges.
package keyform

import (
	"errors"
	"fmt"
	"net/textproto"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrIllegalName reports an address a plane in this package cannot name.
var ErrIllegalName = errors.New("keyform: address has no key on this plane")

func illegal(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegalName, msg)
}

// Form names one of the two candidate key functions, plus the two controls the
// prototype needs in order to say why the refusals are there.
type Form int

const (
	// Bracket is ADR-0003's four-plane table and keys.go's published godoc:
	// the first segment bare, every later segment in brackets. It transforms
	// rather than rejects, which is the posture ADR-0003 states for a key
	// function and the one driver/env takes.
	Bracket Form = iota
	// Flat is ADR-0004's line: a flat join like env's, over a separator, in the
	// same posture.
	Flat
	// BracketStrict is Bracket with a refusal for a segment holding [ or ],
	// which is the row ADR-0003's acceptability table asserts. It is here to
	// measure what that refusal buys.
	BracketStrict
	// FlatStrict is Flat with a refusal for a segment holding the separator,
	// which is driver/kv's posture, for the same reason.
	FlatStrict
)

func (f Form) String() string {
	switch f {
	case Bracket:
		return "bracket"
	case Flat:
		return "flat"
	case BracketStrict:
		return "bracket!"
	case FlatStrict:
		return "flat!"
	default:
		return "Form(?)"
	}
}

// DefaultQuerySeparator is what the flat form joins query-parameter segments
// with. A dot is what Spring, and Go's own flag packages, spell a nested query
// parameter with.
const DefaultQuerySeparator = "."

// HeaderSeparator is what the flat form joins header-name segments with. It is
// the only idiomatic one: an HTTP field name is a token, and - is the byte
// every multi-word field name in the registry already uses.
const HeaderSeparator = "-"

// Query returns the ferry.KeyFunc this form uses on the query-parameter plane.
func (f Form) Query(sep string) ferry.KeyFunc {
	switch f {
	case Bracket:
		return bracketKey(false)
	case BracketStrict:
		return bracketKey(true)
	case Flat:
		return flatKey(sep, false)
	case FlatStrict:
		return flatKey(sep, true)
	default:
		return func(ferry.Path) (string, error) { return "", illegal("no such form") }
	}
}

// Header returns the ferry.KeyFunc this form uses on the header plane: the
// query key function, canonicalised the way net/http canonicalises a field
// name, then checked against the token grammar a field name actually has.
func (f Form) Header(sep string) ferry.KeyFunc {
	inner := f.Query(sep)

	return func(addr ferry.Path) (string, error) {
		key, err := inner(addr)
		if err != nil {
			return "", err
		}

		canon := textproto.CanonicalMIMEHeaderKey(key)
		if !validFieldName(canon) {
			return "", illegal("the rendered name is not an HTTP field token")
		}

		return canon, nil
	}
}

// HeaderDepth1 is the third possibility, and not a form: a header plane that
// refuses to nest at all, on the ground that an HTTP field name has no nesting
// and any spelling of one is invented.
func HeaderDepth1() ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) {
		n := 0
		for range addr.Segments() {
			n++
		}

		if n == 0 {
			return "", illegal("the empty address names nothing")
		}

		if n > 1 {
			return "", illegal("an HTTP field name does not nest, and this address is nested")
		}

		key, err := flatKey(HeaderSeparator, false)(addr)
		if err != nil {
			return "", err
		}

		canon := textproto.CanonicalMIMEHeaderKey(key)
		if !validFieldName(canon) {
			return "", illegal("the rendered name is not an HTTP field token")
		}

		return canon, nil
	}
}

// bracketKey is the bracket form: db[host], db[auth][user], tags[0].
//
// strict controls the one refusal that is in question. A segment holding [ or ]
// has no unambiguous bracket spelling, exactly as a segment holding / has no
// unambiguous kv spelling: /x/"y]["z and /x/"y][z" render alike.
func bracketKey(strict bool) ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) {
		var b strings.Builder

		first := true

		for seg := range addr.Segments() {
			text := seg.Text()
			if text == "" {
				return "", illegal("a segment is empty, and the bracket form gives it no name")
			}

			if strict && strings.ContainsAny(text, "[]") {
				return "", illegal("a segment holds [ or ], which the bracket form spells with")
			}

			if first {
				b.WriteString(text)
				first = false

				continue
			}

			b.WriteByte('[')
			b.WriteString(text)
			b.WriteByte(']')
		}

		if first {
			return "", illegal("the empty address names nothing")
		}

		return b.String(), nil
	}
}

// flatKey is the flat join: segments joined with sep, the segment's own
// spelling kept byte for byte. It folds no case, because a query parameter name
// is case-sensitive; the header key function canonicalises on top of this.
func flatKey(sep string, strict bool) ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) {
		var b strings.Builder

		first := true

		for seg := range addr.Segments() {
			text := seg.Text()
			if text == "" {
				return "", illegal("a segment is empty, and no join gives an empty segment a name")
			}

			if strict && sep != "" && strings.Contains(text, sep) {
				return "", illegal("a segment holds the separator this join spells with")
			}

			if !first {
				b.WriteString(sep)
			}

			first = false

			b.WriteString(text)
		}

		if first {
			return "", illegal("the empty address names nothing")
		}

		return b.String(), nil
	}
}

// validFieldName is RFC 9110's field-name token grammar, which is what
// net/http's own writer holds a request header to.
func validFieldName(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if !tchar(s[i]) {
			return false
		}
	}

	return true
}

func tchar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		return true
	default:
		return false
	}
}
