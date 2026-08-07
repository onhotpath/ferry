package ferry

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// This file is ADR-0021's mechanism: a library declares a struct tag key of its
// own, ferry reads that key beside its own, and hands the words back inert.
//
// ferry's own vocabulary is not extended and this file adds no word to it.
// ADR-0008's four words and its eleven refusals are untouched, and
// ferry:"host,mylib.retry=3" is refused exactly as it was, which is what
// TestFerryTagStaysClosed pins. What is new is that ferry is taught to read a
// key that is not its own, when told to, and those are different sentences.
//
// Three properties are decisions rather than implementation.
//
// A declaration is registered at [NewRegistry] beside the codecs, so
// ADR-0017's construction-is-the-freeze covers it: the declarations are
// complete at the registry's birth and there is no window in which they are
// not. That is also why they reach the schema cache with no new machinery, the
// registry being the cache's outer level already (ADR-0010).
//
// The declaration reduces to a canonical, comparable value that joins
// [schemaKey], and the canonical form is order-independent, so declaring the
// same two extensions in the other order does not mint a second cache entry.
//
// Core parses a declared key's words, validates them against their
// declaration, hands the table back and never acts on them. A driver or a
// library may act, and round-trip fidelity is then that consumer's proof
// obligation, in the same family as ADR-0001's rule that a registrant carries
// the proof of their own extension.

// KeyExtension is one library's declaration: the struct tag key it owns, and
// every word ferry may read under it.
//
//	func Extension() ferry.KeyExtension {
//	    return ferry.KeyExtension{
//	        TagKey: "yamlext",
//	        Words:  []ferry.Word{{Name: "node", TakesValue: true}},
//	    }
//	}
//
// Hand it to [WithTagKeys], which is the only thing that takes one.
//
// The key is yours and never ferry's: a field then carries both tags, and each
// is read with its own vocabulary.
//
//	Wait time.Duration `ferry:"wait,required" yamlext:"node=!mycompany:duration"`
type KeyExtension struct {
	// TagKey is the struct tag key this extension owns. It may not be the key
	// ferry reads, and it is a bare word: no space, quote, colon, comma, dot or
	// equals sign.
	TagKey string

	// Words is the whole vocabulary of the key, and a word outside it is refused
	// where a tag carries it, with the same near-miss suggestion ferry gives for
	// its own.
	Words []Word
}

// Word is one word of an extension's vocabulary: how it is spelled, and whether
// it carries a value.
//
// A word declared with a value is written name=text and one declared without it
// is written name, and a tag that gets that the wrong way round is refused.
type Word struct {
	// Name is the word as a tag spells it: a bare word, with no comma and no
	// equals sign in it.
	Name string

	// TakesValue says the word is written name=text. The text is read with the
	// same token grammar ferry's own default= uses, so a value holding a comma
	// is written in single quotes.
	TakesValue bool
}

// WithTagKeys declares foreign struct tag keys for a registry to read beside
// ferry's own, and is handed to [NewRegistry] like a codec.
//
//	var Registry = ferry.NewRegistry(
//	    ferry.NumberText[big.Int](),
//	    ferry.WithTagKeys(yaml.Extension()),
//	)
//
// A declared key is parsed with that extension's vocabulary and handed back
// inert: core validates the words, puts them in an address-keyed table, and
// never acts on them. Read the table at a driver's own Bind with
// [AddressSet.Extension], or out of band with [ExtensionTable].
//
// An undeclared key is another library's business and is never claimed, so a
// json or a validate tag on the same field is untouched.
//
// A word only reaches the table where the field named an address. A field
// marked "-" and a field ferry never reads carry their extension words nowhere,
// and so does a field under a slice or a map, which names an address shape
// rather than an address: its words are still held to the declaration, and
// there is no address for them to sit at.
//
// It refuses four things, at the [NewRegistry] that was given it rather than at
// any later compile: a key that is not a bare word, a key ferry itself reads, a
// key declared twice, and a word that is empty or is spelled with a comma or an
// equals sign in it. It panics with what [NewRegistry] panics with, for the
// reason given there.
func WithTagKeys(exts ...KeyExtension) Registration {
	return keyDecl{exts: slices.Clone(exts)}
}

// keyDecl is what [WithTagKeys] returns: a declaration, and deliberately not a
// codec. It is a [Registration] because [NewRegistry] takes the whole of what a
// registry is built from, and a declaration is not a pair of halves over a type
// (ADR-0021).
type keyDecl struct{ exts []KeyExtension }

func (d keyDecl) registerOn(r *Registry) {
	for _, e := range d.exts {
		if err := r.exts.declare(e); err != nil {
			panic(err)
		}
	}
}

// extDecl is a registry's whole declaration reduced to one comparable value,
// which is what lets it join [schemaKey] with no new machinery.
//
// It is order-independent by construction: the parts are sorted before they are
// joined, so two registries declaring the same two extensions in opposite
// orders build the same value and a schema compiled under one is the schema
// compiled under the other (ADR-0021).
type extDecl struct{ canon string }

// extSet is a registry's declared foreign keys, resolved once at construction:
// the canonical form for the cache key, the keys in scan order, and each key's
// vocabulary.
type extSet struct {
	decl  extDecl
	keys  []string
	words map[string]map[string]Word
}

// declare records one extension, or reports why it is not one.
//
// Every refusal here is about the declaration alone, so it fires once at the
// registry's birth rather than at a tag, and a program that declares badly is a
// program that cannot start (ADR-0017, ADR-0021).
func (s *extSet) declare(e KeyExtension) error {
	if err := s.declareKey(e.TagKey); err != nil {
		return err
	}

	words := make(map[string]Word, len(e.Words))

	for _, w := range e.Words {
		if err := declareWord(e.TagKey, w, words); err != nil {
			return err
		}

		words[w.Name] = w
	}

	s.words[e.TagKey] = words
	s.keys = append(s.keys, e.TagKey)

	return nil
}

// declareKey holds a declared key to what a foreign tag key may be: a bare
// word, written into a struct tag beside ferry's own, and not ferry's own.
func (s *extSet) declareKey(key string) error {
	switch {
	case key == defaultTagKey:
		return regError(fmt.Sprintf("the extension tag key %q is the key ferry reads: ferry's own grammar is "+
			"under that key and an extension's vocabulary is under its own, so one key cannot be both - "+
			"declare the key your library owns", key))
	case key == "":
		return regError("an extension declares the struct tag key it owns, and the empty string names none: " +
			"give the key your library's users write, as ferry.KeyExtension{TagKey: \"mylib\", ...}")
	case badExtKeyByte(key) >= 0:
		i := badExtKeyByte(key)

		return regError(fmt.Sprintf("the extension tag key %q contains %q: a tag key is a bare word, because it "+
			"is written beside ferry's own in one struct tag", key, key[i:i+1]))
	}

	if _, ok := s.words[key]; ok {
		return regError(fmt.Sprintf("the extension tag key %q is declared twice: one key has one vocabulary, "+
			"because two declarations for one key are a precedence question nothing chooses between - "+
			"declare its words once", key))
	}

	return nil
}

// declareWord holds one declared word to what a word may be, and refuses a
// second declaration of one.
func declareWord(key string, w Word, seen map[string]Word) error {
	switch {
	case w.Name == "":
		return regError(fmt.Sprintf("the extension tag key %q declares a word with no name: a word is what a tag "+
			"spells, so there is nothing for a tag to carry", key))
	case strings.ContainsAny(w.Name, ",="):
		return regError(fmt.Sprintf("the extension word %q under tag key %q contains %q: a comma separates one "+
			"word from the next and an equals sign introduces a value, so neither can be inside a word",
			w.Name, key, wordPunctuation(w.Name)))
	}

	if _, ok := seen[w.Name]; ok {
		return regError(fmt.Sprintf("the extension word %q under tag key %q is declared twice: one word takes a "+
			"value or it does not, and two declarations of it are two answers", w.Name, key))
	}

	return nil
}

// wordPunctuation names which of the two bytes a word carried, so the refusal
// quotes the one that is there.
func wordPunctuation(name string) string {
	if strings.Contains(name, ",") {
		return ","
	}

	return equals
}

// badExtKeyByte is where a key stops being a bare word, or -1. It is the struct
// tag key rule plus the two bytes an extension tag spends on its own grammar
// and the dot a namespaced word would have used.
func badExtKeyByte(key string) int {
	for i := range len(key) {
		if c := key[i]; c <= ' ' || c == ':' || c == '"' || c == del || c == ',' || c == '=' || c == '.' {
			return i
		}
	}

	return -1
}

// seal is the end of construction: the keys sorted so a scan order is stable,
// and the canonical form built so the declaration can key a cache.
func (s *extSet) seal() {
	slices.Sort(s.keys)

	parts := make([]string, 0, len(s.keys))

	for _, key := range s.keys {
		for _, name := range slices.Sorted(maps.Keys(s.words[key])) {
			part := key + ":" + name
			if s.words[key][name].TakesValue {
				part += equals
			}

			parts = append(parts, part)
		}
	}

	s.decl = extDecl{canon: strings.Join(parts, ",")}
}

// claims reports the tag key ferry was told to read being one this registry
// declares, which is one key asked to be read two ways.
func (s extSet) claims(key string) error {
	if _, ok := s.words[key]; !ok {
		return nil
	}

	return optionError(fmt.Sprintf("the tag key is %q, and the registry this call resolves against declares "+
		"%q as an extension key: ferry reads its own grammar under the key it is given, so that key cannot "+
		"also carry an extension's vocabulary", key, key))
}

// lookup is one declared word of one declared key.
func (s extSet) lookup(key, name string) (Word, bool) {
	w, ok := s.words[key][name]

	return w, ok
}

// vocabulary is one key's declared words, sorted, which is what a refusal names
// when a misspelling is near nothing.
func (s extSet) vocabulary(key string) []string {
	return slices.Sorted(maps.Keys(s.words[key]))
}

// parse reads one declared key's tag value against that key's vocabulary,
// reporting every fault it carries rather than the first, which is what
// parseTag does with ferry's own.
func (s extSet) parse(key, value string) (map[string]string, []error) {
	fields := splitFields(value)
	out := make(map[string]string, len(fields))

	var errs []error

	for _, raw := range fields {
		if err := s.addWord(key, raw, out); err != nil {
			errs = append(errs, err)
		}
	}

	return out, errs
}

// addWord reads one word of an extension tag into the table, and refuses a
// second spelling of one word.
func (s extSet) addWord(key, raw string, into map[string]string) error {
	name, text, err := s.readWord(key, raw)
	if err != nil {
		return err
	}

	if _, ok := into[name]; ok {
		return fmt.Errorf("%s tag, word %q is given twice: one word carries one answer", key, name)
	}

	into[name] = text

	return nil
}

// readWord decodes one word: is it declared, is it written the way it was
// declared, and what text does it carry.
func (s extSet) readWord(key, raw string) (name, text string, err error) {
	if raw == "" {
		return "", "", fmt.Errorf("%s tag, empty word: two commas with nothing between them", key)
	}

	if err := checkOptionSpacing(raw); err != nil {
		return "", "", fmt.Errorf("%s tag, %w", key, err)
	}

	head, value, hasValue := strings.Cut(raw, equals)

	w, ok := s.lookup(key, head)

	switch {
	case !ok:
		return "", "", s.unknownWord(key, head)
	case w.TakesValue && !hasValue:
		return "", "", fmt.Errorf("%s tag, word %q needs a value: it is declared with one, so write %s=<text>",
			key, head, head)
	case !w.TakesValue && hasValue:
		return "", "", fmt.Errorf("%s tag, word %q takes no value: it is declared without one", key, head)
	}

	text, err = parseToken(value, "value")
	if err != nil {
		return "", "", fmt.Errorf("%s tag, word %q: %w", key, head, err)
	}

	return head, text, nil
}

// unknownWord refuses a word a key does not declare, with the near-miss search
// run over that key's own vocabulary.
//
// The search is the one ADR-0008 built for ferry's words, over the extension's
// words instead, so a user misspelling a declared word reads the same quality
// of diagnostic as one misspelling ferry's - and ferry's own is untouched,
// because this function is never on its path (ADR-0021).
func (s extSet) unknownWord(key, word string) error {
	words := s.vocabulary(key)

	if near := nearestOf(normaliseWord(word), words); near != "" {
		return fmt.Errorf("%s tag, unknown word %q: did you mean %q?", key, word, near)
	}

	return fmt.Errorf("%s tag, unknown word %q: %s", key, word, extVocabulary(key, words))
}

// extVocabulary is what a key has in the place of the word that was written.
func extVocabulary(key string, words []string) string {
	if len(words) == 0 {
		return "the " + key + " tag key is declared with no words at all"
	}

	return "the " + key + " tag key declares " + strings.Join(words, ", ")
}

// ExtTable is every declared extension word one compiled type's tags carried,
// keyed by tag key and then by the address the word sits at.
//
// [ExtensionTable] returns one, and [AddressSet.Extension] is the same view a
// driver reads at its own Bind. The zero value holds nothing, which is what a
// type compiled against a registry that declares nothing produces.
type ExtTable struct {
	byKey map[string]map[Path]map[string]string
}

// Extension is one tag key's address-keyed view: for each address whose field
// carried that key, the words it carried and the text of each.
//
//	for addr, words := range table.Extension("yamlext") {
//	    nodeTags[addr] = words["node"]
//	}
//
// A word declared without a value reads as the empty string, and asking whether
// the word is there is the two-result map index.
//
// The result is freshly allocated and the caller's to keep, so writing to it
// changes nothing about the compiled schema it came from. A key nothing
// declared, and a key no field carried, both yield an empty view rather than an
// error: an extension nobody used is not a defect.
func (t ExtTable) Extension(key string) map[Path]map[string]string {
	src := t.byKey[key]
	out := make(map[Path]map[string]string, len(src))

	for addr, words := range src {
		out[addr] = maps.Clone(words)
	}

	return out
}

// put records one address's words under one key, while the table is still
// inside the compile that is building it.
func (t *ExtTable) put(key string, addr Path, words map[string]string) {
	if t.byKey == nil {
		t.byKey = map[string]map[Path]map[string]string{}
	}

	if t.byKey[key] == nil {
		t.byKey[key] = map[Path]map[string]string{}
	}

	t.byKey[key][addr] = words
}

// ExtensionTable compiles T and hands back every declared extension word its
// tags carried, without a plane and without a driver.
//
//	table, err := ferry.ExtensionTable[Config](ferry.WithRegistry(registry))
//	for addr, words := range table.Extension("docs") {
//	    fmt.Println(addr, words["desc"])
//	}
//
// It is the door for a consumer that never meets a plane, a documentation
// generator being the plain case. A driver needs none of it: the same view
// rides the [AddressSet] its own Bind is handed, which is [AddressSet.Extension].
//
// It runs exactly the compiler [Load] and [Dump] run and takes the same
// [Option] values, so a type it accepts is a type they accept and a tag it
// refuses is a tag they refuse. It compiles the schema and discards it, so it
// retains no resolution.
//
// The table is empty where the registry this call resolves against declares no
// tag key, which is every call that names no registry of its own.
func ExtensionTable[T any](opts ...Option) (ExtTable, error) {
	sch, err := schemaOf(reflect.TypeFor[T](), opts, discarded)
	if err != nil {
		return ExtTable{}, err
	}

	return sch.addrs.ext, nil
}
