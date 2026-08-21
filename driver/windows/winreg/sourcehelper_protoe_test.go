//go:build protoe

package winreg_test

import "github.com/onhotpath/ferry/driver/windows/winreg"

// source is the read half over one store. It takes no Option list under this
// build, because the one source-only Option this driver had was the callback
// watch and watching is now a conversion on the source itself.
func source(store winreg.Registry) *winreg.Source {
	return winreg.NewSource(winreg.CurrentUser, base, winreg.Store(store))
}
