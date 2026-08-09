package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
)

// declCase is one cell of the required/default x present/absent matrix.
type declCase struct {
	name    string
	environ []string
	core    []ferry.Option
	root    []env.RootOption
}

const rootVarName = "APP_PORT"

func present() []string { return []string{rootVarName + "=8080"} }
func absent() []string  { return []string{"OTHER=1"} }

// runDecl reports what one case did, with the sentinel matches spelled out,
// because "which error class" is the whole question on the driver side.
func runDecl[T any](label string, got T, err error) {
	if err == nil {
		report(label, nil, fmt.Sprintf("%#v", got))

		return
	}

	report(label, err, "")
	fmt.Printf("  %-28s      ErrMissing=%v ErrPlane=%v ErrSchema=%v\n", "",
		errors.Is(err, ferry.ErrMissing), errors.Is(err, ferry.ErrPlane), errors.Is(err, ferry.ErrSchema))
}

// coreShape is candidate 1: the declaration is a core Option, and the driver
// knows nothing about it.
func coreShape() {
	head("SHAPE 1 - core Option: ferry.RootRequired / ferry.RootDefault")

	cases := []declCase{
		{name: "neither, present", environ: present()},
		{name: "neither, absent", environ: absent()},
		{name: "required, present", environ: present(), core: []ferry.Option{ferry.RootRequired()}},
		{name: "required, absent", environ: absent(), core: []ferry.Option{ferry.RootRequired()}},
		{name: "default, present", environ: present(), core: []ferry.Option{ferry.RootDefault("9999")}},
		{name: "default, absent", environ: absent(), core: []ferry.Option{ferry.RootDefault("9999")}},
		{
			name: "both, absent", environ: absent(),
			core: []ferry.Option{ferry.RootRequired(), ferry.RootDefault("9999")},
		},
	}

	for _, c := range cases {
		runCoreCase(c)
	}

	coreOverShape()
	coreStructRootShape()
	coreCacheShape()
	coreDumpShape()
}

// coreCacheShape asks the schema cache the question a compile-affecting Option
// has to answer: do two Options over one type key two schemas?
func coreCacheShape() {
	defer guard("cache")

	runDecl("Compile[int] required+default", 0, ferry.Compile[int](ferry.RootRequired(), ferry.RootDefault("1")))
	runDecl("Compile[int] required only", 0, ferry.Compile[int](ferry.RootRequired()))

	src := env.New(env.Environ(environOf(absent())), env.RootVar(rootVarName))

	a, _ := ferry.Load[int](context.Background(), src, ferry.RootDefault("111"))
	b, _ := ferry.Load[int](context.Background(), src, ferry.RootDefault("222"))
	c, _ := ferry.Load[int](context.Background(), src)

	report("cache: 111 / 222 / none", nil, fmt.Sprintf("%d / %d / %d", a, b, c))
}

// coreDumpShape asks what a load-side declaration means on the write side.
func coreDumpShape() {
	defer guard("dump")

	kvDumpWith(8080, ferry.RootDefault("9999"))
	kvDumpWith(0, ferry.RootDefault("9999"))
	kvDumpWith(0, ferry.RootRequired())
}

func runCoreCase(c declCase) {
	defer guard(c.name)

	// The root variable is named by the driver either way: naming is the
	// driver's and declaring is what this shape moves to core.
	src := env.New(env.Environ(environOf(c.environ)), env.RootVar(rootVarName))

	got, err := ferry.Load[int](context.Background(), src, c.core...)
	runDecl(c.name, got, err)
}

func environOf(e []string) func() []string { return func() []string { return e } }

// coreOverShape is the LoadOver interaction: a seed the caller already holds,
// and a plane that says nothing.
func coreOverShape() {
	defer guard("LoadOver")

	src := env.New(env.Environ(environOf(absent())), env.RootVar(rootVarName))

	got, err := ferry.LoadOver(context.Background(), 4242, src)
	runDecl("LoadOver seed=4242, absent", got, err)

	got, err = ferry.LoadOver(context.Background(), 4242, src, ferry.RootDefault("9999"))
	runDecl("LoadOver seed=4242, default", got, err)

	got, err = ferry.LoadOver(context.Background(), 4242, src, ferry.RootRequired())
	runDecl("LoadOver seed=4242, required", got, err)
}

type portHolder struct {
	Port int `ferry:"port"`
}

// coreStructRootShape asks what the same Options mean where the root is not a
// leaf, which is the case a core-side Option cannot avoid having an answer for.
func coreStructRootShape() {
	defer guard("struct root")

	src := env.New(env.Environ(environOf(absent())))

	got, err := ferry.Load[portHolder](context.Background(), src, ferry.RootRequired())
	runDecl("struct root + RootRequired", got, err)

	got, err = ferry.Load[portHolder](context.Background(), src, ferry.RootDefault("9999"))
	runDecl("struct root + RootDefault", got, err)
}

// driverShape is candidate 2: the declaration rides the driver's own
// root-naming option.
func driverShape() {
	head("SHAPE 2 - driver Option: env.RootVar(name, env.RootRequired()/RootDefault())")

	cases := []declCase{
		{name: "neither, present", environ: present()},
		{name: "neither, absent", environ: absent()},
		{name: "required, present", environ: present(), root: []env.RootOption{env.RootRequired()}},
		{name: "required, absent", environ: absent(), root: []env.RootOption{env.RootRequired()}},
		{name: "default, present", environ: present(), root: []env.RootOption{env.RootDefault("9999")}},
		{name: "default, absent", environ: absent(), root: []env.RootOption{env.RootDefault("9999")}},
		{
			name: "both, absent", environ: absent(),
			root: []env.RootOption{env.RootRequired(), env.RootDefault("9999")},
		},
	}

	for _, c := range cases {
		runDriverCase(c)
	}

	driverOverShape()
	driverDisagreementShape()
}

func runDriverCase(c declCase) {
	defer guard(c.name)

	src := env.New(env.Environ(environOf(c.environ)), env.RootVar(rootVarName, c.root...))

	got, err := ferry.Load[int](context.Background(), src)
	runDecl(c.name, got, err)
}

func driverOverShape() {
	defer guard("LoadOver")

	src := env.New(env.Environ(environOf(absent())), env.RootVar(rootVarName, env.RootDefault("9999")))

	got, err := ferry.LoadOver(context.Background(), 4242, src)
	runDecl("LoadOver seed=4242, default", got, err)
}

// driverDisagreementShape is the cost this shape carries and the other does
// not: one schema, two sources, two answers about the same schema fact.
func driverDisagreementShape() {
	defer guard("two sources disagree")

	strict := env.New(env.Environ(environOf(absent())), env.RootVar(rootVarName, env.RootRequired()))
	lax := env.New(env.Environ(environOf(absent())), env.RootVar(rootVarName))

	got, err := ferry.Load[int](context.Background(), strict)
	runDecl("source A (required), absent", got, err)

	got, err = ferry.Load[int](context.Background(), lax)
	runDecl("source B (not required), absent", got, err)
}
