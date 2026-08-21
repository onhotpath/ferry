//go:build protoe

package env_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
	"github.com/onhotpath/ferry/ferrytest"
)

// TestWatchConformance is what the author of a watchable driver writes: one
// call, over a plane this test knows how to change and how to break.
func TestWatchConformance(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conf")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path := filepath.Join(dir, ".env")

	write := func(to string) {
		if err := os.WriteFile(path, []byte("HOST="+to+"\n"), 0o600); err != nil {
			t.Fatalf("writing the plane: %v", err)
		}
	}

	write("")

	ferrytest.Watchable(t, ferrytest.WatchPlane{
		Name:   "env",
		Open:   func() ferry.WatchableSource { return env.New(env.DotEnv(path), env.Environ(noEnviron)).Watched() },
		Change: write,
		Lose: func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("removing the directory the watch is on: %v", err)
			}
		},
		Unwatchable: func() ferry.WatchableSource {
			return env.New(env.Environ(noEnviron)).Watched()
		},
		Settle: 3 * time.Second,
	})
}
