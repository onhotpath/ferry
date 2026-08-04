// Command step5 reproduces ADR-0003's published injectivity table, and adds
// the transforming key function its prose argues for as a fourth column.
package main

import (
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/proto125/kf"
)

func main() {
	rows := []struct {
		label string
		addrs []ferry.Path
	}{
		{"/DB/HOST, /DB_HOST", []ferry.Path{ferry.At("DB", "HOST"), ferry.At("DB_HOST")}},
		{"/myKey, /MyKey, /MYKEY", []ferry.Path{ferry.At("myKey"), ferry.At("MyKey"), ferry.At("MYKEY")}},
		{"/db.host, /db/host", []ferry.Path{ferry.At("db.host"), ferry.At("db", "host")}},
		{"/db/host, /db/port, /cache/host", []ferry.Path{
			ferry.At("db", "host"), ferry.At("db", "port"), ferry.At("cache", "host"),
		}},
	}

	cols := []kf.Named{
		{Label: "env, uppercase and _", F: kf.EnvUpper},
		{Label: "env, no fold and _", F: kf.EnvExact},
		{Label: "dotted, no fold", F: kf.Dotted},
		{Label: "env, transforming", F: kf.EnvScrub},
	}

	// The ADR's own published cells, for the first three columns only.
	published := [][]string{
		{"rejected", "rejected", "ok"},
		{"rejected", "ok", "ok"},
		{"ok", "ok", "rejected"},
		{"ok", "ok", "ok"},
	}

	fmt.Printf("%-32s", "Address set")

	for _, c := range cols {
		fmt.Printf("  %-26s", c.Label)
	}

	fmt.Println()

	for i, r := range rows {
		fmt.Printf("%-32s", r.label)

		for j, c := range cols {
			got := verdict(r.addrs, c.F)

			cell := got
			if j < len(published[i]) {
				cell = fmt.Sprintf("%s [ADR: %s]", got, published[i][j])
			}

			fmt.Printf("  %-26s", cell)
		}

		fmt.Println()
	}
}

func verdict(addrs []ferry.Path, f ferry.KeyFunc) string {
	if _, err := ferry.NewKeys(ferry.NewAddressSet(addrs...), "env", f); err != nil {
		return "rejected"
	}

	return "ok"
}
