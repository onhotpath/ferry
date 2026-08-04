package main

import (
	"context"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// Config is the realistic struct #136 asks the question over.
type Config struct {
	Tags   []string       `ferry:"tags"`
	Limits map[string]int `ferry:"limits"`
}

func sec6() {
	head("6. What a user gets, per document")

	seed := Config{Tags: []string{"default"}, Limits: map[string]int{"rps": 10}}

	states := []struct {
		doc   string
		how   string
		store map[ferry.Path]ferry.Value
	}{
		{"no tags key at all", "nothing at /tags", map[ferry.Path]ferry.Value{}},
		{
			"tags: []", "Absent at /tags (ADR-0005's measured YAML driver)",
			map[ferry.Path]ferry.Value{},
		},
		{
			"tags: []", "Null at /tags (if a driver reported the node so)",
			map[ferry.Path]ferry.Value{ferry.At("tags"): ferry.Null()},
		},
		{
			"tags: null", "Null at /tags",
			map[ferry.Path]ferry.Value{ferry.At("tags"): ferry.Null()},
		},
		{
			"tags: [a, b]", "two addresses under /tags",
			map[ferry.Path]ferry.Value{
				ferry.At("tags").Elem(0): ferry.String("a"),
				ferry.At("tags").Elem(1): ferry.String("b"),
			},
		},
		{
			"what Dump writes for Tags: []string{}", "Null at /tags",
			map[ferry.Path]ferry.Value{ferry.At("tags"): ferry.Null()},
		},
	}

	fmt.Printf("  %-40s %-52s %-20s %s\n", "document", "what the plane reports", "Load[Config].Tags",
		"LoadOver(seed).Tags")

	for _, s := range states {
		v, err1 := ferry.Load[Config](context.Background(), ferrytest.Static(s.store))
		o, err2 := ferry.LoadOver(context.Background(), seed, ferrytest.Static(s.store))

		note := ""
		if err1 != nil || err2 != nil {
			note = fmt.Sprintf("  err %v %v", err1, err2)
		}

		fmt.Printf("  %-40s %-52s %-20s %#v%s\n", s.doc, s.how, fmt.Sprintf("%#v", v.Tags), o.Tags, note)
	}

	sub("the same rows for a map-typed field")

	mapStates := []struct {
		doc   string
		store map[ferry.Path]ferry.Value
	}{
		{"no limits key", map[ferry.Path]ferry.Value{}},
		{"limits: {}  reported Absent", map[ferry.Path]ferry.Value{}},
		{"limits: null, and what Dump writes for map[string]int{}",
			map[ferry.Path]ferry.Value{ferry.At("limits"): ferry.Null()}},
		{"limits: {rps: 5}", map[ferry.Path]ferry.Value{ferry.At("limits").At("rps"): ferry.Number("5")}},
	}

	for _, s := range mapStates {
		v, _ := ferry.Load[Config](context.Background(), ferrytest.Static(s.store))
		o, _ := ferry.LoadOver(context.Background(), seed, ferrytest.Static(s.store))
		fmt.Printf("  %-56s Load -> %-28s LoadOver(seed) -> %#v\n", s.doc,
			fmt.Sprintf("%#v", v.Limits), o.Limits)
	}

	sub("and the flat plane, where an empty Tags cannot be dumped at all")

	st := newFlatStore()
	err := ferry.Dump(context.Background(), Config{Tags: []string{}, Limits: map[string]int{"rps": 10}},
		flatSink{store: st})
	fmt.Println("  Dump ->", indent(err))
	fmt.Println("  the plane afterwards:", keysOf2(st))
}
