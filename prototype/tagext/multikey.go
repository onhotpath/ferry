package tagext

// The owner's round-2 counter-proposal: extensions live in their OWN
// struct-tag keys, Go's native multi-key mechanism, instead of namespaced
// words inside ferry's tag:
//
//	Host string `ferry:"host,required" mylib:"retry=3" docs:"desc=the host"`
//
// ferry's namespace then never opens at all - ADR-0001's sentence stays
// TRUE. A library declares its tag KEY and its words; schema compile reads
// the declared keys beside ferry's own and mints the same address-keyed
// table. The namespace is the key itself, which Go already forces to be
// unique per tag.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// KeyExtension declares a foreign tag key and its vocabulary.
type KeyExtension struct {
	TagKey string
	Words  []Word
}

// DeclareKeys validates and canonicalises - same rules, same Decl shape,
// same comparable cache-key property as Declare.
func DeclareKeys(ferryKey string, exts ...KeyExtension) (Decl, error) {
	seen := map[string]bool{ferryKey: true}
	var parts []string
	for _, e := range exts {
		if e.TagKey == "" || strings.ContainsAny(e.TagKey, ".,= ") {
			return Decl{}, fmt.Errorf("extension tag key %q: must be a bare word", e.TagKey)
		}
		if seen[e.TagKey] {
			if e.TagKey == ferryKey {
				return Decl{}, fmt.Errorf("extension tag key %q: that key is ferry's own", e.TagKey)
			}
			return Decl{}, fmt.Errorf("extension tag key %q declared twice", e.TagKey)
		}
		seen[e.TagKey] = true
		for _, w := range e.Words {
			part := e.TagKey + ":" + w.Name
			if w.TakesVal {
				part += "="
			}
			parts = append(parts, part)
		}
	}
	sort.Strings(parts)
	return Decl{canon: strings.Join(parts, ",")}, nil
}

// ParseField reads one field's whole raw struct tag: ferry's key with
// ferry's grammar, each DECLARED foreign key with the declared words, and
// every UNDECLARED foreign key left alone - it belongs to some other
// library, which is Go's own convention for tag keys.
func ParseField(addr, rawTag, ferryKey string, d Decl, table ExtTable) (tag, error) {
	st := reflect.StructTag(rawTag)
	ft, ok := st.Lookup(ferryKey)
	if !ok {
		return tag{}, fmt.Errorf("no %s tag", ferryKey)
	}
	t, err := ParseTag(addr, ft, Decl{}, nil) // ferry's grammar, UNTOUCHED: no extension words in here
	if err != nil {
		return tag{}, err
	}
	for _, key := range declaredKeys(d) {
		raw, ok := st.Lookup(key)
		if !ok {
			continue
		}
		if err := parseForeign(addr, key, raw, d, table); err != nil {
			return tag{}, err
		}
	}
	return t, nil
}

// parseForeign parses one declared foreign tag: comma-separated words,
// each checked against the declaration, near-miss on the misses.
func parseForeign(addr, key, raw string, d Decl, table ExtTable) error {
	for _, f := range strings.Split(raw, ",") {
		word, val, hasVal := strings.Cut(f, "=")
		full := key + ":" + word
		takesVal, ok := d.allowsFull(full)
		if !ok {
			return unknownForeign(key, word, d)
		}
		if takesVal != hasVal {
			return fmt.Errorf("%s tag, option %q: declared %s a value", key, word, map[bool]string{true: "with", false: "without"}[takesVal])
		}
		if table[addr] == nil {
			table[addr] = map[string]string{}
		}
		table[addr][full] = val
	}
	return nil
}

func (d Decl) allowsFull(full string) (takesVal, ok bool) {
	for _, p := range strings.Split(d.canon, ",") {
		if strings.TrimSuffix(p, "=") == full {
			return strings.HasSuffix(p, "="), true
		}
	}
	return false, false
}

func declaredKeys(d Decl) []string {
	if d.canon == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(d.canon, ",") {
		key, _, _ := strings.Cut(p, ":")
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func unknownForeign(key, word string, d Decl) error {
	var candidates []string
	for _, p := range strings.Split(d.canon, ",") {
		k, w, _ := strings.Cut(strings.TrimSuffix(p, "="), ":")
		if k == key {
			candidates = append(candidates, w)
		}
	}
	best, bestDist := "", 3
	for _, c := range candidates {
		if dist := editDistance(word, c); dist < bestDist {
			best, bestDist = c, dist
		}
	}
	if best != "" {
		return fmt.Errorf("%s tag: unknown option %q: did you mean %q?", key, word, best)
	}
	return fmt.Errorf("%s tag: unknown option %q", key, word)
}
