package ferry

// RootSentinel decides the address a root leaf compiles to.
//
// Empty means the empty path, which is the literal reading of "legalise the
// root leaf". A non-empty value means the root leaf sits at a one-Name-segment
// address spelled with that text, so every plane has a name to write.
//
// PROTOTYPE ONLY. This is a package-level knob because the four drivers are separate modules
// and a prototype has to flip the mode from all of them. Nothing like it would
// ship.
var RootSentinel = ""

// rootLeafSite is the site a root leaf occupies under the current mode.
func rootLeafSite() site {
	if RootSentinel == "" {
		return site{owned: true}
	}

	return site{addr: At(RootSentinel), owned: true}
}
