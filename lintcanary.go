//go:build ferrylintcanary

package ferry

// deadOnPurpose is never called, and that is the point.
//
// `make lint-canary` builds core with the ferrylintcanary tag and asserts that
// the unused linter reports this function. Nothing else in the tree ever sees
// it: without the tag this file is not part of any package, so the ordinary
// lint, vet, build and test runs do not compile it.
//
// The assertion exists because #271 asked whether unused was running at all.
// It was, but nothing said so. golangci-lint hosts unused inside the shared
// goanalysis_metalinter runner together with staticcheck, so a bundled
// analyser that cannot read the toolchain's source takes unused down with it,
// and the pin that decides which analyser is bundled is moved by Renovate
// rather than by anyone reading .golangci.yml.
func deadOnPurpose() int { return 0 }
