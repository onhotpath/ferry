module github.com/onhotpath/ferry/driver/yaml

// ADR-0002: a driver module declares its own `go` directive and may raise it
// above core's, never lower it, because the floor is transitive through imports
// and the lever only points up. No `toolchain` directive here either; no file
// in this repository carries one.
//
// The floor is go 1.26 while #366 is open, not core's usual 1.27, because 1.27
// is not yet GA.
go 1.26

// This module imports core and does not require it, which is deliberate and
// temporary. Core carries no v* tag yet, so github.com/onhotpath/ferry@v0.0.0
// cannot be resolved from the proxy, and a module with any third-party
// requirement loads the full module graph and reads core's go.mod at the named
// version rather than taking the workspace copy - measured on this module, which
// fails to build with the require present where a dependency-free driver does
// not. go.work resolves core meanwhile.
//
// The event that changes it is core's first `git tag v0.1.0`: at that point a
// require on core belongs here, and CI's GOWORK=off job stops skipping itself.
// A `replace` is not the workaround - ADR-0002 forbids one being checked in, and
// CI fails the build if it finds one.
require go.yaml.in/yaml/v3 v3.0.5
