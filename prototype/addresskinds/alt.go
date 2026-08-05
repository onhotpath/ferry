package addresskinds

// The two rival spellings, implemented far enough to compare
// ergonomics, memory and dispatch cost against S1 (addr.go).

import "fmt"

// ── S2: one Path plus a Kind method ─────────────────────────────────

type PathKind uint8

const (
	PathLeaf PathKind = iota
	PathSection
	PathComposite
)

// KindedPath carries its classification at run time: 24 bytes to
// S1's 16, and every seam must check.
type KindedPath struct {
	p string
	k PathKind
}

func NewKindedPath(p string, k PathKind) KindedPath { return KindedPath{p: p, k: k} }
func (a KindedPath) Kind() PathKind                 { return a.k }
func (a KindedPath) String() string                 { return a.p }

// s2Get is what every driver Get becomes under S2: the wrong
// question still compiles, so the check runs per call and the
// failure is at run time — #219's class survives as a runtime error
// instead of dying at compile.
func s2Get(environ map[string]string, keys map[string]string, addr KindedPath) (Value, error) {
	if addr.Kind() != PathLeaf {
		return Value{}, fmt.Errorf("Get on a %d address %s", addr.Kind(), addr)
	}
	if v, ok := environ[keys[addr.String()]]; ok {
		return Value{Kind: KindString, Text: v}, nil
	}
	return Value{}, nil
}

// ── S3: phantom-typed Addr[K] ───────────────────────────────────────

type leafK struct{}
type sectionK struct{}
type compositeK struct{}

// Addr is compile-safe like S1 and the same 16 bytes — the cost is
// generic noise on every signature and marker types leaking into
// driver code.
type Addr[K any] struct{ p string }

func NewAddr[K any](p string) Addr[K] { return Addr[K]{p: p} }
func (a Addr[K]) String() string      { return a.p }

type s3Reader interface {
	Get(addr Addr[leafK]) (Value, error)
}
