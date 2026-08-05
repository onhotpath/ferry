package valueseam

// The seam, K5-final: interfaces as the contract, func types as
// documentation surface, SpellingFunc as the Handler/HandlerFunc-style
// adapter, With as the variadic composition (the open V4 lean,
// asserted here so the pick is informed).

// Spelling says how one plane spells one payload type. T is the
// payload, C is the carrier this plane hands the driver.
type Spelling[T, C any] interface {
	Parse(c C) (T, error)  // may refuse: unknown spelling, corrupt carrier
	Render(v T) (C, error) // may refuse: domain and size limits, pre-write
}

// Transform is an invertible payload step; both directions may refuse.
type Transform[T any] interface {
	Apply(v T) (T, error)
	Invert(v T) (T, error)
}

// ParseFunc and RenderFunc are documentation surface and type hints.
type ParseFunc[C, T any] func(c C) (T, error)
type RenderFunc[T, C any] func(v T) (C, error)

type funcSpelling[T, C any] struct {
	parse  ParseFunc[C, T]
	render RenderFunc[T, C]
}

func (f funcSpelling[T, C]) Parse(c C) (T, error)  { return f.parse(c) }
func (f funcSpelling[T, C]) Render(v T) (C, error) { return f.render(v) }

// SpellingFunc adapts the two func types into the interface.
func SpellingFunc[T, C any](p ParseFunc[C, T], r RenderFunc[T, C]) Spelling[T, C] {
	if p == nil || r == nil {
		panic("valueseam: SpellingFunc requires both halves")
	}
	return funcSpelling[T, C]{parse: p, render: r}
}

type composed[T, C any] struct {
	s  Spelling[T, C]
	ts []Transform[T]
}

// With stacks transforms under a spelling, variadic:
// render applies t1..tn in reverse order then spells;
// parse un-spells then inverts tn..t1 — a pipeline, read left to right
// as With(spelling, outermost … innermost) on the payload side.
func With[T, C any](s Spelling[T, C], ts ...Transform[T]) Spelling[T, C] {
	if len(ts) == 0 {
		return s
	}
	return composed[T, C]{s: s, ts: ts}
}

func (c composed[T, C]) Render(v T) (C, error) {
	var zero C
	w := v
	var err error
	for i := len(c.ts) - 1; i >= 0; i-- {
		if w, err = c.ts[i].Apply(w); err != nil {
			return zero, err
		}
	}
	return c.s.Render(w)
}

func (c composed[T, C]) Parse(carrier C) (T, error) {
	v, err := c.s.Parse(carrier)
	if err != nil {
		var zero T
		return zero, err
	}
	for _, t := range c.ts {
		if v, err = t.Invert(v); err != nil {
			var zero T
			return zero, err
		}
	}
	return v, nil
}
