package ferryhttp

import (
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrOption reports a driver option this source cannot be built with: a
// separator that is empty, one holding a byte a header field name may not, or a
// [BytesAs] with no spelling in it.
//
// [NewQuerySource] and [NewHeaderSource] take options and return no error, so
// this lands at the first moment the driver is asked for anything, which is
// before any request is looked at. It wraps [ferry.ErrPlane] and stays reachable
// under ferry's wrapper, so errors.Is answers for it on what [ferry.Load]
// returned.
var ErrOption = errors.New("http: unusable driver option")

// QueryOption configures a [NewQuerySource]. It is satisfied by every [Option]
// and by [RootParam], and by nothing outside this package.
type QueryOption interface {
	applyQuery(*config)
}

// HeaderOption configures a [NewHeaderSource]. It is satisfied by every [Option]
// and by [RootField], and by nothing outside this package.
type HeaderOption interface {
	applyHeader(*config)
}

// Option configures a [Source] on either plane, so it may be passed to
// [NewQuerySource] and to [NewHeaderSource] alike. The set is closed at two:
// [Separator] and [BytesAs].
//
// The options naming the root address are not here, because there is one per
// plane and they are not interchangeable: [RootParam] is a [QueryOption] and
// [RootField] a [HeaderOption], so a source handed the other plane's does not
// compile.
type Option interface {
	QueryOption
	HeaderOption
}

// optionFunc is the shared implementation. It carries both plane methods, which
// is exactly what makes a shared option assignable to either constructor's
// parameter.
type optionFunc func(*config)

func (f optionFunc) applyQuery(c *config)  { f(c) }
func (f optionFunc) applyHeader(c *config) { f(c) }

// queryOptionFunc carries the query method and only that one, so the compiler
// refuses it where a [HeaderOption] is wanted (#338).
type queryOptionFunc func(*config)

func (f queryOptionFunc) applyQuery(c *config) { f(c) }

// headerOptionFunc is its counterpart, refused on a query source for the same
// reason.
type headerOptionFunc func(*config)

func (f headerOptionFunc) applyHeader(c *config) { f(c) }

// config is a [Source]'s settled configuration, copied into every binding so
// that a Source reconfigured after it was bound cannot change a binding already
// handed out.
type config struct {
	sep string

	// root is the name this plane's own root option gave the root address, empty
	// until it does. Which option that is is a fact about the constructor that
	// was called, so it lives on the plane rather than here (#338).
	root string

	// bytes is the spelling [BytesAs] declared, and it is nil until one is
	// declared: this plane holds text and nothing else, so a value is a String
	// unless a spelling of this plane's own says the plane carries payloads
	// (ADR-0018).
	bytes ferry.Spelling[[]byte, string]

	// bytesErr is what declaring it refused with, held until Bind for the
	// reason [ErrOption] gives: an Option is applied inside a constructor that
	// returns no error, so the refusal waits for the first moment the driver is
	// asked for anything.
	bytesErr error
}

// QuerySeparator is the string nested fields are joined with in a query
// parameter name when no [Separator] is given.
//
// A query parameter name may hold any byte, so nothing forces this choice and it
// is the spelling most APIs already use for a nested field.
const QuerySeparator = "."

// HeaderSeparator is the string nested fields are joined with in a header field
// name when no [Separator] is given.
//
// It is what every multi-word field name in the IANA registry already uses:
// X-Forwarded-For and X-Forwarded-Proto are the registry's own spelling of a
// nested x-forwarded object.
const HeaderSeparator = "-"

// Separator sets the string nested fields are joined with.
//
//	src := ferryhttp.NewQuerySource(ferryhttp.Separator("..")) // db.host reads db..host
//
// It defaults to [QuerySeparator] for a query source and [HeaderSeparator] for a
// header source, and it is the way out when two fields want one name and neither
// can be renamed: at ".." the fields db.host and the nested db/host stay apart.
// No separator is safe for every schema, because a field name may contain the
// separator itself, so whatever is chosen, two fields the join would collapse
// are still refused before any request is looked at.
//
// For a header source it must be a non-empty run of the bytes a field name may
// hold: letters, digits and !#$%&'*+-.^_`|~. For a query source it must only be
// non-empty.
func Separator(sep string) Option {
	return optionFunc(func(c *config) { c.sep = sep })
}

// BytesAs says this plane carries byte payloads, and how they are spelled.
//
//	src := ferryhttp.NewHeaderSource(ferryhttp.BytesAs(
//	    ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))))
//
// Without it every value this plane holds is text, which a []byte field takes as
// the bytes of that text. With it every value is a payload spelled the way the
// spelling says, so a request carrying base64 fills a []byte field with what the
// base64 decoded to, and a value the spelling has no reading for fails the load
// at the parameter or field it is about.
//
// The sharp edge is that this is a fact about the whole plane and not about one
// field, because a request carries no type information for a driver to consult.
// Every value is read as a payload once this is declared, so a string, an int or
// a duration field over the same source is then a value the field cannot take:
// declare it for a source whose every value is a payload, and read the fields
// that are not through a source of their own.
//
// Build the spelling with [Base64], stack payload steps under it with
// [ferry.With], and write your own by implementing [ferry.Spelling] over a
// string carrier.
func BytesAs(s ferry.Spelling[[]byte, string]) Option {
	return optionFunc(func(c *config) {
		if s == nil {
			c.bytesErr = optionError("ferryhttp.BytesAs was given no spelling, so this plane has no reading " +
				"and no writing for a payload: pass one, or omit the option to read every value as text")

			return
		}

		c.bytes = s
	})
}

// RootParam names the query parameter a schema whose root is a single value is
// read from.
//
//	src := ferryhttp.NewQuerySource(ferryhttp.RootParam("q"))
//	q, err := ferry.Load[string](ferryhttp.WithQuery(r.Context(), r.URL.Query()), src)
//
// Such a schema has one address, the root, and that address carries no part for
// this driver to name it by, so without this option it is refused before any
// request is looked at. It says nothing about any other schema: every address
// with a part of its own is named by that part as before.
//
// The name is one query parameter and it may not be empty. It is a
// [QueryOption] and not an [Option], so [NewHeaderSource] does not take it:
// [RootField] is the header plane's, and mixing the two is a compile error
// rather than a field named for the wrong half of the request.
func RootParam(name string) QueryOption {
	return queryOptionFunc(func(c *config) { c.root = name })
}

// RootField names the header field a schema whose root is a single value is
// read from.
//
//	src := ferryhttp.NewHeaderSource(ferryhttp.RootField("X-Request-Id"))
//	id, err := ferry.Load[string](ferryhttp.WithHeaders(r.Context(), r.Header), src)
//
// It is [RootParam] on the header plane, and it is a [HeaderOption] the same way
// that one is a [QueryOption]: [NewQuerySource] does not take it.
//
// The name is held to what a header field name is - a non-empty run of letters,
// digits and !#$%&'*+-.^_`|~ - and it is canonicalised the way net/http
// canonicalises every field name, so RootField("x-request-id") reads the field a
// request carries as X-Request-Id.
func RootField(name string) HeaderOption {
	return headerOptionFunc(func(c *config) { c.root = name })
}

// notEmpty is the query plane's whole rule for a separator. Every byte survives
// percent-encoding, in a name as well as in a value, so a query parameter name
// is any byte sequence and the only unusable join is no join at all.
func notEmpty(sep string) error {
	if sep == "" {
		return optionError("the separator must not be empty")
	}

	return nil
}

// tokenSeparator is the header plane's rule: the joined name has to stay a field
// name, and a separator holding a byte no field name may hold makes every nested
// field illegal rather than one of them.
func tokenSeparator(sep string) error {
	if err := notEmpty(sep); err != nil {
		return err
	}

	if !fieldName(sep) {
		return optionError("the separator must be a run of the bytes a header field name may hold: " +
			"letters, digits and !#$%&'*+-.^_`|~")
	}

	return nil
}

// optionError states the class this driver has an opinion about and keeps
// [ErrOption] reachable underneath it.
func optionError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrOption, msg)
}
