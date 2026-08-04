package perschema

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strconv"

	"github.com/onhotpath/ferry"
)

type headerCtxKey struct{}

// errNoPlane is what a load with no request in the context refuses with.
var errNoPlane = errors.New("header: no header block in the context")

// WithHeaders puts a request's headers in the context. The plane is per
// request, so it comes from the context and is never held by the Source
// (ADR-0012).
func WithHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, headerCtxKey{}, h)
}

func planeFrom(ctx context.Context) (values, error) {
	h, ok := ctx.Value(headerCtxKey{}).(http.Header)
	if !ok {
		return nil, errors.Join(ferry.ErrPlane, errNoPlane)
	}

	return h, nil
}

func illegal(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrIllegal, msg)
}

func required(n string) error {
	return fmt.Errorf("%w: %w: %q", ferry.ErrPlane, ErrRequired, n)
}

func repeatedAt(n string, held int) error {
	return fmt.Errorf("%w: %w: %q was declared a sequence and the plane holds %s at it, "+
		"and nothing read it as one", ferry.ErrPlane, ErrRepeated, n, strconv.Itoa(held)+" value(s)")
}

func joinAll(errs []error) error { return errors.Join(errs...) }

func sortedKeys[V any](m map[string]V) []string {
	out := slices.Collect(maps.Keys(m))
	sort.Strings(out)

	return out
}

// splitIndex reports the parent and position of an address whose last segment is
// an Index.
func splitIndex(addr ferry.Path) (ferry.Path, uint, bool) {
	var (
		parent ferry.Path
		last   ferry.Segment
		any    bool
	)

	for seg := range addr.Segments() {
		if any {
			parent = appendSeg(parent, last)
		}

		last, any = seg, true
	}

	if !any || last.Kind() != ferry.Index {
		return ferry.Path{}, 0, false
	}

	i, err := strconv.ParseUint(last.Text(), 10, 0)
	if err != nil {
		return ferry.Path{}, 0, false
	}

	return parent, uint(i), true
}

func appendSeg(p ferry.Path, s ferry.Segment) ferry.Path {
	if s.Kind() == ferry.Index {
		i, err := strconv.ParseUint(s.Text(), 10, 0)
		if err != nil {
			return p
		}

		return p.Elem(uint(i))
	}

	return p.At(s.Text())
}

// Header builds a header block from pairs, the way a request carries one.
func Header(pairs ...[2]string) http.Header {
	h := http.Header{}
	for _, p := range pairs {
		h.Add(p[0], p[1])
	}

	return h
}
