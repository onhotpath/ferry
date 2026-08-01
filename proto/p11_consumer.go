package main

// P11: what this looks like to a consumer. One realistic struct, one address
// set, four planes. Everything printed here is computed, not typed by hand.
//
// The tag spellings are ILLUSTRATIVE. The grammar is #11's and the source/sink
// signatures are #5's. What this probe is actually testing is that one address
// set renders onto four unlike planes and that every failure is named before
// any I/O.

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- the struct

type Cred struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

type DBConf struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
	Auth Cred   `ferry:"auth"`
}

type AppConf struct {
	Name    string         `ferry:"name"`
	DB      DBConf         `ferry:"db"`
	Replica DBConf         `ferry:"replica"`
	Tags    []string       `ferry:"tags"`
	Limits  map[string]int `ferry:"limits"`
}

// ------------------------------------------------- a value-driven walk (Dump)

// On Dump the lengths are known, so indexed composites expand. On Load they do
// not, which is #5's enumeration question and is why this probe is dump-side.
func dumpAddrs(v reflect.Value, at Path, out *[]pair) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("ferry"), ",")
			if name == "" {
				name = f.Name
			}
			dumpAddrs(v.Field(i), at.Name(name), out)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			dumpAddrs(v.Index(i), at.Index(i), out)
		}
	case reflect.Map:
		for _, k := range sortedMapKeys(v) {
			dumpAddrs(v.MapIndex(k), at.Name(k.String()), out)
		}
	default:
		*out = append(*out, pair{at, fmt.Sprint(v.Interface())})
	}
}

func sortedMapKeys(v reflect.Value) []reflect.Value {
	ks := v.MapKeys()
	for i := range ks {
		for j := i + 1; j < len(ks); j++ {
			if ks[j].String() < ks[i].String() {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}

type pair struct {
	Addr Path
	Val  string
}

// ---------------------------------------------------------- the four drivers

// Each driver is a key function plus a legality predicate. Both run over the
// whole address set before any I/O.

type driver struct {
	name  string
	key   keyFunc
	legal func(Path) error // can this plane name this address at all?
}

func envDriver() driver {
	return driver{"env", func(p Path) string {
		var b []string
		for s := range p.SegmentsSeq() {
			b = append(b, strings.ToUpper(s.Text))
		}
		return strings.Join(b, "_")
	}, func(p Path) error {
		for s := range p.SegmentsSeq() {
			for i := range len(s.Text) {
				c := s.Text[i]
				ok := c == '_' || (c >= '0' && c <= '9') ||
					(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
				if !ok {
					return fmt.Errorf("%s: segment %q is not a legal environment variable name", p, s.Text)
				}
			}
		}
		return nil
	}}
}

// Windows Registry: every segment but the last is a subkey, the last is a
// value name. Key and value names are case-insensitive, so this driver folds
// and is therefore the one that has to survive the injectivity check.
func registryDriver(root string) driver {
	return driver{"registry", func(p Path) string {
		var segs []string
		for s := range p.SegmentsSeq() {
			segs = append(segs, s.Text)
		}
		key := root
		if len(segs) > 1 {
			key += `\` + strings.Join(segs[:len(segs)-1], `\`)
		}
		return strings.ToLower(key + " : " + segs[len(segs)-1])
	}, func(p Path) error {
		for s := range p.SegmentsSeq() {
			if strings.ContainsAny(s.Text, `\`) {
				return fmt.Errorf(`%s: segment %q contains a backslash`, p, s.Text)
			}
		}
		return nil
	}}
}

// YAML: the address is walked as a tree, so there is no flat key at all. The
// "key" here is only for the injectivity check, and a tree plane's key function
// is injective by construction as long as the address set is prefix-free.
func yamlDriver() driver {
	return driver{"yaml", func(p Path) string { return p.String() }, func(Path) error { return nil }}
}

// HTTP query parameters: url.Values is flat map[string][]string, so nesting
// needs a convention. Brackets, the Rails and PHP spelling.
func queryDriver() driver {
	return driver{"query", func(p Path) string {
		var b strings.Builder
		first := true
		for s := range p.SegmentsSeq() {
			if first {
				b.WriteString(s.Text)
				first = false
				continue
			}
			b.WriteString("[" + s.Text + "]")
		}
		return b.String()
	}, func(p Path) error {
		for s := range p.SegmentsSeq() {
			if strings.ContainsAny(s.Text, "[]&=") {
				return fmt.Errorf("%s: segment %q contains query punctuation", p, s.Text)
			}
		}
		return nil
	}}
}

// accept is the whole per-driver gate: legality then injectivity, over the
// schema's address set, before any I/O.
func (d driver) accept(addrs []Path) error {
	for _, a := range sortedPaths(addrs) {
		if err := d.legal(a); err != nil {
			return fmt.Errorf("%s driver: %w", d.name, err)
		}
	}
	if err := checkInjective(addrs, d.key); err != nil {
		return fmt.Errorf("%s driver: %w", d.name, err)
	}
	return nil
}

// ------------------------------------------------------------------ the probe

func p11Consumer() {
	head("P11  one address set, four planes")

	cfg := AppConf{
		Name: "checkout",
		DB: DBConf{Host: "db.internal", Port: 5432,
			Auth: Cred{User: "svc", Pass: "hunter2"}},
		Replica: DBConf{Host: "replica.internal", Port: 5432,
			Auth: Cred{User: "ro", Pass: "s3cret"}},
		Tags:   []string{"prod", "eu-west"},
		Limits: map[string]int{"rps": 400, "burst": 50},
	}

	var pairs []pair
	dumpAddrs(reflect.ValueOf(cfg), Path{}, &pairs)
	var addrs []Path
	for _, p := range pairs {
		addrs = append(addrs, p.Addr)
	}

	env, reg, yml, qry := envDriver(), registryDriver(`HKCU\Software\Acme`), yamlDriver(), queryDriver()

	fmt.Printf("    %-22s %-24s %-46s %s\n", "ferry address", "env", "windows registry", "query param")
	fmt.Printf("    %-22s %-24s %-46s %s\n", strings.Repeat("-", 22), strings.Repeat("-", 24),
		strings.Repeat("-", 46), strings.Repeat("-", 22))
	for _, p := range pairs {
		fmt.Printf("    %-22s %-24s %-46s %s\n", p.Addr, env.key(p.Addr), reg.key(p.Addr), qry.key(p.Addr))
	}

	fmt.Println("\n    the same set, walked as a tree by the yaml driver:")
	var kv [][2]string
	for _, p := range pairs {
		kv = append(kv, [2]string{p.Addr.String(), p.Val})
	}
	fmt.Print(indentBlock(buildKinded(kv).render(0), "        "))

	fmt.Println("    every driver accepts this schema, before any I/O:")
	for _, d := range []driver{env, reg, yml, qry} {
		fmt.Printf("        %-10s %v\n", d.name, orOK(d.accept(addrs)))
	}
}

func orOK(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

// -------------------------------------------- where each plane draws the line

func p12Rejections() {
	head("P12  the same decision, four different answers, all before I/O")

	env, reg, yml, qry := envDriver(), registryDriver(`HKCU\Software\Acme`), yamlDriver(), queryDriver()
	drivers := []driver{env, reg, yml, qry}

	cases := []struct {
		why   string
		addrs []Path
	}{
		{"a nested db plus a flat db_host leaf",
			[]Path{path("db", "host"), path("db_host")}},
		{"two fields differing only in case",
			[]Path{path("db", "Host"), path("db", "host")}},
		{"a segment with a hyphen",
			[]Path{path("feature-flags", "beta")}},
		{"a segment with non-ASCII text",
			[]Path{path("Kéy")}},
		{"a map key containing a bracket",
			[]Path{path("limits", "rps[eu]")}},
		{"a map key containing a backslash",
			[]Path{path("paths", `c:\tmp`)}},
	}

	fmt.Printf("    %-38s %-8s %-10s %-6s %s\n", "schema", "env", "registry", "yaml", "query")
	for _, c := range cases {
		fmt.Printf("    %-38s", c.why)
		for _, d := range drivers {
			w := map[string]int{"env": 8, "registry": 10, "yaml": 6, "query": 6}[d.name]
			fmt.Printf(" %-*s", w, verdict(d.accept(c.addrs)))
		}
		fmt.Println()
	}

	fmt.Println("\n    the reasons, in full, for the two that matter:")
	fmt.Printf("        %v\n", env.accept([]Path{path("db", "host"), path("db_host")}))
	fmt.Printf("        %v\n", reg.accept([]Path{path("db", "Host"), path("db", "host")}))

	fmt.Println("\n    and the one core rejects for everybody, so no driver ever sees it:")
	fmt.Printf("        %v\n", checkAntichain([]Path{path("db"), path("db", "host")}))
}

func verdict(err error) string {
	if err == nil {
		return "ok"
	}
	return "REJECT"
}

// ------------------------------- what a consumer writes to move between planes

func p13PlaneToPlane() {
	head("P13  the payoff: one struct, env in, yaml out")

	cfg := AppConf{
		Name: "checkout",
		DB:   DBConf{Host: "db.internal", Port: 5432, Auth: Cred{User: "svc", Pass: "hunter2"}},
		Tags: []string{"prod"},
	}
	var pairs []pair
	dumpAddrs(reflect.ValueOf(cfg), Path{}, &pairs)

	env := envDriver()
	fmt.Println("    what the env plane holds:")
	for _, p := range pairs {
		if p.Val == "" || p.Val == "0" {
			continue
		}
		fmt.Printf("        %s=%s\n", env.key(p.Addr), p.Val)
	}
	fmt.Println("    the identical address set dumped to yaml, no retagging:")
	var kv [][2]string
	for _, p := range pairs {
		if p.Val == "" || p.Val == "0" {
			continue
		}
		kv = append(kv, [2]string{p.Addr.String(), p.Val})
	}
	fmt.Print(indentBlock(buildKinded(kv).render(0), "        "))
	fmt.Println("    neither driver knows the other exists, and neither key appears in the struct.")
}

var _ = strconv.Itoa

// dumpAddrsOf rebuilds the P11 schema's address set for reuse in P14.
func dumpAddrsOf(out *[]pair) {
	cfg := AppConf{
		Name: "checkout",
		DB:   DBConf{Host: "db.internal", Port: 5432, Auth: Cred{User: "svc", Pass: "hunter2"}},
		Replica: DBConf{Host: "replica.internal", Port: 5432,
			Auth: Cred{User: "ro", Pass: "s3cret"}},
		Tags:   []string{"prod", "eu-west"},
		Limits: map[string]int{"rps": 400, "burst": 50},
	}
	dumpAddrs(reflect.ValueOf(cfg), Path{}, out)
}
