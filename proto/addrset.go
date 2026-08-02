package main

// The minimum of contract.go this probe needs.

import "errors"

var errorsNew = errors.New

type AddressSet struct{ addrs []Path }

func NewAddressSet(addrs []Path) *AddressSet { return &AddressSet{addrs: sortedPaths(addrs)} }
func (a *AddressSet) All() []Path            { return a.addrs }
func (a *AddressSet) Len() int               { return len(a.addrs) }

var ErrReadOnly = errorsNew("plane is read only")

// AsNumber is the raw source text of a Number, which is what a numeric leaf
// needs: the whole point is that it is never materialised as a machine number
// in the middle.
func (v Value) AsNumber() (string, error) {
	if v.kind != VNumber {
		return "", errKind
	}
	return v.text, nil
}
