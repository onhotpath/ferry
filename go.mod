module github.com/onhotpath/ferry

// ADR-0002: core's require block stays empty, unconditionally. Core's
// buildability must not depend on anything a consumer can switch off, so it
// takes no non-stdlib dependency and no `toolchain` directive either; no file
// in this repository carries one.
//
// The floor is go 1.26, not the go 1.27 ADR-0001 first set, and it stays there
// now that 1.27 has shipped: 1.27 was the floor for encoding/json/v2 alone,
// and the require block above is why that reason was never spent. errors.AsType
// is what holds it as high as 1.26 (#391).
go 1.26
