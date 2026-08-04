// Package kf holds the key functions the #125 prototype measures, plus a
// minimal flat plane to run them through. Prototype code: no lint budget, no
// stability promise.
package kf

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/onhotpath/ferry"
)

// Join renders an address by joining its segment texts with sep, after running
// each text through transform. Index segments render as their base-10 text,
// which is what every flat plane does with a position.
func Join(addr ferry.Path, sep string, transform func(string) string) (string, error) {
	var b strings.Builder

	for seg := range addr.Segments() {
		if b.Len() > 0 {
			b.WriteString(sep)
		}

		text := transform(seg.Text())
		if text == "" {
			return "", errors.New("a segment renders to the empty string, and no plane can name that")
		}

		b.WriteString(text)
	}

	if b.Len() == 0 {
		return "", errors.New("the empty address has no plane key")
	}

	return b.String(), nil
}

// AsWritten is the identity transform.
func AsWritten(s string) string { return s }

// EnvUpper is ADR-0003's first published column: uppercase, join with "_", and
// no character transform. A dot survives into the key.
func EnvUpper(addr ferry.Path) (string, error) { return Join(addr, "_", strings.ToUpper) }

// EnvExact is ADR-0003's second published column: join with "_", no case fold,
// no character transform.
func EnvExact(addr ferry.Path) (string, error) { return Join(addr, "_", AsWritten) }

// Dotted is ADR-0003's third published column: join with ".", no fold.
func Dotted(addr ferry.Path) (string, error) { return Join(addr, ".", AsWritten) }

// EnvScrub is the transforming driver ADR-0003's prose argues for: uppercase,
// join with "_", and map every byte an environment variable name may not carry
// to "_". A dot is such a byte, so it folds into the separator.
func EnvScrub(addr ferry.Path) (string, error) {
	return Join(addr, "_", func(s string) string {
		b := []byte(strings.ToUpper(s))
		for i := range b {
			if !LegalEnvByte(b[i]) {
				b[i] = '_'
			}
		}

		return string(b)
	})
}

// EnvValidate is the third plausible env key function: uppercase, join with
// "_", and refuse outright any segment carrying a byte an environment variable
// name may not hold.
func EnvValidate(addr ferry.Path) (string, error) {
	var bad error

	key, err := Join(addr, "_", func(s string) string {
		up := strings.ToUpper(s)
		for i := range len(up) {
			if !LegalEnvByte(up[i]) {
				bad = fmt.Errorf("segment %q carries %q, which an environment variable name may not hold",
					s, string(up[i]))
			}
		}

		return up
	})

	if bad != nil {
		return "", bad
	}

	return key, err
}

// LegalEnvByte is POSIX's character set for an environment variable name:
// letters, digits and underscore. The prototype does not enforce the
// leading-digit rule, which is not what #125 is about.
func LegalEnvByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
		return true
	default:
		return false
	}
}

// Named pairs a key function with the label the prototype prints for it.
type Named struct {
	Label string
	F     ferry.KeyFunc
}

// EnvThree is the three plausible env key functions, in the order #125 asks for
// them.
func EnvThree() []Named {
	return []Named{
		{"env, uppercase + _, no char transform", EnvUpper},
		{"env, uppercase + _, illegal -> _", EnvScrub},
		{"env, uppercase + _, validating", EnvValidate},
	}
}

// FlatSink is a sink over a flat string-keyed plane, routing every write
// through core's own Keys. It is the shape ADR-0003 asks a flattening driver
// to have.
type FlatSink struct {
	Name  string
	F     ferry.KeyFunc
	Plane map[string]string
}

// NewFlatSink builds a flat sink over an empty plane.
func NewFlatSink(name string, f ferry.KeyFunc) *FlatSink {
	return &FlatSink{Name: name, F: f, Plane: map[string]string{}}
}

// Bind precomputes and checks the plane keys, which is where both of ADR-0003's
// driver-side checks land.
func (s *FlatSink) Bind(a *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	keys, err := ferry.NewKeys(a, s.Name, s.F)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (ferry.Writer, error) {
		return flatWriter{key: keys.Open(), plane: s.Plane}, nil
	}, nil
}

// Keys is the plane's contents, sorted.
func (s *FlatSink) Keys() []string { return slices.Sorted(maps.Keys(s.Plane)) }

type flatWriter struct {
	key   ferry.KeyFunc
	plane map[string]string
}

func (w flatWriter) Set(_ context.Context, addr ferry.Path, v ferry.Value) error {
	key, err := w.key(addr)
	if err != nil {
		return err
	}

	w.plane[key] = Text(v)

	return nil
}

// Text is the plain text a flat plane stores, because a flat plane holds
// strings and nothing else.
func Text(v ferry.Value) string {
	switch v.Kind() {
	case ferry.KindNumber:
		s, _ := v.AsNumber()

		return s
	case ferry.KindBool:
		b, _ := v.AsBool()

		return strconv.FormatBool(b)
	case ferry.KindBytes:
		b, _ := v.AsBytes()

		return string(b)
	case ferry.KindNull:
		return ""
	default:
		s, _ := v.AsString()

		return s
	}
}

// FlatSource is the read half over the same kind of plane. It enumerates by
// inverting its own key table, which is the only way a flat plane can answer
// Children.
type FlatSource struct {
	Name  string
	F     ferry.KeyFunc
	Plane map[string]string
}

// Bind precomputes and checks the plane keys.
func (s *FlatSource) Bind(a *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, err := ferry.NewKeys(a, s.Name, s.F)
	if err != nil {
		return nil, err
	}

	return func(context.Context) (ferry.Reader, error) {
		return &flatReader{key: keys.Open(), plane: s.Plane, prefixF: s.F}, nil
	}, nil
}

type flatReader struct {
	key     ferry.KeyFunc
	prefixF ferry.KeyFunc
	plane   map[string]string
}

func (r *flatReader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.key(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	text, ok := r.plane[key]
	if !ok {
		return ferry.Value{}, nil
	}

	return ferry.String(text), nil
}

// Children recovers the members under a container by taking the container's own
// plane key as a prefix and reading what follows as one segment.
//
// That is the inversion every flat source has to perform, and it is the whole
// of what a flat plane can do: a key holds no record of how many segments the
// driver joined to make it, so the remainder is taken whole. Whatever the key
// function folded away on the way out is gone on the way back.
func (r *flatReader) Children(_ context.Context, prefix ferry.Path) ([]ferry.Path, error) {
	head, err := r.prefixF(prefix)
	if err != nil {
		return nil, err
	}

	seen := map[string]ferry.Path{}

	for k := range r.plane {
		rest, ok := strings.CutPrefix(k, head+"_")
		if !ok {
			continue
		}

		child := prefix.At(rest)
		seen[child.String()] = child
	}

	out := slices.Collect(maps.Values(seen))
	slices.SortFunc(out, ferry.Path.Compare)

	return out, nil
}
