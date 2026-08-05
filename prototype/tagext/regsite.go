package tagext

// Round 3, the owner's two missing pieces, prototyped:
//
//   B2 - WHERE the declaration registers: on the Registry, beside the
//        codecs, under construction-is-the-freeze. The registry is
//        already the OUTER level of the schema cache, so a declaration
//        carried by it joins the cache key with zero new machinery, and
//        there is no mutable phase to race (the #227 lesson, kept).
//
//   B3 - WHERE the values arrive: the address-keyed table rides the
//        AddressSet, which is ALREADY the bind-time handoff every driver
//        receives. A driver reads its own key's view at Bind - no caller
//        plumbing, no drift. Out-of-band consumers (docs, validation)
//        get the same table from an exported function.

import "fmt"

// Registry is the miniature of core's: codecs elided, the declaration
// aboard, complete at construction.
type Registry struct {
	decl Decl
}

// NewRegistry is the single registration site: codecs and tag-key
// extensions together, frozen on the line the value is born.
func NewRegistry(exts ...KeyExtension) (*Registry, error) {
	d, err := DeclareKeys("ferry", exts...)
	if err != nil {
		return nil, err
	}
	return &Registry{decl: d}, nil
}

// AddressSet is the miniature of core's, grown by one method: the
// compiled extension table rides it.
type AddressSet struct {
	addrs []string
	table ExtTable
}

// Extension returns this key's address-keyed view - what a driver reads
// at Bind. A key nobody declared yields an empty view, never an error:
// absence of an extension is not a defect.
func (a *AddressSet) Extension(key string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for addr, words := range a.table {
		for full, val := range words {
			if k, w, _ := cut(full, ':'); k == key {
				if out[addr] == nil {
					out[addr] = map[string]string{}
				}
				out[addr][w] = val
			}
		}
	}
	return out
}

func cut(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// CompileWith is the miniature of schemaOf: fields' raw tags parsed under
// the registry's declaration; the table lands on the AddressSet.
func CompileWith(r *Registry, fields map[string]string) (*AddressSet, error) {
	set := &AddressSet{table: ExtTable{}}
	for addr, raw := range fields {
		if _, err := ParseField(addr, raw, "ferry", r.decl, set.table); err != nil {
			return nil, fmt.Errorf("%s: %w", addr, err)
		}
		set.addrs = append(set.addrs, addr)
	}
	return set, nil
}

// yamlSinkMini is the #156 consumer in miniature: a driver whose Bind
// reads ITS OWN key's view off the AddressSet it already receives -
// the caller constructed the sink with zero table plumbing.
type yamlSinkMini struct {
	nodeTags map[string]string // addr -> node tag, e.g. "!mycompany:duration"
}

func (s *yamlSinkMini) Bind(addrs *AddressSet) error {
	s.nodeTags = map[string]string{}
	for addr, words := range addrs.Extension("yamlext") {
		if tag, ok := words["node"]; ok {
			s.nodeTags[addr] = "!" + tag
		}
	}
	return nil
}
