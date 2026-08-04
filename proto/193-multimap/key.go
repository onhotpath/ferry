package multimap

import (
	"net/textproto"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// QuerySeparator is what the flat join spells a nested query parameter with.
// The form is #184's, settled and merged, and not revisited here.
const QuerySeparator = "."

// HeaderSeparator is what the flat join spells a nested header field name with.
// An HTTP field name is a token, and - is the byte every multi-word field name
// in the registry already uses.
const HeaderSeparator = "-"

// base10 is the only base a position is spelled in, which is what makes the
// rendering of an address unique. driver/env's rule, copied so the prototype
// enumerates the way a shipped driver does.
const base10 = 10

// flatKey is the flat join: segments joined with sep, each segment's own text
// kept byte for byte.
func flatKey(sep string) ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) {
		var b strings.Builder

		first := true

		for seg := range addr.Segments() {
			text := seg.Text()
			if text == "" {
				return "", illegal("a segment is empty, and no join gives an empty segment a name")
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

// headerKey is the flat hyphen join canonicalised the way net/http canonicalises
// a field name, then held to the token grammar a field name actually has.
func headerKey() ferry.KeyFunc {
	inner := flatKey(HeaderSeparator)

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

// position reads a child's text as a sequence position, and reports false where
// the text is not one position's canonical spelling. driver/env's, copied.
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
