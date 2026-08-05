// Package tagext is session 05's B-arc prototype: what opening the tag
// namespace would look like IF it opens, built to prove four claims:
//
//  1. a typed extension declaration reduces to a canonical, comparable
//     form that can join the schema cache key (build-time asserted);
//  2. extension words are namespace-prefixed (mylib.retry=3), so no
//     future ferry word can ever collide with a declared one;
//  3. the words are INERT: parsing yields ferry's own tag fields
//     unchanged plus an address-keyed table handed back to the caller -
//     ferry converts nothing differently;
//  4. collisions refuse loudly at declaration time - an extension
//     claiming a bare (ferry-shaped) word, or two extensions claiming
//     one namespace, never reach a tag parse.
//
// It mirrors core's grammar.go shapes in miniature; nothing here touches
// the real module.
package tagext

import (
	"fmt"
	"sort"
	"strings"
)

// Word is one declared extension option: its spelling and whether it
// takes a value. This is the typed declaration #34 item 3 asks for -
// checkable when supplied, unlike a callback.
type Word struct {
	Name     string
	TakesVal bool
}

// Extension declares a namespace and its words.
type Extension struct {
	Namespace string
	Words     []Word
}

// Decl is the canonical, COMPARABLE form of a declaration list: a single
// interned string. It is what joins the schema cache key.
type Decl struct {
	canon string
}

// The build-time hashability assertion, exactly cache.go's trick: if Decl
// ever grows an incomparable field, this line stops compiling.
var _ = map[struct {
	typ    string
	tagKey string
	decl   Decl
}]struct{}{}

// Declare validates extensions and reduces them to a Decl. Every refusal
// here happens ONCE, at declaration - never per tag, never per field.
func Declare(exts ...Extension) (Decl, error) {
	seen := map[string]bool{}
	var parts []string
	for _, e := range exts {
		if e.Namespace == "" || strings.ContainsAny(e.Namespace, ".,=") {
			return Decl{}, fmt.Errorf("extension namespace %q: must be a bare word", e.Namespace)
		}
		if seen[e.Namespace] {
			return Decl{}, fmt.Errorf("extension namespace %q declared twice", e.Namespace)
		}
		seen[e.Namespace] = true
		for _, w := range e.Words {
			if strings.Contains(w.Name, ".") {
				return Decl{}, fmt.Errorf("extension word %q: the namespace is declared once, not spelled per word", w.Name)
			}
			part := e.Namespace + "." + w.Name
			if w.TakesVal {
				part += "="
			}
			parts = append(parts, part)
		}
	}
	sort.Strings(parts) // declaration order must not mint a second cache entry
	return Decl{canon: strings.Join(parts, ",")}, nil
}

// DeclareBare demonstrates the refusal the namespace rule exists for:
// an extension asking for an UNPREFIXED word - ferry's own shape - is
// refused whether or not ferry uses that word today, because every bare
// word is reserved for future ferry.
func DeclareBare(word string) error {
	return fmt.Errorf("extension word %q: bare words are ferry's, declared extensions spell namespace.word", word)
}

func (d Decl) allows(name string) (takesVal bool, ok bool) {
	if d.canon == "" || !strings.Contains(name, ".") {
		return false, false
	}
	for _, p := range strings.Split(d.canon, ",") {
		bare := strings.TrimSuffix(p, "=")
		if bare == name {
			return strings.HasSuffix(p, "="), true
		}
	}
	return false, false
}

// words lists declared spellings, for the near-miss table.
func (d Decl) words() []string {
	if d.canon == "" {
		return nil
	}
	out := strings.Split(d.canon, ",")
	for i := range out {
		out[i] = strings.TrimSuffix(out[i], "=")
	}
	return out
}

// tag mirrors core's parsed tag: ferry's own fields, and NOTHING of the
// extension's - that is claim 3, structurally. Extension values land in
// the table, not here.
type tag struct {
	name     string
	required bool
	def      string
	hasDef   bool
}

// ExtTable is the address-keyed table an inert extension produces: what
// #156 wants handed to a driver, and what a validation library reads.
type ExtTable map[string]map[string]string // address -> namespaced word -> value

// ParseTag parses one tag under a declaration. Unknown words refuse with
// the near-miss table extended by the declared words - a misspelled
// extension word gets the same quality of diagnostic as ferry's own.
func ParseTag(addr, raw string, d Decl, table ExtTable) (tag, error) {
	fields := strings.Split(raw, ",")
	t := tag{name: fields[0]}
	for _, f := range fields[1:] {
		word, val, hasVal := strings.Cut(f, "=")
		switch {
		case word == "required" && !hasVal:
			t.required = true
		case word == "default" && hasVal:
			t.def, t.hasDef = val, true
		default:
			takesVal, ok := d.allows(word)
			if !ok {
				return tag{}, unknownWord(word, d)
			}
			if takesVal != hasVal {
				return tag{}, fmt.Errorf("option %q: declared %s a value", word, map[bool]string{true: "with", false: "without"}[takesVal])
			}
			if table[addr] == nil {
				table[addr] = map[string]string{}
			}
			table[addr][word] = val
		}
	}
	return t, nil
}

// unknownWord is the diagnostics claim: the near-miss search covers
// ferry's words AND the declared extension words in one table.
func unknownWord(word string, d Decl) error {
	candidates := append([]string{"required", "omitzero", "default"}, d.words()...)
	best, bestDist := "", 3
	for _, c := range candidates {
		if dist := editDistance(word, c); dist < bestDist {
			best, bestDist = c, dist
		}
	}
	if best != "" {
		return fmt.Errorf("unknown option %q: did you mean %q?", word, best)
	}
	return fmt.Errorf("unknown option %q", word)
}

func editDistance(a, b string) int {
	da := make([]int, len(b)+1)
	for j := range da {
		da[j] = j
	}
	for i := 1; i <= len(a); i++ {
		prev := da[0]
		da[0] = i
		for j := 1; j <= len(b); j++ {
			cur := da[j]
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			da[j] = min(da[j]+1, da[j-1]+1, prev+cost)
			prev = cur
		}
	}
	return da[len(b)]
}
