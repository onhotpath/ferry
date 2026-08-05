package concwalk

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func demoStore(n int) (*slowClient, []string) {
	data := make(map[string]string, n)
	keys := make([]string, 0, n)
	for i := range n {
		k := fmt.Sprintf("app/key%03d", i)
		data[k] = fmt.Sprintf("v%d", i)
		keys = append(keys, k)
	}
	return &slowClient{data: data, latency: 200 * time.Microsecond}, keys
}

// All three strategies produce the identical destination - concurrency and
// batching are throughput decisions, never behaviour decisions.
func TestStrategiesAgree(t *testing.T) {
	c1, keys := demoStore(40)
	c2, _ := demoStore(40)
	c3, _ := demoStore(40)
	serial := serialPerKey(c1, keys)
	fanned := fanoutWalk(c2, keys, 8)
	fetched := prefetchOpen(c3, keys)
	if !reflect.DeepEqual(serial, fanned) || !reflect.DeepEqual(serial, fetched) {
		t.Fatal("strategies diverge")
	}
}

// The round-trip ledger: a concurrent walk changes WHEN the round trips
// happen; only the driver boundary changes HOW MANY there are.
func TestRoundTripLedger(t *testing.T) {
	const n = 40
	c1, keys := demoStore(n)
	c2, _ := demoStore(n)
	c3, _ := demoStore(n)
	serialPerKey(c1, keys)
	fanoutWalk(c2, keys, 8)
	prefetchOpen(c3, keys)
	if got := c1.rtts.Load(); got != n {
		t.Fatalf("serial: %d round trips, want %d", got, n)
	}
	if got := c2.rtts.Load(); got != n {
		t.Fatalf("fanout: %d round trips, want %d - concurrency did not remove one", got, n)
	}
	if got := c3.rtts.Load(); got != 1 {
		t.Fatalf("prefetch: %d round trips, want 1", got)
	}
}

// Wall-clock, for the board's table (latency 200µs, 40 leaves):
// serial ≈ 40 RTT, fanout/8 ≈ 5 RTT, prefetch ≈ 1 RTT.
func TestWallClockShape(t *testing.T) {
	const n = 40
	c1, keys := demoStore(n)
	c2, _ := demoStore(n)
	c3, _ := demoStore(n)

	t0 := time.Now()
	serialPerKey(c1, keys)
	serial := time.Since(t0)

	t0 = time.Now()
	fanoutWalk(c2, keys, 8)
	fanned := time.Since(t0)

	t0 = time.Now()
	prefetchOpen(c3, keys)
	fetched := time.Since(t0)

	t.Logf("serial=%v fanout(8)=%v prefetch=%v", serial, fanned, fetched)
	if !(fetched < fanned && fanned < serial) {
		t.Fatalf("expected prefetch < fanout < serial, got serial=%v fanout=%v prefetch=%v", serial, fanned, fetched)
	}
}
