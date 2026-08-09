package protect

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
)

// This file is this package's half of ADR-0021, and it is the half the ADR was
// amended for.
//
// A node tag is an optional annotation, so driver/yaml discards the second
// result of [ferry.AddressSet.Extension] and a registry that was never given its
// declaration loses nothing it was promised. This consumer is the opposite case
// the amendment names: it acts on the *absence* of a word, by writing a value in
// the clear exactly where no field carried one. So a forgotten
// ferry.WithTagKeys(protect.Extension()) would read here as a struct holding no
// secrets, and every marked value would be written in plaintext with nothing
// saying so. The bool is what turns that into a refusal at Bind.
//
// Two things it deliberately is not.
//
// It is not an address list. ADR-0021's own argument against a side table
// applies with the sharpest possible consequence: a list keyed by address drifts
// on the first ferry tag rename, and a field *added* to the struct and not to
// the list is written in plaintext with nothing anywhere able to detect it.
//
// It is not a spelling (ADR-0018). Protection is randomised, which breaks the
// determinism law, and it is a syscall, which breaks the purity law; the seam
// has a kind gate and no address gate; and a spelling is declared through one
// driver's Option set, which would destroy the whole value of composing with
// every plane. Nor is it a codec: a codec sits above the plane boundary, so a
// Secret[T] would emit ciphertext into every sink, including a YAML dump and a
// test fixture, rather than into the one store the descriptor is scoped to.

// ExtensionKey is the struct tag key [Extension] declares.
const ExtensionKey = "protect"

// secretWord is the one word of the vocabulary, and it takes no value.
const secretWord = "secret"

// Extension declares this package's struct tag key, for a registry to read
// beside ferry's own.
//
//	var Registry = ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))
//
//	type Config struct {
//	    Token string `ferry:"token" protect:"secret"`
//	}
//
// The vocabulary is one word. secret marks the value at that address as one to
// encrypt on the way into the plane and decrypt on the way out, and it takes no
// value after it.
//
// There is no word that takes protection away, and there must never be one. Two
// sources of truth that can contradict each other about whether a value is a
// secret can only be resolved in one direction safely, so this vocabulary
// resolves it by having nothing to contradict: a field is marked or it is not.
//
// It belongs on a field the plane holds one value at. A struct, a slice or a map
// is a place rather than a value, and marking one is refused at Bind naming the
// address, because what would be encrypted there is not a thing the plane holds.
//
// Handing this to the registry is not optional when [FromTags] is the selector.
// A registry that was not given it parses the key as another library's business,
// every tag is inert, and [FromTags] refuses at Bind rather than writing the
// values in the clear.
func Extension() ferry.KeyExtension {
	return ferry.KeyExtension{
		TagKey: ExtensionKey,
		Words:  []ferry.Word{{Name: secretWord}},
	}
}

// Selector says which of a schema's addresses hold secrets.
//
// [FromTags] is the one this package ships and the only implementation there is:
// the interface is closed, so a selector is always something whose refusals this
// package can make at Bind.
type Selector interface {
	// selected is the addresses to protect, read once at each Bind. Its being
	// unexported is what seals the interface.
	selected(addrs *ferry.AddressSet) (map[ferry.Path]bool, error)
}

// FromTags selects the addresses whose field carried protect:"secret".
//
//	reg := ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))
//	src := protect.Over(store, protect.CurrentUser, protect.FromTags())
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(reg))
//
// The declaration is the struct author's, which is where it belongs: a field
// that holds a credential holds one on every plane and in every deployment, and
// an address survives a rename of the ferry tag it was minted from because the
// mark travels on the same field.
//
// Three things it refuses, all of them at Bind and before any read or write:
//
//   - a schema whose registry was never given [Extension], with [ErrNotDeclared].
//     That is a forgotten line in the caller's registry, and it would otherwise
//     be indistinguishable from a struct that marks nothing - which is to say,
//     every marked value written in the clear.
//   - the address of a struct, a slice or a map, naming the address.
//   - a word the vocabulary does not have, naming the address.
//
// A registry that was given the declaration and a struct that marks nothing is
// not a refusal. It is a schema with no secrets in it, which is a legitimate
// thing to run a protected source over, and it is exactly the case the second
// result of [ferry.AddressSet.Extension] exists to tell apart from the first.
func FromTags() Selector { return fromTags{} }

// fromTags is [FromTags]'s implementation, and it holds nothing: the whole of
// the selection is a property of the address set it is handed.
type fromTags struct{}

// selected reads this package's own view of the address set, once per Bind.
func (fromTags) selected(addrs *ferry.AddressSet) (map[ferry.Path]bool, error) {
	view, declared := addrs.Extension(ExtensionKey)
	if !declared {
		return nil, notDeclared()
	}

	leaves := leafPaths(addrs)
	out := make(map[ferry.Path]bool, len(view))

	for addr, words := range view {
		if err := checkMark(addr, words, leaves); err != nil {
			return nil, err
		}

		out[addr] = true
	}

	return out, nil
}

// checkMark holds one declaration to what this package can act on: the one word
// it declared, at an address the plane holds a value at.
//
// It is driver/yaml's checkNodeAddr, copied rather than shared, because ADR-0002
// forbids the internal module that would carry it and the two drivers refuse
// different things at the same seam.
func checkMark(addr ferry.Path, words map[string]string, leaves map[ferry.Path]bool) error {
	if !leaves[addr] {
		return ferry.ErrorAt(addr, fmt.Errorf("%w: protect:%q marks one value as a secret and this key holds a "+
			"struct, a list or a map: mark the fields under it, whose keys the plane holds one value at",
			ferry.ErrPlane, secretWord))
	}

	if err := checkWords(words); err != nil {
		return ferry.ErrorAt(addr, err)
	}

	return nil
}

// checkWords refuses a tag this package cannot act on.
//
// Core validates a declared key's words against the declaration before this is
// reached, so the vocabulary is already closed by the time the view is built and
// this cannot fire through a compiled schema. It is written anyway, and tested
// directly, because the alternative is a package that would act on whatever
// arrived if the declaration and this file ever drifted apart - and what it
// would act on is which values get encrypted (ADR-0021: acting is the consumer's
// and so is the proof).
func checkWords(words map[string]string) error {
	if _, marked := words[secretWord]; marked && len(words) == 1 {
		return nil
	}

	return fmt.Errorf("%w: the %s tag here carries %s, and the whole of its vocabulary is %q, written with no "+
		"value after it", ferry.ErrPlane, ExtensionKey, carried(words), secretWord)
}

// carried names what a tag held, for the refusal above.
func carried(words map[string]string) string {
	if len(words) == 0 {
		return "no word at all"
	}

	return strings.Join(slices.Sorted(maps.Keys(words)), ", ")
}

// leafPaths is every address in the set that holds a value.
//
// The three address kinds partition the set (ADR-0016), so this is what says
// that a mark sits somewhere a value can be encrypted: a section's or a
// composite's own address is a place, and the values are at the leaves under it.
func leafPaths(addrs *ferry.AddressSet) map[ferry.Path]bool {
	out := make(map[ferry.Path]bool, addrs.Len())

	for m := range addrs.Seq() {
		if _, ok := m.(ferry.LeafAddr); ok {
			out[m.Path()] = true
		}
	}

	return out
}

// notDeclared is the fail-open refusal, and it names the line that is missing
// rather than the values it would have leaked.
func notDeclared() error {
	return fmt.Errorf("%w: %w: this schema's registry was built without "+
		"ferry.WithTagKeys(protect.Extension()), so every %s:%q on it was ignored and these values would "+
		"be written in plaintext: add the declaration, or select with something other than protect.FromTags()",
		ferry.ErrPlane, ErrNotDeclared, ExtensionKey, secretWord)
}
