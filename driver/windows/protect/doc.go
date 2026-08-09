// Package protect encrypts the values a struct marks as secret on their way
// into a plane, and decrypts them on the way back out, with Windows DPAPI-NG.
//
// It is a decorator rather than a plane of its own. It wraps somebody else's
// [ferry.Source] and [ferry.Sink] - the registry, a key-value store, a file -
// and changes what is stored at the addresses a field marked, leaving every
// other address and everything else about that plane alone.
//
//	type Config struct {
//	    Auth struct {
//	        RefreshToken string `ferry:"refresh_token" protect:"secret"`
//	    } `ferry:"auth"`
//	}
//
//	reg := ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))
//
//	src := protect.Over(store, protect.LocalSystem, protect.FromTags())
//	cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(reg))
//
// Which addresses are secret comes from the struct, in the protect tag key,
// because secrecy is a property of the field rather than of a deployment: a
// refresh token is a secret on every plane and in every environment, and a list
// of addresses kept beside the struct goes stale the day somebody adds a field
// to it. There is one word, secret, and there is deliberately no word that
// takes protection away.
//
// The tag key has to be declared on the registry the schema is compiled
// against, and forgetting to is the one mistake that would be silent, since a
// key nobody declared parses to nothing and every marked value would be written
// in the clear. [FromTags] refuses at Bind instead, before anything is read or
// written.
//
// # What it protects against, and what it does not
//
// DPAPI-NG with [LocalSystem] means that decrypting a value requires running as
// the local system on the machine that encrypted it. That stops another user on
// the machine reading the store, and it stops a copy of the store being read on
// any other machine. It is not a vault: an attacker who takes the SYSTEM
// registry hive and the boot key recovers the value offline, at their leisure.
// Read the package's README for the whole of that statement.
//
// # Migration
//
// A value that is not protected yet is read back as it stands, and the next
// save writes it protected, so a deployment that predates this decorator
// migrates itself. The cost is stated in the README and accepted: anything that
// can write plaintext into the store can downgrade a field.
//
// # Windows
//
// The protection itself is a Windows API. Everywhere else a source or a sink
// built without [Using] refuses at Bind, and supplying a [Protector] of your own
// is what makes the package testable, and usable, elsewhere.
package protect
