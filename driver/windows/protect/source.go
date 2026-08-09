package protect

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// Over puts protection in front of a source: every address the selector picked
// is decrypted on its way out of the plane, and every other address is the
// plane's own answer, untouched.
//
//	reg := ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))
//	src := protect.Over(kv.NewSource(client), protect.LocalSystem, protect.FromTags())
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(reg))
//
// The result is a [ferry.Source] and nothing more, so it goes wherever the
// source it wraps went and composes with every plane rather than with one.
// [OverSink] is the other half, and both halves take the same descriptor, the
// same selector and the same [Using]: a sink protecting under one descriptor and
// a source unprotecting through another never meet, and nothing checks that the
// two agree.
//
// It keeps everything the wrapped reader could do. Probing a container,
// enumerating one, releasing a resource, naming an address in the plane's own
// spelling and tolerating overlapping calls are all discovered by assertion on
// the reader core is handed, so a decorator that quietly dropped one of them
// would change how a schema loads without failing anything - a dropped
// enumeration loads a map as empty. What this hands back declares exactly what
// the plane underneath declared.
//
// A plane that reports a link at a marked address is refused, naming both. The
// mark travels on the field, the address a link points at is a different one that
// carries no mark, and a value read through the link would come back as it is
// stored rather than as what it was protected from.
//
// A value the plane holds that was never protected is read back as it stands.
// That is what lets an existing store migrate: the next save writes it
// protected. What it costs is in the package README, and it is accepted rather
// than overlooked.
//
// It returns no error, so everything it can refuse lands at Bind, before any
// read: a missing tag key declaration, an address the mark cannot sit at, an
// empty descriptor, and, on every operating system but Windows, the absence of
// DPAPI-NG where no [Using] was given.
func Over(src ferry.Source, d Descriptor, sel Selector, opts ...Option) ferry.Source {
	return &source{inner: src, cfg: newConfig(d, sel, opts)}
}

// source is the read half of the decorator.
type source struct {
	inner ferry.Source
	cfg   config
}

var _ ferry.Source = (*source)(nil)

// Bind reads the selection once and binds the plane underneath.
//
// The selection is resolved before the wrapped source is asked for anything, so
// a schema this decorator refuses is refused with nothing having been bound and
// nothing having been read.
func (s *source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	marked, err := s.cfg.bind(s.inner, addrs)
	if err != nil {
		return nil, err
	}

	open, err := s.inner.Bind(addrs)
	if err != nil {
		return nil, err
	}

	cfg := s.cfg

	return func(ctx context.Context) (ferry.Reader, error) {
		r, err := open(ctx)
		if err != nil {
			return nil, err
		}

		return shellReader(&reader{inner: r, marked: marked, keeper: cfg.keeper}, r), nil
	}, nil
}

// reader is one open read side: the plane's own reader with Get in front of it.
//
// It holds nothing mutable, so it is as safe from many goroutines at once as the
// reader it wraps - which is what lets the shell forward [ferry.Concurrent]
// rather than having to withhold it (ADR-0019).
type reader struct {
	inner  ferry.Reader
	marked map[ferry.Path]bool
	keeper Protector
}

var _ ferry.Reader = (*reader)(nil)

// Get answers with what the plane holds, decrypted where the schema marked the
// address as a secret.
//
// Three answers at a marked address, and the difference between the last two is
// the whole of the migration story:
//
//   - the plane holds a value this package wrote: it is decrypted and comes back
//     as the kind and the exact text it was saved from.
//   - the plane holds something else: it comes back as it stands, so a store
//     written before this decorator was put in front of it still loads, and the
//     next save writes it protected.
//   - the plane holds something this package wrote and it cannot be decrypted:
//     that is a failure naming the address, and never a plaintext quietly passed
//     through.
//
// An absence stays an absence and a null stays a null, at every address, because
// neither carries anything that could have been encrypted.
//
// A link the plane reports at a marked address is the fourth case, and it is
// refused: see [linkedAway].
func (r *reader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	v, err := r.inner.Get(ctx, addr)
	if !r.marked[addr.Path()] {
		return v, err
	}

	if err != nil {
		return v, linkedAway(addr, err)
	}

	blob, protected := markedText(v)
	if !protected {
		return v, nil
	}

	out, err := r.unprotect(ctx, blob)
	if err != nil {
		return ferry.Value{}, ferry.ErrorAt(addr.Path(), err)
	}

	return out, nil
}

// linkedAway refuses a [ferry.LeafRedirect] reported at a marked address, and
// passes every other failure through exactly as the plane reported it.
//
// A redirect is not a failure. It is a control answer core acts on by reading
// again at the target, and the target is a different address, which this
// decorator was never told holds a secret. So the ciphertext stored there would
// come back as the field's own text, marker and all, with nothing saying it had
// ever been protected - which is the one thing this package promises not to do,
// and it would be silent.
//
// Following the link here is not the alternative. The chain, the addresses
// already visited and the refusal of a cycle are core's (ADR-0016), and a
// decorator that read the target itself would have none of them. Refusing is,
// and it is loud: it names both addresses and says what to do about it.
func linkedAway(addr ferry.LeafAddr, err error) error {
	var hop *ferry.LeafRedirect
	if !errors.As(err, &hop) {
		return err
	}

	return ferry.ErrorAt(addr.Path(), fmt.Errorf("%w: this plane holds a link here and says the value lives at "+
		"%s, which this schema does not mark as a secret: a value read through a link comes back as it is "+
		"stored, so mark the address the link points at as well, or take the link out from under this one",
		ferry.ErrPlane, hop.Target))
}

// unprotect turns one marked payload back into the value it was written from,
// and everything that can go wrong here is [ErrCiphertext].
func (r *reader) unprotect(ctx context.Context, blob string) (ferry.Value, error) {
	raw, err := wire.DecodeString(blob)
	if err != nil {
		return ferry.Value{}, corruptBy("its payload is not the base64 this package writes", err)
	}

	plain, err := r.keeper.Unprotect(ctx, raw)
	if err != nil {
		return ferry.Value{}, corruptBy("it could not be decrypted, which is a value protected for somebody "+
			"else, on another machine, or since damaged", err)
	}

	return valueOf(plain)
}
