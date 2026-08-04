package ferryhttp

import (
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// ErrOption reports a driver option this source cannot be built with: a
// separator that is empty, or one holding a byte a header field name may not.
//
// [NewQuerySource] and [NewHeaderSource] take options and return no error, so
// this lands at the first moment the driver is asked for anything, which is
// before any request is looked at. It wraps [ferry.ErrPlane] and stays reachable
// under ferry's wrapper, so errors.Is answers for it on what [ferry.Load]
// returned.
var ErrOption = errors.New("http: unusable driver option")

// Option configures a [Source]. The set is closed at one: [Separator].
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
