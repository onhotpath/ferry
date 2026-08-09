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

// Option configures a [Source]. The set is closed at three: [Separator],
// [BytesAs] and [RootName].
type Option interface {
	apply(*config)
}

// optionFunc is the one implementation, which is what makes every option a
// one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// config is a [Source]'s settled configuration, copied into every binding so
// that a Source reconfigured after it was bound cannot change a binding already
// handed out.
type config struct {
	sep string

	// root is the name [RootName] gave the root address, raw and unread, empty
	// until it is given. Each plane derives its own reading of it: the query
	// plane takes it as it stands, the header plane canonicalises it and holds
	// it to the token grammar, both by the plane's own key function (#338).
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

// RootName names the query parameter or header field a schema whose root is a
// single value is read from.
//
//	src := ferryhttp.NewQuerySource(ferryhttp.RootName("q"))
//	q, err := ferry.Load[string](ferryhttp.WithQuery(r.Context(), r.URL.Query()), src)
//
//	src := ferryhttp.NewHeaderSource(ferryhttp.RootName("x-request-id"))
//	id, err := ferry.Load[string](ferryhttp.WithHeaders(r.Context(), r.Header), src)
//
// Such a schema has one address, the root, and that address carries no part for
// this driver to name it by, so without this option it is refused before any
// request is looked at. It says nothing about any other schema: every address
// with a part of its own is named by that part as before.
//
// The name is read by the plane the source was built for, the same way that
// plane reads a name a tag spelled. A query source takes it as it stands and
// only refuses an empty one. A header source canonicalises it the way net/http
// canonicalises every field name, so RootName("x-request-id") reads the field a
// request carries as X-Request-Id, and holds it to what a field name may be - a
// non-empty run of letters, digits and !#$%&'*+-.^_`|~.
//
// The sharp edge is that a source cannot tell a name meant for the other plane
// from one meant for it. RootName("X-Request-Id") on a query source reads a
// query parameter spelled exactly that, and finds none in an ordinary request,
// so the root reads as absent rather than as a mistake.
func RootName(name string) Option {
	return optionFunc(func(c *config) { c.root = name })
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
