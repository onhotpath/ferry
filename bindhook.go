//go:build !protoa

package ferry

// afterBind is the hook the typed watch prototype uses to move a capability
// refusal onto the Bind seam, and it is a build-tagged pair so that the default
// build carries none of it: this half does nothing, and the protoa half asserts
// the source can be watched.
func (c *config) afterBind(Source) error { return nil }
