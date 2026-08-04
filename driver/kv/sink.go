package kv

import (
	"context"
	"errors"
	"fmt"
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
			"together, so there is no per-address half of it to choose against")
	}

	return &Sink{client: client, prefix: cfg.prefix}, nil
}

// Bind computes this schema's store keys and checks them, exactly as
// [Source.Bind] does and for the same reasons.
//
// It does no I/O, so a sink binds successfully against a store it may not be
// allowed to write to. See [ACL] for where that is discovered.
func (s *Sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, err := ferry.NewKeys(addrs, driverName, keyFunc(s.prefix))
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
	root := rootKey(s.prefix)

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

		return &writer{client: s.client, acl: acl, key: keys.Open(), staged: map[string]staged{}}, nil
	}
}

// writer is one open write side: everything the walk hands it, held until
// Commit.
//
// It implements [ferry.Committer] and not [ferry.Releaser]. A Close would be
// `return nil` here, and ADR-0004 refuses that in a driver for a reason beyond
// noise: in the source it is indistinguishable from a driver that should have
// rolled back and did not, and nothing in the type system tells the two apart.
type writer struct {
	client Client

	// acl is the client's own permission check, or nil where it answers no
	// permission questions and is writable everywhere.
	acl ACL

	// key is this open's key function, and everything it mints belongs to this
	// open (ADR-0012).
	key ferry.KeyFunc

	// staged is what Commit will write, keyed by store key so that two addresses
	// arriving at one key are caught here rather than one of them being lost.
	staged map[string]staged

	// order is the keys in the order the walk produced them, so a commit writes
	// in walk order and two identical dumps make identical sequences of calls.
	order []string
}

var _ ferry.Committer = (*writer)(nil)

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
func (w *writer) Set(ctx context.Context, addr ferry.Path, v ferry.Value) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	key, err := w.key(addr)
	if err != nil {
		return err
	}

	value, err := opaque(v)
	if err != nil {
		return err
	}

	if err := w.permits(ctx, key); err != nil {
		return err
	}

	if _, taken := w.staged[key]; taken {
		return fmt.Errorf("%w: this dump has already written the store key this address renders to, and a store "+
			"holds one value per key, so one of the two writes would be lost", ferry.ErrPlane)
	}

	w.staged[key] = staged{addr: addr, value: value}
	w.order = append(w.order, key)

	return nil
}

// permits asks the client whether these credentials may write at one key, and
// says nothing for a client that answers no permission questions.
func (w *writer) permits(ctx context.Context, key string) error {
	if w.acl == nil {
		return nil
	}

	if err := w.acl.CanWrite(ctx, key); err != nil {
		return fmt.Errorf("%w: this client's credentials do not permit writing at this address: %w",
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
func (w *writer) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	errs := make([]error, 0, len(w.order))

	for _, key := range w.order {
		pending := w.staged[key]
		if err := w.client.Put(ctx, key, pending.value); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", pending.addr, err))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("kv: writing the store: %w", err)
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
// mangled. It arrives at a container address from a nil pointer, a nil
// composite or an empty composite (ADR-0005), and the only thing a store could
// put there is a zero-length value - which is what String("") already is, so
// "the field is nil" and "the field is empty text" would become one
// observation. The class is [ferry.ErrValue] rather than the [ferry.ErrPlane]
// core would default to, because nothing is wrong with the store: the value has
// no representation here, and retrying it is pointless in the way an ErrValue
// promises and a plane error does not.
//
// Absent never arrives. It is a reader-side kind, and an omitted address is one
// that gets no Set call at all.
func opaque(v ferry.Value) ([]byte, error) {
	switch v.Kind() {
	case ferry.KindString:
		return bytesOf(v.AsString())
	case ferry.KindNumber:
		return bytesOf(v.AsNumber())
	case ferry.KindBytes:
		return v.AsBytes()
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
var errNoNull = fmt.Errorf("%w: a key-value store holds bytes and has no null, and this address was handed one: "+
	"a nil pointer, a nil composite and an empty composite have nothing to be written as here, and storing a "+
	"zero-length value for them would make them indistinguishable from empty text", ferry.ErrValue)
