package yaml

import "os"

// stamp is what one stat says about the plane, reduced to the part that changes
// when the file does: it is there, it is this long, and it was last written at
// this instant.
//
// It is what a save compares: one taken at the open, one at the commit, and a
// file that changed in between is a save refused rather than an edit discarded.
// Nothing else reads it - the watch is a filesystem notification and takes no
// stamp at all (ADR-0020) - so it holds no mode, no owner and no path.
//
// The fields are all comparable, which is the whole reason the modification time
// is nanoseconds and not a [time.Time]: two stamps are equal or they are not,
// and a wall clock carrying a location pointer answers that question with a
// footnote.
//
// What it cannot see is a rewrite that lands in the same modification-time tick
// and leaves the length alone. That is the cost of a stat, it is stated in the
// godoc of the save that takes it, and the alternative is reading the whole
// file to hash it on a path whose budget is one stat (ADR-0020).
type stamp struct {
	size int64
	mod  int64
	here bool
}

// stampOf is one stat's answer, for a stat that succeeded.
func stampOf(fi os.FileInfo) stamp {
	return stamp{size: fi.Size(), mod: fi.ModTime().UnixNano(), here: true}
}

// planeStamp fingerprints the plane, taking the stat itself.
//
// A stat that fails for any reason is the same answer as a plane that is not
// there. Both callers compare one of these against another taken the same way,
// so a path that cannot be stat'ed at all is steady rather than wrong: what
// changes the answer is the file appearing, going away, or being written.
func planeStamp(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}

	return stampOf(fi)
}
