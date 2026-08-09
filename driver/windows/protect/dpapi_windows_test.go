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
// The descriptor is the current process token's user SID and not
// [protect.LocalSystem]. A value protected for S-1-5-18 can only be unprotected
// by a process running as SYSTEM, so LocalSystem is exactly the descriptor that
// does not round trip on a test runner. The user SID is the principal this
// process already is.

// userDescriptor is the protection descriptor for whoever this process is running
// as, which is the one principal a test can both protect for and unprotect as.
func userDescriptor(t *testing.T) protect.Descriptor {
	t.Helper()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Skipf("this process token has no user on it, so there is no principal to round trip as: %v", err)
	}

	return protect.Descriptor("SID=" + user.User.Sid.String())
}

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
func realHalves(t *testing.T, s *store) (ferry.Source, ferry.Sink) {
	t.Helper()

	d := userDescriptor(t)

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

	s := newStore()
	src, dst := realHalves(t, s)
	want := kinds{
		Text:  "s3cr3t",
		Count: 7,
		Ratio: 3.5,
		On:    true,
		Raw:   []byte{0x00, 0x01, 0xfe, 0xff},
		Open:  "public",
	}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("dumping through the machine's own DPAPI-NG: %v", err)
	}

	if s.holds("s3cr3t") {
		t.Fatalf("the plane holds the secret in the clear: %v", s.at(ferry.At("text")))
	}

	got, err := ferry.Load[kinds](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading back through the machine's own DPAPI-NG: %v", err)
	}

	if got.Text != want.Text || got.Count != want.Count || got.Ratio != want.Ratio || got.On != want.On {
		t.Errorf("it came back as %+v, want %+v: the kind travels inside the ciphertext", got, want)
	}

	if string(got.Raw) != string(want.Raw) {
		t.Errorf("the bytes came back as %v, want %v, byte for byte", got.Raw, want.Raw)
	}

	if got.Open != want.Open {
		t.Errorf("the unmarked address came back as %q, want %q", got.Open, want.Open)
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
		_, dst := realHalves(t, s)
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

	_, dst := realHalves(t, staged)
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

			src, _ := realHalves(t, s)

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
