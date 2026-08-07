package env

import (
	"errors"
	"fmt"
	"strings"

	"github.com/onhotpath/ferry"
)

// ErrIllegalName reports an address this driver cannot name as an environment
// variable at all.
//
// No fold rescues these two: an empty tag name has no environment variable name
// however it is folded, and a name beginning with a digit is one no shell can
// set. A tagged field is refused at Bind, and a map key that mints one is
// refused as it is minted, in either case before the read it belongs to.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrIllegalName = errors.New("env: this cannot be named as an environment variable")

// driverName is what this driver calls itself in a refusal, so that a schema
// which is fine on one plane and impossible on this one reports as this plane's
// problem rather than as ferry's.
const driverName = "env"

// key renders one address as an environment variable name, and it is the
// [ferry.KeyFunc] this driver hands to [ferry.NewKeys].
//
// It transforms rather than validates. An environment variable name may not
// contain a hyphen, so a key function that only validates refuses feature-flags,
// which is an ordinary thing to write in a config struct; one that maps the
// hyphen to _ accepts it and is not thereby less safe, because the injectivity
// check over the whole address set is what catches a fold that collapses two
// addresses into one (ADR-0003).
//
// The alternative was measured and it is worse in both directions. os.Setenv
// takes a name containing a dot from Go, but sh exits 127 on one, so a
// non-transforming key function emits names no operator can set through a .env
// file, a Dockerfile ENV, a Kubernetes env: block or a systemd unit. Keeping the
// dot buys nothing on Load either, because the uppercase fold has already
// destroyed the segment's own spelling.
func (c config) key(addr ferry.Path) (string, error) {
	name, err := c.join(addr)
	if err != nil {
		return "", err
	}

	// Every segment contributes at least one byte, so an empty name is an
	// address with no segments: the empty path, which is not an address at all.
	if name == "" {
		return "", illegalName("there is nothing here to name")
	}

	if !nameStart(name[0]) {
		return "", illegalName("it folds to a name beginning with a digit, and no shell will set one")
	}

	return name, nil
}

// join is the transform and the join, without the checks on the shape of the
// whole name that [config.key] adds around it.
func (c config) join(addr ferry.Path) (string, error) {
	var b strings.Builder

	// One allocation rather than a run of doublings up from nothing. The
	// rendered address carries one delimiter byte per segment and escaping only
	// ever lengthens a segment's text, so its length covers the folded name plus
	// a single-byte separator between every pair of segments. A longer separator
	// can still outgrow the hint, which costs one more growth on a name that
	// would otherwise have paid several.
	b.Grow(len(addr.String()))

	first := true

	for seg := range addr.Segments() {
		if seg.Text() == "" {
			return "", illegalName("a part of it is empty, and no fold gives an empty part a name")
		}

		if !first {
			b.WriteString(c.sep)
		}

		first = false

		writeFolded(&b, seg.Text())
	}

	return b.String(), nil
}

// writeFolded appends one segment's text folded into the name space: upper case
// where the byte is a letter, kept where it is a digit, and _ everywhere else.
//
// It works a byte at a time rather than through strings.ToUpper because every
// byte outside A-Z, 0-9 and _ becomes _ anyway, so a multi-byte rune has no
// case to fold and folding it would only change how many underscores it becomes.
func writeFolded(b *strings.Builder, text string) {
	for i := range len(text) {
		b.WriteByte(foldByte(text[i]))
	}
}

// foldByte is the whole character transform, and the reason it is total: every
// byte has an image in the name space, so legality is about the shape of the
// name rather than about its bytes.
func foldByte(c byte) byte {
	switch {
	case c >= 'a' && c <= 'z':
		return c - lowerToUpper
	case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return c
	default:
		return '_'
	}
}

// lowerToUpper is the ASCII distance between the two cases.
const lowerToUpper = 'a' - 'A'

// nameByte reports whether a byte is one an environment variable name may hold,
// which is what the separator is held to and what [foldByte] always produces.
func nameByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// nameStart reports whether a byte may begin an environment variable name. A
// leading digit is the one shape the fold produces that no shell will accept.
func nameStart(c byte) bool { return nameByte(c) && (c < '0' || c > '9') }

// illegalName states the class this driver has an opinion about and keeps
// [ErrIllegalName] reachable underneath it.
//
// It names no segment text. ADR-0011 makes "ferry's own message text never
// contains a value the plane supplied" a total rule, and a dynamic segment is
// the caller's value; core attaches the address itself, which is structure and
// is what a reader needs in order to act.
func illegalName(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegalName, msg)
}
