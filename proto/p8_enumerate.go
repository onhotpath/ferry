package main

// P8: enumeration, and the asymmetry ADR-0003 says belongs in this ADR.
//
// A struct field's address comes from the type. A map key's address and a
// sequence's length come from the value. Dump holds the value; Load does not.
// So a dynamic address is reachable on Load only if the source can enumerate.
//
// The question is not "can some plane enumerate" - obviously some can. It is
// whether enumeration can be a *required* part of the contract, and if not,
// what ferry's two directions then cover.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Enumerator is the optional interface upgrade under test.
type Enumerator interface {
	// Children reports the addresses the plane actually holds directly
	// under prefix. Only a driver that can do this cheaply implements it.
	Children(ctx context.Context, prefix Path) ([]Path, error)
}

func p8Enumerate() {
	head("P8  can Load reach a map key, and is enumeration requirable?")

	ctx := context.Background()

	// (a) Which planes can enumerate a subtree, honestly.
	fmt.Println("    (a) can this plane list what is under an address?")
	rows := []struct{ plane, verdict, why string }{
		{"env", "yes", "os.Environ() lists everything; a prefix scan is a filter"},
		{"yaml", "yes", "the parsed document is a tree already"},
		{"query params", "yes", "url.Values is fully enumerable"},
		{"kv (Consul)", "yes", "a prefix List is the native operation"},
		{"Vault kv-v2", "partly", "LIST exists but is a separate ACL capability;"},
		{"", "", "a token with read and no list is ordinary"},
		{"a secret broker", "no", "some planes answer only what you name, by design"},
	}
	for _, r := range rows {
		fmt.Printf("        %-14s %-8s %s\n", r.plane, r.verdict, r.why)
	}
	fmt.Println("        So enumeration cannot be required without excluding a plane")
	fmt.Println("        class ferry explicitly wants. It is an optional interface.")

	// (b) It works where it exists.
	fmt.Println("\n    (b) an enumerating source, for the case it does exist")
	dir, _ := os.MkdirTemp("", "ferryproto")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "in.yaml")
	os.WriteFile(file, []byte("limits:\n  rps: 10\n  burst: 20\ntags:\n  - a\n  - b\n"), 0o644)
	src := YAMLEnumSource{Path: file}
	r, err := bindOpen(ctx, src, NewAddressSet(nil))
	if err != nil {
		fmt.Println("        open:", err)
		return
	}
	e := r.(Enumerator)
	for _, p := range []Path{path("limits"), path("tags")} {
		kids, err := e.Children(ctx, p)
		fmt.Printf("        children of %-10s %v err=%v\n", p, kids, err)
	}
	fmt.Println("        Note the kinds: /limits yields Name segments and /tags yields")
	fmt.Println("        Index segments, so the enumerator answers ADR-0003's question")
	fmt.Println("        about which composite it is, from the plane rather than by")
	fmt.Println("        guessing at base-10 text.")

	// (c) The consequence for the two directions.
	fmt.Println("\n    (c) what the two directions then cover")
	fmt.Println("        Dump  : every address, always. The value is in hand, so map")
	fmt.Println("                keys and sequence lengths are known.")
	fmt.Println("        Load  : static addresses always; dynamic addresses only from")
	fmt.Println("                a source that implements Enumerator.")
	fmt.Println("        That is a real asymmetry and it is not hideable. What it is")
	fmt.Println("        NOT is 'map-keyed addresses are Dump-only': they are Load-")
	fmt.Println("        able on any enumerating plane, which is most of them.")

	// (d) The failure has to be loud, per ADR-0001.
	fmt.Println("\n    (d) the failure mode")
	plain, _ := bindOpen(ctx, YAMLSource{Path: file}, NewAddressSet(nil))
	_, canEnum := plain.(Enumerator)
	fmt.Printf("        plain YAMLSource implements Enumerator? %v\n", canEnum)
	fmt.Println("        Loading a map-typed field from a non-enumerating source is")
	fmt.Println("        therefore an error naming the field and the source, never a")
	fmt.Println("        silently empty map. Silently ignoring anything is ruled out")
	fmt.Println("        by ADR-0001, and an empty map is the most plausible-looking")
	fmt.Println("        wrong answer available.")
	fmt.Println("        Whether any supported Go type produces these addresses at")
	fmt.Println("        all is #7's; whether an absent map is empty or defaulted is")
	fmt.Println("        #8's. This ADR only fixes whether the contract can express it.")
}

// YAMLEnumSource is drv_yaml's source plus the optional upgrade, kept here so
// the cost of the upgrade is visible on its own.
type YAMLEnumSource struct{ Path string }

func (s YAMLEnumSource) Bind(*AddressSet) (Binding, error) { return s, nil }

func (s YAMLEnumSource) Open(ctx context.Context) (Reader, error) {
	r, err := (YAMLSource{Path: s.Path}).Open(ctx)
	if err != nil {
		return nil, err
	}
	return yamlEnumReader{r.(yamlReader)}, nil
}

type yamlEnumReader struct{ yamlReader }

func (r yamlEnumReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	n := r.root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for _, seg := range prefix.Segments() {
		var next *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Kind == yaml.MappingNode && n.Content[i].Value == seg.Text {
				next = n.Content[i+1]
			}
		}
		if next == nil {
			return nil, nil
		}
		n = next
	}
	var out []Path
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, prefix.Name(n.Content[i].Value))
		}
	case yaml.SequenceNode:
		for i := range n.Content {
			out = append(out, prefix.Index(i))
		}
	}
	return out, nil
}

var _ = strings.TrimSpace
