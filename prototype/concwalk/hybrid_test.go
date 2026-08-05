package concwalk

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// 12 keys: 2 on svcA, 4 on svcB, 6 on svcC.
func demoSized(base, perKey time.Duration) (*sizedPlane, []string) {
	mk := func(name string, n int) (*sizedBackend, []string) {
		data := map[string]string{}
		keys := make([]string, n)
		for i := range n {
			keys[i] = fmt.Sprintf("%s/k%d", name, i)
			data[keys[i]] = "v"
		}
		return &sizedBackend{name: name, base: base, perKey: perKey, data: data}, keys
	}
	a, ak := mk("svcA", 2)
	b, bk := mk("svcB", 4)
	c, ck := mk("svcC", 6)
	route := map[string]*sizedBackend{}
	var keys []string
	for _, k := range ak {
		route[k] = a
		keys = append(keys, k)
	}
	for _, k := range bk {
		route[k] = b
		keys = append(keys, k)
	}
	for _, k := range ck {
		route[k] = c
		keys = append(keys, k)
	}
	return &sizedPlane{route: route}, keys
}

func TestHybridAgrees(t *testing.T) {
	p1, keys := demoSized(1*time.Millisecond, 500*time.Microsecond)
	p2, _ := demoSized(1*time.Millisecond, 500*time.Microsecond)
	one := onePerBackend(p1, keys)
	hyb := hybridSubdivide(p2, keys, 6)
	if !reflect.DeepEqual(one, hyb) || len(one) != 12 {
		t.Fatal("strategies diverge")
	}
	if p1.totalRTTs() != 3 {
		t.Fatalf("one-per-backend made %d requests, want 3", p1.totalRTTs())
	}
	if got := p2.totalRTTs(); got < 4 || got > 6 {
		t.Fatalf("hybrid made %d requests, want 4..6 (within budget)", got)
	}
}

// When cost grows with batch size, subdividing the big backend's batch
// within the leftover budget shortens the wall - the owner's intuition.
func TestHybridPaysWhenCostGrowsWithSize(t *testing.T) {
	p1, keys := demoSized(1*time.Millisecond, 500*time.Microsecond)
	p2, _ := demoSized(1*time.Millisecond, 500*time.Microsecond)

	t0 := time.Now()
	onePerBackend(p1, keys) // svcC: 1ms + 6×0.5ms = 4ms wall
	one := time.Since(t0)

	t0 = time.Now()
	hybridSubdivide(p2, keys, 6) // svcC split 3×2: 1ms + 2×0.5ms = 2ms wall
	hyb := time.Since(t0)

	t.Logf("size-dependent cost: onePerBackend=%v hybrid(6)=%v", one, hyb)
	if hyb >= one {
		t.Fatalf("hybrid did not pay under size-dependent cost: %v vs %v", hyb, one)
	}
}

// When cost is flat per round trip, subdividing buys nothing - same wall,
// more requests. This is why the split is the driver's call, not core's.
func TestHybridIsAWashWhenCostIsFlat(t *testing.T) {
	p1, keys := demoSized(3*time.Millisecond, 0)
	p2, _ := demoSized(3*time.Millisecond, 0)

	t0 := time.Now()
	onePerBackend(p1, keys)
	one := time.Since(t0)

	t0 = time.Now()
	hybridSubdivide(p2, keys, 6)
	hyb := time.Since(t0)

	t.Logf("flat cost: onePerBackend=%v hybrid(6)=%v", one, hyb)
	// Same wall within scheduling noise; the hybrid must not be meaningfully faster.
	if hyb < one-one/4 {
		t.Fatalf("flat-cost hybrid should not beat one-per-backend: %v vs %v", hyb, one)
	}
}
