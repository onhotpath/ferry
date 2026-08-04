package ferryhttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// The apparatus every test in this package runs against, and the reason it is
// apparatus rather than shipped code.
//
// This package ships no [ferry.Sink] and never will, which is the whole point of
// the plane - and it means that the round-trip half of ferrytest.RoundTrip never
// executes, because ferrytest.Driver guards five of its cases on a nil sink.
// Measured on proto/210-http: against a Sink: nil plane the suite caught none of
// four injected read-side defects and reported "the plane mints no sink" 54
// times instead. So the tests supply a stand-in sink over the same fake request,
// exactly as driver/env does, which exercises the key function against its own
// inverse as a composed pair. It lives in a _test.go file and is reachable from
// nowhere else: a sink in this package's exported surface would be the thing the
// plane exists not to have, and what spelling one should write is a question
// #210 left open on purpose.
//
// The stand-in takes its plane from the context in exactly the way the shipped
// source does, because ADR-0012 requires that of both halves and because
// conformance case 10 asks both halves for it.

// queryPlaneFor describes the query plane to the conformance suites, with both
// halves over one url.Values.
//
// Kinds is a declaration and not a wish, and the one kind missing from it is the
// whole of what this plane cannot do. A query parameter is text, so Bool and
// Number are carried as their spellings - ?limit=50 is the most ordinary query
// parameter there is, and a plane that refused it would be describing something
// other than a query string. ADR-0005 measured a flattening plane with no null
// at 11 of 11 core types, and every value it refused was a nil or empty
// composite, which the walk writes as Null at a container address. So there is
// no Null: ?x= is a zero-length string rather than a null (ADR-0004).
//
// There is no Except either, and that is measured rather than assumed. Every
// byte sequence survives url.Values.Encode and url.ParseQuery, as a name and as
// a value alike, which TestQueryCarriesEveryByteSequence asserts.
//
// There is no Golden and no Contents. A golden artefact pins a driver's own
// spelling of a value, and this driver has none: it never writes, so the only
// spelling a row could pin is the stand-in's below, which is a test's and not a
// compatibility promise (ADR-0013). What a golden row would catch - an encoder
// and a decoder wrong in the same direction - is caught here instead by
// wire_test.go, which pins what the shipped half reads out of a query string
// spelled by hand.
func queryPlaneFor(opts ...Option) ferrytest.Plane {
	return ferrytest.Plane{
		Name:  "query",
		Kinds: flatKinds(),
		Open: func() ferrytest.Instance {
			v := url.Values{}
			src := NewQuerySource(opts...)

			return ferrytest.Instance{
				Source: src,
				Sink:   standInSink{src: src},
				InContext: func(ctx context.Context) context.Context {
					return WithQuery(ctx, v)
				},
			}
		},
	}
}

// headerPlaneFor describes the header plane, and differs from the query plane in
// exactly one declaration.
//
// Except is not a technicality here. A header field value may not hold a control
// character other than a tab, and leading and trailing spaces and tabs are
// stripped on the way through, both of which are properties of HTTP rather than
// of this driver: TestHeaderRefusesWhatNetHTTPRefuses measures them against a
// real net/http round trip rather than asserting them from the specification.
// Two of the suite's own string cases are excepted by it, so declaring it buys a
// loud refusal the stand-in has to actually make rather than a case that stops
// running (ADR-0005).
func headerPlaneFor(opts ...Option) ferrytest.Plane {
	return ferrytest.Plane{
		Name:   "header",
		Kinds:  flatKinds(),
		Except: notAFieldValue,
		Open: func() ferrytest.Instance {
			h := http.Header{}
			src := NewHeaderSource(opts...)

			return ferrytest.Instance{
				Source: src,
				Sink:   standInSink{src: src, holds: fieldValue},
				InContext: func(ctx context.Context) context.Context {
					return WithHeaders(ctx, h)
				},
			}
		},
	}
}

// flatKinds is the declaration both planes make, and it is five of the six kinds
// there are. ADR-0005 names query as a member of the flattening-plane class by
// name, and #153 is the record of the ticket wording that said three.
func flatKinds() []ferry.VKind {
	return []ferry.VKind{
		ferry.KindAbsent, ferry.KindBool, ferry.KindNumber, ferry.KindString, ferry.KindBytes,
	}
}

// notAFieldValue is the header plane's one exception to the kinds above, and it
// is a property of HTTP rather than of the values the suite happens to carry.
func notAFieldValue(v ferry.Value) bool {
	text, err := textOf(v)

	return err == nil && fieldValue(text) != nil
}

// fieldValue reports whether a header field value survives the wire unchanged.
//
// Both clauses were measured against a real net/http client and server rather
// than read off RFC 9110: net/http refuses bytes 0-8, 10-31 and 127 outright,
// and a value with a leading or trailing space or tab arrives trimmed.
func fieldValue(text string) error {
	for i := range len(text) {
		if c := text[i]; c == del || c < space && c != tab {
			return fmt.Errorf("%w: a header field value holds no control character other than a tab",
				ferry.ErrPlane)
		}
	}

	if strings.Trim(text, " \t") != text {
		return fmt.Errorf("%w: a leading or trailing space or tab does not survive a header field value",
			ferry.ErrPlane)
	}

	return nil
}

const (
	tab   = '\t'
	space = ' '
	del   = 0x7f
)

// standInSink is the write half of the fake request, built on the driver's own
// key function so that what a round trip composes is this driver's fold against
// this driver's enumeration and not a test's idea of either.
type standInSink struct {
	src *Source

	// holds refuses a text this plane cannot spell, and is nil for a plane that
	// spells every one of them.
	holds func(text string) error
}

// Bind checks the same things the source's does, through the same helper, so a
// schema the plane refuses is refused in both directions.
func (s standInSink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, _, err := bindPlane(s.src.p, s.src.cfg.sep, addrs)
	if err != nil {
		return nil, err
	}

	p, holds := s.src.p, s.holds

	return func(ctx context.Context) (ferry.Writer, error) {
		vals, err := p.from(ctx)
		if err != nil {
			return nil, err
		}

		return &standInWriter{keys: keys.Open(), vals: vals, holds: holds, cleared: map[string]bool{}}, nil
	}, nil
}

// standInWriter is one open write side. It implements neither ferry.Committer
// nor ferry.Releaser, because a map stages nothing and holds nothing.
//
// It writes a sequence as a repeated name - tags=a&tags=b - which is the
// spelling the shipped reader has to invert, and it replaces rather than
// appends: the first write of one dump at a name drops whatever that name
// already held. Measured on proto/210-http, replace is the only one of the three
// candidates that round trips, because appending into a name the caller already
// set produces q=old&q=new, which this driver's own reader then refuses.
type standInWriter struct {
	keys  ferry.KeyFunc
	vals  values
	holds func(text string) error

	// cleared is the names this dump has already replaced, so a name is cleared
	// once per dump and not once per element of the sequence under it.
	cleared map[string]bool
}

// Set writes one address, or refuses a value this plane cannot hold.
//
// The refusal is per address and loud, which is what a plane with no null owes:
// both planes hold text, so a Null has no representation in either, and writing
// one as an empty string would make an empty composite and a composite of one
// empty element the same request.
func (w *standInWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	text, err := w.carried(v)
	if err != nil {
		return ferry.ErrorAt(addr, err)
	}

	// The element address is rendered even though the value lands under its
	// parent's name, so that the collapse of /tags#0 onto "tags" is checked for
	// injectivity against every other name this dump minted. Swallowing the
	// refusal here is what silently downgrades the spelling for one element and
	// keeps it for the rest (#210).
	if _, err := w.keys(addr); err != nil {
		return err
	}

	parent, i, isElem := splitIndex(addr)
	if !isElem {
		return w.put(addr, text)
	}

	return w.putAt(parent, i, text)
}

// put writes one value at a name of its own.
func (w *standInWriter) put(addr ferry.Path, text string) error {
	key, err := w.keys(addr)
	if err != nil {
		return err
	}

	w.replaceOnce(key)
	w.vals[key] = append(w.vals[key], text)

	return nil
}

// putAt writes one value at a position under a name, which is where a sequence
// lives on a plane whose second dimension is the repetition of that name.
func (w *standInWriter) putAt(parent ferry.Path, i uint, text string) error {
	key, err := w.keys(parent)
	if err != nil {
		return err
	}

	w.replaceOnce(key)

	for uint(len(w.vals[key])) <= i {
		w.vals[key] = append(w.vals[key], "")
	}

	w.vals[key][i] = text

	return nil
}

// replaceOnce is the replace half of the semantics above: the first write of
// this dump at a name drops whatever the request already held there.
func (w *standInWriter) replaceOnce(key string) {
	if w.cleared[key] {
		return
	}

	w.cleared[key] = true

	delete(w.vals, key)
}

// carried is the plane's declaration as a function: the text a parameter or a
// field would hold, or a refusal naming what cannot be spelled.
func (w *standInWriter) carried(v ferry.Value) (string, error) {
	text, err := textOf(v)
	if err != nil {
		return "", err
	}

	if w.holds == nil {
		return text, nil
	}

	return text, w.holds(text)
}

// textOf is the kind half of the declaration. Bool and Number are text here,
// which is what makes ?limit=50 an ordinary query parameter rather than a value
// this plane refuses; Absent and Null are the two that have no spelling.
func textOf(v ferry.Value) (string, error) {
	switch v.Kind() {
	case ferry.KindBool:
		b, err := v.AsBool()

		return strconv.FormatBool(b), err
	case ferry.KindNumber:
		return v.AsNumber()
	case ferry.KindString:
		return v.AsString()
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err
	default:
		return "", fmt.Errorf("%w: this plane holds text, and cannot carry a %s", ferry.ErrPlane, v.Kind())
	}
}
