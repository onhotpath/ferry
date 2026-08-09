package protect_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
)

// The schema every behaviour test here is written over: one marked leaf, one
// unmarked leaf beside it, and one unmarked leaf outside the section, so that
// "only the marked address changed" is a thing a test can assert.
type conf struct {
	Auth  auth   `ferry:"auth"`
	Plain string `ferry:"plain"`
}

type auth struct {
	Token string `ferry:"token" protect:"secret"`
	User  string `ferry:"user"`
}

// The addresses those fields land at.
var (
	addrToken = ferry.At("auth", "token")
	addrUser  = ferry.At("auth", "user")
)

// declaring is a registry that was given this package's tag key, which is what
// every caller of [protect.FromTags] has to build.
func declaring() *ferry.Registry {
	return ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))
}

// halves is one store with both decorated halves over it, built the way this
// package's documentation tells a caller to build them: one descriptor, one
// selector, one protector, both halves.
func halves(s *store, k protect.Protector) (ferry.Source, ferry.Sink) {
	return protect.Over(storeSource{s: s}, protect.LocalSystem, protect.FromTags(), protect.Using(k)),
		protect.OverSink(storeSink{s: s}, protect.LocalSystem, protect.FromTags(), protect.Using(k))
}

func TestAMarkedAddressIsCiphertextInTheStoreAndTheValueAgainOnTheWayBack(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)
	want := conf{Auth: auth{Token: "s3cr3t", User: "bob"}, Plain: "public"}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("dumping through the protected sink: %v", err)
	}

	if s.holds("s3cr3t") {
		t.Errorf("the plane holds the secret in the clear: %v", s.at(addrToken))
	}

	if u := rendered(s.at(addrUser)); u != "bob" {
		t.Errorf("the unmarked address beside it reads %q, want %q: only a marked address changes", u, "bob")
	}

	got, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading back through the protected source: %v", err)
	}

	if got != want {
		t.Errorf("the round trip gave %+v, want %+v", got, want)
	}

	if len(k.descs) != 1 || k.descs[0] != string(protect.LocalSystem) {
		t.Errorf("the value was protected under %v, want one call under %q", k.descs, protect.LocalSystem)
	}
}

func TestProtectingTwiceGivesTwoCiphertextsAndOneValue(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)
	want := conf{Auth: auth{Token: "s3cr3t"}}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("first dump: %v", err)
	}

	first := rendered(s.at(addrToken))

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("second dump: %v", err)
	}

	if second := rendered(s.at(addrToken)); second == first {
		t.Errorf("two saves of one secret stored identical bytes, and protection is randomised: %q", second)
	}

	got, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil || got != want {
		t.Errorf("the second ciphertext loaded as %+v, %v, want %+v", got, err, want)
	}
}

func TestEveryKindAMarkedAddressCanHoldSurvivesTheRoundTrip(t *testing.T) {
	t.Parallel()

	type kinds struct {
		Text  string  `ferry:"text" protect:"secret"`
		Count int     `ferry:"count" protect:"secret"`
		On    bool    `ferry:"on" protect:"secret"`
		Raw   []byte  `ferry:"raw" protect:"secret"`
		Maybe *string `ferry:"maybe" protect:"secret"`
	}

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)
	want := kinds{Text: "t", Count: -42, On: true, Raw: []byte{0, 255, 65}}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("dumping every kind: %v", err)
	}

	if s.at(ferry.At("maybe")).Kind() != ferry.KindNull {
		t.Errorf("a nil pointer at a marked address was stored as %s, want a null: there is nothing at a null "+
			"to encrypt", s.at(ferry.At("maybe")).Kind())
	}

	got, err := ferry.Load[kinds](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading every kind back: %v", err)
	}

	if got.Text != want.Text || got.Count != want.Count || got.On != want.On ||
		string(got.Raw) != string(want.Raw) || got.Maybe != nil {
		t.Errorf("the round trip gave %+v, want %+v: the kind travels inside the ciphertext", got, want)
	}
}

func TestAScheduleWhoseRegistryNeverDeclaredTheKeyIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)

	err := ferry.Dump(t.Context(), conf{Auth: auth{Token: "s3cr3t"}}, dst)
	if !errors.Is(err, protect.ErrNotDeclared) || !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("a dump under a registry that never declared the key failed with %v, want %v under %v",
			err, protect.ErrNotDeclared, ferry.ErrPlane)
	}

	if !s.empty() {
		t.Error("the refusal left something in the plane: it lands at Bind, before any write")
	}

	if _, err := ferry.Load[conf](t.Context(), src); !errors.Is(err, protect.ErrNotDeclared) {
		t.Errorf("a load under the same registry failed with %v, want %v", err, protect.ErrNotDeclared)
	}
}

func TestADeclaredKeyNoFieldCarriesBindsCleanAndChangesNothing(t *testing.T) {
	t.Parallel()

	type nothingMarked struct {
		Host string `ferry:"host"`
	}

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)
	want := nothingMarked{Host: "h"}

	if err := ferry.Dump(t.Context(), want, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("a schema that marks nothing is a schema with no secrets in it, not a refusal: %v", err)
	}

	if h := rendered(s.at(ferry.At("host"))); h != "h" {
		t.Errorf("the plane holds %q, want %q: a schema marking nothing is written exactly as it would be", h, "h")
	}

	if got, err := ferry.Load[nothingMarked](t.Context(), src, ferry.WithRegistry(declaring())); err != nil ||
		got != want {
		t.Errorf("loading it back gave %+v, %v, want %+v", got, err, want)
	}
}

func TestAMarkAtAContainerIsRefusedAtBindNamingTheAddress(t *testing.T) {
	t.Parallel()

	type marked struct {
		Auth auth              `ferry:"auth" protect:"secret"`
		Tags map[string]string `ferry:"tags"`
	}

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)

	err := ferry.Dump(t.Context(), marked{}, dst, ferry.WithRegistry(declaring()))
	if !errors.Is(err, ferry.ErrPlane) || !strings.Contains(err.Error(), "/auth") {
		t.Errorf("marking a struct failed with %v, want a plane refusal naming /auth", err)
	}

	if !s.empty() {
		t.Error("the refusal left something in the plane: it lands at Bind, before any write")
	}

	if _, err := ferry.Load[marked](t.Context(), src, ferry.WithRegistry(declaring())); !errors.Is(err,
		ferry.ErrPlane) {
		t.Errorf("the same schema on the read side failed with %v, want a plane refusal", err)
	}
}

func TestAPlaintextValueLoadsAsItStandsAndTheNextSaveWritesItProtected(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()
	s.seed(addrToken, ferry.String("written-before-any-of-this"))
	s.seed(addrUser, ferry.String("bob"))

	src, dst := halves(s, k)

	got, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading a store that predates this decorator: %v", err)
	}

	if got.Auth.Token != "written-before-any-of-this" {
		t.Fatalf("the plaintext read back as %q, and a value nothing protected is read as it stands",
			got.Auth.Token)
	}

	if err := ferry.Dump(t.Context(), got, dst, ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("saving it back: %v", err)
	}

	if s.holds("written-before-any-of-this") {
		t.Error("the next save left the value in the clear, and migration is the whole point of reading it")
	}
}

func TestAValueThatSaysItIsProtectedAndCannotBeReadBackIsLoud(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		stored string
		keeper *keeper
	}{
		{"a payload that is not base64", "ferry-protect:1:not base64 at all", newKeeper()},
		{"a blob from somewhere else", "ferry-protect:1:AAAAAAAAAAAAAAAA", newKeeper()},
		{"protection that cannot decrypt", "ferry-protect:1:AAAAAAAAAAAAAAAA", newKeeper().undecryptable()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			loudAt(t, tc.stored, tc.keeper)
		})
	}
}

// loudAt loads one staged store and requires the marked address to have failed,
// naming itself.
func loudAt(t *testing.T, storedText string, k *keeper) {
	t.Helper()

	s := newStore().seed(addrToken, ferry.String(storedText))
	src, _ := halves(s, k)

	_, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if !errors.Is(err, protect.ErrCiphertext) || !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("the load failed with %v, want %v under %v: a value that says it is protected and cannot be "+
			"read back is never a plaintext quietly passed through", err, protect.ErrCiphertext, ferry.ErrPlane)
	}

	if !strings.Contains(err.Error(), addrToken.String()) {
		t.Errorf("the refusal reads %v and does not name %s", err, addrToken)
	}
}

func TestAValueThatDecryptsToSomethingThisPackageNeverWroteIsLoud(t *testing.T) {
	t.Parallel()

	k := newKeeper()

	for _, tc := range []struct {
		name  string
		plain []byte
	}{
		{"nothing at all", []byte{}},
		{"a kind tag this package does not write", []byte("?payload")},
		{"a boolean that is not one", []byte("bmaybe")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			blob, err := k.Protect(t.Context(), string(protect.LocalSystem), tc.plain)
			if err != nil {
				t.Fatalf("staging the blob: %v", err)
			}

			loudAt(t, "ferry-protect:1:"+encoded(blob), k)
		})
	}
}

func TestProtectionThatCannotEncryptFailsTheDumpRatherThanWritingTheValue(t *testing.T) {
	t.Parallel()

	s := newStore()
	_, dst := halves(s, newKeeper().refusing())

	err := ferry.Dump(t.Context(), conf{Auth: auth{Token: "s3cr3t"}}, dst, ferry.WithRegistry(declaring()))
	if !errors.Is(err, protect.ErrCiphertext) || !errors.Is(err, errKeeper) {
		t.Errorf("the dump failed with %v, want %v carrying %v", err, protect.ErrCiphertext, errKeeper)
	}

	if s.holds("s3cr3t") {
		t.Error("a failed encryption left the value in the clear, which is the one thing it must never do")
	}
}

func TestASchemaHoldingASliceLoadsAndSavesThroughTheDecorator(t *testing.T) {
	t.Parallel()

	type listed struct {
		Tokens []string `ferry:"tokens"`
		Token  string   `ferry:"token" protect:"secret"`
	}

	s, k := newStore(), newKeeper()
	src, dst := halves(s, k)

	if err := ferry.Dump(t.Context(), listed{Tokens: []string{"a", "b", "c"}, Token: "s3cr3t"}, dst,
		ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("a slice needs the sink's Unsetter and the source's Enumerator, both of them the plane's: %v", err)
	}

	if err := ferry.Dump(t.Context(), listed{Tokens: []string{"a"}, Token: "s3cr3t"}, dst,
		ferry.WithRegistry(declaring())); err != nil {
		t.Fatalf("the second dump: %v", err)
	}

	got, err := ferry.Load[listed](t.Context(), src, ferry.WithRegistry(declaring()))
	if err != nil {
		t.Fatalf("loading the slice back: %v", err)
	}

	if len(got.Tokens) != 1 || got.Tokens[0] != "a" || got.Token != "s3cr3t" {
		t.Errorf("the shorter list came back as %+v: a dropped Unsetter is a list that never shrinks", got)
	}
}

func TestADecoratorBuiltWithoutProtectionRefusesEverywhereButWindows(t *testing.T) {
	t.Parallel()

	if runningOnWindows {
		t.Skip("DPAPI-NG is there, so there is no absence to refuse")
	}

	s := newStore()
	src := protect.Over(storeSource{s: s}, protect.LocalSystem, protect.FromTags())

	_, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring()))
	if !errors.Is(err, protect.ErrNoProtection) || !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("a source built with no protector failed with %v, want %v under %v",
			err, protect.ErrNoProtection, ferry.ErrPlane)
	}
}

func TestADecoratorThatCannotBeBuiltIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	s, k := newStore(), newKeeper()

	for _, tc := range []struct {
		name string
		src  ferry.Source
	}{
		{
			"no plane to decorate",
			protect.Over(nil, protect.LocalSystem, protect.FromTags(), protect.Using(k)),
		},
		{
			"no selector",
			protect.Over(storeSource{s: s}, protect.LocalSystem, nil, protect.Using(k)),
		},
		{
			"an empty descriptor",
			protect.Over(storeSource{s: s}, "", protect.FromTags(), protect.Using(k)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ferry.Load[conf](t.Context(), tc.src, ferry.WithRegistry(declaring()))
			if !errors.Is(err, protect.ErrOption) || !errors.Is(err, ferry.ErrPlane) {
				t.Errorf("it failed with %v, want %v under %v", err, protect.ErrOption, ferry.ErrPlane)
			}
		})
	}
}

func TestASinkThatCannotBeBuiltIsRefusedAtBind(t *testing.T) {
	t.Parallel()

	dst := protect.OverSink(nil, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))

	err := ferry.Dump(t.Context(), conf{}, dst, ferry.WithRegistry(declaring()))
	if !errors.Is(err, protect.ErrOption) {
		t.Errorf("dumping through a sink with no plane under it failed with %v, want %v", err, protect.ErrOption)
	}
}

func TestARefusalFromThePlaneUnderneathReachesTheCaller(t *testing.T) {
	t.Parallel()

	src := protect.Over(refusingSource{}, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))
	dst := protect.OverSink(refusingSink{}, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))

	if _, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring())); !errors.Is(err, errRefused) {
		t.Errorf("the source's own Bind refusal reached the caller as %v, want %v", err, errRefused)
	}

	if err := ferry.Dump(t.Context(), conf{}, dst, ferry.WithRegistry(declaring())); !errors.Is(err, errRefused) {
		t.Errorf("the sink's own Bind refusal reached the caller as %v, want %v", err, errRefused)
	}
}

func TestAFailureFromThePlaneUnderneathReachesTheCaller(t *testing.T) {
	t.Parallel()

	src := protect.Over(failingSource{}, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))
	dst := protect.OverSink(failingSink{}, protect.LocalSystem, protect.FromTags(), protect.Using(newKeeper()))

	if _, err := ferry.Load[conf](t.Context(), src, ferry.WithRegistry(declaring())); !errors.Is(err, errRefused) {
		t.Errorf("the source's open failure reached the caller as %v, want %v", err, errRefused)
	}

	if err := ferry.Dump(t.Context(), conf{}, dst, ferry.WithRegistry(declaring())); !errors.Is(err, errRefused) {
		t.Errorf("the sink's open failure reached the caller as %v, want %v", err, errRefused)
	}
}

// errRefused is what the two planes below refuse and fail with.
var errRefused = errors.New("under: this plane refuses")

type refusingSource struct{}

func (refusingSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) { return nil, errRefused }

type refusingSink struct{}

func (refusingSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) { return nil, errRefused }

type failingSource struct{}

func (failingSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return nil, errRefused }, nil
}

type failingSink struct{}

func (failingSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return nil, errRefused }, nil
}
