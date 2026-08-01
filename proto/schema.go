package main

// A cut-down schema compiler, carried over from the #4 prototype. It exists
// only to produce an address set from a type so the contract probes have
// something realistic to run against. It is not the subject of this ticket.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type leaf struct {
	Path  Path
	Field string
	Type  reflect.Type
}

type schema struct {
	leaves []leaf
	addrs  *AddressSet
}

var compiles int // counts real compilations, to show the cache is real

func compile[T any]() (*schema, error) { return compileType(reflect.TypeFor[T]()) }

var schemaCache = map[reflect.Type]*schema{}

func compileCached[T any]() (*schema, error) {
	t := reflect.TypeFor[T]()
	if s, ok := schemaCache[t]; ok {
		return s, nil
	}
	s, err := compileType(t)
	if err != nil {
		return nil, err
	}
	schemaCache[t] = s
	return s, nil
}

func compileType(t reflect.Type) (*schema, error) {
	compiles++
	s := &schema{}
	if err := walk(t, Path{}, "", s); err != nil {
		return nil, err
	}
	// ADR-0003's core-side rule: the address set is prefix-free, and a path
	// is a prefix of itself, so this subsumes exact duplicates.
	seen := map[Path]leaf{}
	var clashes []string
	for _, l := range s.leaves {
		if prev, ok := seen[l.Path]; ok {
			clashes = append(clashes, fmt.Sprintf("address %s is claimed by both %s and %s", l.Path, prev.Field, l.Field))
			continue
		}
		seen[l.Path] = l
	}
	for _, l := range s.leaves {
		for p := l.Path.Parent(); !p.IsRoot(); p = p.Parent() {
			if prev, ok := seen[p]; ok {
				clashes = append(clashes, fmt.Sprintf("address %s (%s) is a prefix of %s (%s)", p, prev.Field, l.Path, l.Field))
			}
		}
	}
	if len(clashes) > 0 {
		slices.Sort(clashes)
		return nil, fmt.Errorf("schema %s: %s", t, strings.Join(slices.Compact(clashes), "; "))
	}
	paths := make([]Path, len(s.leaves))
	for i, l := range s.leaves {
		paths[i] = l.Path
	}
	s.addrs = NewAddressSet(paths)
	return s, nil
}

func walk(t reflect.Type, at Path, goPath string, s *schema) error {
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("ferry")
		name, _, _ := strings.Cut(tag, ",")
		if !ok {
			name = f.Name
		}
		gp := goPath + "." + f.Name
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && !isLeafStruct(ft) {
			next := at
			if !strings.Contains(tag, ",squash") {
				next = at.Name(name)
			}
			if err := walk(ft, next, gp, s); err != nil {
				return err
			}
			continue
		}
		s.leaves = append(s.leaves, leaf{Path: at.Name(name), Field: gp[1:], Type: f.Type})
	}
	return nil
}

func isLeafStruct(t reflect.Type) bool { return t.String() == "time.Time" }
