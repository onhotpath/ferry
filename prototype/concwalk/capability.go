package concwalk

// The "who writes go" API shape: CORE writes it - once, in the scheduler -
// the DRIVER opts in by asserting an optional capability on its instance
// (the Releaser/Committer idiom), and the CALLER bounds it with an Option.
//
//   ferry.MaxConcurrency(3)   - caller: "you may overlap up to 3 addresses"
//   ConcurrentSafe marker     - driver: "my instance tolerates overlapped Gets"
//   core                      - fans out ONLY when both say yes; serial otherwise
//
// env/yaml never assert it (disk and memory want one call); kv/S3/Consul
// assert it because their planes are behind a network. No driver is broken
// by the Option existing, and no caller can force concurrency onto an
// instance that did not offer it.

import (
	"sync"
	"time"
)

type instance interface {
	get(k string) (string, bool)
}

// concurrentSafe is the opt-in capability, discovered by assertion.
type concurrentSafe interface {
	ConcurrentSafe()
}

// walkLeaves is the miniature of core's leaf loop under this design.
func walkLeaves(inst instance, keys []string, maxConc int) map[string]string {
	if _, ok := inst.(concurrentSafe); !ok || maxConc <= 1 {
		out := make(map[string]string, len(keys))
		for _, k := range keys {
			if v, ok := inst.get(k); ok {
				out[k] = v
			}
		}
		return out
	}
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
			v, ok := inst.get(k)
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

// meteredInstance records the highest number of overlapped get calls it
// ever saw, so a test can PROVE core respected the capability and the bound.
type meteredInstance struct {
	data     map[string]string
	conc     bool
	inflight int
	peak     int
	mu       sync.Mutex
}

func (m *meteredInstance) get(k string) (string, bool) {
	m.mu.Lock()
	m.inflight++
	if m.inflight > m.peak {
		m.peak = m.inflight
	}
	m.mu.Unlock()
	time.Sleep(200 * time.Microsecond) // hold the call open so overlap is observable
	v, ok := m.data[k]
	m.mu.Lock()
	m.inflight--
	m.mu.Unlock()
	return v, ok
}

// concInstance is a metered instance that asserts the capability.
type concInstance struct{ meteredInstance }

func (*concInstance) ConcurrentSafe() {}
