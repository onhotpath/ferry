//go:build !windows

package protect

// open is the machine's own protection, and on an operating system with no
// DPAPI-NG there is nothing to open.
//
// The refusal reaches the caller at Bind rather than at the first read, which is
// ADR-0004's line read exactly: Bind may refuse for what it can see without
// touching the plane, and the protection API not existing on this operating
// system is exactly that. A [Protector] of the caller's own is resolved before
// this is reached, which is what makes this package testable, and usable,
// everywhere.
func open() (Protector, error) { return nil, noProtection() }
