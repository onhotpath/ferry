package main

// A fake hive whose storage model is the Registry's, so the driver above can
// be exercised anywhere.
//
// It is a fake and it is not a mock: it stores what the Registry stores, in
// the shape the Registry stores it, and it folds case the way the Registry
// folds it while PRESERVING the spelling on enumeration, which W0 measured on
// a real hive. Every behaviour it has was measured on the runner first.

import (
	"slices"
	"strings"
)

type wFake struct {
	// subkey path (folded) -> value name (folded) -> value
	vals map[string]map[string]wVal
	// the spelling as first written, which the Registry preserves
	spelling map[string]map[string]string
	subs     map[string][]string
}

func newFake() *wFake {
	return &wFake{
		vals:     map[string]map[string]wVal{},
		spelling: map[string]map[string]string{},
		subs:     map[string][]string{},
	}
}

func (f *wFake) GetValue(sub, name string) (wVal, bool, error) {
	m, ok := f.vals[strings.ToLower(sub)]
	if !ok {
		return wVal{}, false, nil
	}
	v, ok := m[strings.ToLower(name)]
	return v, ok, nil
}

func (f *wFake) SetValue(sub, name string, v wVal) error {
	ls := strings.ToLower(sub)
	if f.vals[ls] == nil {
		f.vals[ls] = map[string]wVal{}
		f.spelling[ls] = map[string]string{}
		f.mkParents(sub)
	}
	ln := strings.ToLower(name)
	if _, exists := f.spelling[ls][ln]; !exists {
		// The Registry preserves the spelling a value was FIRST created with.
		f.spelling[ls][ln] = name
	}
	f.vals[ls][ln] = v
	return nil
}

// mkParents records the subkey tree, because a Registry key exists whether or
// not it holds values.
func (f *wFake) mkParents(sub string) {
	parts := strings.Split(sub, `\`)
	for i := 1; i < len(parts); i++ {
		parent := strings.ToLower(strings.Join(parts[:i], `\`))
		child := parts[i]
		if !slices.ContainsFunc(f.subs[parent], func(s string) bool {
			return strings.EqualFold(s, child)
		}) {
			f.subs[parent] = append(f.subs[parent], child)
		}
	}
}

func (f *wFake) ValueNames(sub string) ([]string, error) {
	ls := strings.ToLower(sub)
	out := make([]string, 0, len(f.vals[ls]))
	for ln := range f.vals[ls] {
		out = append(out, f.spelling[ls][ln])
	}
	slices.Sort(out)
	return out, nil
}

func (f *wFake) SubKeyNames(sub string) ([]string, error) {
	out := slices.Clone(f.subs[strings.ToLower(sub)])
	slices.Sort(out)
	return out, nil
}

func (f *wFake) Close() error { return nil }

// dump is for probes, and it is sorted so a printed hive is deterministic.
func (f *wFake) dump() []string {
	var out []string
	for ls, m := range f.vals {
		for ln, v := range m {
			out = append(out, ls+" : "+f.spelling[ls][ln]+" = "+v.String())
		}
	}
	slices.Sort(out)
	return out
}
