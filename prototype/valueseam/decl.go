package valueseam

import "errors"

// Decl is the D2 sealed declaration type: the unexported field means
// no composite literal and no conversion exists outside the package,
// so the four package values are the only obtainable non-zero
// inhabitants. The zero value is Go's uneliminable residue, refused
// at Register.
type Decl struct{ k VKind }

var (
	DeclBool   = Decl{k: KindBool}
	DeclNumber = Decl{k: KindNumber}
	DeclString = Decl{k: KindString}
	DeclBytes  = Decl{k: KindBytes}
)

// ErrZeroDecl is the loud refusal of the one forgeable value.
var ErrZeroDecl = errors.New("declaration is the zero Decl; use DeclBool, DeclNumber, DeclString or DeclBytes")

// Register stands in for the codec registration gate.
func Register(d Decl) error {
	if d == (Decl{}) {
		return ErrZeroDecl
	}
	return nil
}
