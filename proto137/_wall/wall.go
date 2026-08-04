// Package main is the wall: this file does not compile, on purpose.
package main

import (
	"context"
	"reflect"

	"github.com/onhotpath/ferry"
)

// walkRuntimeType is what a suite handed only a *ferry.Registry would need.
func walkRuntimeType(reg *ferry.Registry, src ferry.Source) {
	for _, t := range reg.Types() {
		// A type parameter is fixed at compile time, so a reflect.Type cannot be
		// one, whatever it was built from.
		_, _ = ferry.Load[t](context.Background(), src, ferry.WithRegistry(reg))

		st := reflect.StructOf([]reflect.StructField{{Name: "Value", Type: t}})
		_, _ = ferry.Load[st](context.Background(), src, ferry.WithRegistry(reg))
	}
}

func main() {}
