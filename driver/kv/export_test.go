package kv

import "github.com/onhotpath/ferry"

// RawSpelling is the spelling [Raw] declares, exported to the test binary alone
// so that the published proof can be run over it.
//
// It is not API. A spelling is a fact about this plane rather than something a
// caller builds here, so it is reachable through the Option, a load and a save,
// and nowhere else (ADR-0018).
var RawSpelling ferry.Spelling[[]byte, []byte] = rawBytes()
