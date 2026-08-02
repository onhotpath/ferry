package main

// The Registry driver, written against ADR-0004's contract.
//
// It is one driver over two backends: a fake whose storage model is the
// Registry's (subkey -> value name -> {type, data}) which runs anywhere, and
// the real hive behind //go:build windows. Both run the SAME driver, so a
// finding on Linux is a finding about ferry and a finding on the runner is a
// finding about Windows, and neither is a finding about two different
// prototypes.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// --- the plane's own type system --------------------------------------------
//
// These are the Registry's value types, named as the Win32 API names them.
// The constants match golang.org/x/sys/windows/registry so the real backend
// needs no translation table.

const (
	wNONE      uint32 = 0
	wSZ        uint32 = 1
	wEXPAND_SZ uint32 = 2
	wBINARY    uint32 = 3
	wDWORD     uint32 = 4
	wMULTI_SZ  uint32 = 7
	wQWORD     uint32 = 11
)

func wTypeStr(t uint32) string {
	switch t {
	case wNONE:
		return "REG_NONE"
	case wSZ:
		return "REG_SZ"
	case wEXPAND_SZ:
		return "REG_EXPAND_SZ"
	case wBINARY:
		return "REG_BINARY"
	case wDWORD:
		return "REG_DWORD"
	case wMULTI_SZ:
		return "REG_MULTI_SZ"
	case wQWORD:
		return "REG_QWORD"
	}
	return "type(" + strconv.Itoa(int(t)) + ")"
}

// wVal is one Registry value: a type, and the data in whichever shape that
// type carries. This is the plane's vocabulary, not ferry's.
type wVal struct {
	typ uint32
	s   string   // SZ, EXPAND_SZ
	ss  []string // MULTI_SZ
	n   uint64   // DWORD, QWORD
	b   []byte   // BINARY, NONE
}

func (v wVal) String() string {
	switch v.typ {
	case wSZ, wEXPAND_SZ:
		return fmt.Sprintf("%s(%q)", wTypeStr(v.typ), v.s)
	case wMULTI_SZ:
		return fmt.Sprintf("%s(%q)", wTypeStr(v.typ), v.ss)
	case wDWORD, wQWORD:
		return fmt.Sprintf("%s(%d)", wTypeStr(v.typ), v.n)
	case wBINARY:
		return fmt.Sprintf("%s(%q)", wTypeStr(v.typ), string(v.b))
	}
	return wTypeStr(v.typ)
}

// wStore is the backend seam. The fake and the real hive both satisfy it.
type wStore interface {
	GetValue(sub, name string) (wVal, bool, error)
	SetValue(sub, name string, v wVal) error
	ValueNames(sub string) ([]string, error)
	SubKeyNames(sub string) ([]string, error)
	Close() error
}

// --- the ferry driver -------------------------------------------------------

type WRegSource struct {
	Store wStore
	Base  string
}

type WRegSink struct {
	Store wStore
	Base  string
	// NumberAs decides the one thing ADR-0004's Value cannot say. See W3.
	NumberAs uint32 // wDWORD or wQWORD
	// ReadOnly forces the ADR-0004 refusal without needing a denied hive.
	ReadOnly bool
}

func (s WRegSource) Bind(a *AddressSet) (FOpenFunc, error) {
	kf := wRegKey{base: s.Base}
	if _, err := kf.bind(a.All()); err != nil {
		return nil, err
	}
	return func(context.Context) (FReader, error) {
		return &wRegReader{store: s.Store, kf: kf}, nil
	}, nil
}

func (s WRegSink) Bind(a *AddressSet) (FOpenWriterFunc, error) {
	kf := wRegKey{base: s.Base}
	if _, err := kf.bind(a.All()); err != nil {
		return nil, err
	}
	num := s.NumberAs
	if num == 0 {
		num = wDWORD
	}
	return func(context.Context) (FWriter, error) {
		if s.ReadOnly {
			// ADR-0004: a plane writable in principle but not right now refuses
			// INSIDE OpenWriterFunc, wrapping ErrReadOnly. Not at Bind, because
			// writability is a fact about the plane and Bind does no I/O.
			return nil, fmt.Errorf("%w: registry: the key was opened without KEY_SET_VALUE", ErrReadOnly)
		}
		return &wRegWriter{store: s.Store, kf: kf, numberAs: num}, nil
	}, nil
}

type wRegReader struct {
	store wStore
	kf    wRegKey
}

func (r *wRegReader) Get(_ context.Context, p Path) (Value, error) {
	sub, name, err := r.kf.key(p)
	if err != nil {
		return Absent, err
	}
	v, ok, err := r.store.GetValue(sub, name)
	if err != nil {
		return Absent, err
	}
	if !ok {
		return Absent, nil
	}
	return wToFerry(v)
}

// Children enumerates a key's VALUE names and its SUBKEY names, and reports
// every child as a Name segment - which is the only thing this plane can
// honestly say, because it has no array type. W2 is why that is enough.
func (r *wRegReader) Children(_ context.Context, prefix Path) ([]Path, error) {
	sub, _, err := r.kf.subkeyOf(prefix)
	if err != nil {
		return nil, err
	}
	names, err := r.store.ValueNames(sub)
	if err != nil {
		return nil, err
	}
	subs, err := r.store.SubKeyNames(sub)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Path
	for _, n := range append(names, subs...) {
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		out = append(out, prefix.Name(n))
	}
	return sortedPaths(out), nil
}

func (r *wRegReader) Close() error { return nil }

type wRegWriter struct {
	store    wStore
	kf       wRegKey
	numberAs uint32
}

func (w *wRegWriter) Set(_ context.Context, p Path, v Value) error {
	sub, name, err := w.kf.key(p)
	if err != nil {
		return err
	}
	rv, err := wFromFerry(v, w.numberAs)
	if err != nil {
		return fmt.Errorf("registry: %s: %w", p, err)
	}
	return w.store.SetValue(sub, name, rv)
}

func (w *wRegWriter) Close() error { return nil }

// --- the mapping, which is the whole of #15's second question ---------------

// wToFerry is Load: a Registry value becomes an ADR-0004 Value.
func wToFerry(v wVal) (Value, error) {
	switch v.typ {
	case wSZ:
		return String(v.s), nil
	case wEXPAND_SZ:
		// The un-expanded text, because expanding is an interpretation and
		// ADR-0001 puts interpretation on the driver's side only where the
		// driver has a choice. Expanding here would make Load non-idempotent
		// against Dump: %SystemRoot% would come back as C:\Windows.
		return String(v.s), nil
	case wDWORD, wQWORD:
		return Number(strconv.FormatUint(v.n, 10)), nil
	case wBINARY:
		return Bytes(v.b), nil
	case wNONE:
		return Null(), nil
	case wMULTI_SZ:
		// THE HOLE. See W3. There is no ADR-0004 kind for a list of strings at
		// one address, and every available answer loses something.
		return Absent, fmt.Errorf("REG_MULTI_SZ has no ferry Value kind")
	}
	return Absent, fmt.Errorf("unknown registry type %d", v.typ)
}

// wFromFerry is Dump: an ADR-0004 Value becomes a Registry value.
func wFromFerry(v Value, numberAs uint32) (wVal, error) {
	switch v.Kind() {
	case VString:
		return wVal{typ: wSZ, s: v.Text()}, nil
	case VBytes:
		return wVal{typ: wBINARY, b: []byte(v.Text())}, nil
	case VNull:
		return wVal{typ: wNONE}, nil
	case VNumber:
		n, err := strconv.ParseUint(v.Text(), 10, 64)
		if err != nil {
			// A negative or fractional number has no Registry integer type at
			// all, so the driver has to fall back to text and lose the plane's
			// own typing. W3 measures what that costs.
			return wVal{typ: wSZ, s: v.Text()}, nil
		}
		if numberAs == wDWORD && n > 0xFFFFFFFF {
			return wVal{typ: wQWORD, n: n}, nil
		}
		return wVal{typ: numberAs, n: n}, nil
	case VBool:
		// The Registry has no boolean. Every convention in the wild is a DWORD
		// 0 or 1, and W3 measures what that does to the round trip.
		if v.Text() == "true" {
			return wVal{typ: wDWORD, n: 1}, nil
		}
		return wVal{typ: wDWORD, n: 0}, nil
	case VAbsent:
		return wVal{}, fmt.Errorf("ferry never hands a sink an Absent (ADR-0006)")
	}
	return wVal{}, fmt.Errorf("unknown value kind %v", v.Kind())
}

// subkeyOf is `key` for a CONTAINER address: every segment is a subkey.
func (f wRegKey) subkeyOf(p Path) (string, string, error) {
	sub := f.base
	for i, s := range p.Segments() {
		if strings.Contains(s.Text, `\`) {
			return "", "", fmt.Errorf("segment %d %q contains a backslash", i, s.Text)
		}
		sub += `\` + s.Text
	}
	return sub, "", nil
}

// WBoolConf and WNoBool are W3's single-question fixtures. They are package
// level for the go1.27rc2 linker reason recorded in t_fixture.go.
type WBoolConf struct {
	On bool `ferry:"on"`
}

type WNoBool struct {
	Name    string        `ferry:"name"`
	Port    int           `ferry:"port"`
	Big     int64         `ferry:"big"`
	Neg     int           `ferry:"neg"`
	Blob    []byte        `ferry:"blob"`
	Timeout time.Duration `ferry:"timeout"`
}

// WTenant is W5's fixture.
type WTenant struct {
	Name string `ferry:"name"`
	DB   WTDB   `ferry:"db"`
}

type WTDB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}

// WCaseConf is W4(e)'s fixture: a map key with capitals in it.
type WCaseConf struct {
	Limits map[string]int `ferry:"limits"`
}
