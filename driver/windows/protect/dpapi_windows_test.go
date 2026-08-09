//go:build windows

package protect_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
)

// This file is the only place the real DPAPI-NG is ever constructed.
//
// Everywhere else this package's tests hand a protector in with protect.Using,
// which is what makes them run on every operating system - and which means the
// implementation behind //go:build windows is reached by nothing at all. So these
// tests build both halves with no Using at all, which is the one way to make
// [protect.Over] resolve the machine's own protection, and then assert a genuine
// round trip through it: protect, unprotect, the same bytes back.
//
// # Which descriptor, and why not [protect.LocalSystem]
//
// A rule string naming a security principal - SID= or SDDL= - is resolved by
// Active Directory's key distribution service, so it needs a domain controller
// the machine can reach. A standalone runner has none, and NCryptProtectSecret
// answers NTE_ENCRYPTION_FAILURE. protect.LocalSystem is one of those rules, and
// it would fail here for that reason on top of the reason it always would: a
// value protected for S-1-5-18 can only be unprotected by a process running as
// SYSTEM.
//
// So the round trip runs under the LOCAL= rules, which the machine resolves for
// itself and which therefore work on every Windows machine there is. The SID
// case has a test of its own, and that one is allowed to skip.

// requireDPAPI skips where there is no DPAPI-NG to reach at all. Anything past
// ncrypt.dll being loadable and exporting the call is a failure rather than a
// skip, because a skip on the failure this file exists to catch would make the
// whole file decorative.
func requireDPAPI(t *testing.T) {
	t.Helper()

	dll := windows.NewLazySystemDLL("ncrypt.dll")
	if err := dll.Load(); err != nil {
		t.Skipf("this machine has no ncrypt.dll, so there is no DPAPI-NG here: %v", err)
	}

	if err := dll.NewProc("NCryptProtectSecret").Find(); err != nil {
		t.Skipf("the ncrypt.dll here exports no NCryptProtectSecret: %v", err)
	}
}

// realHalves is both halves over one store with no protect.Using, so the
// protection is the machine's own.
func realHalves(s *store, d protect.Descriptor) (ferry.Source, ferry.Sink) {
	return protect.Over(storeSource{s: s}, d, protect.FromTags()),
		protect.OverSink(storeSink{s: s}, d, protect.FromTags())
}

func TestDPAPINGRoundTripsEveryKindItIsHanded(t *testing.T) {
	t.Parallel()
	requireDPAPI(t)

	type kinds struct {
		Text  string  `ferry:"text" protect:"secret"`
		Count int     `ferry:"count" protect:"secret"`
		Ratio float64 `ferry:"ratio" protect:"secret"`
		On    bool    `ferry:"on" protect:"secret"`
		Raw   []byte  `ferry:"raw" protect:"secret"`
		Open  string  `ferry:"open"`
	}

	// Both descriptors a machine resolves for itself, because both are shipped
	// as constants and neither has ever been executed anywhere.
	for _, d := range []protect.Descriptor{protect.CurrentUser, protect.LocalMachine} {
		t.Run(string(d), func(t *testing.T) {
			t.Parallel()

			s := newStore()
			src, dst := realHalves(s, d)
			want := kinds{
				Text:  "s3cr3t",
				Count: 7,
				Ratio: 3.5,
				On:    true,
				Raw:   []byte{0x00, 0x01, 0xfe, 0xff},
				Open:  "public",
			}

			if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
				t.Fatalf("dumping through the machine's own DPAPI-NG under %q: %v", d, err)
			}

			if s.holds("s3cr3t") {
				t.Fatalf("the plane holds the secret in the clear: %v", s.at(ferry.At("text")))
			}

			got, err := ferry.Load[kinds](t.Context(), src, ferry.WithRegistry(declaring()))
			if err != nil {
				t.Fatalf("loading back through the machine's own DPAPI-NG under %q: %v", d, err)
			}

			if got.Text != want.Text || got.Count != want.Count || got.Ratio != want.Ratio || got.On != want.On {
				t.Errorf("it came back as %+v, want %+v: the kind travels inside the ciphertext",
					got, want)
			}

			if string(got.Raw) != string(want.Raw) {
				t.Errorf("the bytes came back as %v, want %v, byte for byte", got.Raw, want.Raw)
			}

			if got.Open != want.Open {
				t.Errorf("the unmarked address came back as %q, want %q", got.Open, want.Open)
			}
		})
	}
}

// TestDPAPINGWithASIDDescriptorNeedsADomain is the one test here allowed to
// skip, and what it is really asserting is the refusal's text.
//
// On a domain-joined machine the process's own user SID round trips, and this
// asserts that. On a standalone machine nothing can make it, so the assertion
// moves to the failure: NTE_ENCRYPTION_FAILURE has to be named and the reason
// has to be the directory rather than "could not be encrypted", or an operator
// hitting this on their own machine has nothing to search for.
func TestDPAPINGWithASIDDescriptorNeedsADomain(t *testing.T) {
	t.Parallel()
	requireDPAPI(t)

	type one struct {
		Text string `ferry:"text" protect:"secret"`
	}

	s := newStore()
	src, dst := realHalves(s, currentUserSID(t))

	err := ferry.Dump(t.Context(), one{Text: "s3cr3t"}, dst, ferry.WithRegistry(declaring()))
	if err != nil {
		assertDirectoryRefusal(t, err)
		t.Skipf("this machine cannot resolve a SID descriptor, which is a machine with no Active Directory "+
			"key distribution service to reach: %v", err)
	}

	got, err := ferry.Load[one](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading back what a SID descriptor protected: %v", err)
	}

	if got.Text != "s3cr3t" {
		t.Errorf("it came back as %q, want %q", got.Text, "s3cr3t")
	}
}

// currentUserSID is the SID rule for whoever this process is running as, which
// is the one principal a test could both protect for and unprotect as.
func currentUserSID(t *testing.T) protect.Descriptor {
	t.Helper()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Skipf("this process token has no user on it, so there is no principal to round trip as: %v", err)
	}

	return protect.Descriptor("SID=" + user.User.Sid.String())
}

// assertDirectoryRefusal holds the refusal to what an operator needs from it.
func assertDirectoryRefusal(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, protect.ErrCiphertext) {
		t.Fatalf("a descriptor this machine cannot resolve failed with %v, want it under %v",
			err, protect.ErrCiphertext)
	}

	for _, want := range []string{"NTE_ENCRYPTION_FAILURE", "0x80090034", "Active Directory", "LOCAL=user"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q, and it has to: %v", want, err)
		}
	}
}

func TestDPAPINGProtectsTheSamePlaintextToTwoDifferentBlobs(t *testing.T) {
	t.Parallel()
	requireDPAPI(t)

	type one struct {
		Text string `ferry:"text" protect:"secret"`
	}

	first, second := newStore(), newStore()
	want := one{Text: "s3cr3t"}

	for _, s := range []*store{first, second} {
		_, dst := realHalves(s, protect.CurrentUser)
		if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
			t.Fatalf("dumping through the machine's own DPAPI-NG: %v", err)
		}
	}

	a, b := rendered(first.at(ferry.At("text"))), rendered(second.at(ferry.At("text")))
	if a == b {
		t.Errorf("two protections of one plaintext gave the identical blob %q: DPAPI-NG randomises, and "+
			"nothing here may be built on two ciphertexts being equal", a)
	}
}

func TestDPAPINGRefusesADamagedOrForeignBlobRatherThanDecryptingToRubbish(t *testing.T) {
	t.Parallel()
	requireDPAPI(t)

	type one struct {
		Text string `ferry:"text" protect:"secret"`
	}

	// One real blob, made by the real thing, so that "damaged" means damaged
	// rather than never protected at all.
	staged := newStore()

	_, dst := realHalves(staged, protect.CurrentUser)
	if err := ferry.Dump(t.Context(), one{Text: "s3cr3t"}, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("staging a real blob: %v", err)
	}

	good, ok := strings.CutPrefix(rendered(staged.at(ferry.At("text"))), "ferry-protect:1:")
	if !ok {
		t.Fatalf("the staged value is not one this package wrote: %q", good)
	}

	for _, tc := range []struct {
		name string
		blob string
	}{
		{"a blob with a byte flipped in it", flipped(good)},
		{"a blob from nowhere at all", encoded([]byte("not a DPAPI-NG blob, not even close"))},
		{"a blob that is not base64", "!!!! not base64 !!!!"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newStore()
			s.seed(ferry.At("text"), ferry.String("ferry-protect:1:"+tc.blob))

			src, _ := realHalves(s, protect.CurrentUser)

			got, err := ferry.Load[one](t.Context(), src, ferry.WithRegistry(declaring()))
			if !errors.Is(err, protect.ErrCiphertext) || !errors.Is(err, ferry.ErrPlane) {
				t.Fatalf("it loaded as %+v with %v, want a refusal under %v: a value that says it was "+
					"protected and cannot be read back is never a plaintext quietly passed through",
					got, err, protect.ErrCiphertext)
			}
		})
	}
}

// flipped damages one base64 payload in a way that survives the decoding, so that
// what fails is the decryption and not this package's own envelope.
func flipped(blob string) string {
	raw, err := base64.RawStdEncoding.DecodeString(blob)
	if err != nil || len(raw) == 0 {
		return blob
	}

	raw[len(raw)/2] ^= 0xff

	return encoded(raw)
}
