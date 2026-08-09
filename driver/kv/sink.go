package kv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/onhotpath/ferry"
)

// Sink is the write half of a key-value plane.
//
//	sink, err := kv.NewSink(store, kv.WithPrefix("app"))
//	err = ferry.Dump(ctx, cfg, sink)
//
// It stages every write and performs them together at the end, so a save that
// fails leaves the store untouched. It also means one save reports every field
// it could not write, rather than stopping at the first.
//
// Staging is not a transaction. The writes themselves go through your [Client]
// one key at a time, so a store that fails part way through the commit is left
// part way through it.
type Sink struct {
	client Client
	prefix []string

	// rootName is the key a root value is written at, under the prefix, or
	// empty where [RootKey] named none (#334).
	rootName string

	// raw is the spelling [Raw] declared, or nil where this plane's values are
	// text (ADR-0018).
	raw ferry.Spelling[[]byte, []byte]
}

var _ ferry.Sink = (*Sink)(nil)

// NewSink builds a sink writing through client.
//
//	sink, err := kv.NewSink(consulClient, kv.WithPrefix("app"))
//	err = ferry.Dump(ctx, cfg, sink)
//
// It refuses [WithBatch] rather than ignoring it: a save stages every write and
// commits them together, so there is no per-key half of that Option to choose
// against.
func NewSink(client Client, opts ...Option) (*Sink, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, errNoClient
	}

	if cfg.batch {
		return nil, errors.New("kv: WithBatch is a source's Option: a sink stages every write and commits them " +
			"together, so there is no per-key half of it to choose against")
	}

	return &Sink{client: client, prefix: cfg.prefix, rootName: cfg.rootKey, raw: cfg.raw}, nil
}

// Bind computes this schema's store keys and checks them, exactly as
// [Source.Bind] does and for the same reasons.
//
// It does no I/O, so a sink binds successfully against a store it may not be
// allowed to write to. See [ACL] for where that is discovered.
func (s *Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, err := ferry.NewKeys(addrs, driverName, keyFunc(s.prefix, s.rootName))
	if err != nil {
		return nil, err
	}

	return s.opener(keys), nil
}

// opener is the [ferry.OpenWriterFunc] one Bind hands back, and where a plane
// that cannot be written to right now is refused.
//
// Read-only is a runtime fact and not a schema fact, so the question is asked
// here and nowhere else. Not at Bind, which does no I/O and therefore cannot
// know whether a token still has write access; and not at the first write,
// which has already half-written the plane. That placement is a clause of
// ADR-0004's contract rather than a convention, which is why the refusal wraps
// [ferry.ErrReadOnly]: a portable signal is what lets the conformance suite
// hold every driver to the placement instead of the rule being prose.
func (s *Sink) opener(keys *ferry.Keys) ferry.OpenWriterFunc {
	root := prefixKey(s.prefix)

	return func(ctx context.Context) (ferry.Writer, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		acl, guarded := s.client.(ACL)
		if guarded {
			if err := acl.CanWrite(ctx, root); err != nil {
				return nil, fmt.Errorf("%w: this client's credentials do not permit writing here: %w",
					ferry.ErrReadOnly, err)
			}
		}

		return &writer{client: s.client, acl: acl, names: keys, key: keys.Open(),
			staged: map[string]staged{}, raw: s.raw}, nil
	}
}

// writer is one open write side: everything the walk hands it, held until
// Commit.
//
// It implements [ferry.Committer] and not [ferry.Releaser]. A Close would be
// `return nil` here, and ADR-0004 refuses that in a driver for a reason beyond
// noise: in the source it is indistinguishable from a driver that should have
// rolled back and did not, and nothing in the type system tells the two apart.
//
// It implements [ferry.Unsetter], because a store can forget a key and that is
// the whole of what dump-is-replace needs here. What it forgets is resolved at
// Commit against what this dump staged, rather than by deleting at the moment
// core asks: a staged write arrives after the unset that covers it, so deleting
// first and putting afterwards would be right only while nothing else shared a
// key, and one listing per forgotten folder answers the question exactly
// (ADR-0004).
//
// It implements no [ferry.Ensurer], and that absence is the declaration
// ADR-0016 asks for rather than an omission. A store holds bytes at keys and has
// no way to say that a container is there and holds nothing, so a plane with no
// spelling for one implements nothing and core refuses the dump at the address,
// naming this plane. Storing a zero-length value instead would make "the section
// is present and empty" and "the field is empty text" one observation, which is
// the collision [errNoNull] already exists to refuse at a leaf.
type writer struct {
	client Client

	// acl is the client's own permission check, or nil where it answers no
	// permission questions and is writable everywhere.
	acl ACL

	// names is the binding's checked key table, held for the reports rather than
	// for the writes: it answers what this store calls an address without minting
	// anything (ADR-0011, #159).
	names *ferry.Keys

	// key is this open's key function, and everything it mints belongs to this
	// open (ADR-0012).
	key ferry.KeyFunc

	// staged is what Commit will write, keyed by store key so that two addresses
	// arriving at one key are caught here rather than one of them being lost.
	staged map[string]staged

	// order is the keys in the order the walk produced them, so a commit writes
	// in walk order and two identical dumps make identical sequences of calls.
	order []string

	// raw is this plane's spelling of a payload, or nil where the plane carries
	// text. A bytes value goes through it so that the two directions of [Raw]
	// cannot drift apart (ADR-0018).
	raw ferry.Spelling[[]byte, []byte]

	// forget is the folders this dump replaced, in walk order, each already
	// carrying its trailing separator so that a listing under one cannot reach
	// a sibling whose key merely starts with the same bytes.
	forget []string
}

var (
	_ ferry.Committer  = (*writer)(nil)
	_ ferry.Unsetter   = (*writer)(nil)
	_ ferry.PlaneNamer = (*writer)(nil)
)

// PlaneName is the store key an address is written to, prefix included, which is
// what a report opens with in place of the address.
//
// It reads the table Bind built and never this open's key function, so a report
// composed after a failed dump cannot mint a key or manufacture a collision
// (ADR-0011, #159).
func (w *writer) PlaneName(addr ferry.Path) (string, bool) { return w.names.PlaneName(addr) }

// staged is one pending write: the bytes, and the address they came from, which
// is what lets a failed commit name the address rather than the key.
type staged struct {
	addr  ferry.Path
	value []byte
}

// Set stages one address, and refuses a value this plane cannot hold.
//
// Nothing reaches the store here. A staged write is what makes a failed walk
// leave the plane untouched, and it is the reason this driver is immune to a
// key collision discovered part way through a dump: the addresses a value mints
// are checked as they are minted, and a refusal at the tenth of them leaves the
// first nine unwritten rather than half-applied.
//
// A token that may write some paths and not others refuses here, per address,
// rather than at the open. That is deliberate and it is the reason core's dump
// aggregates: two denied paths are two errors naming two addresses, where a
// driver that stopped at the first would send an operator round the loop twice.
func (w *writer) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	key, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	value, err := w.opaque(v)
	if err != nil {
		return err
	}

	if err := w.permits(ctx, key); err != nil {
		return err
	}

	if _, taken := w.staged[key]; taken {
		return fmt.Errorf("%w: this dump has already written this store key, and a store holds one value per "+
			"key, so one of the two writes would be lost", ferry.ErrPlane)
	}

	w.staged[key] = staged{addr: addr.Path(), value: value}
	w.order = append(w.order, key)

	return nil
}

// Unset records that this dump replaces everything the store holds under one
// composite, which is what stops a save of a shorter list from leaving the
// previous save's later positions behind.
//
// Nothing is listed and nothing is deleted here, for the reason nothing is
// written in Set: a walk that fails afterwards has to leave the store
// byte-identical, and a delete that already happened is the one thing this
// driver could not undo.
//
// A folder is recorded as often as it is named, with no check for one already
// held, because one dump names one composite once and the address set is
// injective over this key space. [writer.stale] collects each key once whatever
// arrives here.
func (w *writer) Unset(ctx context.Context, addr ferry.CompositeAddr) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	key, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	w.forget = append(w.forget, folder(key))

	return nil
}

// permits asks the client whether these credentials may write at one key, and
// says nothing for a client that answers no permission questions.
func (w *writer) permits(ctx context.Context, key string) error {
	if w.acl == nil {
		return nil
	}

	if err := w.acl.CanWrite(ctx, key); err != nil {
		return fmt.Errorf("%w: this client's credentials do not permit writing at this key: %w",
			ferry.ErrPlane, err)
	}

	return nil
}

// Commit writes everything this dump staged, in the order the walk produced it.
//
// It runs only where the walk succeeded, which is core's protocol and not a
// check this driver makes: there is no failure to report to a driver, only a
// commit that does not happen. So a dump that failed anywhere leaves the store
// byte-identical, and this driver never has to undo a partial write.
//
// It does not stop at the first refusal, on the same argument the walk itself
// does not: an operator fixing a token's ACL wants every key it could not write
// in one report. The whole aggregate reaches ferry as one element, because core
// cannot attribute addresses inside a third party's error tree and does not
// rewrite one, so each element names its own address in its own text.
// The removals run first, and they are resolved against what this dump staged
// rather than applied blind, so a key the walk wrote is never deleted and then
// written back.
func (w *writer) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	errs := append(w.replaced(ctx), w.written(ctx)...)

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("kv: writing the store: %w", err)
	}

	return nil
}

// replaced removes every key under a forgotten folder that this dump did not
// write, which is the half of a commit that makes a save a replacement.
//
// A listing that fails is one error and not one per key: what could not be read
// is the folder, and the keys under it were never learned.
func (w *writer) replaced(ctx context.Context) []error {
	stale, err := w.stale(ctx)
	if err != nil {
		return []error{err}
	}

	errs := make([]error, 0, len(stale))

	for _, key := range stale {
		if err := w.remove(ctx, key); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// written puts everything this dump staged, in the order the walk produced it.
func (w *writer) written(ctx context.Context) []error {
	errs := make([]error, 0, len(w.order))

	for _, key := range w.order {
		pending := w.staged[key]
		if err := w.client.Put(ctx, key, pending.value); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", pending.addr, err))
		}
	}

	return errs
}

// stale is every key under a forgotten folder that this dump did not write,
// sorted, which is the set difference that makes a save a replacement.
//
// Sorted because two identical dumps must make identical sequences of calls, and
// a store's listing is a map: ADR-0001's determinism invariant reaches the call
// order and not only the answer.
func (w *writer) stale(ctx context.Context) ([]string, error) {
	out := make([]string, 0, len(w.forget))
	seen := map[string]bool{}

	for _, under := range w.forget {
		held, err := w.client.List(ctx, under)
		if err != nil {
			return nil, fmt.Errorf("listing %q, which this save replaces: %w", under, err)
		}

		out = w.superseded(held, seen, out)
	}

	slices.Sort(out)

	return out, nil
}

// superseded adds the keys of one listing that this dump did not write and has
// not already collected.
func (w *writer) superseded(held map[string][]byte, seen map[string]bool, out []string) []string {
	for key := range held {
		if _, written := w.staged[key]; written || seen[key] {
			continue
		}

		seen[key] = true

		out = append(out, key)
	}

	return out
}

// remove deletes one key this save replaced, asking the same permission question
// a write asks: removing a key is writing at it, and a token allowed to do
// neither should hear about both in one report.
func (w *writer) remove(ctx context.Context, key string) error {
	if err := w.permits(ctx, key); err != nil {
		return err
	}

	if err := w.client.Delete(ctx, key); err != nil {
		return fmt.Errorf("removing %q, which this save replaces: %w", key, err)
	}

	return nil
}

// opaque is the whole of what this plane does to a value: it takes the text the
// boundary spelled and stores the bytes of it.
//
// Every kind but one has bytes to store, and the kind switch is a formality of
// the accessors rather than five behaviours: a Value carries source text
// whatever its kind, and the accessors are per-kind so that a caller cannot
// read a Number as a Bool by accident.
//
// Null is the one this plane cannot hold, and it is refused rather than
// mangled. It arrives at a leaf from a nil pointer to one, and the only thing a
// store could put there is a zero-length value - which is what String("")
// already is, so "the field is nil" and "the field is empty text" would become
// one observation. The class is [ferry.ErrValue] rather than the
// [ferry.ErrPlane] core would default to, because nothing is wrong with the
// store: the value has no representation here, and retrying it is pointless in
// the way an ErrValue promises and a plane error does not.
//
// A null at a container's own address no longer arrives here at all. That is
// not this plane gaining one: core asks [ferry.Ensurer] for it now, this writer
// implements none, and the refusal is core's with the same outcome (ADR-0016).
//
// Absent never arrives. It is a reader-side kind, and an omitted address is one
// that gets no Set call at all.
func (w *writer) opaque(v ferry.Value) ([]byte, error) {
	switch v.Kind() {
	case ferry.KindString:
		return bytesOf(v.AsString())
	case ferry.KindNumber:
		return bytesOf(v.AsNumber())
	case ferry.KindBytes:
		return w.payload(v)
	case ferry.KindBool:
		b, err := v.AsBool()

		return bytesOf(strconv.FormatBool(b), err)
	case ferry.KindNull, ferry.KindAbsent:
		return nil, errNoNull
	default:
		return nil, fmt.Errorf("%w: this store was handed %s, which is not a kind ferry's boundary has",
			ferry.ErrValue, v.Kind())
	}
}

// payload is the bytes arm, and it is the one kind this plane holds with no
// conversion of any sort: the store takes bytes and a bytes value carries them.
//
// [Raw] is what puts a spelling in the way, and the spelling is the identity,
// so what this buys is not a different byte written but the two directions of
// that Option going through one place. A plane declared to read payloads and
// writing them through a different rule would be the drift the seam exists to
// stop (ADR-0018).
func (w *writer) payload(v ferry.Value) ([]byte, error) {
	b, err := v.AsBytes()
	if err != nil || w.raw == nil {
		return b, err
	}

	return w.raw.Render(b)
}

// bytesOf is the text half of every accessor above, so that the switch reads as
// one decision rather than five copies of the same error check.
func bytesOf(text string, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}

	return []byte(text), nil
}

// errNoNull is the refusal that makes this a plane with no null rather than a
// plane that quietly has one.
var errNoNull = fmt.Errorf("%w: a key-value store holds bytes and has no null, and this key was handed one: "+
	"a nil pointer to a value has nothing to be written as here, and storing a zero-length value for it would "+
	"make it indistinguishable from empty text", ferry.ErrValue)
