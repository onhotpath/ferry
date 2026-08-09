package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
)

const rootVarName = "APP_PORT"

func present() []string { return []string{rootVarName + "=8080"} }
func absent() []string  { return []string{"OTHER=1"} }

func environOf(e []string) func() []string { return func() []string { return e } }

func rootSrc(e []string) ferry.Source {
	return env.New(env.Environ(environOf(e)), env.RootVar(rootVarName))
}

// runDecl reports one case, with the sentinel matches spelled out.
func runDecl[T any](label string, got T, err error) {
	if err == nil {
		report(label, nil, fmt.Sprintf("%#v", got))

		return
	}

	report(label, err, "")
	fmt.Printf("  %-32s ErrMissing=%v ErrPlane=%v ErrSchema=%v\n", "",
		errors.Is(err, ferry.ErrMissing), errors.Is(err, ferry.ErrPlane), errors.Is(err, ferry.ErrSchema))
}

// trimmedShape is the ratified API: ferry.RootRequired as a value, and no
// RootDefault, because a seed is the caller's default.
func trimmedShape() {
	head("Load - no seed")
	loadCase("Load, present, no declaration", present())
	loadCase("Load, absent, no declaration", absent())
	loadCase("Load, present, RootRequired", present(), ferry.RootRequired)
	loadCase("Load, absent, RootRequired", absent(), ferry.RootRequired)
	loadCase("Load, RootRequired twice", absent(), ferry.RootRequired, ferry.RootRequired)

	head("LoadOver - the seed IS the default")
	overCase("LoadOver 4242, present, no declaration", present(), 4242)
	overCase("LoadOver 4242, absent, no declaration", absent(), 4242)
	overCase("LoadOver 4242, absent, RootRequired", absent(), 4242, ferry.RootRequired)
	overCase("LoadOver 4242, present, RootRequired", present(), 4242, ferry.RootRequired)

	head("no regression")
	structRootCase()
	cacheCase()
	dumpCase()
	immutabilityCase()
}

func loadCase(label string, environ []string, opts ...ferry.Option) {
	defer guard(label)

	got, err := ferry.Load[int](context.Background(), rootSrc(environ), opts...)
	runDecl(label, got, err)
}

func overCase(label string, environ []string, seed int, opts ...ferry.Option) {
	defer guard(label)

	got, err := ferry.LoadOver(context.Background(), seed, rootSrc(environ), opts...)
	runDecl(label, got, err)
}

type portHolder struct {
	Port int `ferry:"port"`
}

func structRootCase() {
	defer guard("struct root")

	src := env.New(env.Environ(environOf(absent())))

	got, err := ferry.Load[portHolder](context.Background(), src, ferry.RootRequired)
	runDecl("struct root + RootRequired, absent", got, err)

	got, err = ferry.Load[portHolder](context.Background(), env.New(env.Environ(environOf([]string{"PORT=1"}))))
	runDecl("struct root, no declaration, present", got, err)
}

// cacheCase asks whether one flag keys two schemas.
func cacheCase() {
	defer guard("cache")

	runDecl("Compile[int], RootRequired", 0, ferry.Compile[int](ferry.RootRequired))
	runDecl("Compile[int], no declaration", 0, ferry.Compile[int]())

	a, aerr := ferry.Load[int](context.Background(), rootSrc(absent()), ferry.RootRequired)
	b, berr := ferry.Load[int](context.Background(), rootSrc(absent()))

	report("cache: same type, two Option sets", nil,
		fmt.Sprintf("required -> (%d, err=%v) / none -> (%d, err=%v)", a, aerr != nil, b, berr != nil))
}

func dumpCase() {
	defer guard("dump")

	kvDumpWith(8080, ferry.RootRequired)
	kvDumpWith(0, ferry.RootRequired)
}

// immutabilityCase is the var-versus-constructor question, asked of the
// compiler rather than of the reader.
func immutabilityCase() {
	// This compiles, and it is the whole safety argument: rootRequired is a
	// struct{}, so the only value assignable to the exported var is the value
	// it already holds. An Option-typed var would accept ferry.TagKey("x")
	// here, process-wide.
	ferry.RootRequired = ferry.RootRequired

	got, err := ferry.Load[int](context.Background(), rootSrc(absent()), ferry.RootRequired)
	runDecl("after reassigning ferry.RootRequired", got, err)
}
