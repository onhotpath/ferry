package keyform

import (
	"fmt"
	"maps"
	"slices"

	"github.com/onhotpath/ferry"
)

func sortedKeys[V any](m map[string]V) []string {
	out := slices.Collect(maps.Keys(m))
	slices.Sort(out)

	return out
}

func sortedAddrs(a *ferry.AddressSet) []ferry.Path {
	return slices.Collect(a.All())
}

func sprint(v any) string { return fmt.Sprintf("%#v", v) }

func show(v any) string { return fmt.Sprintf("%v", v) }
