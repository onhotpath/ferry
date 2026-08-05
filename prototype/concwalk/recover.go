package concwalk

// The R-card follow-up: release is deferred (ruled), and a codec panic
// should surface in the error chain rather than crash the caller. The open
// question is the recover's SCOPE. This prototypes the narrow fence:
// recover wraps exactly the call into user code (a codec half, a
// caller-supplied callable), never ferry's own walk logic - so a ferry bug
// still crashes honestly while a user codec bug becomes an addressed,
// aggregatable error and the walk continues with the other leaves.

import "fmt"

// errCodecPanic marks errors minted from a recovered user-code panic, so a
// caller (and ferrytest) can tell them from ordinary refusals.
type errCodecPanic struct {
	addr  string
	value any
}

func (e *errCodecPanic) Error() string {
	return fmt.Sprintf("leaf %s: codec panicked: %v", e.addr, e.value)
}

// guarded runs one user-code call under a recover fence. The fence is the
// call, not the walk: f contains ONLY the user's half, so a panic in ferry's
// own logic never reaches this recover.
func guarded(addr string, f func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = &errCodecPanic{addr: addr, value: p}
		}
	}()
	return f()
}

// loadLeaves is a miniature walk over leaves whose codecs may panic:
// every leaf is attempted, panics become addressed errors, and the
// aggregate carries them next to ordinary failures.
func loadLeaves(plane map[string]string, codecs map[string]func(string) (string, error)) (map[string]string, []error) {
	out := make(map[string]string, len(plane))
	var errs []error
	for _, k := range sortedKeys(plane) {
		err := guarded(k, func() error {
			v, err := codecs[k](plane[k])
			if err != nil {
				return fmt.Errorf("leaf %s: %w", k, err)
			}
			out[k] = v
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return out, errs
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
