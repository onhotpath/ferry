package concwalk

// Round 4: the owner's hybrid - if MaxConcurrency is not exhausted after
// one batch per backend, subdivide a backend's keys into concurrent
// requests to the SAME service.
//
// Whether that helps depends on the service's latency model, which is
// exactly why the subdivision must be the driver's call with the caller's
// budget visible to it:
//   - flat cost per round trip (one RTT dominates): splitting a batch in
//     two concurrent halves saves nothing - same wall, more requests.
//   - cost grows with batch size (large values, per-key server work):
//     splitting pays, bounded by the leftover budget.
// Core cannot know which model a plane has; the driver does. So the budget
// is ONE number the caller sets, core obeys it in its own fanout, and the
// driver reads the same number (delivered on the open's ctx) to size its
// request parallelism.

import (
	"sync"
	"sync/atomic"
	"time"
)

// sizedBackend models cost = base + perKey*len(batch).
type sizedBackend struct {
	name   string
	base   time.Duration
	perKey time.Duration
	data   map[string]string
	rtts   atomic.Int64
}

func (b *sizedBackend) batch(keys []string) map[string]string {
	b.rtts.Add(1)
	time.Sleep(b.base + time.Duration(len(keys))*b.perKey)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := b.data[k]; ok {
			out[k] = v
		}
	}
	return out
}

type sizedPlane struct{ route map[string]*sizedBackend }

func (p *sizedPlane) totalRTTs() int64 {
	seen := map[*sizedBackend]struct{}{}
	var n int64
	for _, b := range p.route {
		if _, ok := seen[b]; !ok {
			seen[b] = struct{}{}
			n += b.rtts.Load()
		}
	}
	return n
}

func groupByBackend(p *sizedPlane, keys []string) map[*sizedBackend][]string {
	g := map[*sizedBackend][]string{}
	for _, k := range keys {
		g[p.route[k]] = append(g[p.route[k]], k)
	}
	return g
}

// onePerBackend: round 3's answer - one batch per service, concurrent.
func onePerBackend(p *sizedPlane, keys []string) map[string]string {
	return runChunks(chunkPerBackend(groupByBackend(p, keys), 0))
}

// hybridSubdivide: same grouping, then each backend's keys split into up
// to its fair share of the leftover budget - concurrent requests to the
// SAME service, never more than `budget` requests in flight overall.
func hybridSubdivide(p *sizedPlane, keys []string, budget int) map[string]string {
	return runChunks(chunkPerBackend(groupByBackend(p, keys), budget))
}

type chunk struct {
	b    *sizedBackend
	keys []string
}

// chunkPerBackend splits each backend's keys into its share of the budget.
// budget 0 means one chunk per backend.
func chunkPerBackend(group map[*sizedBackend][]string, budget int) []chunk {
	total := 0
	for _, ks := range group {
		total += len(ks)
	}
	var chunks []chunk
	for b, ks := range group {
		parts := 1
		if budget > 0 {
			parts = max(1, budget*len(ks)/total)
			parts = min(parts, len(ks))
		}
		size := (len(ks) + parts - 1) / parts
		for i := 0; i < len(ks); i += size {
			chunks = append(chunks, chunk{b: b, keys: ks[i:min(i+size, len(ks))]})
		}
	}
	return chunks
}

func runChunks(chunks []chunk) map[string]string {
	var mu sync.Mutex
	out := map[string]string{}
	var wg sync.WaitGroup
	for _, c := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			part := c.b.batch(c.keys)
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
