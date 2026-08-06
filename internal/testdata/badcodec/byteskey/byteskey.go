// Package byteskey does not compile, and that is what it is for. See
// [github.com/onhotpath/ferry/internal/testdata/badcodec/halfpair] for why a
// deliberately broken package lives under testdata.
package byteskey

import (
	"github.com/onhotpath/ferry"
)

// blob is a type whose boundary form is bytes, which is a kind no address
// segment can be spelled with.
type blob []byte

// BytesKey declares a bytes registration usable as a map key, which is
// ADR-0017's key eligibility seen from the side that must not work: AsMapKey is
// a method on the return type of the two constructors whose kind may key a map,
// so a bytes-keyed map is a build error rather than a refusal at registration.
var BytesKey = ferry.BytesValue(
	func(b blob) ([]byte, error) { return b, nil },
	func(b []byte) (blob, error) { return blob(b), nil },
).AsMapKey()
