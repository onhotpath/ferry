// Command step3 renders the same addresses under each plausible env key
// function.
package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/proto125/kf"
)

func main() {
	addrs := []ferry.Path{
		ferry.At("db", "host"),
		ferry.At("db.host"),
		ferry.At("limits", "extra", "http.port"),
		ferry.At("limits", "extra", "http_port"),
		ferry.At("feature-flags"),
		ferry.At("labels", "app.name"),
		ferry.At("tags").Elem(0),
	}

	fns := kf.EnvThree()

	fmt.Println("A: env, uppercase, join with _, no character transform")
	fmt.Println("B: env, uppercase, join with _, illegal character -> _")
	fmt.Println("C: env, uppercase, join with _, refuse an illegal character")
	fmt.Println()
	fmt.Printf("%-26s  %-24s  %-24s  %s\n", "address", "A", "B", "C")

	for _, addr := range addrs {
		fmt.Printf("%-26s  %-24s  %-24s  %s\n", addr.String(),
			render(fns[0].F, addr), render(fns[1].F, addr), render(fns[2].F, addr))
	}
}

func render(f ferry.KeyFunc, addr ferry.Path) string {
	key, err := f(addr)
	if err != nil {
		return "REFUSED: " + err.Error()
	}

	return key
}
