module github.com/onhotpath/ferry/driver/kv

// ADR-0002: a driver module declares its own `go` directive and may raise it
// above core's, never lower it, because the floor is transitive through imports
// and the lever only points up. No `toolchain` directive here either; no file
// in this repository carries one.
//
// The floor is go 1.26 while #366 is open, not core's usual 1.27, because 1.27
// is not yet GA.
go 1.26

// There is deliberately no require on github.com/onhotpath/ferry, even though
// this module imports it. Core carries no v* tag yet, so `v0.0.0` cannot be
// resolved from the proxy and naming it breaks the build for any module that
// also loads a third-party graph. go.work resolves the sibling on disk
// meanwhile, and CI's GOWORK=off job skips itself by reading the repository's
// tag list rather than a flag.
//
// The first `git tag v0.1.0` on core is the event that turns this comment into
// an ordinary require. No `replace` stands in for it: ADR-0002 bars one, and CI
// fails the build if one is checked in, because a checked-in replace means CI
// never once builds against the version a consumer resolves.

// There is no go.sum, and that is a property rather than an omission: this
// module's only import outside the standard library is core, and the client is
// an interface with an in-repo fake behind it, so no third-party dependency is
// needed to reach a real key-value store in a test.
