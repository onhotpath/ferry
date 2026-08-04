module github.com/onhotpath/ferry/proto/multimap

// Throwaway prototype for issue #193. Never merges. It is deliberately outside
// the root go.work so that nothing in the shipped tree sees it; the go.work
// beside this file is what resolves core sibling-on-disk.
//
// It carries third-party dependencies on purpose: the prior-art survey decodes
// the same query strings through gorilla/schema, go-playground/form and gin, so
// that "what the ecosystem expects" is a measurement rather than a memory.
// ADR-0002's empty-require rule is core's, and this module is not core.
go 1.27

