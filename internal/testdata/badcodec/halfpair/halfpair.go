// Package halfpair does not compile, and that is what it is for.
//
// ADR-0007 makes "a codec is a pair" a rule, and ADR-0009 makes it
// unrepresentable-otherwise rather than documented: every constructor takes
// both halves at once, so half a pair is a build error and nothing on the
// registration path has to check for one at run time. A rule enforced by the
// compiler needs a fixture the compiler rejects in order to be asserted at all.
//
// It lives under testdata because the go command never matches a directory
// named testdata against ./... at any depth, so a package that cannot compile
// is never built, vetted or linted with the module while an explicit import
// path still resolves it; and the internal element above it means no importer
// outside ferry can reach it.
package halfpair

import (
	"net/netip"

	"github.com/onhotpath/ferry"
)

// Half is one half of a pair, which is not a codec.
var Half = ferry.StringValue(addrText)

// addrText is an encode half in the shape every constructor names: a T in, the
// payload and an error out.
func addrText(a netip.Addr) (string, error) { return a.String(), nil }
