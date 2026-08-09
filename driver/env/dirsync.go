//go:build !windows

package env

import "os"

// syncDir flushes the directory entry a rename wrote, which is the half of
// durability the staged file's own Sync does not buy.
//
// It opens the directory read-only, which is all fsync(2) needs and is the one
// mode a caller with no read bit on the directory cannot get - so a directory
// that cannot be opened is reported rather than skipped, since the alternative
// is a Dump that reports durability it did not obtain.
//
// The gosec suppression is the same one [readDotEnv] carries, for the same
// reason, and readDotEnv's comment is where it is written out.
//
// It is driver/yaml's syncDir copied rather than shared, because ADR-0002
// forbids the internal module that would carry it.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // G304: the directory is the plane's own, and naming it is the whole API.
	if err != nil {
		return err
	}

	err = d.Sync()

	// A read-only handle has nothing buffered of its own, so a close that fails
	// says nothing about whether the entry reached the disk, and the sync's own
	// answer is the one that decides the commit.
	_ = d.Close()

	return err
}
