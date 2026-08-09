package protect

import (
	"context"

	"github.com/onhotpath/ferry"
)

// OverSink puts protection in front of a sink: every address the selector picked
// is encrypted on its way into the plane, and every other address is written
// exactly as it would have been.
//
//	reg := ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))
//	dst := protect.OverSink(kv.NewSink(client), protect.LocalSystem, protect.FromTags())
//	err := ferry.Dump(ctx, cfg, dst, ferry.WithRegistry(reg))
//
// It is a second constructor rather than a second argument to [Over], because a
// [ferry.Source] and a [ferry.Sink] are two interfaces with one method name and
// no common type: one value cannot have two Bind methods, which is the same
// reason a plane's own driver ships two types. A plane with a read half and no
// honest write half - the process environment is one - is decorated by calling
// this not at all.
//
// It keeps everything the wrapped writer could do: committing, releasing,
// spelling a container at its own address, forgetting a composite, and being
// handed a dump's realised addresses before the first write. Those are
// discovered by assertion, so a decorator that dropped one would break a schema
// rather than report anything - a sink whose [ferry.Unsetter] went missing is
// refused at the open for every schema holding a slice or a map.
//
// What is written at a marked address is a string: this package's marker and
// then the ciphertext, base64 encoded. The kind the value had travels inside the
// ciphertext, so a number comes back as the same number in the same spelling and
// a bool as the same bool. A null is written as a null, because there is nothing
// at one to encrypt.
//
// It returns no error, and everything it can refuse lands at Bind, before
// anything is written, on the same terms as [Over].
func OverSink(dst ferry.Sink, d Descriptor, sel Selector, opts ...Option) ferry.Sink {
	return &sink{inner: dst, cfg: newConfig(d, sel, opts)}
}

// sink is the write half of the decorator.
type sink struct {
	inner ferry.Sink
	cfg   config
}

var _ ferry.Sink = (*sink)(nil)

// Bind reads the selection once and binds the plane underneath.
//
// The selection is resolved before the wrapped sink is asked for anything, so a
// schema this decorator refuses is refused with nothing bound and nothing
// written.
func (s *sink) Bind(addrs *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	marked, err := s.cfg.bind(s.inner, addrs)
	if err != nil {
		return nil, err
	}

	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	cfg := s.cfg

	return func(ctx context.Context) (ferry.Writer, error) {
		w, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return shellWriter(&writer{inner: w, marked: marked, keeper: cfg.keeper, desc: string(cfg.desc)}, w), nil
	}, nil
}

// writer is one open write side: the plane's own writer with Set in front of it.
type writer struct {
	inner  ferry.Writer
	marked map[ferry.Path]bool
	keeper Protector
	desc   string
}

var _ ferry.Writer = (*writer)(nil)

// Set writes one value, encrypted where the schema marked the address as a
// secret.
//
// The value the plane is handed is a string: the marker, then the ciphertext.
// The kind that was written travels inside it, which is what makes the round
// trip exact rather than approximate - a number comes back in its own spelling
// and a bool comes back a bool.
//
// A null is passed through unencrypted. It says the field is nil, there is
// nothing at it to encrypt, and a ciphertext in its place would be the plane
// holding something where it held nothing.
//
// A failure to encrypt fails the dump, naming the address. There is no arm here
// that writes the value in the clear instead.
func (w *writer) Set(ctx context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	if !w.marked[addr.Path()] {
		return w.inner.Set(ctx, addr, v)
	}

	plain, has, err := plainOf(v)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), err)
	}

	if !has {
		return w.inner.Set(ctx, addr, v)
	}

	blob, err := w.keeper.Protect(ctx, w.desc, plain)
	if err != nil {
		return ferry.ErrorAt(addr.Path(), unprotectable(err))
	}

	return w.inner.Set(ctx, addr, stored(blob))
}

// unprotectable states the class a failed encryption has and keeps
// [ErrCiphertext] reachable underneath it: the value at this address is one this
// package could not turn into a ciphertext, and the dump stops rather than
// storing what it was handed.
func unprotectable(err error) error {
	return corruptBy("this value could not be encrypted, and it is not written in the clear instead", err)
}
