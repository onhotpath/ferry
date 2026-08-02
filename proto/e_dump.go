package main

// A Dump that talks to a real writer and can aggregate, which dumpD cannot:
// dumpD records Set calls in memory and never fails one, so it cannot answer
// the question Dump actually raises for #9 - aggregating on Load costs nothing
// outside the process, and on Dump it costs WRITES.
//
// Kept separate from dumpD so #8's probes are untouched.

import "reflect"

// eWriter is ADR-0004's Writer, minus the context this probe does not need.
type eWriter interface {
	Set(p Path, v Value) error
}

// recSink is the non-staging sink: every Set hits the plane immediately. This
// is ADR-0004's "http PUT per key" row, and it is the one with something to
// lose.
type recSink struct {
	written  []Path
	attempts int
	failOn   map[string]bool
}

func (r *recSink) Set(p Path, v Value) error {
	r.attempts++
	if r.failOn[p.String()] {
		return errorsNew("kv: 403 no write ACL")
	}
	r.written = append(r.written, p)
	return nil
}

// stageSink is ADR-0004's Committer: writes are staged and the plane only
// changes at Commit, which runs ONLY on success.
type stageSink struct {
	staged   []Path
	attempts int
	plane    []Path
	failOn   map[string]bool
}

func (s *stageSink) Set(p Path, v Value) error {
	s.attempts++
	if s.failOn[p.String()] {
		return errorsNew("kv: 403 no write ACL")
	}
	s.staged = append(s.staged, p)
	return nil
}
func (s *stageSink) Commit() { s.plane = append(s.plane, s.staged...) }

func dumpE(v reflect.Value, s *schema, w eWriter, sink *errSink) error {
	emit := func(e *Error) error {
		if sink != nil {
			sink.add(e)
			return nil
		}
		return e
	}
	var rec func(v reflect.Value, p, sp Path) error
	rec = func(v reflect.Value, p, sp Path) error {
		opts := s.at(sp)
		if opts.omitzero && v.IsZero() {
			return nil
		}
		switch classify(v.Type()) {
		case shapeLeaf:
			val, err := encLeaf(v)
			if err != nil {
				// An encode failure is deterministic and per-field, and it
				// happens BEFORE any write, so aggregating it costs the plane
				// nothing at all.
				return emit(errAt(mWalk, ErrValue, p, "%s", "cannot be represented as "+val.Kind().String()).withCause(err))
			}
			if err := w.Set(p, val); err != nil {
				return emit(fromDriver(mWalk, p, true, err))
			}
			return nil
		case shapePointer:
			if v.IsNil() {
				if err := w.Set(p, Null()); err != nil {
					return emit(fromDriver(mWalk, p, true, err))
				}
				return nil
			}
			return rec(v.Elem(), p, sp)
		case shapeStruct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() {
					continue
				}
				n, _, _ := fieldTag(f)
				if err := rec(v.Field(i), p.Name(n), sp.Name(n)); err != nil {
					return err
				}
			}
			return nil
		case shapeSlice:
			if v.Len() == 0 && v.Kind() != reflect.Array {
				return emitSet(w, emit, p, Null())
			}
			for i := range v.Len() {
				esp := sp.Name("*")
				if v.Kind() == reflect.Array {
					esp = sp.Index(i)
				}
				if err := rec(v.Index(i), p.Index(i), esp); err != nil {
					return err
				}
			}
			return nil
		case shapeMap:
			if v.Len() == 0 {
				return emitSet(w, emit, p, Null())
			}
			keys := v.MapKeys()
			sortMapKeys(keys)
			for _, k := range keys {
				if err := rec(v.MapIndex(k), p.Name(mapKeyText(k)), sp.Name("*")); err != nil {
					return err
				}
			}
			return nil
		}
		return emit(errAt(mCompile, ErrSchema, p, "unsupported type %s", v.Type()))
	}
	if err := rec(v, Path{}, Path{}); err != nil {
		return err
	}
	return sink.result()
}

func emitSet(w eWriter, emit func(*Error) error, p Path, val Value) error {
	if err := w.Set(p, val); err != nil {
		return emit(fromDriver(mWalk, p, true, err))
	}
	return nil
}
