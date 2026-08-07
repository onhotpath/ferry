package ferry

import "fmt"

// Spelling is how one plane spells one payload: the pair of functions that
// turns what the plane carries into a payload and back.
//
// T is the payload, which is what a [Value] holds. C is the carrier, which is
// what the plane hands the driver: text for an environment or a query string,
// bytes for a store that keeps bytes, and whatever a binary plane carries.
// It is a type parameter so that a plane holding bytes never passes them
// through a string on the way past.
//
//	type onOff struct{}
//
//	func (onOff) Parse(text string) (bool, error) { ... }
//	func (onOff) Render(v bool) (string, error)   { ... }
//
// Parse refuses a carrier this plane has no reading for, and Render refuses a
// payload it has no writing for: a value past a size budget, or outside a
// charset. A Render refusal lands before anything is written, which is where a
// failure that could be known without touching the plane belongs.
//
// Five rules bind every implementation, and a plane that breaks one writes data
// it cannot read back:
//
//   - Parse of what Render produced returns the value it started from.
//   - What Render writes is always something Parse accepts, and Parse may accept
//     more than that. Wider in, canonical out.
//   - Render is deterministic: one value, one spelling.
//   - A refusal is an error and never a zero value, and never a guess.
//   - A spelling changes how a value is written, never what it means.
//
// Build one as a type with the two methods, or from a pair of functions with
// [SpellingFunc], and stack payload steps under it with [With]. Ferry ships the
// contract and no spelling of its own: which spellings a plane has is the
// driver's to declare, through the driver's own options.
type Spelling[T, C any] interface {
	// Parse turns what the plane carries into a payload.
	Parse(c C) (T, error)
	// Render turns a payload into what the plane carries.
	Render(v T) (C, error)
}

// Transform is a payload step that runs under a [Spelling] and undoes itself:
// compression, a size budget, a canonical form.
//
// Apply runs on the way out, before the payload is spelled, and Invert runs on
// the way in, after it is read. Both may refuse, and the outbound refusal is
// the one that is easy to forget: a payload too big for the plane's budget, or
// outside the form it requires, is refused before anything is written. Invert
// refuses data the plane gave back that this step cannot undo, a truncated
// compressed stream among them, rather than handing back something plausible.
//
// Invert of what Apply produced returns the payload it started from, for every
// payload Apply accepted.
type Transform[T any] interface {
	// Apply runs on the way out, before the payload is spelled.
	Apply(v T) (T, error)
	// Invert runs on the way in, after the payload is read back.
	Invert(v T) (T, error)
}

// ParseFunc is the reading half of a [Spelling] as a function: a carrier in, a
// payload out.
type ParseFunc[C, T any] func(c C) (T, error)

// RenderFunc is the writing half of a [Spelling] as a function: a payload in, a
// carrier out.
type RenderFunc[T, C any] func(v T) (C, error)

// SpellingFunc builds a [Spelling] from a pair of functions, for a driver that
// has two closures and no reason to declare a type for them.
//
//	sp := ferry.SpellingFunc(
//	    func(text string) (bool, error) { ... },
//	    func(v bool) (string, error) { ... },
//	)
//
// Both halves are required. A spelling built without one refuses at every call
// rather than at some later one, and says which half is missing.
//
// The two functions must be pure: the same input twice gives the same output,
// and nothing outside them is read or written. A closure over state something
// else can change is a plane whose spelling changes underneath a binding that
// was already handed out, and no shape of constructor can stop that - which is
// why a driver's own options take words and numbers rather than functions.
func SpellingFunc[T, C any](p ParseFunc[C, T], r RenderFunc[T, C]) Spelling[T, C] {
	if p == nil || r == nil {
		return halfless[T, C](missingHalf(p == nil, r == nil))
	}

	return spelling[T, C]{parse: p, render: r}
}

// With stacks payload steps under a spelling, outermost first.
//
//	sp := ferry.With(base64(), gzip(), maxSize(4<<10))
//
// On the way out the steps run right to left and the spelling runs last, so the
// line above caps the size, compresses, then spells. On the way in the spelling
// runs first and the steps run left to right, undoing exactly what was done.
//
// With no steps the spelling is returned unchanged. The result is a spelling
// like any other, so it composes again and satisfies the same rules, provided
// each step does.
func With[T, C any](s Spelling[T, C], ts ...Transform[T]) Spelling[T, C] {
	if s == nil {
		return halfless[T, C]("the spelling it stacks under")
	}

	if len(ts) == 0 {
		return s
	}

	return stacked(s, ts)
}

// spelling is the one implementation core ships, and it is a func pair in
// http.HandlerFunc's idiom: the interface is the contract, the func pair is the
// escape hatch, and everything core builds - the adapter, a composition, and
// the refusal a missing half yields - is one of these (ADR-0018).
//
// It is one type rather than three because a spelling has exactly two
// behaviours and no state worth naming, so three types would be three
// implementations of one thing whose only difference is which closures they
// hold.
type spelling[T, C any] struct {
	parse  ParseFunc[C, T]
	render RenderFunc[T, C]
}

func (s spelling[T, C]) Parse(c C) (T, error)  { return s.parse(c) }
func (s spelling[T, C]) Render(v T) (C, error) { return s.render(v) }

// stacked is what [With] returns, and it is a [spelling] over two closures so
// that stacking steps adds no name to the published surface (ADR-0018).
func stacked[T, C any](s Spelling[T, C], ts []Transform[T]) spelling[T, C] {
	return spelling[T, C]{
		parse:  func(c C) (T, error) { return parseThrough(s, ts, c) },
		render: func(v T) (C, error) { return renderThrough(s, ts, v) },
	}
}

// renderThrough runs the steps innermost first: the written order reads as a
// nesting, spell(t1(t2(v))), so the step written last is the one closest to the
// payload and runs first (ADR-0018).
func renderThrough[T, C any](s Spelling[T, C], ts []Transform[T], v T) (C, error) {
	var zero C

	w := v

	for i := len(ts) - 1; i >= 0; i-- {
		stepped, err := ts[i].Apply(w)
		if err != nil {
			return zero, err
		}

		w = stepped
	}

	return s.Render(w)
}

// parseThrough undoes [renderThrough] exactly: the spelling first, then the
// steps in written order, which is the reverse of the order Apply ran them in.
func parseThrough[T, C any](s Spelling[T, C], ts []Transform[T], carrier C) (T, error) {
	var zero T

	v, err := s.Parse(carrier)
	if err != nil {
		return zero, err
	}

	for _, t := range ts {
		inverted, err := t.Invert(v)
		if err != nil {
			return zero, err
		}

		v = inverted
	}

	return v, nil
}

// halfless is the spelling a missing half yields.
//
// It refuses at every call rather than panicking, which is the rule ADR-0017
// settled for the whole package: a refusal is an error everywhere, and a panic
// lives under a Must name. There is no Must name here because there is nothing
// to build eagerly - a spelling is not registered, so the first call is the
// first moment anything could have been checked anyway.
//
// The message names the half that is missing and nothing the plane supplied,
// which ADR-0011 makes a total rule for every message ferry authors.
func halfless[T, C any](missing string) spelling[T, C] {
	refuse := func() error {
		return fmt.Errorf("%w: this spelling was built without %s, so it can neither read nor write "+
			"this plane: give it both halves where it is constructed", ErrSchema, missing)
	}

	return spelling[T, C]{
		parse: func(C) (T, error) {
			var zero T

			return zero, refuse()
		},
		render: func(T) (C, error) {
			var zero C

			return zero, refuse()
		},
	}
}

// missingHalf names the halves [SpellingFunc] was not given, so that the
// message says which one to supply rather than that one is missing (ADR-0011).
func missingHalf(noParse, noRender bool) string {
	switch {
	case noParse && noRender:
		return "either half"
	case noParse:
		return "the half that reads the plane"
	default:
		return "the half that writes the plane"
	}
}
