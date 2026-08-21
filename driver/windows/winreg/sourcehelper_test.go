//go:build !protoe

package winreg_test

import "github.com/onhotpath/ferry/driver/windows/winreg"

// source and sink are the two halves over one store, built the way this package's
// documentation tells a caller to build them: one list of settings, both halves.
func source(store winreg.Registry, opts ...winreg.Option) *winreg.Source {
	return winreg.NewSource(winreg.CurrentUser, base, append([]winreg.Option{winreg.Store(store)}, opts...)...)
}
