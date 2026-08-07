package yaml

import "github.com/onhotpath/ferry"

// Numbers is this plane's number spelling, exported to the test binary alone so
// that the published proof can be run over it.
//
// It is not API. A spelling is a fact about this plane rather than something a
// caller declares here, so it is reachable through a load and a dump and
// nowhere else (ADR-0018).
var Numbers ferry.Spelling[string, string] = numbers
