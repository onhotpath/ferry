package main

// The "any struct" constraint question, settled by compiling rather than by
// repeating the research doc.
//
// This file compiles. The commented lines do not, and the diagnostic each
// produces is recorded next to it, verified on go1.27rc2.

// A constraint whose type set is "any type whose underlying type is a struct"
// cannot be written. `~struct{}` is a valid constraint element, and its type
// set is exactly the types whose underlying type is the EMPTY struct.
type structish interface{ ~struct{} }

type empty struct{}

func e1Empty[T structish](t T) T { return t }

var _ = e1Empty(empty{})

// e1Empty(E1DB{}) does not compile:
//   E1DB does not satisfy structish (possibly missing ~ for struct{...} in structish)
//
// There is no wildcard. `interface{ ~struct{ ... } }` names one shape; there
// is no `~struct` and no `comparable`-style predeclared "any struct".
//
// So Load[T any] cannot reject Load[int] at compile time, and ErrNotStruct
// stays a runtime error under EVERY candidate entry point. #16 does not get
// to claim otherwise.
//
// What the type parameter does delete is ErrNotPointer: there is no `&` for a
// caller to forget, because there is no destination argument at all.
