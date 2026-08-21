//go:build protoe

package yaml

// startWatch is the protoe half of the build-tagged pair, and it starts
// nothing: under this build there is no callback option, and watching is the
// conversion in protoe_watched.go.
func startWatch([]SourceOption, string) {}
