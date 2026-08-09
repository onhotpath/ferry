// Package paramonheader does not compile, and that is what it is for. See
// [github.com/onhotpath/ferry/driver/http/internal/testdata/crossedroot/fieldonquery]
// for why a deliberately broken package lives under testdata.
package paramonheader

import ferryhttp "github.com/onhotpath/ferry/driver/http"

// Crossed hands the query plane's root option to the header constructor.
var Crossed = ferryhttp.NewHeaderSource(ferryhttp.RootParam("value"))
