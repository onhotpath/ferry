module github.com/onhotpath/ferry/driver/windows

// ADR-0002: a driver module declares its own `go` directive and may raise it
// above core's, never lower it, because the floor is transitive through imports
// and the lever only points up. No `toolchain` directive here either; go.work
// carries the pin.
go 1.27

// There is deliberately no `require` on github.com/onhotpath/ferry, even though
// this module imports it. Core carries no `v*` tag, so v0.0.0 cannot be resolved
// from the proxy, and a module that requires it fails to load the module graph
// the moment it also has a third-party dependency. go.work resolves core
// sibling-on-disk meanwhile, and CI's GOWORK=off job skips itself until the tag
// exists. The first `git tag v0.1.0` on core is the event that changes this: the
// require lands then, and not before.
//
// No `replace` directive either, ever. That is ADR-0002's rule rather than a
// convenience, and CI fails a build that checks one in.

require golang.org/x/sys v0.29.0
