// Command step4 runs each env key function through core's own NewKeys over
// address sets that contain a dot, and prints what Bind says.
package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/proto125/kf"
)

type set struct {
	label string
	addrs []ferry.Path
}

func main() {
	sets := []set{
		{"/db.host beside /db/host", []ferry.Path{
			ferry.At("db.host"), ferry.At("db", "host"),
		}},
		{"ADR-0003's own pair", []ferry.Path{
			ferry.At("limits", "extra", "http.port"), ferry.At("limits", "extra", "http_port"),
		}},
		{"/db.host alone", []ferry.Path{
			ferry.At("db.host"),
		}},
		{"/feature-flags alone", []ferry.Path{
			ferry.At("feature-flags"),
		}},
		{"/feature-flags beside /feature_flags", []ferry.Path{
			ferry.At("feature-flags"), ferry.At("feature_flags"),
		}},
	}

	for _, s := range sets {
		fmt.Printf("### %s\n", s.label)

		for _, addr := range s.addrs {
			fmt.Printf("    address: %s\n", addr)
		}

		for _, f := range kf.EnvThree() {
			keys, err := ferry.NewKeys(ferry.NewAddressSet(s.addrs...), "env", f.F)

			switch {
			case err != nil:
				fmt.Printf("  %-38s Bind REFUSED\n", f.Label)

				for _, e := range ferry.Elements(err) {
					fmt.Printf("      %s\n", e)
				}
			default:
				fmt.Printf("  %-38s Bind ok:", f.Label)
				open := keys.Open()

				for _, addr := range s.addrs {
					k, _ := open(addr)
					fmt.Printf(" %s->%s", addr, k)
				}

				fmt.Println()
			}
		}

		fmt.Println()
	}
}
