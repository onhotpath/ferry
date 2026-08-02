//go:build windows

package main

// The real hive behind the same wStore seam the fake satisfies, so every W3
// finding is re-run against Windows rather than against a model of it.

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

type wReal struct {
	root registry.Key
	// ro forces every open to ask for QUERY_VALUE only, which W0 measured as
	// the deterministic way to produce ERROR_ACCESS_DENIED on a runner that
	// can otherwise write anywhere.
	ro bool
}

func (r wReal) open(sub string, write bool) (registry.Key, error) {
	access := uint32(registry.QUERY_VALUE | registry.ENUMERATE_SUB_KEYS)
	if write && !r.ro {
		access |= registry.SET_VALUE | registry.CREATE_SUB_KEY
	}
	if write && !r.ro {
		k, _, err := registry.CreateKey(r.root, sub, access)
		return k, err
	}
	return registry.OpenKey(r.root, sub, access)
}

func (r wReal) GetValue(sub, name string) (wVal, bool, error) {
	k, err := r.open(sub, false)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return wVal{}, false, nil
		}
		return wVal{}, false, err
	}
	defer k.Close()

	_, typ, err := k.GetValue(name, nil)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return wVal{}, false, nil
		}
		return wVal{}, false, err
	}
	switch typ {
	case registry.SZ, registry.EXPAND_SZ:
		s, _, err := k.GetStringValue(name)
		return wVal{typ: typ, s: s}, true, err
	case registry.DWORD, registry.QWORD:
		n, _, err := k.GetIntegerValue(name)
		return wVal{typ: typ, n: n}, true, err
	case registry.BINARY:
		b, _, err := k.GetBinaryValue(name)
		return wVal{typ: typ, b: b}, true, err
	case registry.MULTI_SZ:
		ss, _, err := k.GetStringsValue(name)
		return wVal{typ: typ, ss: ss}, true, err
	case registry.NONE:
		return wVal{typ: wNONE}, true, nil
	}
	return wVal{typ: typ}, true, nil
}

func (r wReal) SetValue(sub, name string, v wVal) error {
	k, err := r.open(sub, true)
	if err != nil {
		return err
	}
	defer k.Close()
	switch v.typ {
	case wSZ:
		return k.SetStringValue(name, v.s)
	case wEXPAND_SZ:
		return k.SetExpandStringValue(name, v.s)
	case wDWORD:
		return k.SetDWordValue(name, uint32(v.n))
	case wQWORD:
		return k.SetQWordValue(name, v.n)
	case wBINARY:
		return k.SetBinaryValue(name, v.b)
	case wMULTI_SZ:
		return k.SetStringsValue(name, v.ss)
	case wNONE:
		// x/sys has no setter for REG_NONE, so the driver's own null is
		// written as an empty binary. That is a driver decision and W3's
		// REG_NONE row is measured against the fake for this reason.
		return k.SetBinaryValue(name, nil)
	}
	return errors.New("registry: unwritable type")
}

func (r wReal) ValueNames(sub string) ([]string, error) {
	k, err := r.open(sub, false)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer k.Close()
	return k.ReadValueNames(0)
}

func (r wReal) SubKeyNames(sub string) ([]string, error) {
	k, err := r.open(sub, false)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer k.Close()
	return k.ReadSubKeyNames(0)
}

func (r wReal) Close() error { return nil }

// wCleanReal deletes the probe tree so a rerun starts empty.
func wCleanReal(base string) {
	var rec func(sub string)
	rec = func(sub string) {
		k, err := registry.OpenKey(registry.CURRENT_USER, sub, registry.ALL_ACCESS)
		if err != nil {
			return
		}
		subs, _ := k.ReadSubKeyNames(0)
		k.Close()
		for _, s := range subs {
			rec(sub + `\` + s)
		}
		registry.DeleteKey(registry.CURRENT_USER, sub)
	}
	rec(base)
}
