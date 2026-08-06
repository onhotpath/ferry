// Package mismatched does not compile, and that is what it is for. See
// [github.com/onhotpath/ferry/internal/testdata/badcodec/halfpair] for why a
// deliberately broken package lives under testdata.
package mismatched

import (
	"net/netip"

	"github.com/onhotpath/ferry"
)

// Mismatched is two halves over two different types, which is the case a
// registration API without a single type parameter would take and then fail on
// at run time.
var Mismatched = ferry.StringValue(addrText, netip.ParsePrefix)

// addrText is an encode half in the shape every constructor names.
func addrText(a netip.Addr) (string, error) { return a.String(), nil }
