package main

import (
	"context"
	"fmt"
)

type E9Conf struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=8080"`
}

type E9Bad struct {
	Host string `ferry:"host,requird"`
}

func runE9() {
	ctx := context.Background()
	empty := map[Path]Value{}

	fmt.Println("--- does Compile[T] answer a question Load can answer? ---")
	fmt.Printf("  Compile[E9Conf]()                 -> %v\n", Compile[E9Conf]())
	_, err := loadFrom(ctx, E9Conf{}, empty)
	fmt.Printf("  Load[E9Conf] from an EMPTY plane   -> %v\n", err)
	fmt.Println()
	fmt.Printf("  Compile[E9Bad]()                  -> %v\n", Compile[E9Bad]())
	_, err = loadFrom(ctx, E9Bad{}, empty)
	fmt.Printf("  Load[E9Bad] from an EMPTY plane    -> %v\n", err)

	fmt.Println("\n--- does Compile ever look at a value or a plane? ---")
	fmt.Println("  Compile[T](opts...) is schemaFor(reflect.TypeFor[T](), opts).")
	fmt.Println("  No Source argument exists in its signature, so no plane is reachable,")
	fmt.Println("  and no value of T is constructed at any point.")
}
