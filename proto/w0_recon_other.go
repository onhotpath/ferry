//go:build !windows

package main

// The non-Windows half of #15's entry point. Everything that needs a real hive
// is skipped by name rather than silently absent, so a run here says what it
// did not do.

const onWindows = false

func w15probes() []w15probe {
	return []w15probe{
		{"W0", "reconnaissance: what this runner can actually do", func() {}, true},
	}
}
