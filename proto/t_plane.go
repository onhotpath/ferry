package main

// The apparatus #14 builds on, and every piece of it is something ADR-0004
// already names as a combinator over the two interfaces. Nothing here is a new
// contract for a driver to implement, which is the whole test: if template
// generation needs a new core interface, ADR-0001 bucketed it wrong.

import (
	"context"
	"fmt"
	"slices"
)

// --- Recorder: ADR-0004's named combinator, a Sink that captures ------------

type tRecorder struct {
	vals  map[Path]Value
	order []Path
	bound []Path // the address set Bind was handed
}

func newRecorder() *tRecorder { return &tRecorder{vals: map[Path]Value{}} }

func (r *tRecorder) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	r.bound = slices.Clone(a.All())
	return func(context.Context) (FWriter, error) { return r, nil }, nil
}

func (r *tRecorder) Set(_ context.Context, p Path, v Value) error {
	if _, dup := r.vals[p]; !dup {
		r.order = append(r.order, p)
	}
	r.vals[p] = v
	return nil
}

func (r *tRecorder) addrs() []Path { return sortedPaths(r.order) }

// --- an empty plane: every address Absent -----------------------------------
//
// This is ADR-0004's `Static` combinator holding nothing. It is also the plane
// a template generator conceptually starts from: "the user has configured
// nothing yet, what should the file say".

type tEmptySource struct{ seen *[]Path }

func (s tEmptySource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return tEmptyReader(s), nil }, nil
}

type tEmptyReader struct{ seen *[]Path }

func (r tEmptyReader) Get(_ context.Context, p Path) (Value, error) {
	if r.seen != nil {
		*r.seen = append(*r.seen, p)
	}
	return Absent, nil
}

// Children exists and returns nothing, which is the honest answer for an empty
// plane. Without it a map- or slice-typed field is a loud "this source cannot
// enumerate" rather than "there is nothing there", and the two are different
// facts.
func (r tEmptyReader) Children(context.Context, Path) ([]Path, error) { return nil, nil }

// --- a plane holding exactly what it is told ---------------------------------

type tFixedSource struct {
	vals map[Path]Value
	seen *[]Path
}

func (s tFixedSource) Bind(*AddressSet) (FOpenFunc, error) {
	return func(context.Context) (FReader, error) { return tFixedReader(s), nil }, nil
}

type tFixedReader struct {
	vals map[Path]Value
	seen *[]Path
}

func (r tFixedReader) Get(_ context.Context, p Path) (Value, error) {
	if r.seen != nil {
		*r.seen = append(*r.seen, p)
	}
	return r.vals[p], nil
}

func (r tFixedReader) Children(_ context.Context, p Path) ([]Path, error) {
	return children(r.vals, p), nil
}

// --- a sink that refuses everything, for T5 ---------------------------------

type tRefusingSink struct{ why string }

func (s tRefusingSink) Bind(*AddressSet) (FOpenWriterFunc, error) {
	return func(context.Context) (FWriter, error) { return nil, fmt.Errorf("%s", s.why) }, nil
}

func kv(p Path, v Value) string { return fmt.Sprintf("%s=%s", p, v.GoString()) }
