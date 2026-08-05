package valueseam

// Driver-side stand-ins: in ferry these live with env, http and kv
// (K1: compositions per driver). They are here so the seam has real
// cells to prove.

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// BoolWords: Spelling[bool, string]. Accept may be wider than the
// write form (law 2); render always writes truthy/falsy.
func BoolWords(truthy, falsy string, accept ...string) (Spelling[bool, string], error) {
	if len(accept)%2 != 0 {
		return nil, fmt.Errorf("accept list wants truthy/falsy pairs, got %d words", len(accept))
	}
	table := map[string]bool{truthy: true, falsy: false}
	for i, w := range accept {
		table[w] = i%2 == 0
	}
	return SpellingFunc(
		func(text string) (bool, error) {
			b, ok := table[text]
			if !ok {
				return false, fmt.Errorf("bool spelling %q is not an accepted word", text)
			}
			return b, nil
		},
		func(v bool) (string, error) {
			if v {
				return truthy, nil
			}
			return falsy, nil
		},
	), nil
}

// Negated: Transform[bool] for negative-polarity variables.
type negated struct{}

func (negated) Apply(b bool) (bool, error)  { return !b, nil }
func (negated) Invert(b bool) (bool, error) { return !b, nil }

func Negated() Transform[bool] { return negated{} }

// Base64: Spelling[[]byte, string] with the query-safe alphabet.
type base64Spelling struct{}

func (base64Spelling) Parse(text string) ([]byte, error) {
	return base64.URLEncoding.DecodeString(text)
}

func (base64Spelling) Render(v []byte) (string, error) {
	return base64.URLEncoding.EncodeToString(v), nil
}

func Base64() Spelling[[]byte, string] { return base64Spelling{} }

// MaxSize: Transform[[]byte] whose Apply refuses oversize, pre-write.
type maxSize struct{ n int }

func (m maxSize) Apply(v []byte) ([]byte, error) {
	if len(v) > m.n {
		return nil, fmt.Errorf("payload is %d bytes, the plane's budget is %d", len(v), m.n)
	}
	return v, nil
}

func (m maxSize) Invert(v []byte) ([]byte, error) { return v, nil }

func MaxSize(n int) Transform[[]byte] { return maxSize{n: n} }

// Gzip: Transform[[]byte] whose Invert refuses corrupt data.
type gzipT struct{}

func (gzipT) Apply(v []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(v); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (gzipT) Invert(v []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(v))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func Gzip() Transform[[]byte] { return gzipT{} }

// Raw: Spelling[[]byte, []byte] — the kv carrier proof.
type rawSpelling struct{}

func (rawSpelling) Parse(v []byte) ([]byte, error)  { return v, nil }
func (rawSpelling) Render(v []byte) ([]byte, error) { return v, nil }

func Raw() Spelling[[]byte, []byte] { return rawSpelling{} }

// YAMLishNumber: Spelling[string, string] canonicalising a plane's
// number spellings (hex, octal, binary, underscores) to the decimal
// text a Number payload carries — the #259 fix in miniature. The
// payload type is the canonical text itself.
func YAMLishNumber() Spelling[string, string] {
	return SpellingFunc(
		func(text string) (string, error) {
			clean := strings.ReplaceAll(text, "_", "")
			n, err := strconv.ParseInt(clean, 0, 64) // base 0: 0x, 0o, 0b
			if err != nil {
				return "", fmt.Errorf("number spelling %q: %w", text, err)
			}
			return strconv.FormatInt(n, 10), nil
		},
		func(v string) (string, error) {
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return "", fmt.Errorf("payload %q is not canonical decimal text: %w", v, err)
			}
			return v, nil // canonical-out; original spelling is memo territory
		},
	)
}
