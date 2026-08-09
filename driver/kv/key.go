package kv

import (
	"fmt"
	"strings"

	"github.com/onhotpath/ferry"
)

// driverName is what this driver calls itself in a refusal core prints, so a
// schema that is fine on one plane and impossible on this one is reported as
// this plane's problem rather than as ferry's.
const driverName = "kv"

// separator is how a store spells the step from one folder to the next, and it
// is not configurable.
//
// It is the store's own syntax rather than a taste: a Consul-shaped key space
// is a slash-separated hierarchy, and a driver that joined with something else
// would be writing keys that the store's own list, prefix and ACL rules cannot
// see the structure of.
const separator = "/"

// keyFunc is this driver's mapping from a ferry address to a store key: the
// prefix's segments, then the address's own, joined with a separator.
//
// It is a [ferry.KeyFunc] handed to [ferry.NewKeys], which is what runs
// ADR-0003's two obligations over it - legality per address, injectivity over
// the set - once per schema and before any backend call. Nothing here checks
// injectivity, because one call cannot see a set.
//
// # What is refused, and why no transformation rescues it
//
// ADR-0003 expects a key function to transform segment text rather than reject
// it, on the argument that a many-to-one transformation is safe precisely
// because the injectivity check catches what it folds together. Two segments
// have no image under any transformation here, so they are refused instead.
//
// An empty segment has no name in the store: two separators with nothing
// between them are one separator, so the address would be written at a key that
// is some other address's.
//
// A segment holding a separator is worse, because it succeeds. The map key
// "a/b" under /m would be stored at "m/a/b", which the store reads back as a
// folder "a" holding "b", so a load would enumerate a member called "a" and
// find nothing at it - a dumped entry silently replaced by a different, empty
// one. That is data loss with no error anywhere, which is what ADR-0001 rules
// out, and there is no escape to map it onto: any byte the escape used would be
// a byte a segment is entitled to contain.
//
// # The root is a key under the prefix or it is nothing
//
// A schema whose root is a single value has the empty path as its only address,
// and there is no key it can take on its own: this driver's key for the empty
// path would be the prefix, which is the folder every other address is written
// under, so a store would end up holding a value on an interior node. [RootKey]
// names an ordinary key under the prefix for it instead, and without one the
// address is refused on both the source and the sink side (#334).
//
// # What it deliberately does not refuse
//
// An Index segment is written as its own base-10 text, so /tags#0 is "tags/0",
// which is the spelling an operator filling the store by hand would write. The
// cost is that the store's key space carries no segment kind, so /tags#0 and a
// map key "0" under /tags are one key. That is not silent: they are two members
// of one address set, and the injectivity check refuses a schema holding both,
// before any backend call. [reader.Children] documents the one residue.
func keyFunc(prefix []string, root string) ferry.KeyFunc {
	return func(addr ferry.Path) (string, error) {
		parts := make([]string, 0, len(prefix)+segmentsHint)
		parts = append(parts, prefix...)

		// The empty path is the schema's own root, which has no segment of its
		// own to be named by, so the loop below would skip every check and hand
		// back the prefix itself - the folder every other address is written
		// under. A comparison is what asks, because Path is comparable.
		if addr == (ferry.Path{}) {
			return rootName(parts, root)
		}

		for seg := range addr.Segments() {
			text := seg.Text()
			if err := nameable(text); err != nil {
				return "", err
			}

			parts = append(parts, text)
		}

		return strings.Join(parts, separator), nil
	}
}

// segmentsHint is how many segments an ordinary address is guessed to hold, so
// that a key needs one allocation rather than three.
const segmentsHint = 4

// nameable reports whether the store can name a segment holding this text.
//
// The message is the driver's own and is printed under ferry's wrapper, so it
// says what to do about it. It names no value the plane supplied: the text it
// quotes came from the schema or from a map key ferry is writing, and the
// address it belongs to is already in the line ferry prints (ADR-0011).
func nameable(text string) error {
	switch {
	case text == "":
		return fmt.Errorf("%w: this key has an empty part, and a key-value store has no name for one: "+
			"two separators with nothing between them are one separator, so it would be written at "+
			"another key", ferry.ErrPlane)
	case strings.Contains(text, separator):
		return fmt.Errorf("%w: this key has a part containing %q, and the store reads that as "+
			"another step in its hierarchy: it would be written under a folder of that name and read back as a "+
			"different, empty member", ferry.ErrPlane, separator)
	default:
		return nil
	}
}

// rootName is the key the schema's own root value is written at: the prefix's
// segments and the name [RootKey] gave it, so that it is an ordinary key under
// the prefix and never the prefix itself (#334).
//
// parts already holds the prefix, and it is the caller's own slice with room to
// spare, so the name is appended to it rather than joined a second time.
func rootName(parts []string, root string) (string, error) {
	if err := nameableRoot(root); err != nil {
		return "", err
	}

	return strings.Join(append(parts, root), separator), nil
}

// nameableRoot reports whether the store can name the schema's root at this
// name, which is the two rules [nameable] holds every other segment to: a name
// this driver was not given is empty, and a name holding a separator is a path
// rather than a key.
//
// The empty case is the refusal of a root nobody named, because [RootKey] is
// the only thing that fills it and a store has no key for an unnamed root
// (#334).
func nameableRoot(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: this schema's root is a single value, and a key-value store has no key for one: "+
			"the store's own key at this prefix is the folder every other address is written under, so writing "+
			"the value there would put a value on an interior node - name the key with kv.RootKey", ferry.ErrPlane)
	case strings.Contains(name, separator):
		return fmt.Errorf("%w: the root key %q contains %q, and the store reads that as another step in its "+
			"hierarchy: kv.RootKey names one key under the prefix rather than a path down from it",
			ferry.ErrPlane, name, separator)
	default:
		return nil
	}
}

// prefixKey is the key every address this driver reaches lies at or under, which
// is the prefix and nothing else. It is the empty string for a driver with no
// prefix, which is the whole store.
//
// It is a folder and never a key holding a value, which is why the schema's own
// root is named beside it by [rootName] rather than written here (#334).
func prefixKey(prefix []string) string { return strings.Join(prefix, separator) }

// folder is the key prefix everything strictly under key begins with.
//
// The separator is appended rather than assumed, so listing under /list cannot
// return the key of an unrelated address whose own key merely starts with the
// same bytes. The root is the exception and has none: every key is under it.
func folder(key string) string {
	if key == "" {
		return ""
	}

	return key + separator
}
