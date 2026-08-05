package concwalk

// The #254 half of the prototype: release must be deferred, and
// closed-without-Commit must remain the abort signal on the panic path.
//
// entryShipped mirrors entry.go:141/entry.go:191 - join(walked, released(w))
// on the straight line, so a panic anywhere in the walk unwinds past release
// and the handle leaks.
//
// entryDeferred is the proposed rule: release is deferred unconditionally,
// Commit stays on the success path only, and the panic itself is let through
// untouched - by the time it reaches the caller the plane has already been
// told "closed without Commit", which is the abort signal ADR-0004 promised.

import "errors"

// plane records the protocol it observed.
type plane struct {
	committed bool
	closed    bool
}

func (p *plane) Commit() error { p.committed = true; return nil }
func (p *plane) Close() error  { p.closed = true; return nil }

// entryShipped is the shape at entry.go:175-192 today.
func entryShipped(p *plane, walk func() error) error {
	walked := walk()
	if walked == nil {
		walked = p.Commit()
	}
	return errors.Join(walked, p.Close())
}

// entryDeferred is the proposed rule. The named return lets the deferred
// release join its error into the normal path; on a panic the release still
// runs and the panic continues unwinding.
func entryDeferred(p *plane, walk func() error) (err error) {
	defer func() {
		err = errors.Join(err, p.Close())
	}()
	err = walk()
	if err == nil {
		err = p.Commit()
	}
	return err
}
