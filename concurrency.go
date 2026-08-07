package ferry

import (
	"context"
	"sync"
)

// This file is the whole of the concurrency model, and it is one file so that
// the three parties can be read in one place: the caller's bound ([MaxConcurrency]),
// the driver's tolerance ([Concurrent]), and the number that reaches the driver
// behind its own open ([ConcurrencyBudget]). Core writes `go` here and nowhere
// else (ADR-0019).

// Concurrent is implemented by a [Reader] whose open instance tolerates
// overlapping calls. It is the driver's half of the consent: without it a walk
// is serial whatever the caller asked for.
//
// MaxConcurrent reports the instance's own tolerance - a pool size, a rate
// limit, whatever number the plane behind it can really take. A value of zero
// or less means the instance imposes no bound of its own, so the caller's
// number stands alone. Core never overlaps more than the smaller of the two.
//
// It is discovered by assertion on the instance an [OpenFunc] returned, in the
// same idiom as [Releaser] and [Committer], because tolerating overlap is a
// property of the open instance rather than of the [Source] value.
//
// Declaring it is a promise about everything the instance reaches. Get, Probe
// and Children may all be called from several goroutines at once, and so may
// anything the instance closes over: a client, a cache, a key function, a
// caller-supplied callback. An instance that keeps mutable state per open
// guards it, or does not declare this.
//
// Nothing on the write side asserts it. [Dump] does not fan out: a sink that
// wants overlap has it at [Committer.Commit], where it batches already.
type Concurrent interface {
	MaxConcurrent() int
}

// serialBudget is the budget of a walk nobody bounded: one call into the plane
// at a time, which is what ferry does when no Option says otherwise (ADR-0019).
// It is 1 rather than 0 so that it reads the same way as every other budget and
// leaves 0 free to mean "no bound of my own" on [Concurrent].
const serialBudget = 1

// budgetKey is the context key the open's budget rides under. It is an
// unexported empty struct, so nothing outside this package can write the value
// [ConcurrencyBudget] reads (ADR-0019).
type budgetKey struct{}

// ConcurrencyBudget reports how many calls into the plane the caller allowed to
// overlap, for a driver that wants to spend it behind its own open.
//
//	func (o opener) open(ctx context.Context) (ferry.Reader, error) {
//	    return o.fetch(ctx, ferry.ConcurrencyBudget(ctx))
//	}
//
// It is the same number the caller gave [MaxConcurrency], and it is one budget
// rather than two: whatever a driver spends here, core spends no more of it
// walking. A driver that splits one batch into several requests sizes the split
// with this, and stays inside what the caller granted.
//
// It returns 1 where the caller set no budget, which means one call at a time.
// It never returns less than 1, so it is always a legal count of goroutines.
//
// The context it reads is the one handed to the [OpenFunc] and the one the walk
// runs under, so a [Reader] may read it at open and again inside a Get.
func ConcurrencyBudget(ctx context.Context) int {
	n, ok := ctx.Value(budgetKey{}).(int)
	if !ok || n < serialBudget {
		return serialBudget
	}

	return n
}

// budgeted puts the caller's budget on the context the open and the walk both
// run under, which is ADR-0019's "one budget, both layers". The serial budget
// is left off entirely, because it is what an absent value already reads as and
// a value costs an allocation on the load path of every caller who asked for
// nothing.
func budgeted(ctx context.Context, budget int) context.Context {
	if budget <= serialBudget {
		return ctx
	}

	return context.WithValue(ctx, budgetKey{}, budget)
}

// schedulerFor is the gate: core fans out only where the caller supplied a
// budget and the open instance declared it tolerates overlap, and never past
// the smaller of the two numbers (ADR-0019).
//
// Absence of the capability is serial, which is why no existing driver changes
// behaviour when a caller sets the Option.
func schedulerFor(budget int, plane any) sched {
	n := tolerated(budget, plane)
	if n <= serialBudget {
		return serial
	}

	return newFanout(n).run
}

// tolerated is min(the caller's bound, the instance's own), with a
// non-[Concurrent] instance tolerating one and a non-positive declaration
// meaning the instance imposes no bound of its own (ADR-0019).
func tolerated(budget int, plane any) int {
	c, ok := plane.(Concurrent)
	if !ok {
		return serialBudget
	}

	if own := c.MaxConcurrent(); own > 0 {
		return min(budget, own)
	}

	return budget
}

// fanout is core's second scheduler: one semaphore for one whole walk, shared
// by every container the walk enters (ADR-0019).
//
// One semaphore per walk rather than one per container is what makes the budget
// a number about the walk: a semaphore minted at each container would let a
// three-level struct spend the budget cubed.
//
// The construction that makes that safe is the one rule this type has. A
// container runs its first member inline, on the goroutine that called it, and
// only tries to take a slot for the rest; a member that cannot get one runs
// inline too. So progress is guaranteed with zero slots free, and a parent
// holding a slot while it waits on its children cannot deadlock at any depth.
// It also keeps a small container free: a walk of one member writes no `go`.
type fanout struct {
	// slots holds one token per goroutine the walk may spend beyond the one it
	// is already running on, which is why the capacity is one less than the
	// bound: the caller's goroutine is inside the budget too.
	slots chan struct{}
}

// newFanout builds the semaphore one walk shares. n is the bound already
// clamped to what both parties allowed, and is at least 2.
func newFanout(n int) *fanout {
	return &fanout{slots: make(chan struct{}, n-1)}
}

// take tries to reserve one goroutine's worth of the walk's budget, and never
// waits: a caller that cannot have one runs the work itself.
func (f *fanout) take() bool {
	select {
	case f.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// give returns a slot to the walk.
func (f *fanout) give() { <-f.slots }

// run is the [sched] this fanout serves: every member, overlapped as far as the
// budget reaches, and combined in member order once every one of them has
// returned.
//
// The fold is outside the goroutines and after the wait, which is what makes a
// concurrent walk report byte-identically to a serial one and what makes
// [outcome.took] adopting a member's minted map sound: the subtree that built
// it has finished with it (ADR-0019).
func (f *fanout) run(n int, run func(i int) (outcome, error)) (outcome, error) {
	if n < 2 {
		return serial(n, run)
	}

	t := &tasks{outs: make([]outcome, n), errs: make([]error, n)}

	t.spread(f, run)
	t.at(0, run)
	t.wg.Wait()

	return t.fold()
}

// tasks is one container's members mid-flight: what each of them returned, held
// at its own index so that nothing is written to by two goroutines and the
// combination can happen in member order afterwards.
type tasks struct {
	outs []outcome
	errs []error
	wg   sync.WaitGroup
}

// spread starts every member but the first, on a goroutine where the walk's
// budget had a slot for it and inline where it did not.
func (t *tasks) spread(f *fanout, run func(i int) (outcome, error)) {
	for i := 1; i < len(t.outs); i++ {
		if !f.take() {
			t.at(i, run)

			continue
		}

		t.wg.Add(1)

		go t.spawned(f, i, run)
	}
}

// spawned is one member on a goroutine of its own: the slot is returned and the
// wait is satisfied however the member left.
//
// The recover fence is not here and does not need to be. It sits inside the
// call into user code, on whichever goroutine makes it, so a codec that panics
// under fanout becomes an addressed error on this member's own return value and
// its siblings finish (ADR-0011, ADR-0019).
func (t *tasks) spawned(f *fanout, i int, run func(i int) (outcome, error)) {
	defer t.wg.Done()
	defer f.give()

	t.at(i, run)
}

// at runs one member and records what it said, at its own index.
func (t *tasks) at(i int, run func(i int) (outcome, error)) {
	t.outs[i], t.errs[i] = run(i)
}

// fold combines the members in member order and never in completion order,
// through the same [batch] the serial scheduler folds through, so there is one
// aggregation and it produces one flat report (ADR-0011, ADR-0019).
func (t *tasks) fold() (outcome, error) {
	var b batch

	for i := range t.outs {
		b.add(t.outs[i], t.errs[i])
	}

	return b.done()
}
