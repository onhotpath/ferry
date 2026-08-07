package yaml

import "github.com/onhotpath/ferry"

// Numbers is this plane's number spelling, exported to the test binary alone so
// that the published proof can be run over it.
//
// It is not API. A spelling is a fact about this plane rather than something a
// caller declares here, so it is reachable through a load and a dump and
// nowhere else (ADR-0018).
var Numbers ferry.Spelling[string, string] = numbers

// DocumentName is this plane's own rendering of an address, exported to the test
// binary alone so that the one address it has no rendering for can be reached.
//
// A member with no name at all is refused before the address that would carry it
// is built, so no document produces one and no load or dump can enter that arm
// (ADR-0016, #159). It is not API for the reason [Numbers] is not: what this
// driver calls an address is reachable through a report and nowhere else.
func DocumentName(addr ferry.Path) (string, bool) { return nameInDocument(addr) }
