// Package paramonheader does not compile, and that is what it is for. It is
// [github.com/onhotpath/ferry/driver/http/internal/testdata/crossedroot/fieldonquery]
// the other way round, and that package says why a deliberately broken one
// lives under testdata (#338).
package paramonheader

import ferryhttp "github.com/onhotpath/ferry/driver/http"

// Crossed hands the query plane's root option to the header constructor.
var Crossed = ferryhttp.NewHeaderSource(ferryhttp.RootParam("value"))
