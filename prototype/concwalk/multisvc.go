package concwalk

// Round 3: the owner's actual #20 scenario. One Load, 8 keys, routed to
// three backends (2 on svcA, 3 on svcB, 3 on svcC), each backend reachable
// only over the network. Where does the `go` fire so that wall-clock is
// the LONGEST backend, not the sum?
//
//   msSerial          - shipped shape: 8 sequential round trips.
//   msCoreFanout      - core writes `go` per ADDRESS, bounded by
//                       MaxConcurrency(n): still 8 round trips, overlapped.
//   msBackendBatches  - the driver/composite groups keys by backend and
//                       fires one BATCH per backend, concurrently: 3 round
//                       trips, wall-clock = slowest backend.

import (
	"sync"
	"sync/atomic"
	"time"
)

type backend struct {
	name    string
	latency time.Duration
	data    map[string]string
	rtts    atomic.Int64
}

func (b *backend) get(k string) (string, bool) {
	b.rtts.Add(1)
	time.Sleep(b.latency)
	v, ok := b.data[k]
	return v, ok
}

// batch is one round trip for any number of keys - the API every networked
// store has (mget, List, BatchGetItem, consul txn).
func (b *backend) batch(keys []string) map[string]string {
	b.rtts.Add(1)
	time.Sleep(b.latency)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := b.data[k]; ok {
			out[k] = v
		}
	}
	return out
}

// multiPlane routes each key to its backend - the composite-source shape.
type multiPlane struct {
	route map[string]*backend
}

func (p *multiPlane) totalRTTs() int64 {
	seen := map[*backend]struct{}{}
	var n int64
	for _, b := range p.route {
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		n += b.rtts.Load()
	}
	return n
}

func msSerial(p *multiPlane, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := p.route[k].get(k); ok {
			out[k] = v
		}
	}
	return out
}

// msCoreFanout: core writes `go` per address, the caller bounds it.
// Results land by index (W1's rule: completion order never leaks).
func msCoreFanout(p *multiPlane, keys []string, maxConc int) map[string]string {
	type kv struct {
		v  string
		ok bool
	}
	got := make([]kv, len(keys))
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			v, ok := p.route[k].get(k)
			got[i] = kv{v, ok}
			<-sem
		}()
	}
	wg.Wait()
	out := make(map[string]string, len(keys))
	for i, k := range keys {
		if got[i].ok {
			out[k] = got[i].v
		}
	}
	return out
}

// msBackendBatches: the driver (or composite) groups by backend at open -
// it KNOWS the routing, which core never does - and fires one goroutine
// per BATCH, not per address. Wall-clock = slowest backend.
func msBackendBatches(p *multiPlane, keys []string) map[string]string {
	group := map[*backend][]string{}
	for _, k := range keys {
		group[p.route[k]] = append(group[p.route[k]], k)
	}
	var mu sync.Mutex
	out := make(map[string]string, len(keys))
	var wg sync.WaitGroup
	for b, ks := range group {
		wg.Add(1)
		go func() {
			defer wg.Done()
			part := b.batch(ks)
			mu.Lock()
			for k, v := range part {
				out[k] = v
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
