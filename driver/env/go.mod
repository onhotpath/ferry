module github.com/onhotpath/ferry/driver/env

// ADR-0002: a driver module declares its own `go` directive and may raise it
// above core's, never lower it, because the floor is transitive through imports
// and the lever only points up. No `toolchain` directive here either; no file
// in this repository carries one.
//
// The floor is go 1.26 while #366 is open, not core's usual 1.27, because 1.27
// is not yet GA.
go 1.26

// Core is an ordinary dependency now, resolved from the proxy at the version
// this module was released against, which is where a driver says which core it
// works with. It carried a comment in place of this require until core's first
// v* tag existed, because v0.0.0 resolves from nothing.
//
// No `replace` stands in for it, ever. ADR-0002 bars one being checked in, and
// CI fails a build that finds one, because a checked-in replace means CI never
// once builds against the version a consumer resolves.

require github.com/onhotpath/ferry v0.2.0

require github.com/fsnotify/fsnotify v1.10.1

require golang.org/x/sys v0.13.0 // indirect
