package concwalk

// The M-card deep dive: where does concurrency actually buy throughput when
// Load/Dump for a Path is I/O bound - inside the walk, or at the driver
// boundary?
//
// Three strategies over the same slow plane (every round trip costs one
// simulated RTT), all producing identical destinations:
//
//   serialPerKey   - shipped shape: the walk Gets each leaf, one round trip
//                    per leaf. N leaves = N round trips, serial.
//   fanoutWalk     - a concurrent walk: leaf Gets run from F goroutines.
//                    Still N round trips; wall-clock divides by F; the driver
//                    now services concurrent Gets against one Reader.
//   prefetchOpen   - the driver boundary answer: Bind already handed the
//                    driver the complete AddressSet, so open issues ONE batch
//                    round trip (kv List / mget) and every leaf Get is a
//                    memory read. 1 round trip, walk stays serial.
//
// The deterministic metric is round trips, counted by the client itself.

import (
	"sync"
	"sync/atomic"
	"time"
)

// slowClient simulates a per-key remote store. rtts counts round trips.
type slowClient struct {
	data    map[string]string
	latency time.Duration
	rtts    atomic.Int64
}

func (c *slowClient) get(k string) (string, bool) {
	c.rtts.Add(1)
	time.Sleep(c.latency)
	v, ok := c.data[k]
	return v, ok
}

// list is the batch operation the plane already has (kv.Client.List):
// one round trip for a whole prefix.
func (c *slowClient) list() map[string]string {
	c.rtts.Add(1)
	time.Sleep(c.latency)
	out := make(map[string]string, len(c.data))
	for k, v := range c.data {
		out[k] = v
	}
	return out
}

// serialPerKey: the shipped walk shape over a per-key reader.
func serialPerKey(c *slowClient, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := c.get(k); ok {
			out[k] = v
		}
	}
	return out
}

// fanoutWalk: a concurrent walk with F workers - what M2/M3 would build.
// Results land by index so completion order cannot leak (W1's rule).
func fanoutWalk(c *slowClient, keys []string, workers int) map[string]string {
	type kv struct {
		v  string
		ok bool
	}
	got := make([]kv, len(keys))
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			v, ok := c.get(k)
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

// prefetchOpen: the driver-boundary answer. The AddressSet was the bind's,
// so the open knows every address the walk will ask for and fetches the
// batch in one round trip; the walk stays serial and every Get is local.
func prefetchOpen(c *slowClient, keys []string) map[string]string {
	snapshot := c.list() // one round trip, at open
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := snapshot[k]; ok {
			out[k] = v
		}
	}
	return out
}
