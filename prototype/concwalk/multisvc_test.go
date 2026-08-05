package concwalk

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// The owner's scenario: 8 keys, 2 on svcA (1ms), 3 on svcB (2ms), 3 on svcC (3ms).
func demoMulti() (*multiPlane, []string) {
	mk := func(name string, lat time.Duration, keys ...string) *backend {
		data := map[string]string{}
		for _, k := range keys {
			data[k] = "v:" + k
		}
		return &backend{name: name, latency: lat, data: data}
	}
	a := mk("svcA", 1*time.Millisecond, "a1", "a2")
	b := mk("svcB", 2*time.Millisecond, "b1", "b2", "b3")
	c := mk("svcC", 3*time.Millisecond, "c1", "c2", "c3")
	route := map[string]*backend{}
	for _, k := range []string{"a1", "a2"} {
		route[k] = a
	}
	for _, k := range []string{"b1", "b2", "b3"} {
		route[k] = b
	}
	for _, k := range []string{"c1", "c2", "c3"} {
		route[k] = c
	}
	return &multiPlane{route: route}, []string{"a1", "a2", "b1", "b2", "b3", "c1", "c2", "c3"}
}

func TestMultiStrategiesAgree(t *testing.T) {
	p1, keys := demoMulti()
	p2, _ := demoMulti()
	p3, _ := demoMulti()
	serial := msSerial(p1, keys)
	fanned := msCoreFanout(p2, keys, 3)
	batched := msBackendBatches(p3, keys)
	if !reflect.DeepEqual(serial, fanned) || !reflect.DeepEqual(serial, batched) {
		t.Fatal("strategies diverge")
	}
	if len(serial) != 8 {
		t.Fatalf("lost keys: %v", serial)
	}
}

// The ledger again: core fanout still pays one round trip per ADDRESS;
// only the driver, which knows the routing, can pay one per BACKEND.
func TestMultiRoundTripLedger(t *testing.T) {
	p1, keys := demoMulti()
	p2, _ := demoMulti()
	p3, _ := demoMulti()
	msSerial(p1, keys)
	msCoreFanout(p2, keys, 3)
	msBackendBatches(p3, keys)
	if got := p1.totalRTTs(); got != 8 {
		t.Fatalf("serial: %d round trips, want 8", got)
	}
	if got := p2.totalRTTs(); got != 8 {
		t.Fatalf("core fanout: %d round trips, want 8", got)
	}
	if got := p3.totalRTTs(); got != 3 {
		t.Fatalf("backend batches: %d round trips, want 3 (one per service)", got)
	}
}

// Wall clock: serial = sum of everything (~19ms), core fanout(3) overlaps
// but still pays per key, backend batches = the slowest service (~3ms) -
// the owner's "only the longest Load is the wall-clock".
func TestMultiWallClockShape(t *testing.T) {
	p1, keys := demoMulti()
	p2, _ := demoMulti()
	p3, _ := demoMulti()

	t0 := time.Now()
	msSerial(p1, keys)
	serial := time.Since(t0)

	t0 = time.Now()
	msCoreFanout(p2, keys, 3)
	fanned := time.Since(t0)

	t0 = time.Now()
	msBackendBatches(p3, keys)
	batched := time.Since(t0)

	t.Logf("serial=%v coreFanout(3)=%v backendBatches=%v", serial, fanned, batched)
	if !(batched < fanned && fanned < serial) {
		t.Fatalf("expected batches < fanout < serial, got %v %v %v", serial, fanned, batched)
	}
}

// The capability gate: one Option, two drivers. The instance that did not
// assert ConcurrentSafe is NEVER called concurrently, whatever the caller
// asked for; the one that did stays within the caller's bound.
func TestCapabilityGate(t *testing.T) {
	data := map[string]string{}
	keys := make([]string, 12)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%02d", i)
		data[keys[i]] = "v"
	}

	plain := &meteredInstance{data: data}
	out := walkLeaves(plain, keys, 4) // caller asks for 4...
	if len(out) != 12 {
		t.Fatal("lost keys")
	}
	if plain.peak != 1 { // ...but the driver never opted in
		t.Fatalf("uncapable instance saw %d overlapped calls - core broke the gate", plain.peak)
	}

	conc := &concInstance{meteredInstance{data: data}}
	out = walkLeaves(conc, keys, 4)
	if len(out) != 12 {
		t.Fatal("lost keys")
	}
	if conc.peak < 2 {
		t.Fatalf("capable instance never overlapped (peak %d) - fanout did not engage", conc.peak)
	}
	if conc.peak > 4 {
		t.Fatalf("bound broken: peak %d > MaxConcurrency 4", conc.peak)
	}
}
