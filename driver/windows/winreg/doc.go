// Package winreg loads configuration from the Windows registry into a Go struct,
// and writes a struct back into it.
//
//	src := winreg.NewSource(winreg.LocalMachine, `SOFTWARE\Example`)
//	cfg, err := ferry.Load[Config](ctx, src)
//
//	sink := winreg.NewSink(winreg.CurrentUser, `Software\Example`)
//	err = ferry.Dump(ctx, cfg, sink)
//
// # Addresses are subkeys and values
//
// The registry keeps two namespaces under every key: values, which hold data, and
// subkeys, which hold more of both. A field's address maps onto that exactly. A
// field tagged host is the value host under the key this driver was built over; a
// nested db.host is the value host under the subkey db; a slice tagged tags is the
// subkey tags holding the values 0, 1 and 2; a map of structs is the subkey it is
// tagged with, holding one subkey per key. A schema whose root is a single value
// is written at the key's own unnamed value, which regedit shows as (Default).
//
// A value host and a subkey host under one key are two different objects and both
// are legal, so a struct with a field tagged a and another tagged a holding a
// nested struct loads and saves.
//
// # The registry does not care about case, so this driver does
//
// Setting Host and then host leaves one value, named Host, holding the second
// write's data, and no error is raised anywhere. So this driver folds every part
// of an address to lower case before it compares them, and a schema naming two
// addresses that fold together is refused when the load starts, naming both. The
// case you wrote is what is stored, because the registry keeps whichever spelling
// wrote a name first.
//
// The fold is Go's own lower-casing and the registry's is Windows' own table, and
// the two agree on ASCII and can disagree outside it. Where they do, this one
// folds less, so the pair that gets through is one the registry would merge.
//
// # What a value is stored as
//
// Reading is wide and writing is narrow. A value stored as REG_SZ or
// REG_EXPAND_SZ arrives as text, one stored as REG_DWORD or REG_QWORD arrives as a
// number, and one stored as REG_BINARY arrives as bytes. REG_EXPAND_SZ is read
// exactly as it is stored, so %SystemRoot% reaches your field as those twelve
// characters and never as the directory. REG_MULTI_SZ is refused: it spells a
// sequence inside one value, and ferry addresses each element of a sequence in its
// own right.
//
// A save writes a []byte field as REG_BINARY and everything else as REG_SZ.
// REG_SZ is the one type that carries a number's own spelling intact, and 007,
// 3.14159265358979 and 18446744073709551615 all come back exactly as they went in.
//
// # Sharp edges
//
// A save replaces a value's type. An operator who retyped a value to REG_DWORD by
// hand gets it retyped back to REG_SZ on the next save. The data survives, the
// type annotation does not.
//
// A save is ordered and it is not atomic. The removals a slice or a map implies
// run first, then the keys a present-and-empty container needs, then every value
// in the order the walk produced it. A machine that fails half way through leaves
// the registry half way through.
//
// A registry string is UTF-16 and ends at its first NUL, so a Go string holding a
// NUL or holding bytes that are not valid UTF-8 is refused rather than stored
// mangled. Store those in a []byte field, which is written as REG_BINARY and
// carries every byte.
//
// A 32-bit process on 64-bit Windows is redirected into WOW6432Node without being
// told, so a 32-bit service and a 64-bit installer writing the same path write two
// different keys. Name the view with [WithView] rather than inheriting it.
//
// Writing under HKEY_LOCAL_MACHINE needs administrator rights, and a process
// without them is refused when the save starts rather than part way through it.
//
// Value data round-trips up to 16384 characters, and a value name may be at most
// 16383.
//
// # Everywhere that is not Windows
//
// The module builds and its tests run on every platform, and a source or a sink
// with no registry behind it refuses at Bind rather than pretending. [Store] is
// where a registry of your own is handed over, which is what a test supplies.
//
// The design records behind these decisions are in docs/adr/.
package winreg
