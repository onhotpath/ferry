// Package ferryhttp loads configuration from an HTTP request into a Go struct:
// its query parameters, or its header fields.
//
//	b, err := ferry.Bind[Filter](ferryhttp.NewQuerySource()) // once, at start-up
//	...
//	f, err := b.Load(ferryhttp.WithQuery(r.Context(), r.URL.Query())) // per request
//
// A server binds once and loads through the binding on every request: the type
// a handler reads does not change between requests, and only the values do.
// [ferry.Load] takes the same source and is the shape for a one-off, such as a
// URL parsed in a script.
//
// # The request arrives in the context, not in the source
//
// A source is built once, at start-up, and is safe to share across every
// goroutine net/http runs a handler in. The values it reads belong to one
// request, so they travel in the context instead: [WithQuery] puts a request's
// query parameters there and [WithHeaders] puts its header fields there. A load
// whose context carries neither is refused before anything is read, because a
// handler that forgot the call would otherwise see every field reported missing.
//
// # Parameter names come from the tags
//
// Each part of a field's address contributes its own text, and nested fields are
// joined. A field tagged host inside one tagged db reads db.host as a query
// parameter and Db-Host as a header field, because the join is "." for one and
// "-" for the other. Header names are matched the way net/http spells them,
// which is case-insensitively, so x-request-id and X-Request-Id are one field.
// Widen either join with [Separator] when two fields want one name; that
// collision fails the load before anything is read and names both fields.
//
// # A repeated name is a sequence
//
// ?tags=a&tags=b fills a []string with two elements, and so do two
// X-Tags: header lines. One occurrence fills a one-element slice. The same
// values also read as tags.0=a&tags.1=b, and a request that uses both spellings
// for one position is refused rather than resolved.
//
// A name occurring more than once is a sequence and nothing else, so reading it
// into a plain string field is refused too. Nothing quietly takes the first
// value.
//
// # Set but empty is not the same as absent
//
// ?x= loads as the empty string, and x not being in the query at all is a
// different observation: a field tagged required is satisfied by ?token= and
// fails when token is absent.
//
// # A header field cannot hold every string
//
// A header value may not contain a control character other than a tab, and
// leading and trailing spaces and tabs do not survive the wire. Query parameters
// have no such limit: any byte sequence survives, in a name and in a value
// alike. Nothing else about a value's type survives either trip, because both
// planes hold text and neither carries type information of its own.
//
// # There is no way to write back
//
// This package loads only. Nothing in it implements [ferry.Sink], so
// [ferry.Dump] with it does not compile rather than failing at run time.
// Building an outbound request is a different job, and it belongs to a package
// written for it.
//
// # Values are attacker-supplied, and names are safe to print
//
// Everything this package reads came off the wire. A refusal from it names the
// parameter or field it is about and never quotes what that name held, so an
// error may be logged and returned without leaking a token in a query string or
// an Authorization header. A parameter name minted by a map is part of the
// address and does appear.
//
// The design records behind these decisions are in docs/adr/.
package ferryhttp
