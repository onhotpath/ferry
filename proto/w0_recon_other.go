//go:build !windows

package main

// The non-Windows half of #15's entry point. Everything that needs a real hive
// is skipped by name rather than silently absent, so a run here says what it
// did not do.

const onWindows = false

func w15probes() []w15probe {
	return []w15probe{
		{"W0", "reconnaissance: what this runner can actually do", func() {}, true},
		{"W1", "does ADR-0003's address model express a Registry path", runW1, false},
		{"W2", "what an Enumerator can say about a plane with no list type", runW2, false},
		{"W3", "do natively typed values fit, or is there a lossy trip through strings", runW3, false},
		{"W4", "the same driver against a real hive: permissions, case, namespaces", func() {}, true},
		{"W5", "the second data point #25 asked for", runW5, false},
		{"W6", "can a codec and a named type close the REG_MULTI_SZ hole", runW6, false},
		{"W7", "is REG_MULTI_SZ common on a real install", func() {}, true},
	}
}
