package kv

import "github.com/onhotpath/ferry"

// rawBytes is this plane's spelling of a stored value as the bytes it already
// is, and it is the identity in both directions (ADR-0018).
//
// The carrier here is []byte rather than text, which is the whole of what the
// spelling seam's second type parameter is for: a store hands this driver the
// bytes it holds, and a spelling over a string carrier would put a conversion
// on both sides of a plane that never needed one.
//
// It refuses nothing. Every byte sequence a store holds is a payload and every
// payload is storable, so law 4 has no case here and laws 1, 2 and 3 hold by
// construction: what Render writes is what Parse reads, one value has one
// spelling, and there is nothing wider to accept.
//
// The closures are over nothing at all, which is the purity law satisfied by
// having no state to consult (ADR-0018, law 6).
func rawBytes() ferry.Spelling[[]byte, []byte] {
	return ferry.SpellingFunc(
		func(stored []byte) ([]byte, error) { return stored, nil },
		func(payload []byte) ([]byte, error) { return payload, nil },
	)
}
