package kv_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/kv"
)

// fake is the in-repo store every test in this package runs against, and the
// reason none of them reaches a network.
//
// It is test apparatus rather than module surface, and it lives in a _test.go
// file so that it is neither shipped code nor covered code. What it exists to
// do is the three things the driver's own axes need staged - fail the third
// Get, deny two paths, and block until a context is cancelled - and every one
// of those is a knob that only a driver's own tests want. Exporting them would
// commit this module to an API for breaking itself on purpose.
//
// Every method locks, because -race is part of the run and a blocking Get is
// read from one goroutine while the test cancels from another.
type fake struct {
	mu   sync.Mutex
	data map[string][]byte

	// The call counters, which are what the batch-versus-lazy assertion reads.
	gets, lists, puts int

	// failGetOn is the 1-based Get this store fails, or zero for a store that
	// answers every one of them.
	failGetOn int

	// failList fails every listing, and failPut names the keys whose write
	// fails, which is what a partial failure looks like from below.
	failList bool
	failPut  []string

	// blockGet holds a Get until the context ends, which is how a cancellation
	// is staged against a client that is genuinely in flight rather than
	// against one that was never called.
	blockGet bool
	entered  chan struct{}
	once     sync.Once
}

// errFakeRead is what a staged read failure reports, and the sentinel a test
// looks for under ferry's wrapper.
var errFakeRead = errors.New("fake: the store could not be read")

// errFakeDenied is what a denied write reports.
var errFakeDenied = errors.New("fake: this token may not write here")

// errFakeWrite is what a staged write failure reports.
var errFakeWrite = errors.New("fake: the store could not be written")

func newFake() *fake {
	return &fake{data: map[string][]byte{}, entered: make(chan struct{})}
}

// failGet makes the n-th Get of this store fail, counted from one.
func (f *fake) failGet(n int) *fake {
	f.failGetOn = n

	return f
}

// blockGets makes every Get wait for the context to end, and closes entered
// when the first one arrives so a test can cancel at a known moment.
func (f *fake) blockGets() *fake {
	f.blockGet = true

	return f
}

// failLists makes every listing fail.
func (f *fake) failLists() *fake {
	f.failList = true

	return f
}

// failPuts makes the write of each named key fail, and leaves the rest to
// succeed, which is what a store that is partly unavailable looks like.
func (f *fake) failPuts(keys ...string) *fake {
	f.failPut = keys

	return f
}

func (f *fake) Get(ctx context.Context, key string) ([]byte, bool, error) {
	f.mu.Lock()
	f.gets++
	n, blocking, failOn := f.gets, f.blockGet, f.failGetOn
	f.mu.Unlock()

	if blocking {
		f.once.Do(func() { close(f.entered) })
		<-ctx.Done()

		return nil, false, ctx.Err()
	}

	if n == failOn {
		return nil, false, errFakeRead
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	value, found := f.data[key]

	return value, found, nil
}

func (f *fake) List(_ context.Context, prefix string) (map[string][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lists++

	if f.failList {
		return nil, errFakeRead
	}

	out := map[string][]byte{}

	for key, value := range f.data {
		if strings.HasPrefix(key, prefix) {
			out[key] = value
		}
	}

	return out, nil
}

func (f *fake) Put(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.puts++

	if slices.Contains(f.failPut, key) {
		return errFakeWrite
	}

	f.data[key] = value

	return nil
}

// calls is every backend call this store has answered, which is what "three
// backend calls lazily and one in batch" is counted with.
func (f *fake) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.gets + f.lists + f.puts
}

func (f *fake) putCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.puts
}

// contents renders the whole store, sorted, quoted and one pair per line.
//
// It is what [ferrytest.Instance.Contents] hands the golden artefact case, so
// it has to be deterministic and injective over stores: a key-value store is a
// set of pairs with no document of its own, and two different stores rendering
// alike would be a golden row that cannot see the difference. Quoting both
// halves is what buys that, since a key holding an equals sign and a value
// holding a newline are both things a store can hold.
func (f *fake) contents() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()

	var b strings.Builder

	for _, key := range slices.Sorted(maps.Keys(f.data)) {
		b.WriteString(strconv.Quote(key))
		b.WriteString(" = ")
		b.WriteString(strconv.Quote(string(f.data[key])))
		b.WriteString("\n")
	}

	return []byte(b.String())
}

// guarded is a fake whose credentials answer permission questions: the [kv.ACL]
// half, over the same store.
//
// It is a second type rather than a flag on the first, because "a client that
// implements ACL and permits everything" and "a client that implements no
// permission check at all" are two different clients and the driver takes a
// different path for each.
type guarded struct {
	*fake

	// deny is the key prefixes these credentials may not write under. The empty
	// string denies the whole store, which is the token with no write ACL at
	// all.
	deny []string

	// checked records every key the driver asked about, in order, which is what
	// lets a test assert that a refusal at the open was never followed by a
	// question about an address - so no Set was reached.
	checked []string
}

func newGuarded(deny ...string) *guarded {
	return &guarded{fake: newFake(), deny: deny}
}

func (g *guarded) CanWrite(_ context.Context, key string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.checked = append(g.checked, key)

	for _, d := range g.deny {
		if d == "" || d == key || strings.HasPrefix(key, d+"/") {
			return errFakeDenied
		}
	}

	return nil
}

// asked is the keys this client was asked about, in order.
func (g *guarded) asked() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.checked)
}

// The fake is a client and the guarded one is a client that answers permission
// questions, asserted here rather than discovered at a call site.
var (
	_ kv.Client = (*fake)(nil)
	_ kv.Client = (*guarded)(nil)
	_ kv.ACL    = (*guarded)(nil)
)

// The schemas these tests hand the driver, and the door they come through.
//
// The three address kinds are sealed and the schema compiler is the only thing
// that mints one (ADR-0016), so a test that wants to ask this driver about an
// address asks the compiler for it. Compiling a real struct is the same door
// core comes in through, which keeps these tests asserting about the driver
// rather than about a set a test invented.
type (
	// hostOnly is the smallest schema with a leaf, and dbSection nests it.
	hostOnly struct {
		Host string `ferry:"host"`
	}
	dbSection struct {
		DB hostOnly `ferry:"db"`
	}

	// emptyMissing is one key held with no bytes and one never held.
	emptyMissing struct {
		Empty   string `ferry:"empty"`
		Missing string `ferry:"missing"`
	}

	// tagsLabels is the two container shapes whose members come from the value.
	tagsLabels struct {
		Tags   []string          `ferry:"tags"`
		Labels map[string]string `ferry:"labels"`
	}

	// threeKinds is one value of every kind this plane stores as text.
	threeKinds struct {
		Flag bool   `ferry:"flag"`
		Raw  []byte `ferry:"raw"`
		Port int    `ferry:"port"`
	}
)

// captureSource records the address set core hands a Bind and reads nothing.
type captureSource struct{ set *ferry.AddressSet }

func (c *captureSource) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	c.set = addrs

	return func(context.Context) (ferry.Reader, error) { return captureReader{}, nil }, nil
}

type captureReader struct{}

func (captureReader) Get(context.Context, ferry.LeafAddr) (ferry.Value, error) {
	return ferry.Value{}, nil
}

// addrsOf is every address T names, typed, straight from the compiler.
func addrsOf[T any](t *testing.T) *ferry.AddressSet {
	t.Helper()

	c := &captureSource{}
	if _, err := ferry.Bind[T](c); err != nil {
		t.Fatalf("compiling the fixture: %v", err)
	}

	return c.set
}

// leafAt and compositeAt find one address of one kind in a compiled set. A miss
// is a test naming an address its own fixture does not have.
func leafAt(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.LeafAddr {
	t.Helper()

	for m := range set.Seq() {
		if a, ok := m.(ferry.LeafAddr); ok && a.Path() == at {
			return a
		}
	}

	t.Fatalf("the fixture names no leaf at %s", at)

	return ferry.LeafAddr{}
}

func compositeAt(t *testing.T, set *ferry.AddressSet, at ferry.Path) ferry.CompositeAddr {
	t.Helper()

	for m := range set.Seq() {
		if a, ok := m.(ferry.CompositeAddr); ok && a.Path() == at {
			return a
		}
	}

	t.Fatalf("the fixture names no composite at %s", at)

	return ferry.CompositeAddr{}
}
