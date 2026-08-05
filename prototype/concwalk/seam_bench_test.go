package concwalk

// The #176-section-2 question: the seam costs one heap closure per member
// plus a slice per container (94 allocs, 32% of a load). A third answer
// exists between "keep it" and "kill it": an INDEX seam - the scheduler
// receives a count and one body, so a container costs one closure however
// many members it has. Aggregation stays the scheduler's alone, so
// TestAggregationIsTheSchedulersAndNotTheWalks keeps its premise.

import (
	"errors"
	"testing"
)

// idxSched is the reshaped seam: n members, one body.
type idxSched func(n int, run func(i int) error) error

var idxSerial idxSched = func(n int, run func(i int) error) error {
	var errs []error
	for i := range n {
		if err := run(i); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var sink int

func members(i int) error { sink += i; return nil }

func BenchmarkClosureSeam(b *testing.B) {
	const width = 20
	b.ReportAllocs()
	for b.Loop() {
		tasks := make([]func() error, 0, width)
		for i := range width {
			tasks = append(tasks, func() error { return members(i) })
		}
		if err := serial(tasks); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIndexSeam(b *testing.B) {
	const width = 20
	b.ReportAllocs()
	for b.Loop() {
		if err := idxSerial(width, members); err != nil {
			b.Fatal(err)
		}
	}
}

// The index seam still owns aggregation: a first-error scheduler swaps in
// and the walk's behaviour changes without the walk changing - the property
// the shipped seam exists to prove.
func TestIndexSeamAggregationIsTheSchedulers(t *testing.T) {
	var firstError idxSched = func(n int, run func(i int) error) error {
		for i := range n {
			if err := run(i); err != nil {
				return err
			}
		}
		return nil
	}
	fail := func(i int) error {
		if i%2 == 1 {
			return errors.New("member failed")
		}
		return nil
	}
	all := idxSerial(4, fail)
	first := firstError(4, fail)
	if len(errors.Join(all).Error()) <= len(first.Error()) {
		t.Fatal("aggregating scheduler must report more than first-error one")
	}
}
