//go:build !protoc

package env

// afterNew is the hook the typed watch prototype uses to hand a driver its own
// built Source, and it is a build-tagged pair so that the default build carries
// none of it: this half does nothing, and the protoc half starts the watch.
func (c *config) afterNew(*Source) {}
