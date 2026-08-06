// Package swapped does not compile, and that is what it is for. See
// [github.com/onhotpath/ferry/internal/testdata/badcodec/halfpair] for why a
// deliberately broken package lives under testdata.
package swapped

import (
	"net/netip"

	"github.com/onhotpath/ferry"
)

// Swapped is both halves of a pair, in the wrong order, which inference reports
// against the first argument rather than accepting and running backwards.
var Swapped = ferry.StringValue(netip.ParseAddr, addrText)

// addrText is an encode half in the shape every constructor names.
func addrText(a netip.Addr) (string, error) { return a.String(), nil }
