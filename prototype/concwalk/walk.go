// Package concwalk is the session-04 prototype: what a subtree hands upward
// across the scheduler seam, and why the shape decides whether a concurrent
// walk can ever be correct.
//
// It miniaturises core's walk to the parts #122 and #20 argue about:
//   - a tree of containers and leaves over a flat plane,
//   - optional containers that materialise a pointer iff their subtree wrote,
//   - a scheduler seam the walk hands its member batch to.
//
// Two walks are implemented over the same tree:
//   - sharedWalk mirrors shipped core: presence is a shared counter read as a
//     before/after delta around each optional subtree (walk.go:261, the ADR-0010
//     hazard, plus its two siblings dumpTo.minted and encodePhase.writes).
//   - outcomeWalk is the proposed shape: every subtree returns one outcome
//     value (wrote bit + minted addresses + staged writes) and the scheduler
//     combines outcomes exactly as it already combines errors.
//
// The tests prove: identical under the serial scheduler; under a concurrent
// scheduler the shared counter materialises a pointer because a SIBLING wrote
// (race-free and wrong - xload defect 5.2's shape), while the outcome walk
// stays correct with byte-identical error reports in deterministic order.
package concwalk

import (
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
)

// node is a schema node. optional means the destination field is a pointer
// that must materialise only if the subtree wrote (ADR-0006's presence rule).
type node struct {
	name     string
	leaf     bool
	optional bool
	children []*node
}

// dest mirrors the destination struct: a leaf value, or a container whose
// ptr bit records whether the optional pointer was materialised.
type dest struct {
	val      string
	set      bool
	ptr      bool
	children map[string]*dest
}

func newDest() *dest { return &dest{children: map[string]*dest{}} }

// outcome is what a subtree hands upward in the proposed shape. One value,
// three facts - the generalisation of #122's "return the bool": presence
// composes by OR, minted and staged writes compose by append in task order.
// (outcome, error) is two results, inside function-result-limit 3.
type outcome struct {
	wrote  bool
	minted []string
	writes []string
}

func (o outcome) merge(p outcome) outcome {
	return outcome{
		wrote:  o.wrote || p.wrote,
		minted: append(o.minted, p.minted...),
		writes: append(o.writes, p.writes...),
	}
}

// ---- the shared-counter walk (shipped core's shape) ----

type sched func(tasks []func() error) error

var serial sched = func(tasks []func() error) error {
	var errs []error
	for _, t := range tasks {
		if err := t(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// goroutines runs every task at once. The counter is atomic, so the race
// detector is silent - which is the point: the failure is logical, not a race.
var goroutines sched = func(tasks []func() error) error {
	errs := make([]error, len(tasks))
	done := make(chan int)
	for i, t := range tasks {
		go func() { errs[i] = t(); done <- i }()
	}
	for range tasks {
		<-done
	}
	return errors.Join(sortedNonNil(errs)...)
}

type sharedWalk struct {
	plane map[string]string
	wrote *int64
	sch   sched
	// pause, when set, runs between the before-read and the descend of the
	// named optional subtree - the deterministic stand-in for "a sibling's
	// write lands between the two reads". gate runs at task entry, letting a
	// test hold one sibling back until another has opened its window.
	pause func(path string)
	gate  func(path string)
}

func (w *sharedWalk) walk(path string, n *node, d *dest) error {
	if n.leaf {
		v, ok := w.plane[path]
		if !ok {
			return nil
		}
		if v == "!" {
			return fmt.Errorf("leaf %s: bad value", path)
		}
		d.val, d.set = v, true
		atomic.AddInt64(w.wrote, 1)
		return nil
	}
	tasks := make([]func() error, 0, len(n.children))
	for _, c := range n.children {
		cd := newDest() // minted serially before dispatch, like core's struct fields
		d.children[c.name] = cd
		cp := path + "/" + c.name
		tasks = append(tasks, func() error {
			if w.gate != nil {
				w.gate(cp)
			}
			if !c.optional {
				return w.walk(cp, c, cd)
			}
			before := atomic.LoadInt64(w.wrote)
			if w.pause != nil {
				w.pause(cp)
			}
			err := w.walk(cp, c, cd)
			after := atomic.LoadInt64(w.wrote)
			cd.ptr = after > before // the delta read: correct only when nothing else runs in between
			return err
		})
	}
	return w.sch(tasks)
}

// ---- the outcome walk (the proposed shape) ----

// oSched is the seam with the outcome crossing it: the scheduler combines
// outcomes exactly as it combines errors, in task order regardless of
// completion order - determinism is the scheduler's obligation, stated once.
type oSched func(tasks []func() (outcome, error)) (outcome, error)

var oSerial oSched = func(tasks []func() (outcome, error)) (outcome, error) {
	var out outcome
	var errs []error
	for _, t := range tasks {
		o, err := t()
		out = out.merge(o)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return out, errors.Join(errs...)
}

var oGoroutines oSched = func(tasks []func() (outcome, error)) (outcome, error) {
	outs := make([]outcome, len(tasks))
	errs := make([]error, len(tasks))
	done := make(chan int)
	for i, t := range tasks {
		go func() { outs[i], errs[i] = t(); done <- i }()
	}
	for range tasks {
		<-done
	}
	var out outcome
	for _, o := range outs { // task order, not completion order
		out = out.merge(o)
	}
	return out, errors.Join(sortedNonNil(errs)...)
}

type outcomeWalk struct {
	plane map[string]string
	sch   oSched
	pause func(path string)
}

func (w *outcomeWalk) walk(path string, n *node, d *dest) (outcome, error) {
	if n.leaf {
		v, ok := w.plane[path]
		if !ok {
			return outcome{}, nil
		}
		if v == "!" {
			return outcome{}, fmt.Errorf("leaf %s: bad value", path)
		}
		d.val, d.set = v, true
		return outcome{wrote: true, minted: []string{path}, writes: []string{path + "=" + v}}, nil
	}
	tasks := make([]func() (outcome, error), 0, len(n.children))
	for _, c := range n.children {
		cd := newDest() // minted serially before dispatch, like core's struct fields
		d.children[c.name] = cd
		cp := path + "/" + c.name
		tasks = append(tasks, func() (outcome, error) {
			if w.pause != nil && c.optional {
				w.pause(cp)
			}
			o, err := w.walk(cp, c, cd)
			if c.optional {
				cd.ptr = o.wrote // the subtree's OWN bit - no window exists
			}
			return o, err
		})
	}
	return w.sch(tasks)
}

// sortedNonNil drops nils and orders errors by message, the stand-in for
// core's sort-at-construction rule (ADR-0011): concurrent collection must not
// leak completion order into the report.
func sortedNonNil(errs []error) []error {
	var out []error
	for _, e := range errs {
		if e != nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Error() < out[j].Error() })
	return out
}
