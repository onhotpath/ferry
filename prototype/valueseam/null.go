package valueseam

// Session 3b: the null escape hatch under payload-typed halves.
// One modifier covers all four kinds instead of four …OrNull twins.

// WithNull grafts a null policy onto any registration:
//
//	load:   what T a Null observation becomes
//	isNull: which T values dump as Null
//
// The closure law bends here knowingly: isNull(load()) should hold,
// and ferrytest checks it — a policy that loads a sentinel it cannot
// recognise on the way back is refused.
func WithNull[T any](r Reg, load func() (T, error), isNull func(T) bool) Reg {
	if load == nil || isNull == nil {
		panic(ErrNilHalf)
	}
	innerEncode, innerDecode := r.encode, r.decode
	r.encode = func(v any) (Value, error) {
		if isNull(v.(T)) {
			return Null(), nil
		}
		return innerEncode(v)
	}
	r.decode = func(v Value) (any, error) {
		if v.Kind() == KindNull {
			return load()
		}
		return innerDecode(v)
	}
	return r
}
