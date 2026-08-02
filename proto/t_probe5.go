package main

// Round three, opened by review.
//
// T29..T32 re-derive the tag key as a configurable Option, which the first
// round refused. The refusal was argued from the wrong case: it measured
// pointing ferry at `json`, where the conflict is real, and never measured
// the case a library author actually has, which is renaming the key while
// keeping ferry's grammar. Renaming has no conflict with strictness at all.
//
// T33 is the escape-character comparison, worked rather than asserted.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func init() { t11Hooks = append(t11Hooks, runRound3); t11Hooks = append(t11Hooks, runQuotedEdges); t11Hooks = append(t11Hooks, runQuotedEndToEnd) }

// A struct annotated for a library built ON ferry, which is the case that
// decides the Option.
type wrapped struct {
	Host string `mylib:"host"`
	Port int    `mylib:"port,default=8080"`
	Tags []string
}

type wrappedOK struct {
	Host string `mylib:"host"`
	Port int    `mylib:"port,default=8080"`
	Tags []string `mylib:"tags"`
}

// The same struct annotated for somebody else's library, which is the case
// the first round measured and mistook for the deciding one.
type jsonAnnotated struct {
	Host string `json:"host,omitempty"`
	Name string `json:"name"`
}

// Two keys on one field: what a migration looks like mid-flight.
type bothKeys struct {
	Host string `ferry:"ferry_host" mylib:"mylib_host"`
}

func withKey(k string, f func()) {
	old := tagKeyName
	tagKeyName = k
	defer func() { tagKeyName = old }()
	f()
}

func runRound3() {
	hdr("T29  the case the Option is for: a library renaming the key")
	withKey("mylib", func() {
		s, err := compileT(reflect.TypeFor[wrappedOK]())
		if err != nil {
			printErrs("  ", err)
			return
		}
		fmt.Printf("  mylib:\"host\" etc, tag key = %q -> %v\n", tagKeyName, sortedPaths(s.addrs))
		p := path("port")
		fmt.Printf("  and the declaration still compiles: /port %s\n", s.at(p).def.GoString())
	})
	fmt.Println("  ferry's grammar under somebody else's key. Nothing about strictness")
	fmt.Println("  changes, because the vocabulary is still ferry's.")
	fmt.Println()
	fmt.Println("  and the field rule still bites, which is the point of keeping it strict:")
	withKey("mylib", func() {
		_, err := compileT(reflect.TypeFor[wrapped]())
		printErrs("    ", err)
	})

	hdr("T30  the case the first round measured, and what it now says")
	withKey("json", func() {
		_, err := compileT(reflect.TypeFor[jsonAnnotated]())
		printErrs("  ", err)
	})
	fmt.Println("  still refused, and it should be. What changed is that this is no longer")
	fmt.Println("  an argument against the Option: it is the Option used against its grain,")
	fmt.Println("  and the diagnosis now says so.")

	hdr("T31  the key itself is validated where it is supplied")
	for _, k := range []string{"ferry", "mylib", "my-lib", "", "my lib", `my"lib`, "my:lib"} {
		fmt.Printf("  %-10q %v\n", k, ValidTagKey(k))
	}

	hdr("T32  what a configurable key costs #16")
	withKey("ferry", func() {
		s1, _ := compileT(reflect.TypeFor[bothKeys]())
		withKey("mylib", func() {
			s2, _ := compileT(reflect.TypeFor[bothKeys]())
			fmt.Printf("  one reflect.Type, two keys:  %v  and  %v\n", s1.addrs, s2.addrs)
		})
	})
	fmt.Println("  so the key IS part of whatever keys the schema cache. Measured:")
	benchKeys()
	fmt.Println()
	fmt.Println("  ferry reads exactly ONE key. A list would be a precedence question, and")
	fmt.Println("  the struct above shows why: two keys on one field give two address sets,")
	fmt.Println("  and nothing in the tag says which is meant.")
	fmt.Println()
	fmt.Println("  Validate must take the same Option, or it validates a different schema")
	fmt.Println("  than Load compiles:")
	withKey("mylib", func() {
		fmt.Printf("    Validate[wrappedOK]() under key %q -> %v\n", tagKeyName, Validate[wrappedOK]())
	})
	fmt.Printf("    Validate[wrappedOK]() under key %q -> %s\n", tagKeyName, trimTo(fmt.Sprint(Validate[wrappedOK]()), 88))

	hdr("T33  the escape character, worked rather than asserted")
	escapeComparison()
}

func benchKeys() {
	type keyB struct {
		t   reflect.Type
		tag string
	}
	types := []reflect.Type{
		reflect.TypeFor[wrappedOK](), reflect.TypeFor[dbConf](), reflect.TypeFor[appNested](),
		reflect.TypeFor[hostile](), reflect.TypeFor[skipCases](),
	}
	a := map[reflect.Type]int{}
	b := map[keyB]int{}
	for i, t := range types {
		a[t] = i
		b[keyB{t, "ferry"}] = i
	}
	ra := testing.Benchmark(func(bb *testing.B) {
		var n int
		for i := 0; bb.Loop(); i++ {
			n += a[types[i%len(types)]]
		}
		_ = n
	})
	rb := testing.Benchmark(func(bb *testing.B) {
		var n int
		for i := 0; bb.Loop(); i++ {
			n += b[keyB{types[i%len(types)], "ferry"}]
		}
		_ = n
	})
	fmt.Printf("    map[reflect.Type]                  %5.1f ns/op  %d allocs\n", float64(ra.NsPerOp()), ra.AllocsPerOp())
	fmt.Printf("    map[struct{reflect.Type; string}]  %5.1f ns/op  %d allocs\n", float64(rb.NsPerOp()), rb.AllocsPerOp())
	fmt.Println("    both hashable. ADR-0006's other kind of compile-affecting Option is not:")
	fmt.Println("    it measured `hash of unhashable type` because an option list is funcs.")
}

// ---- T33: the four escape models, on the same seven tags ----

type escModel struct {
	name string
	// render writes the intent as a tag value in this model, or "" if the
	// model cannot express it.
	render func(intent) string
	// parse reports what the model makes of a tag value.
	parse func(string) (name string, opts []string, err string)
}

type intent struct {
	what string
	name string
	opts []string // "required", or "default=<text>"
}

func escapeComparison() {
	intents := []intent{
		{"the ordinary case", "host", nil},
		{"a default", "port", []string{"default=8080"}},
		{"a hyphen in a name", "cors-origins", nil},
		{"a comma in a DEFAULT", "greeting", []string{"default=Hello, world"}},
		{"a tilde in a default", "home", []string{"default=~/config"}},
		{"a comma in a NAME", "a,b", nil},
		{"a percent in a default", "cpu", []string{"default=80%"}},
		{"a home-dir path default", "cache", []string{"default=~/.cache/app"}},
		{"a name that is exactly -", "-", nil},
		{"an apostrophe in a default", "greeting", []string{"default=it's here"}},
		{"a broker list default", "brokers", []string{"default=h1:9092,h2:9092"}},
	}

	models := []escModel{
		{"A  ~ follows the char", renderTilde, parseTilde},
		{"B  ,, doubling", renderDouble, parseDouble},
		{"C  no escape, ~ reserved", renderNone, parseNone},
		{"D  ~ lenient", renderLenient, parseLenient},
		{"F  bare or 'quoted'", renderQuoted, parseQuoted},
	}

	fmt.Println("  how each intent is WRITTEN in each model")
	fmt.Printf("  %-24s %-38s %s\n", "intent", models[0].name, models[4].name)
	for _, in := range intents {
		row := make([]string, len(models))
		for i, m := range models {
			r := m.render(in)
			if r == "" {
				row[i] = "UNWRITABLE"
			} else {
				row[i] = `ferry:"` + r + `"`
			}
		}
		fmt.Printf("  %-24s %-38s %s\n", in.what, row[0], row[4])
	}

	fmt.Println()
	fmt.Println("  and what each does with the typo `host,,required`")
	for _, m := range models {
		n, o, e := m.parse("host,,required")
		if e != "" {
			fmt.Printf("  %-26s REFUSED: %s\n", m.name, e)
			continue
		}
		fmt.Printf("  %-26s name=%q opts=%v\n", m.name, n, o)
	}

	fmt.Println()
	fmt.Println("  and with `host,required` written correctly, as a control")
	for _, m := range models {
		n, o, e := m.parse("host,required")
		if e != "" {
			fmt.Printf("  %-26s REFUSED: %s\n", m.name, e)
			continue
		}
		fmt.Printf("  %-26s name=%q opts=%v\n", m.name, n, o)
	}

	fmt.Println()
	fmt.Println("  where `~` already appears, measured rather than recalled:")
	fmt.Println("    in Go source        the type-constraint operator only: 445 ~int, 215 ~string,")
	fmt.Println("                        63 ~byte, 58 ~bool across go1.27rc2's own source")
	fmt.Println("    in a struct tag     0 of 5690 real tag names in the corpus contain ~,")
	fmt.Println("                        and 0 contain %, ^, !, |, +, *, &, @ or ? either")
	fmt.Println("    as an ESCAPE        RFC 6901, implemented in the stdlib at")
	fmt.Println("                        encoding/json/jsontext/state.go:158-212, ~0 -> ~ and ~1 -> /")
	fmt.Println("                        which is the model ADR-0003 already adopted for the address")
}

// escMin writes what a person would actually type: the escape is ACCEPTED
// anywhere and REQUIRED only where the character would otherwise be read as
// the grammar's own. `,` and `~` always; `=` only in a name; `-` only when
// the name is exactly `-`.
func escMin(s string, isName bool) string {
	var b strings.Builder
	if isName && s == "-" {
		return "~-"
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ',' || c == '~' || (isName && c == '=') {
			b.WriteByte('~')
		}
		b.WriteByte(c)
	}
	return b.String()
}

func renderTilde(in intent) string {
	parts := []string{escMin(in.name, true)}
	for _, o := range in.opts {
		if k, v, ok := strings.Cut(o, "="); ok {
			parts = append(parts, k+"="+escMin(v, false))
		} else {
			parts = append(parts, o)
		}
	}
	return strings.Join(parts, ",")
}

func parseTilde(v string) (string, []string, string) {
	d, errs := parseFerryTag(v)
	if len(errs) > 0 {
		return "", nil, errs[0].Error()
	}
	var o []string
	if d.required {
		o = append(o, "required")
	}
	if d.hasDef {
		o = append(o, "default="+d.defText)
	}
	return d.name, o, ""
}

func renderDouble(in intent) string {
	dbl := func(s string) string { return strings.ReplaceAll(s, ",", ",,") }
	parts := []string{dbl(in.name)}
	for _, o := range in.opts {
		if k, v, ok := strings.Cut(o, "="); ok {
			parts = append(parts, k+"="+dbl(v))
		} else {
			parts = append(parts, o)
		}
	}
	return strings.Join(parts, ",")
}

func parseDouble(v string) (string, []string, string) {
	r := dblName(v)
	return r.name, r.opts, ""
}

func renderNone(in intent) string {
	if strings.ContainsAny(in.name, ",~") {
		return ""
	}
	parts := []string{in.name}
	for _, o := range in.opts {
		if strings.ContainsAny(o, ",~") {
			return ""
		}
		parts = append(parts, o)
	}
	return strings.Join(parts, ",")
}

func parseNone(v string) (string, []string, string) {
	f := strings.Split(v, ",")
	for _, o := range f[1:] {
		if o == "" {
			return "", nil, "empty option: two commas with nothing between them"
		}
	}
	return f[0], f[1:], ""
}


// renderLenient / parseLenient: `~` escapes only when the next byte is one of
// the grammar's own punctuation, and is a literal `~` otherwise. No input is
// read differently from model A; the only difference is whether `~x` for some
// other x is an error or a literal.
func renderLenient(in intent) string {
	esc := func(s string, isName bool) string {
		if isName && s == "-" {
			return "~-"
		}
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c == ',' || (isName && c == '=') {
				b.WriteByte('~')
			}
			if c == '~' && i+1 < len(s) && strings.IndexByte(tagPunct, s[i+1]) >= 0 {
				b.WriteByte('~')
			}
			b.WriteByte(c)
		}
		return b.String()
	}
	parts := []string{esc(in.name, true)}
	for _, o := range in.opts {
		if k, v, ok := strings.Cut(o, "="); ok {
			parts = append(parts, k+"="+esc(v, false))
		} else {
			parts = append(parts, o)
		}
	}
	return strings.Join(parts, ",")
}

func parseLenient(v string) (string, []string, string) {
	fields := splitFields(v)
	un := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '~' && i+1 < len(s) && strings.IndexByte(tagPunct, s[i+1]) >= 0 {
				b.WriteByte(s[i+1])
				i++
				continue
			}
			b.WriteByte(s[i])
		}
		return b.String()
	}
	for _, o := range fields[1:] {
		if o == "" {
			return "", nil, "empty option: two commas with nothing between them"
		}
	}
	var opts []string
	for _, o := range fields[1:] {
		opts = append(opts, un(o))
	}
	return un(fields[0]), opts, ""
}
