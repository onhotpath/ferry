package protect_test

import (
	"context"
	"encoding/base64"
	"runtime"

	"github.com/onhotpath/ferry"
)

// runningOnWindows is what makes the one test about the absence of DPAPI-NG say
// out loud that it did not run rather than assert something false.
var runningOnWindows = runtime.GOOS == "windows"

// encoded is the spelling a stored ciphertext carries, which a test staging one
// by hand has to write the same way this package does.
func encoded(blob []byte) string { return base64.RawStdEncoding.EncodeToString(blob) }

// erringSource is a plane whose reads fail, which stages the one path through
// [protect.Over]'s reader that answers before it looks at the value at all.
type erringSource struct{}

func (erringSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return erringReader{}, nil }, nil
}

type erringReader struct{}

func (erringReader) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.Value{}, errRefused
}
