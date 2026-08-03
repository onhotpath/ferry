module github.com/onhotpath/ferry

// ADR-0002: core's require block stays empty, unconditionally. Core's
// buildability must not depend on anything a consumer can switch off, so it
// takes no non-stdlib dependency and no `toolchain` directive either. The
// toolchain pin lives in go.work, which consumers never see.
go 1.27
