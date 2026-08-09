package protect_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
)

// A plane may answer a Get with a link rather than a value, and core acts on it
// by reading again at the target. The target is a different address, and the mark
// this package selects on travels on the field rather than on the plane, so the
// two directions of a link across the selection are not the same question.
//
// Reading through a link *out of* a marked address would hand back whatever the
// target holds, which for a store this package has written to is a marker and a
// ciphertext. That is the one answer the package promises never to give, so it is
// refused.
//
// Reading through a link *into* a marked address is fine and is asserted below,
// because the target is where the decryption happens and it still happens.

func TestALinkOutOfAMarkedAddressIsRefusedRatherThanReadThrough(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()

	blob, err := k.Protect(t.Context(), string(protect.LocalSystem), []byte("ss3cr3t"))
	if err != nil {
		t.Fatalf("staging the ciphertext the link points at: %v", err)
	}

	// The plane holds the protected value at the address the link points at, and
	// a link at the marked address itself.
	s.seed(ferry.At("plain"), ferry.String("ferry-protect:1:"+encoded(blob)))
	s.seed(addrUser, ferry.String("bob"))

	src := protect.Over(linking{s: s, from: addrToken, to: ferry.At("plain")},
		protect.LocalSystem, protect.FromTags(), protect.Using(k))

	_, err = ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("a link at a marked address loaded as %v, want a refusal under %v", err, ferry.ErrPlane)
	}

	if !strings.Contains(err.Error(), "/plain") {
		t.Errorf("the refusal reads %q, and it has to name the address the link points at", err)
	}
}

func TestALinkIntoAMarkedAddressStillDecryptsAtTheTarget(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()

	blob, err := k.Protect(t.Context(), string(protect.LocalSystem), []byte("ss3cr3t"))
	if err != nil {
		t.Fatalf("staging the ciphertext: %v", err)
	}

	s.seed(addrToken, ferry.String("ferry-protect:1:"+encoded(blob)))
	s.seed(addrUser, ferry.String("bob"))

	// The unmarked address is the one holding the link, and it points at the
	// marked one.
	src := protect.Over(linking{s: s, from: ferry.At("plain"), to: addrToken},
		protect.LocalSystem, protect.FromTags(), protect.Using(k))

	got, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("a link into a marked address: %v", err)
	}

	if got.Plain != "s3cr3t" || got.Auth.Token != "s3cr3t" {
		t.Errorf("it loaded as %+v: the decryption happens at the target, which is the address that is marked",
			got)
	}
}

// linking is a plane that answers one address with a link to another, which is
// what [ferry.LeafRedirect] is for and what nothing else in this package's tests
// produces.
//
// The target has to be an address the schema names, because a driver cannot mint
// one, so it is picked out of the set at Bind rather than built here.
type linking struct {
	s        *store
	from, to ferry.Path
}

var _ ferry.Source = linking{}

func (l linking) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	target, found := leafAt(addrs, l.to)
	if !found {
		return nil, errors.New("linking: this schema does not name the address the link points at")
	}

	return func(context.Context) (ferry.Reader, error) {
		return linkingReader{s: l.s, from: l.from, to: target}, nil
	}, nil
}

type linkingReader struct {
	s    *store
	from ferry.Path
	to   ferry.LeafAddr
}

func (r linkingReader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if addr.Path() == r.from {
		return ferry.Value{}, &ferry.LeafRedirect{Target: r.to}
	}

	return r.s.at(addr.Path()), nil
}

// leafAt is the leaf address in the set at one path, which is the only way to get
// hold of one: addresses are minted by core and handed to a driver at Bind.
func leafAt(addrs *ferry.AddressSet, at ferry.Path) (ferry.LeafAddr, bool) {
	for m := range addrs.Seq() {
		if leaf, ok := m.(ferry.LeafAddr); ok && leaf.Path() == at {
			return leaf, true
		}
	}

	return ferry.LeafAddr{}, false
}
