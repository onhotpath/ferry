package main

// #41 D9 and D17: ADR-0008's grammar, ported from proto/11-tag-grammar's
// t_grammar.go, t_quoted.go and t_mech.go.
//
// The audit's section 1 is the reason this is a port rather than a rewrite:
// ADR-0008 was measured on a branch line that dead-ends, and `e_schema.go`
// re-implemented "the grammar in the small" from scratch - four tag words, one
// option parser, five refusals. What that subset dropped is the whole of the
// tag's diagnostic surface and the whole of its well-formedness tier.
//
// Two things are ported, and they are the two the audit named:
//
//	D9   core does not call reflect.StructTag.Get or Lookup. It scans
//	     reflect.StructField.Tag with its own parser and reports what Get
//	     answers with a silent empty string.
//	D17  three tiers, edit distance, the neighbourhood's vocabulary, and
//	     surrounding whitespace as its own diagnosis.
//
// One thing is adapted rather than ported: the tag KEY is an opts field on this
// branch and a package-level `tagKeyName` on #11's, so every function here
// takes the key rather than reading a global. ADR-0008 makes the key an Option,
// so the parameter is the ADR's own model and the global was the prototype's.
//
// One thing is DELIBERATELY NOT PORTED: the splitter. #11's splitFieldsQ does
// not implement the rule ADR-0008 decided, and the tip's splitTag does. See the
// note above unquoteQ. Porting it wholesale would have regressed
// `ferry:"it's,required"` from two tokens to one.

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// --- tier 1a: the raw struct tag --------------------------------------------

// rawFerryTag is the scanner ferry ships instead of reflect.StructTag.Get.
//
// The scanning loop is Lookup's own, with the error paths kept rather than
// collapsed into `break`, so the three things Get answers with a silent "" each
// become a diagnosis:
//
//	a bare double quote      truncates the value, and destroys the json and
//	                         yaml tags on the same field
//	an invalid Go escape     makes the ferry tag INVISIBLE rather than wrong
//	two ferry tags           reports the first
//
// Returns (nil, nil) when the field genuinely carries no ferry tag, which is
// the distinction Lookup cannot draw at all.
//
// SCOPING, which is ADR-0008's own: ferry refuses a struct tag that does not
// parse only when the text `ferry:"` occurs in it. A field whose json tag is
// malformed and whose ferry tag was read cleanly is go vet's problem.
func rawFerryTag(tag reflect.StructTag, key string) (*string, error) {
	var found *string
	t := string(tag)
	// mine is ADR-0008's scoping rule: the text `ferry:"` occurs, so a
	// malformation here may be ferry's own.
	mine := strings.Contains(string(tag), key+`:"`)
	// lastWasFerry decides the second half of the rule, and it is the only
	// thing that separates the two cases:
	//
	//	ferry:"origins,default=["value"]"   ferry's own value swallowed the
	//	                                    quote, so the truncation is ferry's
	//	                                    and is the defect it must report
	//	ferry:"host" json:"a"b"             ferry's tag was read cleanly and the
	//	                                    damage is downstream, which is
	//	                                    go vet's problem
	//
	// Without it the two are indistinguishable, because both leave the scanner
	// staring at a stray quote.
	lastWasFerry := false
	// ferrys reports whether a malformation reached here is ferry's to diagnose.
	ferrys := func() bool { return mine && (found == nil || lastWasFerry) }
	for t != "" {
		// Skip leading space.
		i := 0
		for i < len(t) && t[i] == ' ' {
			i++
		}
		t = t[i:]
		if t == "" {
			break
		}
		// Scan to colon.
		i = 0
		for i < len(t) && t[i] > ' ' && t[i] != ':' && t[i] != '"' && t[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(t) || t[i] != ':' || t[i+1] != '"' {
			if !ferrys() {
				return found, nil
			}
			hint := "; the usual cause is a bare double quote inside a " + key +
				" tag, which a struct tag value cannot contain"
			return nil, fmt.Errorf(
				"struct tag is not in the conventional `key:\"value\"` form, at %q%s", truncTag(t), hint)
		}
		name := t[:i]
		t = t[i+1:]

		// Scan the quoted string to find the value.
		i = 1
		for i < len(t) && t[i] != '"' {
			if t[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(t) {
			if !ferrys() {
				return found, nil
			}
			return nil, fmt.Errorf("struct tag key %q has an unterminated quoted value", name)
		}
		qvalue := t[:i+1]
		t = t[i+1:]

		if name != key {
			lastWasFerry = false
			continue
		}
		lastWasFerry = true
		value, err := strconv.Unquote(qvalue)
		if err != nil {
			return nil, fmt.Errorf(
				"%s tag value %s is not a valid Go quoted string (%v); a struct tag value is unquoted "+
					"by strconv.Unquote, so it may not contain a bare double quote and may not contain "+
					"an escape Go does not define", key, qvalue, err)
		}
		if found != nil {
			return nil, fmt.Errorf(
				"the field carries two %s tags, %q and %q; reflect.StructTag.Get returns the first "+
					"and go vet does not check it", key, *found, value)
		}
		v := value
		found = &v
	}
	return found, nil
}

func truncTag(s string) string {
	if len(s) > 24 {
		return s[:24] + "..."
	}
	return s
}

// --- tier 1b: the grammar ----------------------------------------------------
//
// A name or an option value is BARE, or SINGLE-QUOTED with a literal quote
// doubled inside it. Only a LEADING quote is significant, so an apostrophe
// inside a bare token is just an apostrophe. There is no escape character.

// The splitter is NOT ported from proto/11. Its splitFieldsQ toggles quote
// state on ANY quote, which is not what ADR-0008 decided:
//
//	"A bare token carries no escapes at all, and only a leading quote is
//	 significant. Because only a leading quote is significant, an apostrophe
//	 inside a bare token is just an apostrophe, and `default=it's here` needs
//	 no quoting."
//
// Measured, the difference, on a tag proto/11's fixtures never contained:
//
//	ferry:"it's,required"   splitFieldsQ  ->  ["it's,required"]
//	                                          one address /it's,required
//	                                          and `required` silently swallowed
//	                        splitTag      ->  ["it's", "required"]
//
// That is the failure ADR-0008 rejected the `,,` doubling model FOR - "a stray
// comma becomes a wrongly-named address with no diagnostic" - occurring in the
// model it chose. proto/11 could not see it because no fixture put an
// apostrophe and an option on one tag.
//
// splitTag in e_schema.go already implements the decision: a quote is
// significant only at the start of a comma-separated part or immediately after
// `default=`, which is exactly "the start of a token". It is the single
// splitter, and this grammar calls it.

// unquoteQ reads one token. A token that begins with ' is quoted and must be
// terminated; anything else is bare and carries no escapes at all, which is
// the same "only a leading quote is significant" rule the splitter obeys.
func unquoteQ(s string, what string) (string, error) {
	if !strings.HasPrefix(s, "'") {
		return s, nil
	}
	var b strings.Builder
	i := 1
	for i < len(s) {
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i += 2
				continue
			}
			if i != len(s)-1 {
				return "", fmt.Errorf("%s %q has text after the closing quote", what, s)
			}
			return b.String(), nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", fmt.Errorf(
		"%s %q is not terminated: a quoted %s ends at a single quote, and a literal quote inside it "+
			"is doubled", what, s, what)
}

// --- the option vocabulary ---------------------------------------------------

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
// another mapper will reach for. Each gets its own diagnosis rather than a bare
// "unknown option", because json/v2's `inline` was a ~29k-use silent no-op in
// Kubernetes and the lesson of that is not "reject it", it is "reject it and
// say what to write instead".
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

// normOpt is encoding/json/v2's normalisation: lowercase, and strip the
// separators a user might reach for.
func normOpt(s string) string {
	s = strings.ToLower(s)
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(s)
}

// nearMiss is v2's shape plus edit distance, which is the layer v2 does not
// have and which is what catches requird, reqired, defualt and deafult.
func nearMiss(tok string) (string, bool) {
	nt := normOpt(tok)
	for _, o := range vocabulary {
		if normOpt(o.name) == nt {
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

// otherPeoplesKeys fires the hint only when the tag key has been pointed at a
// tag some other library owns. The rule it explains is ADR-0008's answer to the
// key question: the key says where to look, never what the content means.
var otherPeoplesKeys = map[string]bool{
	"json": true, "yaml": true, "toml": true, "xml": true,
	"mapstructure": true, "env": true, "bson": true, "db": true,
}

func foreignKeyHint(key string) string {
	if !otherPeoplesKeys[key] {
		return ""
	}
	return fmt.Sprintf(" (the ferry tag key is set to %q, which %s also uses; ferry validates its own "+
		"grammar under whatever key it is told to read)", key, key)
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

// --- the parser --------------------------------------------------------------

// parseFerryTag parses one tag value. It never looks at the field's type: a
// grammar error is a property of the text alone, and a type error is
// applyOptions' pass. Keeping them apart is what makes the tier rule
// implementable at all - tier 1 is this function, tiers 2 and 3 are
// applyOptions, and tier 2 fires only for a field that cleared tier 1.
func parseFerryTag(value, key string) (tag, []error) {
	var d tag
	var errs []error

	if value == "-" {
		d.skip = true
		return d, nil
	}

	fields := splitTag(value)

	// --- the name ---
	rawName := fields[0]
	switch {
	case rawName == "":
		errs = append(errs, fmt.Errorf(
			"a %s tag must name the segment this field addresses; write %s:\"<name>\", or %s:%q to "+
				"leave the field unmapped", key, key, key, "-"))
	case rawName == "-":
		errs = append(errs, fmt.Errorf(
			"`-` names no segment: write %s:%q on its own to leave the field unmapped, or %s:%q to "+
				"name the segment `-`", key, "-", key, `'-',...`))
	case !strings.HasPrefix(rawName, "'") && strings.Contains(rawName, "="):
		before, _, _ := strings.Cut(rawName, "=")
		if _, ok := lookupOpt(before); ok {
			errs = append(errs, fmt.Errorf(
				"a name may not contain `=`, and %q looks like the %s option with no name in front of "+
					"it; write %s:\"<name>,%s\"", rawName, before, key, rawName))
		} else {
			errs = append(errs, fmt.Errorf(
				"a name may not contain `=`; quote the name if it really contains one, as '%s'", rawName))
		}
	default:
		n, err := unquoteQ(rawName, "name")
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
		k, text, hasEq := strings.Cut(raw, "=")
		// Surrounding whitespace is its own diagnosis rather than an unknown
		// option, because ferry does not trim and `ferry:"h, required"` is a
		// mistake a reader's eye slides over.
		if trimmed := strings.TrimSpace(k); trimmed != k {
			errs = append(errs, fmt.Errorf(
				"option %q has surrounding whitespace; ferry does not trim it, so write %q instead",
				raw, strings.TrimSpace(raw)))
			continue
		}
		spec, ok := lookupOpt(k)
		if !ok {
			if msg, isForeign := foreign[normOpt(k)]; isForeign {
				errs = append(errs, fmt.Errorf("unknown option %q: %s%s", k, msg, foreignKeyHint(key)))
				continue
			}
			if sug, has := nearMiss(k); has {
				errs = append(errs, fmt.Errorf(
					"has invalid appearance of %q tag option; specify %q instead", k, sug))
				continue
			}
			errs = append(errs, fmt.Errorf(
				"unknown option %q; ferry's options are %s%s", k, vocabList(), foreignKeyHint(key)))
			continue
		}
		if seen[spec.name] {
			errs = append(errs, fmt.Errorf("option %q appears more than once", spec.name))
			continue
		}
		seen[spec.name] = true
		switch {
		case spec.takesText && !hasEq:
			errs = append(errs, fmt.Errorf(
				"option %q needs a value; write `%s=` for an empty one, which is a real default and is "+
					"not the same as leaving the option off", spec.name, spec.name))
		case !spec.takesText && hasEq:
			errs = append(errs, fmt.Errorf("option %q takes no value, and %q gives it one", spec.name, raw))
		case spec.name == "required":
			d.required = true
		case spec.name == "omitzero":
			d.omitzero = true
		case spec.name == "default":
			t, err := unquoteQ(text, "value")
			if err != nil {
				errs = append(errs, err)
				continue
			}
			d.hasDefault, d.def = true, t
		}
	}
	return d, errs
}
