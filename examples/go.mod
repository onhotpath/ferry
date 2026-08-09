module github.com/onhotpath/ferry/examples

// ADR-0002: a module in this workspace declares its own `go` directive and may
// raise it above core's, never lower it, because the floor is transitive
// through imports and the lever only points up. No `toolchain` directive here
// either; no file in this repository carries one.
//
// The floor is go 1.26 while #366 is open, not core's usual 1.27, because 1.27
// is not yet GA.
go 1.26

// There is deliberately no `require` on github.com/onhotpath/ferry, even though
// this module imports it, and this is the same convention driver/env and
// driver/yaml carry for the same reason. Core has no v* tag, so v0.0.0 cannot
// be resolved from the proxy. go.work resolves core sibling-on-disk meanwhile,
// and CI's GOWORK=off job skips itself until the tag exists. The first
// `git tag v0.1.0` on core is the event that changes this: the require lands
// then, and not before.
//
// This module also takes no third-party dependency, deliberately. An example is
// read before it is run, and a reader should not have to resolve a helper
// library to follow one. That keeps the require block empty in both directions.
//
// No `replace` directive either, ever. That is ADR-0002's rule rather than a
// convenience, and CI fails a build that checks one in.
