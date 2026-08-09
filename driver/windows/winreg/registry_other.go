//go:build !windows

package winreg

// open is the machine's own registry, and on an operating system that has none
// there is nothing to open.
//
// The refusal reaches the caller at Bind rather than at the first read, which is
// ADR-0004's line read exactly: Bind may refuse for what it can see without
// touching the plane, and the plane not existing on this operating system is
// exactly that. A [Store] of the caller's own is resolved before this is reached,
// which is what makes this package testable, and usable, everywhere.
func open(Hive, string, View) (Registry, error) { return nil, noRegistry() }
