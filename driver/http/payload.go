package ferryhttp

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/onhotpath/ferry"
)

// Base64 spells a byte payload as base64 text, which is how a []byte field is
// carried by a plane that holds nothing but text.
//
//	src := ferryhttp.NewHeaderSource(ferryhttp.BytesAs(ferryhttp.Base64()))
//
// It reads and writes standard base64 with padding, which is what a header
// field and a query parameter both survive unchanged. A value spelled in none
// of it is refused rather than half-decoded, and the refusal names the offset
// the decoder stopped at and never the text: everything this plane holds came
// off the wire, so a message quoting it is a token in a log.
//
// Stack payload steps under it with [ferry.With]:
//
//	ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))
//
// Hand the result to [BytesAs], which is where this plane's spelling is
// declared and where the sharp edge of declaring one is written down.
func Base64() ferry.Spelling[[]byte, string] {
	return ferry.SpellingFunc(parseBase64, renderBase64)
}

// parseBase64 is the reading half, and the refusal it makes quotes nothing.
//
// ADR-0018's law 4 scopes its exception to the message whose whole content is
// the offending text, and this plane is the one where that exception is not
// taken: this driver's published promise is that a refusal names the parameter
// or field and never what it held, because the values are attacker-supplied.
// What is actionable here is structural anyway - base64 reports the byte the
// decoder stopped at - so the promise costs the message nothing (ADR-0011).
func parseBase64(text string) ([]byte, error) {
	payload, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("%w: this value is not standard base64 text: %w", ferry.ErrValue, err)
	}

	return payload, nil
}

// renderBase64 is the writing half, and it cannot fail: every byte sequence has
// one standard base64 spelling, which is ADR-0018's law 3 holding by
// construction rather than by a check.
func renderBase64(payload []byte) (string, error) {
	return base64.StdEncoding.EncodeToString(payload), nil
}

// Gzip compresses a payload on the way out and decompresses it on the way in.
//
//	ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))
//
// It is a payload step and not a spelling, so it runs under whichever spelling
// carries the bytes: written as above, a dump caps the payload, compresses it
// and spells the result as base64, and a load undoes exactly that.
//
// Data the plane held that is not a gzip stream, or is one that was cut short,
// is refused rather than returned half-read.
//
// The sharp edge is on the way in, and it is the reason a size step belongs in
// the same stack: a compressed payload expands before anything under it sees
// the result, so a small request can decompress into a large allocation, and
// [MaxSize] refuses it only once it is already in memory. Bound what reaches
// this plane at the server - net/http's own header limit, or http.MaxBytesReader
// - rather than here.
func Gzip() ferry.Transform[[]byte] { return gzipPayload{} }

// gzipPayload is [Gzip]'s implementation, and it holds nothing: the option
// takes no data, so its two halves close over nothing a caller could keep a
// handle to and change later (ADR-0018).
type gzipPayload struct{}

// Apply compresses on the way out.
func (gzipPayload) Apply(payload []byte) ([]byte, error) {
	var out bytes.Buffer

	w := gzip.NewWriter(&out)

	_, err := w.Write(payload)
	if err == nil {
		err = w.Close()
	}

	if err != nil {
		// Unreachable: the only writer under this one is a bytes.Buffer, whose
		// Write never fails, so a failure here would be the compressor
		// reporting one nothing produced. It is returned rather than dropped
		// because a step that swallowed it would write a truncated stream.
		return nil, fmt.Errorf("%w: this payload could not be compressed: %w", ferry.ErrValue, err)
	}

	return out.Bytes(), nil
}

// Invert decompresses on the way in, and refuses what it cannot undo.
//
// The reader is not closed, because it holds nothing: what it reads from is a
// byte slice already in memory, and every error a Close would report has
// already been reported by the read that ended the stream.
func (gzipPayload) Invert(payload []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, notGzip(err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		return nil, notGzip(err)
	}

	return out, nil
}

// notGzip is the inbound refusal, and it names the decompressor's own reason
// and nothing the plane held, for [parseBase64]'s reason.
func notGzip(err error) error {
	return fmt.Errorf("%w: this value is not a gzip stream, so it cannot be read back: %w", ferry.ErrValue, err)
}

// MaxSize refuses a payload larger than n bytes, in both directions.
//
//	ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))
//
// It is a payload step and not a spelling, and it is the one that refuses on the
// way out as well as on the way in: a payload past the budget fails before
// anything is written, which is where a failure that can be known without
// touching the plane belongs. On the way in it is the last step to run, so the
// size it holds is the size of the payload a field is about to be given rather
// than the size of what arrived on the wire.
//
// The bytes it counts are the bytes at its own position in the stack. Written
// as above it caps the payload itself, on both sides of the compression;
// written as ferry.With(ferryhttp.Base64(), ferryhttp.MaxSize(4<<10),
// ferryhttp.Gzip()) it caps the compressed form instead.
//
// A refusal names both sizes and nothing else, because a size is structure
// rather than something the plane supplied.
func MaxSize(n int) ferry.Transform[[]byte] { return maxSize{n: n} }

// maxSize is [MaxSize]'s implementation. It holds a number rather than a
// function, which is the whole of what keeps its two halves pure (ADR-0018).
type maxSize struct{ n int }

// Apply refuses a payload too large to write.
func (m maxSize) Apply(payload []byte) ([]byte, error) {
	return m.within(payload, "written to")
}

// Invert refuses a payload too large to have come from this plane, which is the
// same budget read from the other side.
func (m maxSize) Invert(payload []byte) ([]byte, error) {
	return m.within(payload, "read from")
}

// within is the one check both halves are, so that the two cannot drift.
func (m maxSize) within(payload []byte, direction string) ([]byte, error) {
	if len(payload) > m.n {
		return nil, fmt.Errorf("%w: this payload is %d bytes and at most %d may be %s this request",
			ferry.ErrValue, len(payload), m.n, direction)
	}

	return payload, nil
}
