package env

import (
	"errors"
	"fmt"
	"time"

	"github.com/onhotpath/ferry"
)

// ErrWatch reports a watch this driver could not open.
//
// [WatchFiles] with no [DotEnv] beside it, and a directory that is not there, are
// both this: a watch that succeeded silently and never fired is the failure mode
// the option exists to avoid, so it is refused at Bind instead.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper.
var ErrWatch = errors.New("env: this watch could not be opened")

// settle is how long the watcher waits for a file to stop changing before it
// calls back.
//
// One editor save produces several events - a write, a rename, a chmod - and a
// reload per event is several reloads of one change. Fifty milliseconds is long
// enough to swallow that burst and short enough that a reload still lands while
// the operator is looking at the terminal.
const settle = 50 * time.Millisecond

// watchError states the class this driver has an opinion about and keeps
// [ErrWatch] reachable underneath it.
func watchError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrWatch, msg)
}
