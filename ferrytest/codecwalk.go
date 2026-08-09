package ferrytest

import (
	"context"
	"reflect"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/internal/valuewalk"
)

// valueWalker is core's reflect.Value-rooted walk, declared here over ferry's
// own types and recovered from the internal seam by one assertion.
//
// It is what lets a suite handed nothing but a *[ferry.Registry] walk the types
// that registry holds. [ferry.Dump] compiles its schema from its type
// parameter, a type parameter is fixed at compile time, and
// [ferry.Registry.Types] hands back reflect.Type - so without this there is no
// route from a registered type to a walk over it, which is the wall #109
// records for [Golden] and #137 measured for [Codec].
//
// Nothing here is exported and nothing in core is either. The seam lives under
// internal/, so it is unreachable outside this module, and whether a
// reflect.Value root should be a public capability stays #134's question rather
// than one a test harness settled from the side.
type valueWalker interface {
	DumpValue(ctx context.Context, v reflect.Value, sink ferry.Sink, opts []ferry.Option) error
	LoadValue(ctx context.Context, dst reflect.Value, src ferry.Source, opts []ferry.Option) error
}

// coreWalk is the seam, and coreWalkOK is whether this build of core published
// one whose shape matches the interface above.
//
// The assertion is safe at package-variable scope because a package's
// dependencies are fully initialised - variables and init functions both -
// before its own variables are, and core installs the seam from an init.
var coreWalk, coreWalkOK = valuewalk.Seam.(valueWalker)

// dumpZero is what ferry writes for the zero value of one registered type,
// through the caller's own registry and the real walk.
//
// The zero value is the one value core holds without being given one, which is
// the same bound [Register]'s totality check works under. Anything wider needs
// values, and values are what a [Proof] carries.
func (c *codecRun) dumpZero(t reflect.Type) (ferry.Value, error) {
	return c.dumpRoot(reflect.New(t).Elem())
}

// dumpRoot records what ferry encodes at the root address, which is where a
// type resolving to a leaf sits (ADR-0003, ADR-0010).
func (c *codecRun) dumpRoot(root reflect.Value) (ferry.Value, error) {
	rec := recording(nowhere{})

	if err := coreWalk.DumpValue(context.Background(), root, rec, c.with(c.reg)); err != nil {
		return ferry.Value{}, err
	}

	return rec.seen[ferry.Path{}], nil
}

// loadInto builds a t from a plane holding exactly v at the root address, and
// hands back the populated value.
func (c *codecRun) loadInto(t reflect.Type, v ferry.Value) (reflect.Value, error) {
	dst := reflect.New(t).Elem()
	src := Static(map[ferry.Path]ferry.Value{{}: v})

	if err := coreWalk.LoadValue(context.Background(), dst, src, c.with(c.reg)); err != nil {
		return reflect.Value{}, err
	}

	return dst, nil
}

// textOf is the text a [ferry.Value] carries, for the three kinds that carry
// one, and false for the three that do not.
//
// Comparing across kinds is deliberate. A registration over a type's own text
// pair may name either [ferry.KindString] or [ferry.KindNumber], so "did ferry
// write the appender's bytes" is a question about the text and not about the
// kind.
func textOf(v ferry.Value) (string, bool) {
	switch v.Kind() {
	case ferry.KindString:
		s, err := v.AsString()

		return s, err == nil
	case ferry.KindNumber:
		s, err := v.AsNumber()

		return s, err == nil
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return string(b), err == nil
	default:
		// Absent, Null and Bool. None of the three is a text a codec chose:
		// the first two are the plane's answer about presence, and a Bool's
		// text is the constructor's rather than the codec's.
		return "", false
	}
}
