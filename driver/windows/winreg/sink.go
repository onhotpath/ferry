package winreg

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// Sink is the write half of a registry plane.
//
//	sink := winreg.NewSink(winreg.CurrentUser, `Software\Example`)
//	err := ferry.Dump(ctx, cfg, sink)
//
// It stages every write and performs them together at the end, so a save that
// fails leaves the registry untouched and one save reports every address it could
// not write rather than stopping at the first.
//
// Staging is not a transaction. The writes go through the registry one value at a
// time, in the order the walk produced them, so a machine that fails part way
// through the commit is left part way through it. The registry does have
// transactions, and Microsoft deprecated them; this driver does not use them.
type Sink struct {
	cfg config
}

var _ ferry.Sink = (*Sink)(nil)

// NewSink builds a sink over one subkey of one hive.
//
//	sink := winreg.NewSink(winreg.CurrentUser, `Software\Example`)
//	err := ferry.Dump(ctx, cfg, sink)
//
// Give it the same [Common] settings the source has. Nothing checks that the two
// agree, and a sink writing the 64-bit view with a source reading the 32-bit one
// is a round trip that loses everything.
//
// It touches nothing, and in particular it does not check that the key can be
// written. A sink over a hive this process may not write is legal to build, and
// the save refuses when it starts.
func NewSink(hive Hive, subkey string, opts ...SinkOption) *Sink {
	return &Sink{cfg: newConfig(hive, subkey, func(c *config) {
		for _, o := range opts {
			o.applySink(c)
		}
	})}
}

// Bind computes this schema's registry keys and checks them, exactly as
// [Source.Bind] does and for the same reasons.
//
// It does no I/O, so a sink binds successfully against a hive it may not be
// allowed to write. That refusal lands when the save starts, which is before
// anything has been written rather than part way through.
func (s *Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if err := s.cfg.validate(); err != nil {
		return nil, err
	}

	keys, err := ferry.NewKeys(addrs, driverName, key)
	if err != nil {
		return nil, err
	}

	cfg := s.cfg

	return func(ctx context.Context) (ferry.Writer, error) { return openWriter(ctx, &cfg, keys) }, nil
}

// openWriter opens the key this save writes under, which is the one place a
// registry that is writable in principle and not right now is found.
//
// Read-only is a runtime fact and not a schema fact, so the question is asked
// here and nowhere else. Not at Bind, which does no I/O and so cannot know
// whether this process still has write access to HKEY_LOCAL_MACHINE; and not at
// the first write, which on a driver that did not stage would already have
// half-written the plane. Creating the key is the question and the answer at once:
// a save is going to need it, and a token that may not create it is a token that
// may not write anything under it either.
func openWriter(ctx context.Context, cfg *config, keys *ferry.Keys) (ferry.Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := cfg.store.Create(ctx, ""); err != nil {
		// ErrReadOnly is the class whatever the reason the key could not be
		// created, and it is subordinate to ErrPlane, so a caller matching either
		// one is answered. What the registry said stays in the chain.
		return nil, fmt.Errorf("%w: this key could not be opened for writing: %w", ferry.ErrReadOnly, err)
	}

	return &writer{cfg: *cfg, names: keys, key: keys.Open(), wrote: map[string]bool{}}, nil
}

// pending is one staged write: where it goes, what goes there, and the address it
// came from, which is what lets a failed commit name the address rather than the
// key.
type pending struct {
	at    ferry.Path
	place place
	datum Datum
}

// subkey is one subkey this dump names: the checked plane key, which is what the
// staging is reasoned about in, and the caller's own spelling, which is what the
// registry is asked about.
type subkey struct {
	key    string
	subkey string
}

// writer is one open write side: everything the walk hands it, held until Commit.
//
// It implements [ferry.Committer] and not [ferry.Releaser]. A Close would be
// `return nil` here, and in the source that is indistinguishable from a driver
// that should have rolled back and did not.
//
// It implements [ferry.Unsetter], because the registry can forget a value and a
// subkey and that is the whole of what dump-is-replace needs. What it forgets is
// resolved at Commit against what this dump staged, rather than by deleting at
// the moment core asks: a staged write arrives after the unset that covers it, so
// deleting first and putting afterwards would be right only while nothing else
// shared a key (ADR-0004).
//
// It implements [ferry.Ensurer], and that is where it parts company with the flat
// planes. An empty subkey is a real registry object, so a section that is there
// and holds nothing has a spelling here and does not have to become absence on
// reload. A null does not, and it is refused: see [writer.Ensure].
//
// It implements no [ferry.Preparer]. A staging sink is written to as the walk
// runs, so a dump that fails is a Commit that never happens and there is nothing
// left for a check across the whole address set to save.
type writer struct {
	cfg config

	// names is the binding's checked key table, held for the reports rather than
	// for the writes: it answers what this plane calls an address without minting
	// anything (ADR-0011).
	names *ferry.Keys

	// key is this open's key function, and everything it mints belongs to this
	// open (ADR-0012). Every write goes through it, which is what puts a minted
	// map key under the injectivity check.
	key ferry.KeyFunc

	// wrote is every value key this dump has written, and it is the backstop in
	// front of the staging list: the registry would silently take the second of
	// two writes at one name, and this turns that into a refusal at the last
	// possible moment. It is driver/env's own guard, copied rather than shared,
	// because ADR-0002 forbids the internal module that would carry it.
	wrote map[string]bool

	// staged is what Commit will write, in the order the walk produced it, so two
	// identical dumps make identical sequences of calls.
	staged []pending

	// ensured is every container this dump was asked to spell at its own address,
	// which is a subkey created and left empty.
	ensured []subkey

	// forget is every composite this dump replaces, in walk order.
	forget []subkey
}

var (
	_ ferry.Writer     = (*writer)(nil)
	_ ferry.Ensurer    = (*writer)(nil)
	_ ferry.Unsetter   = (*writer)(nil)
	_ ferry.Committer  = (*writer)(nil)
	_ ferry.PlaneNamer = (*writer)(nil)
)

// PlaneName is the registry key an address is written to, hive and subkey
// included, which is what a report opens with in place of the address.
//
// It reads the table Bind built and never this open's key function, so a report
// composed after a failed dump cannot mint a key or manufacture a collision
// (ADR-0011).
func (w *writer) PlaneName(addr ferry.Path) (string, bool) {
	k, ok := w.names.PlaneName(addr)
	if !ok {
		return "", false
	}

	return w.cfg.name(k), true
}

// Set stages one value, and refuses one this plane cannot hold.
//
// Nothing reaches the registry here. A staged write is what makes a failed walk
// leave the registry untouched, and it is why a key collision discovered part way
// through a dump costs nothing: the addresses a value mints are checked as they
// are minted, and a refusal at the tenth of them leaves the first nine unwritten
// rather than half-applied.
//
// A value is written as REG_BINARY where it is bytes and as REG_SZ otherwise, and
// the write replaces whatever type was at the name before.
func (w *writer) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	k, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	d, err := stored(v)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	if w.wrote[k] {
		return ferry.ErrorAt(addr.Path(), fmt.Errorf("%w: this dump has already written this registry value, "+
			"and a key holds one value per name, so one of the two writes would be lost", ferry.ErrPlane))
	}

	w.wrote[k] = true
	w.staged = append(w.staged, pending{at: addr.Path(), place: placeOf(addr.Path()), datum: d})

	return nil
}

// Ensure spells a container at its own address, which here is the subkey the
// container is.
//
// A present container is a subkey created and left empty, and that is a real
// registry object rather than an inference: a key with no values and no subkeys
// exists, and a key that was never written does not. So a non-nil optional
// section whose every field was omitted survives a save and a load, which is the
// thing a flat plane cannot do.
//
// A null is refused, and the reason it is refused is the same fact read the other
// way. A subkey is there or it is not, and "there and holding nothing" is already
// spent on present, so a null would have to be stored as the very same object -
// which would make a nil map and a section that is there and empty one
// observation. This plane declares no null, and refusing one here is the other
// half of that declaration.
//
// The default arm is a live refusal rather than dead code. An absent container
// gets no call at all, so reaching it means core is asking something this method
// has no answer for, and a method that always returns nil is one nothing would
// catch changing.
func (w *writer) Ensure(ctx context.Context, addr ferry.Container, p ferry.Presence) error {
	switch p {
	case ferry.PresencePresent:
		return w.record(ctx, addr.Path())
	case ferry.PresenceNull:
		return ferry.ErrorAt(addr.Path(), errNoNull)
	default:
		return ferry.ErrorAt(addr.Path(), fmt.Errorf("%w: a subkey is there or it is not, so there is nothing "+
			"to write for a %s container", ferry.ErrValue, p))
	}
}

// record stages one subkey this dump asks to exist.
func (w *writer) record(ctx context.Context, at ferry.Path) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	k, err := w.key(at)
	if err != nil {
		return err
	}

	w.ensured = append(w.ensured, subkey{key: k, subkey: subkeyOf(at)})

	return nil
}

// Unset records that this dump replaces everything the registry holds under one
// slice or one map, which is what stops a save of a shorter list from leaving the
// previous save's later positions behind.
//
// Nothing is listed and nothing is removed here, for the reason nothing is
// written in [writer.Set]: a walk that fails afterwards has to leave the registry
// as it was, and a delete that already happened is the one thing this driver could
// not undo. The removals run first at the commit, resolved against what this dump
// staged.
//
// The composite's own subkey is not removed, only what it holds. A composite that
// still has members is written back into it, and one that lost every member is a
// key that is there and empty - which is a state this plane can spell.
func (w *writer) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	k, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	w.forget = append(w.forget, subkey{key: k, subkey: subkeyOf(addr.Path())})

	return nil
}

// Commit removes what this dump replaced, creates the subkeys it was asked to
// spell, and writes everything it staged, in that order.
//
// The order is the contract. A member this dump does write arrives after the
// unset covering it and has to survive, so the removals resolve against what was
// staged rather than deleting blind, and the writes come last.
//
// It runs only where the walk succeeded, which is core's protocol and not a check
// this driver makes: there is no failure to report to a driver, only a commit that
// does not happen.
//
// It does not stop at the first refusal, on the same argument the walk itself does
// not: an operator fixing a permission wants every address it could not write in
// one report.
func (w *writer) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	errs := append(w.replaced(ctx), w.created(ctx)...)
	errs = append(errs, w.written(ctx)...)

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("winreg: writing the registry: %w", err)
	}

	return nil
}

// replaced removes everything under a forgotten composite that this dump did not
// write.
func (w *writer) replaced(ctx context.Context) []error {
	var errs []error

	for _, f := range w.forget {
		errs = append(errs, w.prune(ctx, f)...)
	}

	return errs
}

// prune is one replaced subkey: its own values and its own subkeys, each kept
// where this dump wrote it and removed where it did not.
//
// The subkey itself stays. It is the composite's own address, and what a dump
// replaces is the members under it rather than the container they are in.
func (w *writer) prune(ctx context.Context, f subkey) []error {
	listing, found, err := w.cfg.store.List(ctx, f.subkey)
	if err != nil {
		return []error{fmt.Errorf("%s: listing what this save replaces: %w", f.key, err)}
	}

	if !found {
		return nil
	}

	return append(w.pruneValues(ctx, f, listing.Values), w.pruneKeys(ctx, f, listing.Keys)...)
}

// pruneValues removes every value under a replaced subkey that this dump did not
// write.
//
// The names are sorted first, because two identical dumps must make identical
// sequences of calls and a registry's enumeration order is its own.
func (w *writer) pruneValues(ctx context.Context, f subkey, names []string) []error {
	var errs []error

	for _, name := range sorted(names) {
		if w.wrote[joinKey(f.key, fold(name))] {
			continue
		}

		if err := w.cfg.store.DeleteValue(ctx, f.subkey, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: removing a value this save replaces: %w", f.key, err))
		}
	}

	return errs
}

// pruneKeys removes every subkey under a replaced subkey that this dump wrote
// nothing into, and descends into the ones it did.
func (w *writer) pruneKeys(ctx context.Context, f subkey, names []string) []error {
	var errs []error

	for _, name := range sorted(names) {
		child := subkey{key: joinKey(f.key, fold(name)), subkey: joinKey(f.subkey, name)}

		if w.keeps(child.key) {
			errs = append(errs, w.prune(ctx, child)...)

			continue
		}

		if err := w.cfg.store.DeleteKey(ctx, child.subkey); err != nil {
			errs = append(errs, fmt.Errorf("%s: removing a subkey this save replaces: %w", child.key, err))
		}
	}

	return errs
}

// keeps reports whether this dump put anything inside one subkey, which is what
// stops the sweep removing a key the walk is about to write into.
//
// A value key equal to the subkey key does not count, because a value and a
// subkey of one name are two registry objects and only one of them is being
// asked about.
func (w *writer) keeps(key string) bool {
	// Every key this is asked about is a subkey of a replaced composite, so it
	// is never the driver's own key and the join below never produces a bare
	// backslash.
	inside := key + separator

	for k := range w.wrote {
		if strings.HasPrefix(k, inside) {
			return true
		}
	}

	for _, e := range w.ensured {
		if e.key == key || strings.HasPrefix(e.key, inside) {
			return true
		}
	}

	return false
}

// created makes every subkey this dump was asked to spell at its own address.
func (w *writer) created(ctx context.Context) []error {
	errs := make([]error, 0, len(w.ensured))

	for _, e := range w.ensured {
		if err := w.cfg.store.Create(ctx, e.subkey); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.key, err))
		}
	}

	return errs
}

// written puts everything this dump staged, in the order the walk produced it.
func (w *writer) written(ctx context.Context) []error {
	errs := make([]error, 0, len(w.staged))

	for _, p := range w.staged {
		if err := w.cfg.store.Set(ctx, p.place.subkey, p.place.name, p.datum); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.at, err))
		}
	}

	return errs
}

// sorted is one listing in a fixed order, over a copy, so that nothing this
// driver does depends on the order a registry enumerated in and nothing it does
// reorders a slice the caller's own [Registry] handed it.
func sorted(names []string) []string {
	out := slices.Clone(names)
	slices.Sort(out)

	return out
}
