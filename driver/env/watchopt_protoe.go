//go:build protoe

package env

// startWatch is the protoe half of the build-tagged pair, and it opens nothing:
// under this build there is no callback option, and watching is the conversion
// in protoe_watched.go.
func startWatch([]Option, *config) {}
