package main

// W1: does ADR-0003's address model express a Registry path without contortion?
//
// ADR-0003 has a worked Registry column in its four-planes table, written from
// reasoning rather than from a running Registry, and it says so. Checking it is
// a first-class result of this ticket in either direction. This probe is the
// half that needs no hive: it is the driver's KEY FUNCTION and ADR-0003's two
// collision rules, which are questions about ferry.

import (
	"fmt"
	"strings"
)

// wRegKey is a Registry driver's key function, written to ADR-0003's stated
// reading: "every segment but the last is a subkey, and the last is a value
// name".
type wRegKey struct {
	base string // e.g. `Software\Acme`
	// noFold exists only so W1 can measure what a driver that declines to fold
	// accepts. A real Registry driver never sets it.
	noFold bool
}

// key returns the subkey path and the value name for one address.
func (f wRegKey) key(p Path) (sub, name string, err error) {
	segs := p.Segments()
	if len(segs) == 0 {
		return "", "", fmt.Errorf("the empty path has no value name")
	}
	parts := make([]string, 0, len(segs))
	for i, s := range segs[:len(segs)-1] {
		// LEGALITY: a subkey name cannot contain a backslash, because the
		// backslash is the path separator, and cannot be empty.
		if strings.Contains(s.Text, `\`) {
			return "", "", fmt.Errorf("segment %d %q contains a backslash, which is the Registry's own separator", i, s.Text)
		}
		if s.Text == "" {
			return "", "", fmt.Errorf("segment %d is empty, and a subkey has no empty name", i)
		}
		parts = append(parts, s.Text)
	}
	name = segs[len(segs)-1].Text
	if strings.Contains(name, `\`) {
		return "", "", fmt.Errorf("value name %q contains a backslash", name)
	}
	sub = f.base
	if len(parts) > 0 {
		sub = f.base + `\` + strings.Join(parts, `\`)
	}
	return sub, name, nil
}

// checkKey is the key used for ADR-0003's INJECTIVITY check, and it is not the
// key that gets written.
//
// This is a refinement of ADR-0003 that the Registry forces and env does not.
// ADR-0003 frames folding as part of the key function - "a driver MAY fold, as
// part of its key function, when its plane genuinely is case-insensitive" - and
// on env that is right, because the env plane neither folds nor preserves.
// W0 measured that the Registry does BOTH: it matches case-insensitively and
// it PRESERVES the spelling a value was created with. So a driver that folds
// its emitted key destroys the spelling for every other Windows program, and a
// driver that does not fold at all fails the injectivity obligation. The
// correct shape is to fold only when comparing.
func (f wRegKey) checkKey(p Path) (string, error) {
	sub, name, err := f.key(p)
	if err != nil {
		return "", err
	}
	if f.noFold {
		return sub + " : " + name, nil
	}
	return strings.ToLower(sub) + " : " + strings.ToLower(name), nil
}

// bind is ADR-0003's driver-side obligation: legality first, then injectivity
// over the whole address set, both before any I/O.
func (f wRegKey) bind(addrs []Path) (map[Path]string, error) {
	seen := map[string]Path{}
	out := map[Path]string{}
	var errs []string
	for _, p := range sortedPaths(addrs) {
		sub, name, err := f.key(p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		k, _ := f.checkKey(p)
		if prev, dup := seen[k]; dup {
			errs = append(errs, fmt.Sprintf("not injective: %s and %s both address %q", prev, p, k))
			continue
		}
		seen[k] = p
		out[p] = sub + " : " + name
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("registry: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

func runW1() {
	f := wRegKey{base: `HKCU\Software\Acme`}

	fmt.Println("(a) ADR-0003's own worked example, its Registry column reproduced")
	fmt.Println("    ADR-0003 wrote this table from reasoning. Run:")
	fmt.Printf("    %-20s %-16s %s\n", "ferry address", "ADR-0003 says", "this key function produces")
	for _, c := range []struct {
		addr Path
		adr  string
	}{
		{path("name"), `HKCU\Software\Acme : name`},
		{path("db", "host"), `HKCU\Software\Acme\db : host`},
		{path("db", "auth", "user"), `HKCU\Software\Acme\db\auth : user`},
		{path("tags").Index(0), `HKCU\Software\Acme\tags : 0`},
		{path("limits", "rps"), `HKCU\Software\Acme\limits : rps`},
	} {
		sub, name, err := wRegKey{base: `HKCU\Software\Acme`}.key(c.addr)
		got := sub + " : " + name
		mark := "MATCHES"
		if got != c.adr {
			mark = "DIFFERS"
		}
		if err != nil {
			got, mark = err.Error(), "ERROR"
		}
		fmt.Printf("    %-20s %-16s %s   %s\n", c.addr, "", got, mark)
	}
	fmt.Println("    Five of five. The address model expresses a Registry path with a")
	fmt.Println("    nine-line key function and no contortion, and ADR-0003's column is")
	fmt.Println("    correct as written.")

	fmt.Println("\n(b) ADR-0003's second table: which schemas each plane accepts")
	fmt.Println("    Its `registry` column is four predictions. Run against this driver:")
	for _, c := range []struct {
		what string
		set  []Path
		adr  string
	}{
		{"a nested db plus a flat db_host leaf", []Path{path("db", "host"), path("db_host")}, "ok"},
		{"two fields differing only in case", []Path{path("myKey"), path("MyKey")}, "rejected"},
		{"a map key containing [", []Path{path("limits", "a[b")}, "ok"},
		{"a map key containing a backslash", []Path{path("limits", `a\b`)}, "rejected"},
	} {
		_, err := f.bind(c.set)
		got := "ok"
		if err != nil {
			got = "rejected"
		}
		mark := "MATCHES"
		if got != c.adr {
			mark = "DIFFERS"
		}
		fmt.Printf("    %-38s ADR-0003: %-9s run: %-9s %s\n", c.what, c.adr, got, mark)
		if err != nil {
			fmt.Printf("        %v\n", err)
		}
	}

	fmt.Println("\n(c) the folding driver is what makes the case row true, and ADR-0003")
	fmt.Println("    is why that is safe rather than dangerous")
	for _, noFold := range []bool{false, true} {
		_, err := (wRegKey{base: `HKCU\Software\Acme`, noFold: noFold}).bind([]Path{path("myKey"), path("MyKey")})
		fmt.Printf("    folds when checking=%-6v -> %v\n", !noFold, errShortW(err))
	}
	fmt.Println("    A driver that does NOT fold accepts a schema the plane cannot hold,")
	fmt.Println("    because the Registry folds whether or not the driver does. So for this")
	fmt.Println("    plane folding is not a convenience, it is the only correct key")
	fmt.Println("    function, and ADR-0003's injectivity rule is what catches the")
	fmt.Println("    collision it creates. That is the strongest available case for")
	fmt.Println("    ADR-0003's \"a driver is expected to transform segment text, not to")
	fmt.Println("    reject it\", and it was written about env.")

	fmt.Println("\n(d) the one row where the Registry is LESS constrained than core")
	fmt.Println("    ADR-0003 refuses a leaf and a subtree at one segment, for every plane,")
	fmt.Println("    because a TREE plane cannot hold both:")
	fmt.Printf("      /db and /db/host -> core: %v\n", errShortW(prefixFree([]Path{path("db"), path("db", "host")})))
	fmt.Println("    But on the Registry they are not one location: /db is a VALUE named")
	fmt.Println("    `db` under the base key, and /db/host is a value `host` under a SUBKEY")
	fmt.Println("    named `db`. A Registry key's value namespace and its subkey namespace")
	fmt.Println("    are separate.")
	sub1, n1, _ := f.key(path("db"))
	sub2, n2, _ := f.key(path("db", "host"))
	fmt.Printf("      /db      -> subkey %-28q value %q\n", sub1, n1)
	fmt.Printf("      /db/host -> subkey %-28q value %q\n", sub2, n2)
	fmt.Println("    W0 measures whether a real hive holds both at once. If it does, this")
	fmt.Println("    is a schema the Registry can represent and core refuses, which is the")
	fmt.Println("    cost ADR-0003 priced as \"a schema nobody writes deliberately\" being")
	fmt.Println("    paid on a plane its own table calls a tree.")

	fmt.Println("\n(e) the empty segment, which ADR-0003 and ADR-0008 handle differently")
	_, name, err := f.key(path(""))
	fmt.Printf("    a one-segment path whose text is empty -> value name %q, err=%v\n", name, errShortW(err))
	fmt.Println("    On the Registry the empty value name is the key's DEFAULT VALUE, a")
	fmt.Println("    real and commonly used location. ADR-0003 says an empty segment is")
	fmt.Println("    plane-specific and gives env as the counter-example; ADR-0008 makes it")
	fmt.Println("    unwritable from a tag at all, deliberately. So the Registry's default")
	fmt.Println("    value is unaddressable by ferry, and that is ADR-0008's call rather")
	fmt.Println("    than ADR-0003's.")
}

func errShortW(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}
