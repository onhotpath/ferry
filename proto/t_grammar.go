package main

// #11: the candidate grammar.
//
// ferry:"<name>[,<option>]..."
//
//	<name>    the segment text this field addresses. Required on a named
//	          field. Optional on an embedded one, where its absence means
//	          "promote", not "guess a name".
//	<option>  required | omitzero | default=<text>
//	"-"       as the whole value: this field is not mapped.
//
// Escaping. `~` introduces an escape and `~x` yields x for x in the grammar's
// own punctuation, `~ , = -`. Anything else after `~` is an error. This is the
// same rule the address rendering uses, with a different alphabet, which is
// why it is stated once.
//
// Nothing in the grammar needs a character that is not a valid Go string
// escape, which T1/T2 measured to be a hard constraint rather than a
// preference.

import (
	"fmt"
	"reflect"
	"strings"
)

type tagDecl struct {
	skip     bool
	name     string
	hasName  bool
	required bool
	omitzero bool
	hasDef   bool
	defText  string
}

const tagPunct = "~,=-"

// ---------- escaping ----------

// splitFields splits on unescaped commas. It returns the raw (still escaped)
// fields, because the name and an option value are unescaped separately.
func splitFields(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '~' && i+1 < len(s) {
			cur.WriteByte(s[i])
			cur.WriteByte(s[i+1])
			i++
			continue
		}
		if s[i] == ',' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	out = append(out, cur.String())
	return out
}

func unescape(s string) (string, error) {
	if !strings.ContainsRune(s, '~') {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '~' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf("a lone %q at the end of %q: %q introduces an escape, so write %q for a literal one", "~", s, "~", "~~")
		}
		c := s[i+1]
		if !strings.ContainsRune(tagPunct, rune(c)) {
			return "", fmt.Errorf("%q in %q is not an escape ferry defines: %q escapes only its own punctuation, %s", "~"+string(c), s, "~", "`~` `,` `=` `-`")
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), nil
}

// hasUnescaped reports whether c occurs outside an escape.
func hasUnescaped(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '~' {
			i++
			continue
		}
		if s[i] == c {
			return true
		}
	}
	return false
}

// ---------- the option vocabulary ----------

type optSpec struct {
	name      string
	takesText bool
}

var vocabulary = []optSpec{
	{"required", false},
	{"omitzero", false},
	{"default", true},
}

// foreign is the vocabulary of the neighbourhood: words a user arriving from
// another mapper will reach for. Each gets its own diagnosis rather than a
// bare "unknown option", because json/v2's `inline` was a ~29k-use silent
// no-op in Kubernetes and the lesson of that is not "reject it", it is
// "reject it and say what to write instead".
var foreign = map[string]string{
	"omitempty": "ferry has no omitempty; its omission option is `omitzero`, which compares against the Go zero value rather than against a plane-specific idea of empty",
	"inline":    "ferry has no inline; an embedded struct's fields are promoted by default, and a ferry tag on an embedded field nests them under that name instead",
	"embed":     "ferry has no embed; an embedded struct's fields are promoted by default, and a ferry tag on an embedded field nests them under that name instead",
	"squash":    "ferry has no squash; an embedded struct's fields are promoted by default, and a ferry tag on an embedded field nests them under that name instead",
	"prefix":    "ferry has no prefix; a nested struct's own name is the prefix for everything beneath it, and a plane-wide prefix is the source's, not the tag's",
	"delimiter": "ferry has no delimiter; a composite gets one address per element rather than one delimited string, so there is nothing to delimit",
	"separator": "ferry has no separator; a composite gets one address per element, and how a driver joins segments into a plane key is the driver's option",
	"string":    "ferry has no string option; a plane's own type information is respected rather than overridden, and how a value is spelled is the driver's",
	"case":      "ferry has no case option; core never folds case, and a plane that is genuinely case-insensitive folds in its driver's key function",
	"nocase":    "ferry has no case option; core never folds case, and a plane that is genuinely case-insensitive folds in its driver's key function",
	"optional":  "ferry has no optional; every address is optional by default, and `required` is the assertion that the plane supplied one",
	"flow":      "ferry has no flow; how a plane spells a composite is the driver's, and core mints one address per element either way",
	"remain":    "ferry has no remain; ferry maps a subset of a plane and keys it does not map are left alone, so there is nothing left over to collect",
	"multiline": "ferry has no multiline; how a plane spells a string is the driver's",
	"nodump":    "ferry has no nodump; a field ferry loads but never writes cannot round-trip, so the way to keep a value off a plane is a sink that does not accept it",
	"readonly":  "ferry has no readonly; a field ferry loads but never writes cannot round-trip, so the way to keep a value off a plane is a sink that does not accept it",
}

// nearMiss returns a suggestion for an unrecognised option token, in
// encoding/json/v2's shape: "has invalid appearance of %s tag option; specify
// %s instead". Three tests, cheapest first.
func normOpt(s string) string {
	s = strings.ToLower(s)
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(s)
}

func nearMiss(tok string) (string, bool) {
	norm := normOpt
	nt := norm(tok)
	for _, o := range vocabulary {
		if norm(o.name) == nt {
			return o.name, true
		}
	}
	for _, o := range vocabulary {
		if editDistance(nt, o.name) <= maxEdits(o.name) {
			return o.name, true
		}
	}
	return "", false
}

func maxEdits(s string) int {
	if len(s) <= 4 {
		return 1
	}
	return 2
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// ---------- the parser ----------

// parseFerryTag parses one tag value. It never looks at the field's type: a
// grammar error is a property of the text alone, and a type error is
// somebody else's pass. Keeping them apart is what makes the diagnostic rule
// ADR-0006 requires implementable at all.
func parseFerryTag(value string) (tagDecl, []error) {
	var d tagDecl
	var errs []error

	if value == "-" {
		d.skip = true
		return d, nil
	}

	fields := splitFields(value)

	// --- the name ---
	rawName := fields[0]
	switch {
	case rawName == "":
		errs = append(errs, fmt.Errorf("a ferry tag must name the segment this field addresses; write ferry:\"<name>\", or ferry:\"-\" to leave the field unmapped"))
	case rawName == "-":
		errs = append(errs, fmt.Errorf("`-` names no segment: write ferry:\"-\" on its own to leave the field unmapped, or ferry:\"~-,...\" to name the segment `-`"))
	case hasUnescaped(rawName, '='):
		before, _, _ := strings.Cut(rawName, "=")
		if _, ok := lookupOpt(before); ok {
			errs = append(errs, fmt.Errorf("a name may not contain `=`, and %q looks like the %s option with no name in front of it; write ferry:\"<name>,%s\"", rawName, before, rawName))
		} else {
			errs = append(errs, fmt.Errorf("a name may not contain `=`; write `~=` for a literal one"))
		}
	default:
		n, err := unescape(rawName)
		if err != nil {
			errs = append(errs, err)
		} else {
			d.name, d.hasName = n, true
		}
	}

	// --- the options ---
	seen := map[string]bool{}
	for _, raw := range fields[1:] {
		if raw == "" {
			errs = append(errs, fmt.Errorf("empty option: two commas with nothing between them"))
			continue
		}
		key, text, hasEq := strings.Cut(raw, "=")
		if trimmed := strings.TrimSpace(key); trimmed != key {
			errs = append(errs, fmt.Errorf("option %q has surrounding whitespace; ferry does not trim it, so write %q instead", raw, strings.TrimSpace(raw)))
			continue
		}
		spec, ok := lookupOpt(key)
		if !ok {
			if msg, isForeign := foreign[normOpt(key)]; isForeign {
				errs = append(errs, fmt.Errorf("unknown option %q: %s", key, msg))
				continue
			}
			if sug, has := nearMiss(key); has {
				errs = append(errs, fmt.Errorf("has invalid appearance of %q tag option; specify %q instead", key, sug))
				continue
			}
			errs = append(errs, fmt.Errorf("unknown option %q; ferry's options are %s", key, vocabList()))
			continue
		}
		if seen[spec.name] {
			errs = append(errs, fmt.Errorf("option %q appears more than once", spec.name))
			continue
		}
		seen[spec.name] = true
		switch {
		case spec.takesText && !hasEq:
			errs = append(errs, fmt.Errorf("option %q needs a value; write `%s=` for an empty one, which is a real default and is not the same as leaving the option off", spec.name, spec.name))
		case !spec.takesText && hasEq:
			errs = append(errs, fmt.Errorf("option %q takes no value, and %q gives it one", spec.name, raw))
		case spec.name == "required":
			d.required = true
		case spec.name == "omitzero":
			d.omitzero = true
		case spec.name == "default":
			t, err := unescape(text)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			d.hasDef, d.defText = true, t
		}
	}
	return d, errs
}

func lookupOpt(k string) (optSpec, bool) {
	for _, o := range vocabulary {
		if o.name == k {
			return o, true
		}
	}
	return optSpec{}, false
}

func vocabList() string {
	var n []string
	for _, o := range vocabulary {
		if o.takesText {
			n = append(n, o.name+"=")
		} else {
			n = append(n, o.name)
		}
	}
	return strings.Join(n, ", ")
}

// ---------- the field rule ----------

type fieldPlan struct {
	skip    bool
	promote bool // an embedded struct whose fields land at the parent
	decl    tagDecl
}

// planField is the whole rule for what a struct field contributes, and it is
// where the naming decision lives.
//
//	unexported            skipped, per ADR-0005
//	ferry:"-"             skipped
//	named, no ferry tag   REFUSED. Measured: a name equal to the Go field
//	                      name is what 5% of the corpus actually writes, and
//	                      core never folds, so a Go-name default is wrong far
//	                      more often than it is right - and it would let
//	                      exporting a field change the plane.
//	embedded, no tag      promoted
//	embedded, ferry tag   nested under that name
func planField(f reflect.StructField) (fieldPlan, []error) {
	raw, err := rawFerryTag(f.Tag)
	if err != nil {
		return fieldPlan{skip: true}, []error{err}
	}
	if raw == nil {
		if !f.IsExported() {
			return fieldPlan{skip: true}, nil
		}
		if f.Anonymous {
			return fieldPlan{promote: true}, nil
		}
		return fieldPlan{skip: true}, []error{fmt.Errorf(
			"field %s carries no ferry tag: every exported field must name the segment it addresses, or be marked ferry:\"-\"", f.Name)}
	}
	if !f.IsExported() {
		return fieldPlan{skip: true}, []error{fmt.Errorf(
			"field %s is unexported and carries a ferry tag; reflect cannot set an unexported field, so the tag can never do anything", f.Name)}
	}
	d, errs := parseFerryTag(*raw)
	if d.skip {
		return fieldPlan{skip: true}, errs
	}
	return fieldPlan{decl: d}, errs
}
