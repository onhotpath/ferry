// Command step1 prints what a ferry address is made of.
package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
)

func main() {
	for _, addr := range []ferry.Path{
		ferry.At("db", "host"),
		ferry.At("db.host"),
		ferry.At("limits", "extra", "http.port"),
		ferry.At("limits", "extra", "http_port"),
		ferry.At("tags").Elem(0),
	} {
		fmt.Printf("%-28s  %d segment(s)\n", addr.String(), count(addr))

		i := 0
		for seg := range addr.Segments() {
			fmt.Printf("      [%d] kind=%-5s text=%q\n", i, seg.Kind(), seg.Text())
			i++
		}
	}

	fmt.Println()
	fmt.Printf("At(\"db\",\"host\") == At(\"db.host\")  -> %v\n", ferry.At("db", "host") == ferry.At("db.host"))
	fmt.Printf("At(\"db\",\"host\") == At(\"db\",\"host\") -> %v\n", ferry.At("db", "host") == ferry.At("db", "host"))
}

func count(p ferry.Path) int {
	n := 0
	for range p.Segments() {
		n++
	}

	return n
}
