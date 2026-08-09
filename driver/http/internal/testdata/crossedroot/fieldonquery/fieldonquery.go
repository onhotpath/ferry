// Package fieldonquery does not compile, and that is what it is for.
//
// A root option belongs to one plane. The header plane's, given to a query
// source, would name the root out of the other half of the request, so the
// constructor's own parameter type refuses it and there is no run-time
// behaviour to observe: what the rule produces is a build error, and a rule
// nothing asserts is a rule the next refactor drops (#338).
//
// It lives under testdata so that the go command never matches it against
// ./..., and the module builds, vets and lints clean around it.
package fieldonquery

import ferryhttp "github.com/onhotpath/ferry/driver/http"

// Crossed hands the header plane's root option to the query constructor.
var Crossed = ferryhttp.NewQuerySource(ferryhttp.RootField("Value"))
