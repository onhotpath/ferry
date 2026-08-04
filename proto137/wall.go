package main

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"

	"github.com/onhotpath/ferry"
)

// t0 is the one context every probe uses.
func t0() context.Context { return context.Background() }

// sectionWall demonstrates the route from a runtime reflect.Type to a walk, and
// where it stops.
func sectionWall() {
	reg := ferry.NewRegistry()
	if err := reg.Register(lossyMeters()); err != nil {
		fmt.Println("register:", err)

		return
	}

	fmt.Println("-- what (*Registry).Types() hands back")

	for _, t := range reg.Types() {
		fmt.Printf("  %-24s kind=%-8s pkg=%s  (a reflect.Type, and nothing else is readable off reg)\n",
			t, t.Kind(), t.PkgPath())
	}

	t := reg.Types()[0]

	fmt.Println()
	fmt.Println("-- what reflect.StructOf does buy: an annotated root type, built at run time")

	st := reflect.StructOf([]reflect.StructField{{
		Name: "Value",
		Type: t,
		Tag:  reflect.StructTag(`ferry:"value"`),
	}})

	root := reflect.New(st).Elem()
	root.Field(0).Set(reflect.ValueOf(Meters(1.0 / 3.0)).Convert(t))

	fmt.Printf("  reflect.StructOf -> %s\n", st)
	fmt.Printf("  a value of it    -> %#v, addressable=%v\n", root.Interface(), root.CanAddr())

	fmt.Println()
	fmt.Println("-- what it does not buy: every exported walk takes a type parameter")

	err := ferry.Dump(t0(), root.Interface(), newSpy(), ferry.WithRegistry(reg))
	fmt.Printf("  ferry.Dump(ctx, root.Interface(), sink)   T infers to `any`\n    -> %v\n", wrap(oneLine(fmt.Sprint(err))))

	perr := ferry.Compile[any](ferry.WithRegistry(reg))
	fmt.Printf("  ferry.Compile[any]()\n    -> %v\n", wrap(oneLine(fmt.Sprint(perr))))

	fmt.Println()
	fmt.Println("-- and the type parameter itself cannot be a variable: go build on proto137/_wall/wall.go")

	out, _ := exec.Command("go", "build", "-o", "/dev/null", "proto137/_wall/wall.go").CombinedOutput()
	fmt.Printf("%s", indent(string(out)))
}

// indent prefixes compiler output so it reads as output in a fenced block.
func indent(s string) string {
	var out string

	for _, line := range splitLines(s) {
		if line == "" {
			continue
		}

		out += "  " + line + "\n"
	}

	return out
}

// splitLines is strings.Split on newline, kept here so wall.go reads top to
// bottom.
func splitLines(s string) []string {
	var (
		out  []string
		cur  string
		runs = []rune(s)
	)

	for _, r := range runs {
		if r == '\n' {
			out = append(out, cur)
			cur = ""

			continue
		}

		cur += string(r)
	}

	return append(out, cur)
}
