//go:build protoe

package winreg_test

import (
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestWatchConformance runs the watchable suite against the fake store, which
// is where this driver's contract can be exercised off Windows.
//
// The machine's own registry runs it too, on Windows, through the machine
// tests. What the fake proves here is the seam: register once, wait once, and a
// registration placed before the reload that consumes the last one.
func TestWatchConformance(t *testing.T) {
	store := newFake()

	ferrytest.Watchable(t, ferrytest.WatchPlane{
		Name: "winreg",
		Open: func() ferry.WatchableSource { return source(store).Watched() },
		Change: func(to string) {
			if err := store.Set(t.Context(), "", "host", winreg.Datum{Type: winreg.TypeString, Text: to}); err != nil {
				t.Fatalf("writing the plane: %v", err)
			}
		},
		// The fake decides at the start of a wait, so losing the watch is the
		// flag plus one change to end the wait that is already running. A real
		// registry fails the wait in flight.
		Lose: func() {
			store.failWatch()

			if err := store.Create(t.Context(), "poke"); err != nil {
				t.Fatalf("poking the store: %v", err)
			}
		},
		Unwatchable: func() ferry.WatchableSource { return source(quiet{newFake()}).Watched() },
		Settle:      3 * time.Second,
	})
}
