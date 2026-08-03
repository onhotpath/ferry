module github.com/onhotpath/ferry/driver/kv

// ADR-0002: a driver module declares its own `go` directive and may raise it
// above core's, never lower it, because the floor is transitive through imports
// and the lever only points up. No `toolchain` directive here either; go.work
// carries the pin.
go 1.27
