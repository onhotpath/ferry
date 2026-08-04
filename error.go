package ferry

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// moment is when in a run a failure happened.
//
// It is a field on one error type rather than a family of types, because the
// aggregate, the location, the sort and the formatter are identical work at
// every moment, and a caller doing Load cannot avoid handling both halves of
// the split anyway: a schema failure surfaces through the same call as a
// missing key (ADR-0011).
//
// The order is load-bearing, because the moment is the first term of the sort
// key: an open failure precedes the walk errors it caused, and a close failure
// follows them rather than heading a report it had nothing to do with. There is
// no accessor, because nobody branches on it; what it is for is that ordering
// and the words ferry uses about a driver failure.
type moment uint8

const (
	momentRegister moment = iota // registering a codec, before any schema exists
	momentCompile                // schema compile, provable from the type alone
	momentBind                   // a driver being handed the address set
	momentOpen                   // opening the plane
	momentWalk                   // the walk over the schema
	momentCommit                 // committing a dump
	momentClose                  // closing the plane
	momentUnknown                // an element that did not come from ferry
)

// momentName is the moment set made mechanical, in moment order, and the
// assertion under it stops the package compiling if a moment is added without a
// name.
var momentName = [...]string{
	"register", "compile", "bind", "open", "walk", "commit", "close", "unknown",
}

var _ [len(momentName)]struct{} = [int(momentUnknown) + 1]struct{}{}

// String names the moment in the spelling the report uses. A moment past the
// end renders as itself rather than borrowing a neighbour's name.
func (m moment) String() string {
	if int(m) < len(momentName) {
		return momentName[m]
	}

	return "moment(" + strconv.Itoa(int(m)) + ")"
}

// The vocabulary is sentinels and nothing else: there is no Kind enum and no
// KindOf, because errors.Is is what the standard library already does for this
// job and it costs ErrDriver nothing to be a second axis (ADR-0011).
//
// The sentinel text is load-bearing, and that was found by rendering rather
// than by choosing. A driver declares its class by wrapping a sentinel, so the
// sentinel's own text lands inside the driver's message: "plane" read as a
// stray word in `...cannot contain a space: plane`, where "plane error" reads
// as a sentence.

// ErrSchema is a failure provable from the destination type plus the codec
// registry, with no plane in sight: a malformed tag, an unsupported type, a
// contradictory declaration. It is what [Compile] reports.
var ErrSchema = errors.New("schema error")

// ErrMissing is the plane being silent at an address the schema marks required.
// It is kept apart from [ErrValue] so that "these six keys are unset" and
// "these two hold garbage" are two lists rather than one.
var ErrMissing = errors.New("missing")

// ErrValue is the plane speaking and what it said not fitting the target type.
var ErrValue = errors.New("invalid value")

// ErrPlane is ferry being unable to talk to the plane, or a driver refusing the
// address set it was bound to.
var ErrPlane = errors.New("plane error")

// ErrDriver is provenance rather than a class, and it crosses the other four:
// it says the cause came from below the boundary. Core supplies it, so a driver
// cannot forge it.
//
// It is the closest thing to a retry signal ferry offers. Whether a particular
// backend failure is worth retrying is the driver's knowledge, and its own
// sentinel stays reachable underneath; what ferry can say is that retrying an
// [ErrValue] is always pointless and retrying a driver's read is sometimes not.
var ErrDriver = errors.New("driver")

// ErrReadOnly is a plane that is writable in principle but not right now: a KV
// with no write ACL, a file sink over an unwritable directory.
//
// A sink raises it when it opens for writing, so a Dump refused this way has
// written nothing at all rather than half a struct. A driver wraps this and its
// own error, so errors.Is answers for both.
var ErrReadOnly = errors.New("plane is read only")

// classRule maps a sentinel a driver or a codec may wrap onto the class it
// thereby declares.
type classRule struct{ sentinel, class error }

// classRules is the whole of what a driver can say about the class, and core
// keeps its answer over the default for the moment. A driver can also be wrong
// about it and nothing checks that, which is a conformance case in the same
// family as ADR-0004's optional interfaces.
//
// The last two rows are the subordinate sentinels. ErrReadOnly is a kind of
// ErrPlane and ErrWrongKind is a kind of ErrValue, so each composes with a
// class rather than standing outside the vocabulary: an accessor's refusal
// reaching a caller through core answers to errors.Is(err, ErrValue) as well as
// to itself.
var classRules = [...]classRule{
	{ErrSchema, ErrSchema},
	{ErrMissing, ErrMissing},
	{ErrValue, ErrValue},
	{ErrPlane, ErrPlane},
	{ErrReadOnly, ErrPlane},
	{ErrWrongKind, ErrValue},
}

// declaredClass is the class an error already declares, or nil where it
// declares none and core's default for the moment stands.
func declaredClass(err error) error {
	for _, r := range classRules {
		if errors.Is(err, r.sentinel) {
			return r.class
		}
	}

	return nil
}

// Error is one ferry failure: where it happened, when in the run, which class
// it belongs to, and what caused it.
//
//	if fe, ok := errors.AsType[*ferry.Error](err); ok {
//	    log.Println(fe.Address(), errors.Is(fe, ferry.ErrValue))
//	}
//
// It has one accessor, [Error.Address], and no exported fields, so there is
// nothing to switch on: the class is matched with errors.Is against
// [ErrSchema], [ErrMissing], [ErrValue], [ErrPlane], [ErrDriver] or
// [ErrReadOnly].
//
// The address is the plane address, except at schema compile, where it is the
// Go field path - a field with no tag never named an address, and that is the
// whole error. An error with no location, a close failure among them, returns
// the zero [Path].
//
// The cause stays in the chain, so errors.Is against a driver's own sentinel or
// against strconv.ErrRange still answers.
//
// Message text is not API. Match on the sentinels and on the address. ferry's
// own text never repeats a value the plane supplied, because ferry cannot know
// which addresses hold secrets; what it names instead is structure, such as the
// observed kind, the target type, or an array's length.
type Error struct {
	// loc is the location, and the zero Path is "no location": an address has
	// at least one segment, so no flag is needed to tell them apart.
	loc   Path
	mom   moment
	class error // one of the four, or nil where ferry claims none
	// driver records that the cause came from below. It is what ErrDriver
	// matches, and core supplies it so a driver cannot forge it.
	driver bool
	// msg is ferry's own text, and never the plane's.
	msg string
	// cause is reachable and, except for a driver's own error, never printed.
	cause error
}

// Error() is on the pointer receiver, which is the whole of survey item 5.14's
// fourth entry: declaring it on the value where a pointer is returned makes
// both forms satisfy error, and the natural value-form errors.As is then a
// silent false rather than a compile error.
var _ error = (*Error)(nil)

// Error renders the failure on one line, prefixed with "ferry: ". The text is
// not API; match on the sentinels and on [Error.Address].
func (e *Error) Error() string { return errPrefix + e.line() }

// Unwrap returns the cause, so a driver's own error and a decode failure's
// strconv sentinel both stay matchable through ferry's wrapper.
func (e *Error) Unwrap() error { return e.cause }

// Address is where the failure happened: the plane address, or the Go field
// path for a schema compile failure. An error with no location returns the zero
// [Path].
func (e *Error) Address() Path { return e.loc }

// Is matches the class sentinel and the provenance marker, which is what makes
// errors.Is the whole of the matching mechanism.
func (e *Error) Is(target error) bool {
	if e.class != nil && target == e.class {
		return true
	}

	return e.driver && target == ErrDriver
}

// Format renders %v as the one line, %+v as the full report, %s as the one line
// and %q as it quoted.
func (e *Error) Format(f fmt.State, verb rune) { writeVerb(f, verb, e) }

// line is the error without the "ferry: " prefix, which is the form an
// aggregate's report prints: the header already said "ferry", so the per-line
// prefix is suppressed rather than repeated once per element.
func (e *Error) line() string {
	var b strings.Builder

	if e.loc != (Path{}) {
		b.WriteString(e.loc.String())
		b.WriteString(": ")
	}

	b.WriteString(e.msg)

	// A driver's own text is printed, and the obligation not to put plane
	// values in it is the driver's - the same shape as every other driver
	// obligation in ADR-0004. A decode cause is not printed, because ferry
	// chose to call strconv and strconv's habit of quoting its input is
	// therefore ferry's problem.
	if e.driver && e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}

	return b.String()
}

func (e *Error) oneLine() string { return e.Error() }

// report is %+v for a single failure: the line, and under it the structure
// ferry knows. It names no value the plane supplied either.
func (e *Error) report() string {
	var b strings.Builder

	b.WriteString(e.Error())
	b.WriteString(reportIndent)
	b.WriteString(e.mom.String())

	if e.class != nil {
		b.WriteString(listSep)
		b.WriteString(e.class.Error())
	}

	if e.driver {
		b.WriteString(listSep)
		b.WriteString(ErrDriver.Error())
	}

	return b.String()
}

const (
	errPrefix    = "ferry: "
	listSep      = ", "
	reportIndent = "\n  "
)

// newError is core's own constructor, and the only party that mints a class,
// a moment or a location is core.
func newError(m moment, class error, loc Path, msg string) *Error {
	return &Error{loc: loc, mom: m, class: class, msg: msg}
}

// withCause attaches a cause that stays reachable, and adopts a class the cause
// already declares. That is where a driver or a codec holds its one opinion:
// core supplies the default class for the moment unless the error it was handed
// carries a ferry sentinel, in which case core keeps it.
func (e *Error) withCause(cause error) *Error {
	e.cause = cause

	if class := declaredClass(cause); class != nil {
		e.class = class
	}

	return e
}

// fromDriver wraps whatever a driver returned. Core supplies the address, the
// moment and the provenance marker, and a driver can change none of them.
//
// It is one ferry error per address the driver named with ErrorAt, because a
// driver refusing over a whole address set generally dislikes more than one
// member of it, and ADR-0011's aggregation rule reports every failure that is
// not a consequence of another one already reported (#211).
//
// Where core already knows the address, core's wins, so a driver cannot
// misattribute a read at one address to another.
func fromDriver(m moment, loc Path, err error) error {
	return join(driverErrors(nil, m, loc, err)...)
}

// driverErrors is one error per failure a driver reported: one for every
// carrier ErrorAt left in the tree, and one for the whole of anything that
// holds no carrier at all.
//
// The carrier is unwrapped away where it is found, because leaving it in the
// chain prints the address twice, once from ferry's location and once from the
// carrier's own text. Descending is what makes that hold past the first one:
// errors.AsType returns the first match in tree order, so reading one address
// off the tree and taking that carrier's inner error as the cause discards
// every other failure the driver joined beside it (#211).
//
// Anything holding no carrier stays whole, which is ADR-0011's flatness
// promise: ferry cannot attribute addresses to a third party's children, and a
// tree it can address is one the driver addressed itself with core's own
// constructor.
func driverErrors(out []error, m moment, loc Path, err error) []error {
	at, ok := errors.AsType[*atError](err)

	switch {
	case !ok:
		return append(out, driverError(m, loc, err))
	case identical(at, err):
		return append(out, driverError(m, coreFirst(loc, at.at), at.err))
	}

	nested := unwrapped(err)
	if len(nested) == 0 {
		return append(out, driverError(m, loc, err))
	}

	for _, inner := range nested {
		out = driverErrors(out, m, loc, inner)
	}

	return out
}

// driverError is one wrapped driver failure. withCause reads the class off this
// failure's own cause, so each member of a join a driver returned keeps its own
// opinion about the class rather than inheriting the first member's.
func driverError(m moment, loc Path, cause error) *Error {
	e := newError(m, ErrPlane, loc, driverMsg(m)).withCause(cause)
	e.driver = true

	return e
}

// coreFirst is core's address where core has one, and the driver's otherwise.
func coreFirst(core, named Path) Path {
	if core == (Path{}) {
		return named
	}

	return core
}

// unwrapped is what an error wraps: a join's elements, the one error a wrapper
// wraps, or nothing where it wraps nothing.
//
// It asks the value in hand rather than the chain behind it, which is why this
// is an assertion and not errors.As: the question is one step of the tree walk,
// and errors.As is the whole walk with the answer thrown away.
func unwrapped(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}

	if single, ok := err.(interface{ Unwrap() error }); ok {
		return []error{single.Unwrap()}
	}

	return nil
}

// fromBind is what a failed Bind becomes, and it is the one place a driver
// hands core an error core itself wrote.
//
// A driver's own refusal is wrapped as one. A refusal [NewKeys] produced is
// not: it reached the driver only because the driver called core's key helper,
// it already carries core's moment, core's class and one address per element,
// and wrapping it would attribute core's own report to the driver and collapse
// a sorted set into a single element that [Elements] cannot range.
func fromBind(err error) error {
	if mine(err) {
		return err
	}

	return fromDriver(momentBind, Path{}, err)
}

// nilPlane is what a plane the caller never supplied becomes, and it is beside
// fromBind rather than inside it because nothing was ever bound: no driver was
// asked, so there is no cause from below and no provenance to mark, and routing
// it through fromBind would attribute core's own refusal to a driver that does
// not exist.
//
// The class is ErrPlane, because a plane that is not there is the limiting case
// of one that cannot answer, and inventing a sentinel for it would split the one
// question a caller asks. The moment is bind, which is where the run stops. The
// location is the zero Path, because a nil plane holds no address: this is an
// element with no location, and it sorts within its moment the way a close
// failure does.
func nilPlane(msg string) *Error {
	return newError(momentBind, ErrPlane, Path{}, msg)
}

// The two halves are two sentences on purpose. A caller who passed a nil Source
// and one who passed a nil Sink made different mistakes, and a shared line would
// make them read the call site to find out which. The remedy is the same for
// both, because there are only two ways a plane comes to be nil at all.
const (
	nilSourceMsg = "the source is nil, so there is nothing to load from: assign one, or check the error of the " +
		"constructor that was meant to return it"
	nilSinkMsg = "the sink is nil, so there is nothing to dump to: assign one, or check the error of the " +
		"constructor that was meant to return it"
)

// mine reports whether err is core's own error and not a driver's.
//
// It is about the outermost error only, which is why it compares identity
// rather than taking the first match in the chain: a driver that wrapped core's
// refusal added context of its own, and that context is the driver's.
//
// The aggregate is asked about first, because errors.AsType returns the first
// match in tree order and an aggregate of core's own errors holds one.
func mine(err error) bool {
	if l, ok := errors.AsType[*errorList](err); ok {
		return identical(l, err)
	}

	e, ok := errors.AsType[*Error](err)

	return ok && identical(e, err)
}

// identical reports whether the match found by unwrapping is the value that was
// unwrapped.
//
// It takes any rather than error deliberately, and that is the whole of it: the
// question is about the dynamic value in hand and not about the chain behind it,
// which is the question errors.Is exists to answer differently. Comparing two
// any values cannot panic here either, because two different dynamic types are
// unequal before either value is examined.
func identical(found, whole any) bool { return found == whole }

// driverMsg is what ferry says about a driver failure before the driver's own
// text. It is the moment in words, which is ferry's own text and so always
// safe, and it is what stops a location-less driver error rendering as the bare
// word "driver".
//
// It is also why no direction is carried: what a driver failure wants is a
// verb, and the call site supplies one without the error storing it.
func driverMsg(m moment) string {
	switch m {
	case momentBind:
		return "the driver refused the address set"
	case momentOpen:
		return "opening the plane"
	case momentCommit:
		return "committing"
	case momentClose:
		return "closing the plane"
	default:
		return "the driver failed"
	}
}

// ErrorAt attaches an address to an error a driver is returning, for the case
// core cannot supply one: a driver refusing over a whole address set knows
// which members it disliked, and core does not.
//
//	return ferry.ErrorAt(addr, fmt.Errorf("%w: %s", ferry.ErrPlane, why))
//
// A driver that disliked several may join several of these, and core reports
// one failure per address, each keeping its own cause and its own class. What
// a driver returns without an address on it stays whole and is reported as one
// failure with no address.
//
// It attaches and never classifies. On its own the result is not an [Error] and
// matches no class; core reads the address off it and wraps it. A nil err
// returns nil.
func ErrorAt(addr Path, err error) error {
	if err == nil {
		return nil
	}

	return &atError{at: addr, err: err}
}

// atError is ErrorAt's carrier. It is unexported because a caller matches on
// the sentinels and reads the address off ferry's own error, never off this.
type atError struct {
	at  Path
	err error
}

func (a *atError) Error() string { return a.at.String() + ": " + a.err.Error() }

func (a *atError) Unwrap() error { return a.err }

// summaryMax is how many addresses the one-line form names before it elides.
// Three is the rendering ADR-0011 measured: at forty errors the line is still
// one line, and it states the count it did not name.
//
// The elision is a presentation cap and not a data one. %+v and Elements both
// still hold everything, so ADR-0001's ban on dropping anything silently holds.
const summaryMax = 3

// errorList is the aggregate. It is flat and sorted at construction, and it is
// unexported because Elements is the whole of what a caller needs from it and
// two exported error types whose names differ by one letter is a trap.
type errorList struct{ errs []error }

var _ error = (*errorList)(nil)

// Error is the one-line form: the count, then the addresses, eliding past
// summaryMax with the rest stated as a number.
func (l *errorList) Error() string { return l.oneLine() }

// Unwrap is what makes errors.Is keep errors.Join's meaning on an aggregate:
// it answers "at least one element is of this class". Counting is the caller's
// range over Elements.
func (l *errorList) Unwrap() []error { return l.errs }

// Format renders %v as the one line and %+v as the report.
func (l *errorList) Format(f fmt.State, verb rune) { writeVerb(f, verb, l) }

func (l *errorList) oneLine() string {
	var b strings.Builder

	b.WriteString(errPrefix)
	b.WriteString(strconv.Itoa(len(l.errs)))
	b.WriteString(" errors: ")

	named := min(len(l.errs), summaryMax)
	for i, err := range l.errs[:named] {
		if i > 0 {
			b.WriteString(listSep)
		}

		b.WriteString(label(err))
	}

	if rest := len(l.errs) - named; rest > 0 {
		b.WriteString(", and ")
		b.WriteString(strconv.Itoa(rest))
		b.WriteString(" more")
	}

	return b.String()
}

func (l *errorList) report() string {
	var b strings.Builder

	b.WriteString(errPrefix)
	b.WriteString(strconv.Itoa(len(l.errs)))
	b.WriteString(" errors:")

	for _, err := range l.errs {
		b.WriteString(reportIndent)
		writeElement(&b, err)
	}

	return b.String()
}

// label is how an element names itself in the one-line summary. The address is
// what an operator acts on, so it is what the summary names; an element with no
// address names its moment instead.
func label(err error) string {
	e, ok := errors.AsType[*Error](err)

	switch {
	case !ok:
		return "(unknown)"
	case e.loc != (Path{}):
		return e.loc.String()
	default:
		return "(" + e.mom.String() + ")"
	}
}

// writeElement writes one line of a report, with a ferry element's own "ferry: "
// prefix suppressed because the header already said it.
func writeElement(b *strings.Builder, err error) {
	if e, ok := errors.AsType[*Error](err); ok {
		b.WriteString(e.line())

		return
	}

	b.WriteString(err.Error())
}

// rendered is the pair of forms every ferry error has, so that the leaf and the
// aggregate cannot drift in how they answer a verb.
type rendered interface {
	oneLine() string
	report() string
}

func writeVerb(f fmt.State, verb rune, r rendered) {
	switch verb {
	case 'v':
		if f.Flag('+') {
			_, _ = io.WriteString(f, r.report())

			return
		}

		fallthrough
	case 's':
		_, _ = io.WriteString(f, r.oneLine())
	case 'q':
		_, _ = io.WriteString(f, strconv.Quote(r.oneLine()))
	default:
		_, _ = fmt.Fprintf(f, "%%!%c(%T=%s)", verb, r, r.oneLine())
	}
}

// join is ferry's one aggregate constructor, and ferry never calls errors.Join:
// a Join result is invisible to Elements, is ordered by insertion, and renders
// as the newline dump this model replaces.
//
// It is where sorting happens, and sorting at construction rather than in
// Format is what makes the fix cover the programmatic reader. errors.AsType
// returns the first match in tree order, so an aggregate ordered only at print
// time hands two identical runs different elements while printing identically.
//
// One failure returns the leaf bare, as errors.Join does, and no aggregate ever
// holds a nil element, which the errors package documents as invalid.
func join(errs ...error) error {
	// out is left nil until the first failure, so a successful walk aggregates
	// nothing and allocates nothing here. On a walk that does fail the append
	// grows it, which costs a little more than one exact make and is not a path
	// that is hot.
	var out []error
	for _, err := range errs {
		out = appendElement(out, err)
	}

	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	default:
		slices.SortStableFunc(out, compareErrors)

		return &errorList{errs: out}
	}
}

// appendElement adds one error to an aggregate under construction.
//
// ferry never nests ferry aggregates, so the pairwise tree is unrepresentable
// rather than merely avoided, and the address already encodes the tree anyway.
// A driver's own joined error is left alone and enters as one element with its
// internal shape intact: ferry cannot attribute addresses to a third party's
// children, and rewriting somebody else's error tree is not ferry's business.
func appendElement(out []error, err error) []error {
	if err == nil {
		return out
	}

	if l, ok := errors.AsType[*errorList](err); ok {
		return append(out, l.errs...)
	}

	return append(out, err)
}

// sortKey is the three-part key an element sorts on.
type sortKey struct {
	mom moment
	loc Path
	msg string
}

// compareErrors orders on moment, then location, then message.
//
// The moment is first because of close: a failed dump can hold field errors and
// a close failure, and a close failure has no location and explains nothing, so
// "location-less sorts first" alone would put it at the head of a report it had
// nothing to do with. Within a moment the location-less element does sort
// first, which falls out of Path.Compare ordering a prefix before what extends
// it, and that is what puts an open failure above the errors it caused.
//
// The message tiebreak is not decoration: one field can produce two errors at
// one address, so the address is not a key, and insertion order is not an
// ordering that survives a concurrent walk.
func compareErrors(a, b error) int {
	ka, kb := sortKeyOf(a), sortKeyOf(b)

	if c := cmp.Compare(ka.mom, kb.mom); c != 0 {
		return c
	}

	if c := ka.loc.Compare(kb.loc); c != 0 {
		return c
	}

	return strings.Compare(ka.msg, kb.msg)
}

// sortKeyOf reads the key off an element. An element ferry did not build sorts
// last by moment and on its own text, which keeps the order total whatever a
// driver hands back.
func sortKeyOf(err error) sortKey {
	if e, ok := errors.AsType[*Error](err); ok {
		return sortKey{mom: e.mom, loc: e.loc, msg: e.line()}
	}

	return sortKey{mom: momentUnknown, msg: err.Error()}
}

// Elements splits a ferry failure into the individual failures it reports.
//
//	for _, e := range ferry.Elements(err) {
//	    if errors.Is(e, ferry.ErrMissing) { ... }
//	}
//
// A failed call reports every failure that is not a consequence of another one
// it is already reporting, so a struct with six unset required fields is six
// elements rather than the first. They are sorted, and the order is the same on
// every run.
//
// It returns a one-element slice for a single failure, so the loop above reads
// the same whether one field failed or forty, and nil for a nil error. The
// slice is the caller's to keep.
func Elements(err error) []error {
	if err == nil {
		return nil
	}

	if l, ok := errors.AsType[*errorList](err); ok {
		return slices.Clone(l.errs)
	}

	return []error{err}
}
