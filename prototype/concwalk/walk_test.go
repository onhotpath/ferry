package concwalk

import (
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// tree: root { a *optional { x leaf }, b { y leaf } } - a's subtree is absent
// on the plane, b's leaf is present. The correct answer is always: a.ptr false.
func demoTree() *node {
	return &node{name: "root", children: []*node{
		{name: "a", optional: true, children: []*node{{name: "x", leaf: true}}},
		{name: "b", children: []*node{{name: "y", leaf: true}}},
	}}
}

func demoPlane() map[string]string {
	return map[string]string{"/b/y": "1"}
}

// Both shapes agree everywhere under the serial scheduler.
func TestSerialEquivalence(t *testing.T) {
	tree := &node{name: "root", children: []*node{
		{name: "a", optional: true, children: []*node{{name: "x", leaf: true}}},
		{name: "b", optional: true, children: []*node{{name: "y", leaf: true}}},
		{name: "c", children: []*node{{name: "z", leaf: true}}},
	}}
	planes := []map[string]string{
		{},
		{"/a/x": "v"},
		{"/b/y": "w", "/c/z": "u"},
		{"/a/x": "v", "/b/y": "w"},
	}
	for i, plane := range planes {
		ds := newDest()
		sw := &sharedWalk{plane: plane, wrote: new(int64), sch: serial}
		if err := sw.walk("", tree, ds); err != nil {
			t.Fatalf("plane %d shared: %v", i, err)
		}
		do := newDest()
		ow := &outcomeWalk{plane: plane, sch: oSerial}
		if _, err := ow.walk("", tree, do); err != nil {
			t.Fatalf("plane %d outcome: %v", i, err)
		}
		if !reflect.DeepEqual(ds, do) {
			t.Fatalf("plane %d: shapes diverge under serial\nshared:  %+v\noutcome: %+v", i, ds, do)
		}
	}
}

// The 5.2 shape, made deterministic: under a concurrent scheduler the shared
// counter materialises a's pointer because SIBLING b wrote inside a's
// before/after window. No data race fires - the counter is atomic - the
// answer is simply wrong.
func TestSharedCounterMaterialisesOnSiblingWrite(t *testing.T) {
	wrote := new(int64)
	aWindowOpen := make(chan struct{})
	var once sync.Once
	sw := &sharedWalk{
		plane: demoPlane(),
		wrote: wrote,
		sch:   goroutines,
		gate: func(path string) {
			if path == "/b" {
				<-aWindowOpen // b may not start until a has read its before value
			}
		},
		pause: func(path string) {
			once.Do(func() { close(aWindowOpen) })
			for atomic.LoadInt64(wrote) == 0 { // hold a's window open until b's write lands
				runtime.Gosched()
			}
		},
	}
	d := newDest()
	if err := sw.walk("", demoTree(), d); err != nil {
		t.Fatal(err)
	}
	if !d.children["a"].ptr {
		t.Fatal("expected the wrong answer: a materialised by b's write; the hazard did not reproduce")
	}
	if d.children["a"].children["x"].set {
		t.Fatal("a/x must be absent - the pointer above it is the lie")
	}
}

// Same tree, same adversarial delay, same concurrent scheduler: the outcome
// walk cannot be wrong, because a's bit is a's own subtree's return value.
func TestOutcomeComposesUnderConcurrentScheduler(t *testing.T) {
	ow := &outcomeWalk{
		plane: demoPlane(),
		sch:   oGoroutines,
		pause: func(path string) {
			for range 10000 { // give b every chance to finish first
				runtime.Gosched()
			}
		},
	}
	d := newDest()
	o, err := ow.walk("", demoTree(), d)
	if err != nil {
		t.Fatal(err)
	}
	if d.children["a"].ptr {
		t.Fatal("a materialised with an absent subtree - outcome shape failed")
	}
	if !o.wrote || len(o.writes) != 1 || o.writes[0] != "/b/y=1" {
		t.Fatalf("root outcome wrong: %+v", o)
	}
}

// 5.4's rule held: aggregation is the scheduler's, and a concurrent outcome
// scheduler reports byte-identically to the serial one - completion order
// never leaks into the report.
func TestConcurrentErrorsByteIdenticalToSerial(t *testing.T) {
	tree := &node{name: "root", children: []*node{
		{name: "p", children: []*node{{name: "q", leaf: true}}},
		{name: "r", children: []*node{{name: "s", leaf: true}}},
	}}
	plane := map[string]string{"/p/q": "!", "/r/s": "!"}

	_, serr := (&outcomeWalk{plane: plane, sch: oSerial}).walk("", tree, newDest())
	if serr == nil {
		t.Fatal("expected two failures")
	}
	for range 50 {
		_, cerr := (&outcomeWalk{plane: plane, sch: oGoroutines}).walk("", tree, newDest())
		if cerr == nil || cerr.Error() != serr.Error() {
			t.Fatalf("reports diverge:\nserial:     %v\nconcurrent: %v", serr, cerr)
		}
	}
}
