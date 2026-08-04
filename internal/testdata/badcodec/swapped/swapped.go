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
var Swapped = ferry.StringCodec(netip.ParseAddr, netip.Addr.String)
