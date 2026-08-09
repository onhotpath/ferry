module github.com/onhotpath/ferry

// ADR-0002: core's require block stays empty, unconditionally. Core's
// buildability must not depend on anything a consumer can switch off, so it
// takes no non-stdlib dependency and no `toolchain` directive either; no file
// in this repository carries one.
//
// The floor is go 1.26, not the go 1.27 ADR-0001 sets, because 1.27 is not yet
// GA. errors.AsType is what holds it at 1.26. Tracked by #366, reverted to
// 1.27 the day it ships.
go 1.26
