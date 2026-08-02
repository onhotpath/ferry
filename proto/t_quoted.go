package main

// Model F: a token is BARE or SINGLE-QUOTED, and inside quotes a literal
// quote is doubled. No escape character anywhere.
//
// The single-quoted form is json/v2's own design, and its source states the
// reason ferry measured independently: "both backtick and double quotes
// cannot be used verbatim in a struct tag". ferry differs only in the inner
// escape, taking SQL's doubling over v2's backslash, because a backslash in
// a struct tag value must be written `\\` and one short of that produces a
// tag reflect.StructTag.Get cannot read at all.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// splitFieldsQ splits on commas that are outside a single-quoted run.
func splitFieldsQ(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && inQ && i+1 < len(s) && s[i+1] == '\'':
			cur.WriteString("''")
			i++
		case c == '\'':
			inQ = !inQ
			cur.WriteByte(c)
		case c == ',' && !inQ:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}

// unquoteQ reads one token. A token that begins with ' is quoted and must be
// terminated; anything else is bare and carries no escapes at all.
func unquoteQ(s string, what string) (string, error) {
	// Only a LEADING quote is significant. A bare token carries no escapes at
	// all, so an apostrophe inside one is just an apostrophe.
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
	return "", fmt.Errorf("%s %q is not terminated: a quoted %s ends at a single quote, and a literal quote inside it is doubled", what, s, what)
}

func renderQuoted(in intent) string {
	q := func(s string, isName bool) string {
		needs := strings.Contains(s, ",") || strings.HasPrefix(s, "'") ||
			(isName && (s == "-" || strings.Contains(s, "=")))
		if !needs {
			return s
		}
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	parts := []string{q(in.name, true)}
	for _, o := range in.opts {
		if k, v, ok := strings.Cut(o, "="); ok {
			parts = append(parts, k+"="+q(v, false))
		} else {
			parts = append(parts, o)
		}
	}
	return strings.Join(parts, ",")
}

func parseQuoted(v string) (string, []string, string) {
	if v == "-" {
		return "", nil, "not mapped"
	}
	fields := splitFieldsQ(v)
	name, err := unquoteQ(fields[0], "name")
	if err != nil {
		return "", nil, err.Error()
	}
	if name == "" {
		return "", nil, "a ferry tag must name the segment this field addresses"
	}
	var opts []string
	for _, raw := range fields[1:] {
		if raw == "" {
			return "", nil, "empty option: two commas with nothing between them"
		}
		k, text, hasEq := strings.Cut(raw, "=")
		if !hasEq {
			opts = append(opts, k)
			continue
		}
		t, err := unquoteQ(text, "value")
		if err != nil {
			return "", nil, err.Error()
		}
		opts = append(opts, k+"="+t)
	}
	return name, opts, ""
}

// The hostile fixture, rewritten in model F. Every name carries punctuation
// the grammar itself uses, which is the trap the handoff names.
type hostileQ struct {
	Comma   string `ferry:"'a,b'"`
	Equals  string `ferry:"'a=b'"`
	Quote   string `ferry:"'a''b'"`
	Dash    string `ferry:"'-'"`
	Slash   string `ferry:"a/b"`
	Hash    string `ferry:"a#b"`
	Space   string `ferry:"a b"`
	Tilde   string `ferry:"a~b"`
	Apos    string `ferry:"it's"`
	Greet   string `ferry:"greet,default='Hello, world'"`
	Home    string `ferry:"home,default=~/.cache/app"`
	Brokers string `ferry:"brokers,default='h1:9092,h2:9092'"`
}

func runQuotedEdges() {
	hdr("T34  model F's failure modes, and the hostile fixture rewritten in it")
	fmt.Println("  what goes wrong, and how loudly:")
	for _, c := range []string{
		"host,default='abc",
		"host,default='abc'def",
		"host,default=it's here",
		"host,default='it''s here'",
		"'a,b",
		"host,default=''",
		"host,default=",
		"'a,b',required",
	} {
		n, o, e := parseQuoted(c)
		if e != "" {
			fmt.Printf("    %-26s REFUSED %s\n", `ferry:"`+c+`"`, trimTo(e, 86))
			continue
		}
		fmt.Printf("    %-26s name=%q opts=%v\n", `ferry:"`+c+`"`, n, o)
	}

	fmt.Println()
	fmt.Println("  and the same names ferry compiled under model A, written in model F:")
	for _, tag := range []string{"'a,b'", "a=b", "'a''b'", "'-'", "a/b", "a#b", "a b", "a~b", "it's"} {
		n, _, e := parseQuoted(tag)
		if e != "" {
			fmt.Printf("    %-12s REFUSED %s\n", `ferry:"`+tag+`"`, e)
			continue
		}
		fmt.Printf("    %-12s segment %q\n", `ferry:"`+tag+`"`, n)
	}
}

func runQuotedEndToEnd() {
	hdr("T35  model F end to end, through the real YAML driver")
	quotedMode, t11Mode = true, true
	defer func() { quotedMode, t11Mode = false, false }()

	s, err := compileT(reflectTypeHostileQ())
	if err != nil {
		printErrs("  ", err)
		return
	}
	for _, p := range sortedPaths(s.addrs) {
		fmt.Printf("      %-16s segments %q\n", p, segTexts(p))
	}
	v := hostileQ{Comma: "c", Equals: "e", Quote: "q", Dash: "d", Slash: "s",
		Hash: "h", Space: "sp", Tilde: "t", Apos: "ap"}
	yamlText, back, err := roundTripYAML(v, s)
	if err != nil {
		fmt.Println("  ", err)
		return
	}
	fmt.Println("  the YAML the driver wrote:")
	for _, l := range strings.Split(strings.TrimRight(yamlText, "\n"), "\n") {
		fmt.Println("      " + l)
	}
	want := v
	fmt.Printf("  every hostile name round-trips: %v\n", back.Comma == want.Comma &&
		back.Equals == want.Equals && back.Quote == want.Quote && back.Dash == want.Dash &&
		back.Slash == want.Slash && back.Hash == want.Hash && back.Space == want.Space &&
		back.Tilde == want.Tilde && back.Apos == want.Apos)
	fmt.Printf("  the three declared defaults arrived as:\n    greet   %q\n    home    %q\n    brokers %q\n",
		back.Greet, back.Home, back.Brokers)
}

func reflectTypeHostileQ() reflect.Type { return reflect.TypeFor[hostileQ]() }

func roundTripYAML(v hostileQ, s *schema) (string, hostileQ, error) {
	var zero hostileQ
	dir, _ := os.MkdirTemp("", "ferry11q")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "conf.yaml")
	calls, err := dumpD(reflect.ValueOf(v), s)
	if err != nil {
		return "", zero, err
	}
	ow, err := FYAMLSink{Path: file}.Bind(nil)
	if err != nil {
		return "", zero, err
	}
	w, err := ow(context.Background())
	if err != nil {
		return "", zero, err
	}
	for _, c := range calls {
		if err := w.Set(context.Background(), c.p, c.v); err != nil {
			return "", zero, err
		}
	}
	if cm, ok := w.(interface{ Commit(context.Context) error }); ok {
		if err := cm.Commit(context.Background()); err != nil {
			return "", zero, err
		}
	}
	if cl, ok := w.(interface{ Close() error }); ok {
		_ = cl.Close()
	}
	b, _ := os.ReadFile(file)
	of, err := FYAMLSource{Path: file}.Bind(nil)
	if err != nil {
		return string(b), zero, err
	}
	r, err := of(context.Background())
	if err != nil {
		return string(b), zero, err
	}
	vals := map[Path]Value{}
	for _, p := range s.addrs {
		got, err := r.Get(context.Background(), p)
		if err != nil {
			continue
		}
		vals[p] = got
	}
	// delete the three defaulted addresses so the declarations are what is
	// under test rather than the dumped empty strings
	for _, n := range []string{"greet", "home", "brokers"} {
		delete(vals, path(n))
	}
	var back hostileQ
	_, err = loadD(vals, s, reflect.ValueOf(&back).Elem(), loadOpts{})
	return string(b), back, err
}
