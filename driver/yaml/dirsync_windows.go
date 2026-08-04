package yaml

// syncDir does nothing on Windows, which has no directory fsync to call:
// FlushFileBuffers wants a file handle and refuses a directory one, so there is
// no call here that could be made and no failure to report (#187).
//
// The rename is still atomic, because that is MoveFileEx's own guarantee rather
// than something this driver builds on top of it. What a Windows caller does not
// get is the second half: the replacement is durable when the filesystem gets
// around to it, and a save that returned nil is not proof it already has.
func syncDir(_ string) error { return nil }
