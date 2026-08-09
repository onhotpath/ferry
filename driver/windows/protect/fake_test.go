package protect_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"hash/fnv"
	"sync"

	"github.com/onhotpath/ferry/driver/windows/protect"
)

// keeper is the in-repo protection every test in this package runs against, and
// the reason all of them run on every operating system.
//
// The real seam is behind //go:build windows and cannot be entered here at all,
// so this is not a convenience: it is what puts the selection, the envelope, the
// migration and the shells under test on the machines the tests are run on.
//
// It is not cryptography and does not pretend to be. What it does model is the
// three properties this package is built on and would be wrong without:
//
//   - the output is randomised, so protecting one plaintext twice gives two
//     different blobs and the only property that holds is the round trip. Any
//     test comparing two ciphertexts would fail here exactly as it would fail
//     against DPAPI-NG.
//   - a blob that has been damaged, or that came from somewhere else, fails to
//     unprotect rather than decrypting to rubbish, which is what makes "a value
//     that says it is protected and cannot be read back is loud" testable.
//   - the descriptor is recorded, so a test can assert which principal a value
//     was protected for.
//
// It is test apparatus rather than module surface, and it lives in a _test.go
// file so that it is neither shipped code nor covered code.
type keeper struct {
	mu sync.Mutex

	// descs is every descriptor this keeper was asked to protect under, in call
	// order.
	descs []string

	// failProtect and failUnprotect stage the two failures a protector has: one
	// that cannot encrypt, and one that cannot decrypt.
	failProtect   bool
	failUnprotect bool
}

// errKeeper is what a staged failure reports, and the sentinel a test looks for
// under ferry's wrapper.
var errKeeper = errors.New("keeper: the protection could not be reached")

func newKeeper() *keeper { return &keeper{} }

func (k *keeper) refusing() *keeper {
	k.failProtect = true

	return k
}

func (k *keeper) undecryptable() *keeper {
	k.failUnprotect = true

	return k
}

// nonceLen and sumLen are the two headers every blob carries: the randomness
// that makes two blobs of one plaintext differ, and the integrity check that
// makes a damaged one fail.
const (
	nonceLen = 4
	sumLen   = 4
)

var _ protect.Protector = (*keeper)(nil)

func (k *keeper) Protect(ctx context.Context, descriptor string, plaintext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	k.mu.Lock()
	k.descs = append(k.descs, descriptor)
	fail := k.failProtect
	k.mu.Unlock()

	if fail {
		return nil, errKeeper
	}

	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	out := append([]byte{}, nonce...)
	out = append(out, checksum(plaintext)...)

	return append(out, masked(nonce, plaintext)...), nil
}

func (k *keeper) Unprotect(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	k.mu.Lock()
	fail := k.failUnprotect
	k.mu.Unlock()

	if fail {
		return nil, errKeeper
	}

	if len(ciphertext) < nonceLen+sumLen {
		return nil, errKeeper
	}

	nonce, sum := ciphertext[:nonceLen], ciphertext[nonceLen:nonceLen+sumLen]

	plain := masked(nonce, ciphertext[nonceLen+sumLen:])
	if !bytes.Equal(sum, checksum(plain)) {
		return nil, errKeeper
	}

	return plain, nil
}

// masked is the reversible transform, and it is its own inverse.
func masked(nonce, in []byte) []byte {
	out := make([]byte, len(in))
	for i, b := range in {
		out[i] = b ^ nonce[i%len(nonce)]
	}

	return out
}

// checksum is what tells a blob this keeper wrote from one it did not.
func checksum(b []byte) []byte {
	h := fnv.New32a()
	h.Write(b)

	return h.Sum(nil)
}
