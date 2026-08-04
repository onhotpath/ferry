package main

import (
	"context"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// section is the optional-section shape: a pointer to a struct is the only
// thing that can be nil at a container address and still have fields under it.
type section struct {
	Name string `ferry:"name"`
}

// shapes is every container shape #136 asks about, in one struct so that one
// dump answers the whole table.
type shapes struct {
	NilSlice   []string          `ferry:"nilslice"`
	EmptySlice []string          `ferry:"emptyslice"`
	FullSlice  []string          `ferry:"fullslice"`
	NilMap     map[string]string `ferry:"nilmap"`
	EmptyMap   map[string]string `ferry:"emptymap"`
	FullMap    map[string]string `ferry:"fullmap"`
	NilPtr     *section          `ferry:"nilptr"`
	SetPtr     *section          `ferry:"setptr"`
	NilPtrSl   *[]string         `ferry:"nilptrslice"`
	Array      [2]string         `ferry:"array"`
}

func shapesValue() shapes {
	return shapes{
		EmptySlice: []string{},
		FullSlice:  []string{"a", "b"},
		EmptyMap:   map[string]string{},
		FullMap:    map[string]string{"k": "v"},
		SetPtr:     &section{Name: "n"},
		Array:      [2]string{"p", "q"},
	}
}

// sec1 asks what Dump hands a sink, per address, for every container shape.
// ferrytest.Record is a dump into a sink that keeps what it was handed, so this
// is the real walk and no plane is involved.
func sec1() {
	head("1. What Dump writes at a container address")

	mapped, err := ferrytest.Record(context.Background(), shapesValue())
	if err != nil {
		fmt.Println("Record:", err)

		return
	}

	for _, addr := range sorted(mapped) {
		row(addr.String(), show(mapped[addr]))
	}

	sub("the container addresses themselves, and whether a Value landed on one")

	for _, name := range []string{
		"nilslice", "emptyslice", "fullslice",
		"nilmap", "emptymap", "fullmap",
		"nilptr", "setptr", "nilptrslice", "array",
	} {
		at := ferry.At(name)

		v, ok := mapped[at]
		if !ok {
			row(at.String(), "nothing written at the container address")

			continue
		}

		row(at.String(), show(v))
	}
}
