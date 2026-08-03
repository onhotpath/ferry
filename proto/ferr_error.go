package main

// ADR-0011's error model, PORTED from proto/9-errors:proto/e_error.go.
//
// #41 D8: "there is no error model" - ADR-0011 was measured on proto/9-errors,
// which is on a branch line the tip does not descend from, so none of its code
// arrived here. This is a port and not a redesign: the file is byte-identical
// to its origin apart from this header and the `ferr_` filename, which the port
// needed because proto/16-entry-point took the `e_` prefix for its own eleven
// probes and the two sets of e_*.go are unrelated files with the same names.
//
// THIS FILE IS THE PORTABLE PART of the prototype: it is a
// pure module with no I/O, no probe code and no terminal code, so it is the
// thing that lifts into core if the ADR is accepted. Everything in e_probe*.go
// and e_tui.go is a shell over it and is throwaway.
//
// The question it answers: ADRs 0001 through 0009 defer "the error types every
// refusal here produces" to #9, roughly 23 times. Is ONE model enough for all
// of them, and what does it cost?
//
// The shape under test:
//
//   - one exported type name with NO exported fields, so errors.AsType works
//     and the struct stays evolvable
//   - a closed classification of four, spelled as sentinels matched by
//     errors.Is, plus ErrDriver as provenance
//   - four carried things: location, moment, class, cause
//   - a flat aggregate, sorted AT CONSTRUCTION on (moment, location, message)
//   - ferry's own message text never contains plane-supplied text

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// The moment. Carried as a field rather than as a type split, which is the
// whole "one model" bet. The ORDER is load-bearing: it is the first term of
// the sort key, so an Open failure precedes the walk errors it caused and a
// Close failure follows them.
// ---------------------------------------------------------------------------

type moment uint8

const (
	mRegister moment = iota
	mCompile
	mBind
	mOpen
	mWalk
	mCommit
	mClose
	mNone // anything that did not come from ferry; sorts last
)

var momentName = [...]string{
	"register", "compile", "bind", "open", "walk", "commit", "close", "unknown",
}

func (m moment) String() string {
	if int(m) < len(momentName) {
		return momentName[m]
	}
	return "moment(" + strconv.Itoa(int(m)) + ")"
}

// ---------------------------------------------------------------------------
// The vocabulary. Sentinels, not an enum: errors.Is is the whole matching
// mechanism, so ErrDriver costs nothing extra despite being a second axis.
//
// ErrReadOnly already exists (addrset.go) from ADR-0004; under this model it
// stops being an exception beside an enum and becomes a member of one family.
// ---------------------------------------------------------------------------

var (
	// The sentinel TEXT matters, and this was found by rendering rather than by
	// choosing: a driver declares its class by wrapping one of these, so the
	// text lands inside the driver's own message. "plane" read as a stray word
	// in `...cannot contain a space: plane`; "plane error" reads as a sentence.
	//
	// ErrSchema is provable from reflect.TypeFor[T]() plus the registry, with
	// no plane in sight. Defined as "what Validate[T]() can catch", which
	// reuses ADR-0008's line rather than drawing a new one.
	ErrSchema = errors.New("schema error")
	// ErrMissing is the plane being silent at an address `required` names.
	ErrMissing = errors.New("missing")
	// ErrValue is the plane speaking and what it said not fitting the type.
	ErrValue = errors.New("invalid value")
	// ErrPlane is ferry being unable to talk to the plane, or a driver
	// refusing the address set.
	ErrPlane = errors.New("plane error")
	// ErrDriver is PROVENANCE, not severity, and it crosses the four above.
	// ferry cannot know whether a Consul 503 is worth retrying - that is the
	// driver's knowledge, and ADR-0001's plane-agnosticism veto says core must
	// not have an opinion about it. What ferry does know for certain is that
	// the cause came from below.
	ErrDriver = errors.New("driver")
)

var classes = []error{ErrSchema, ErrMissing, ErrValue, ErrPlane}

// ---------------------------------------------------------------------------
// The type.
// ---------------------------------------------------------------------------

// Error is ferry's error. The NAME is exported so errors.AsType[*Error] works;
// no FIELD is, so the struct can grow without breaking anybody and a caller
// cannot build a switch over its internals.
//
// Error() is on the POINTER receiver, which is survey item 5.14: xload declares
// Error() on the value and returns the pointer, so both forms satisfy `error`
// and the natural `var e xload.ErrRequired; errors.As(err, &e)` is a silent
// false.
type Error struct {
	loc    Path
	hasLoc bool
	mom    moment
	class  error // one of the four, or nil (a cancellation has no ferry class)
	driver bool  // provenance: the cause came from driver code

	// msg is FERRY'S OWN text and never contains plane-supplied text. That is
	// the redaction rule, and it is total because ferry cannot know which
	// addresses hold secrets without knowing what the plane is for.
	msg string

	// cause stays in the chain even when it is never printed, so
	// errors.Is(err, strconv.ErrSyntax) still works while the input that
	// failed to parse never reaches a log.
	cause error
}

var _ error = (*Error)(nil)

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("ferry: ")
	e.body(&b)
	return b.String()
}

// body writes everything after the "ferry: " prefix. Split out because inside
// an aggregate's report the header already said "ferry", so the per-line prefix
// is suppressed rather than printed once per element.
func (e *Error) body(b *strings.Builder) {
	if e.hasLoc {
		// pathOrRoot rather than loc.String(): the root Path renders as the empty
		// string, and "ferry: : ..." is not a sentence. #41 D8's compiler half
		// brought schema errors into this type, and those DO sit at the root -
		// "the root type T is not a struct ferry walks" is one - so the two
		// spellings had to become one. "(root)" is what the compiler already
		// printed.
		b.WriteString(pathOrRoot(e.loc))
		b.WriteString(": ")
	}
	b.WriteString(e.msg)
	// A driver's error is the driver's own text, and printing it is what makes
	// "kv: read timeout" reach the operator. The obligation not to put plane
	// values in it is the driver's, and it is a conformance case - the same
	// shape as every other driver obligation in ADR-0004.
	//
	// A decode cause is NOT printed: ferry chose to call strconv, so
	// strconv.NumError's habit of quoting its input is ferry's problem.
	if e.driver && e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}
}

func (e *Error) Unwrap() error { return e.cause }

// Address is the ONE accessor. At schema compile it is the Go field path,
// because a field that never named a segment has no address; everywhere else
// it is the plane address.
func (e *Error) Address() Path { return e.loc }

// Is matches the class sentinel and the provenance marker. Neither is in the
// Unwrap chain, so this method is what makes errors.Is the whole mechanism.
func (e *Error) Is(target error) bool {
	if e.class != nil && target == e.class {
		return true
	}
	return e.driver && target == ErrDriver
}

func (e *Error) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('+') {
			io.WriteString(f, "ferry: ")
			var b strings.Builder
			e.body(&b)
			io.WriteString(f, b.String())
			// %+v expands what ferry knows and still prints no plane text.
			io.WriteString(f, "\n  ")
			io.WriteString(f, e.mom.String())
			if e.class != nil {
				io.WriteString(f, ", ")
				io.WriteString(f, e.class.Error())
			}
			if e.driver {
				io.WriteString(f, ", driver")
			}
			return
		}
		fallthrough
	case 's':
		io.WriteString(f, e.Error())
	case 'q':
		io.WriteString(f, strconv.Quote(e.Error()))
	}
}

// ---------------------------------------------------------------------------
// Construction. Core's own path.
// ---------------------------------------------------------------------------

func errAt(m moment, class error, p Path, format string, args ...any) *Error {
	return &Error{loc: p, hasLoc: true, mom: m, class: class, msg: fmt.Sprintf(format, args...)}
}

func errNoLoc(m moment, class error, format string, args ...any) *Error {
	return &Error{mom: m, class: class, msg: fmt.Sprintf(format, args...)}
}

// errCause attaches a cause that is REACHABLE but not printed. Used for leaf
// decode failures, where strconv's message carries the plane's own text.
func (e *Error) withCause(err error) *Error { e.cause = err; return e }

// fromDriver wraps whatever a driver returned. Core always supplies the
// address, the moment and the provenance marker, and a driver can change none
// of them. The CLASS is the one thing a driver may hold an opinion about: if
// its error already carries a ferry class sentinel, core keeps it.
func fromDriver(m moment, p Path, hasLoc bool, err error) *Error {
	class := error(ErrPlane)
	for _, c := range classes {
		if errors.Is(err, c) {
			class = c
			break
		}
	}
	// ErrorAt lets a driver name the address it disliked, which it must be able
	// to do for Bind's legality check (ADR-0004 leaves that with the driver).
	//
	// Once core has taken the address, the carrier is UNWRAPPED away: leaving it
	// in the chain prints the address twice, once from ferry's own location and
	// once from the carrier's Error(). Measured in E9 before this line existed.
	var at *atError
	if errors.As(err, &at) {
		if !hasLoc {
			p, hasLoc = at.at, true
		}
		err = at.err
	}
	return &Error{loc: p, hasLoc: hasLoc, mom: m, class: class, driver: true, msg: driverMsg(m), cause: err}
}

// driverMsg is what ferry says about a driver failure before the driver's own
// text. It is the MOMENT in words, which is ferry's own text and therefore
// always safe, and it is what stops a location-less driver error rendering as
// the bare word "driver".
//
// This is also why direction is not a carried field: at the walk the two
// directions want different verbs, and the call site knows which it is without
// the error having to store it.
func driverMsg(m moment) string {
	switch m {
	case mBind:
		return "the driver refused the address set"
	case mOpen:
		return "opening the plane"
	case mCommit:
		return "committing"
	case mClose:
		return "closing the plane"
	default:
		return "the driver failed"
	}
}

// ---------------------------------------------------------------------------
// ErrorAt: the ONE constructor ferry exports to implementors.
//
// It attaches an address and NEVER classifies, which is what stops it being a
// second constructor for the same thing (survey 5.14's first item, "two ways to
// set the loader"). Returns `error` and not *Error, which is what closes the
// typed-nil trap: there is no concrete return type to smuggle a nil through.
// ---------------------------------------------------------------------------

type atError struct {
	at  Path
	err error
}

func (a *atError) Error() string { return a.at.String() + ": " + a.err.Error() }
func (a *atError) Unwrap() error { return a.err }

func ErrorAt(addr Path, err error) error {
	if err == nil {
		return nil
	}
	return &atError{at: addr, err: err}
}

// ---------------------------------------------------------------------------
// The aggregate. Unexported: Elements is the only thing a caller needs from it,
// and exporting it would mean two exported error types whose names differ by
// one letter.
// ---------------------------------------------------------------------------

// summaryMax is how many addresses the ONE-LINE form names before it elides.
// The elision is a PRESENTATION cap and not a data one: the count is stated,
// and %+v and Elements both still have everything, so ADR-0001's ban on silent
// truncation holds.
const summaryMax = 3

type errorList struct{ errs []error }

var _ error = (*errorList)(nil)

func (l *errorList) Unwrap() []error { return l.errs }

func (l *errorList) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ferry: %d errors: ", len(l.errs))
	n := min(len(l.errs), summaryMax)
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(locLabel(l.errs[i]))
	}
	if rest := len(l.errs) - n; rest > 0 {
		fmt.Fprintf(&b, ", and %d more", rest)
	}
	return b.String()
}

func (l *errorList) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('+') {
			fmt.Fprintf(f, "ferry: %d errors:\n", len(l.errs))
			for _, e := range l.errs {
				io.WriteString(f, "  ")
				if fe, ok := e.(*Error); ok {
					// The header already said "ferry", so the per-line prefix
					// is suppressed rather than repeated once per element.
					var b strings.Builder
					fe.body(&b)
					io.WriteString(f, b.String())
				} else {
					io.WriteString(f, e.Error())
				}
				io.WriteString(f, "\n")
			}
			return
		}
		fallthrough
	case 's':
		io.WriteString(f, l.Error())
	case 'q':
		io.WriteString(f, strconv.Quote(l.Error()))
	}
}

func locLabel(err error) string {
	var e *Error
	if errors.As(err, &e) && e.hasLoc {
		// pathOrRoot, for the same reason body() uses it: a schema refusal can
		// sit at the root, and a summary reading `2 errors: , /v/IP` names
		// nothing at all for the first one.
		return pathOrRoot(e.loc)
	}
	if errors.As(err, &e) {
		return "(" + e.mom.String() + ")"
	}
	return "(unknown)"
}

// ---------------------------------------------------------------------------
// join is where the aggregate is built, and where SORTING happens.
//
// Sorting at CONSTRUCTION rather than in Format is not tidiness. errors.AsType
// returns the first match in TREE order, so an aggregate that is only ordered
// at print time hands two identical runs different elements while printing
// identically. P3 measures it.
// ---------------------------------------------------------------------------

func join(errs ...error) error {
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		if e == nil {
			continue // the errors package doc: an Unwrap() []error may not hold a nil
		}
		// ferry never nests ferry aggregates: 5.4's pairwise tree is
		// unrepresentable rather than merely avoided. A DRIVER's own tree is
		// left alone - ferry cannot attribute addresses to a third party's
		// children, and rewriting somebody else's error tree is not ferry's
		// business.
		if l, ok := e.(*errorList); ok {
			out = append(out, l.errs...)
			continue
		}
		out = append(out, e)
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0] // one failure returns the leaf bare, as errors.Join does
	}
	slices.SortStableFunc(out, compareErrs)
	return &errorList{errs: out}
}

// compareErrs is the three-part key: moment, then location, then message.
//
// The message tiebreak is not decoration. ADR-0006 measured one field producing
// two errors at one address, and #20 may make the walk concurrent, at which
// point insertion order is survey item 5.5 all over again.
func compareErrs(a, b error) int {
	am, ap, ahas, amsg := sortKey(a)
	bm, bp, bhas, bmsg := sortKey(b)
	if am != bm {
		return int(am) - int(bm)
	}
	// Within a moment, the location-less sort first: an Open failure explains
	// the address-level errors under it.
	if ahas != bhas {
		if !ahas {
			return -1
		}
		return 1
	}
	if ahas {
		if c := CompareSegmentwise(ap, bp); c != 0 {
			return c
		}
	}
	return strings.Compare(amsg, bmsg)
}

func sortKey(err error) (moment, Path, bool, string) {
	var e *Error
	if errors.As(err, &e) {
		return e.mom, e.loc, e.hasLoc, e.msg
	}
	return mNone, Path{}, false, err.Error()
}

// ---------------------------------------------------------------------------
// Elements: the reader's side of "read the error set", which ADR-0001 makes a
// feature rather than diagnostics.
//
// It returns a ONE-ELEMENT slice for a bare error rather than nil, so the
// caller's loop reads the same whether one field failed or forty.
// ---------------------------------------------------------------------------

func Elements(err error) []error {
	if err == nil {
		return nil
	}
	var l *errorList
	if errors.As(err, &l) {
		return slices.Clone(l.errs)
	}
	return []error{err}
}
