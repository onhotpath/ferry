package winreg_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// fake is the in-repo registry every test in this package runs against, and the
// reason all of them run on every operating system.
//
// The real seam is behind //go:build windows and cannot be entered here at all,
// so this is not a convenience: it is what puts the whole driver - the key
// function, the two namespaces, the staging, the sweep and the watch - under the
// conformance suite on the machines the suite is run on. The Windows job runs the
// same suite over the real registry.
//
// It is test apparatus rather than module surface, and it lives in a _test.go file
// so that it is neither shipped code nor covered code.
//
// It models the two things about the registry this driver is built around. Names
// are case-insensitive and case-preserving, so a key or a value is stored under
// its folded name and remembers the spelling whoever wrote it first used. And
// values and subkeys are separate namespaces, so a value host and a subkey host
// under one key are two objects.
//
// Every method locks, because -race is part of the run and the reader declares
// that it tolerates overlapping calls.
type fake struct {
	mu sync.Mutex

	// keys maps a folded subkey path to the spelling it was first created under.
	// The empty path is the driver's own key and is always there.
	keys map[string]string

	// vals maps a folded subkey path to that key's values, each keyed by folded
	// name.
	vals map[string]map[string]entry

	// changed is what a watch waits on: every write closes it and arms the next
	// one, which is the shape RegNotifyChangeKeyValue has.
	changed chan struct{}

	// failOn is the folded subkeys whose every operation fails, which is how a
	// read failure and a key that may not be written are both staged.
	failOn map[string]bool

	// watchFails stages a watch that is lost rather than one that fires, and
	// watchEnds one that ends quietly without being cancelled.
	watchFails bool
	watchEnds  bool

	// failDelete is the folded value names whose removal fails, which is the one
	// failure the sweep cannot stage through failOn: a listing that succeeded is
	// what produced the names in the first place.
	failDelete map[string]bool

	// phantom is a value name List reports and Get does not hold, which is what
	// another process removing a value between the two calls looks like.
	phantom string

	// answered counts every call this store has taken, which is what "the
	// registry was not reached at all" is read off.
	answered int

	// cancel is called at the end of every call, so the driver's next context
	// check is the one that fires. It is how a load or a save cancelled in
	// flight is staged against a plane that is genuinely in the middle of one.
	cancel context.CancelFunc
}

// entry is one stored value: the spelling of its name, and what it holds.
type entry struct {
	name string
	d    winreg.Datum
}

// errFake is what a staged failure reports, and the sentinel a test looks for
// under ferry's wrapper.
var errFake = errors.New("fake: the registry could not be reached")

func newFake() *fake {
	return &fake{
		keys:       map[string]string{"": ""},
		vals:       map[string]map[string]entry{},
		changed:    make(chan struct{}),
		failOn:     map[string]bool{},
		failDelete: map[string]bool{},
	}
}

// failUnder makes every call about one subkey fail. It stages a read failure a
// driver must report rather than answer Absent with, and, at the driver's own
// key, a key this process may not write.
func (f *fake) failUnder(subkey string) *fake {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failOn[fold(subkey)] = true

	return f
}

// failWatch makes every wait report the watch as lost, which is the one thing a
// watcher says out loud.
func (f *fake) failWatch() *fake {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.watchFails = true

	return f
}

// endWatch makes every wait end quietly, which is a registry that has stopped
// reporting without anything having gone wrong.
func (f *fake) endWatch() *fake {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.watchEnds = true

	return f
}

// failRemoval makes the removal of one value name fail wherever it is held.
func (f *fake) failRemoval(name string) *fake {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failDelete[fold(name)] = true

	return f
}

// listPhantom makes every listing report one name the store does not hold, which
// is the race a real registry has: another process removed the value between the
// listing and the read.
func (f *fake) listPhantom(name string) *fake {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.phantom = name

	return f
}

// cancelAfterEachCall cancels the context once this store has answered, so that
// the driver's next check is the one that finds it done.
func (f *fake) cancelAfterEachCall(cancel context.CancelFunc) *fake {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cancel = cancel

	return f
}

// put writes one value with the type the registry recorded, which is how a test
// stages what somebody else's tooling left behind.
func (f *fake) put(subkey, name string, d winreg.Datum) *fake {
	if err := f.Set(context.Background(), subkey, name, d); err != nil {
		panic(err)
	}

	return f
}

var (
	_ winreg.Registry = (*fake)(nil)
	_ winreg.Notifier = (*fake)(nil)
)

// fold is the registry's own case rule as this store models it.
func fold(s string) string { return strings.ToLower(s) }

// join is one step down a subkey path, with the empty path meaning the driver's
// own key.
func join(prefix, name string) string {
	if prefix == "" {
		return name
	}

	return prefix + `\` + name
}

// parts is one subkey path split into its steps, and none at all for the driver's
// own key.
func parts(subkey string) []string {
	if subkey == "" {
		return nil
	}

	return strings.Split(subkey, `\`)
}

func (f *fake) Get(ctx context.Context, subkey, name string) (winreg.Datum, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.refuse(ctx, subkey); err != nil {
		return winreg.Datum{}, false, err
	}

	e, ok := f.vals[fold(subkey)][fold(name)]

	return e.d, ok, nil
}

func (f *fake) List(ctx context.Context, subkey string) (winreg.Listing, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.refuse(ctx, subkey); err != nil {
		return winreg.Listing{}, false, err
	}

	if _, ok := f.keys[fold(subkey)]; !ok {
		return winreg.Listing{}, false, nil
	}

	values := f.valueNames(subkey)
	if f.phantom != "" {
		values = append(values, f.phantom)
	}

	return winreg.Listing{Values: values, Keys: f.childNames(subkey)}, true, nil
}

// valueNames is the spelled names of one key's values.
func (f *fake) valueNames(subkey string) []string {
	held := f.vals[fold(subkey)]

	out := make([]string, 0, len(held))
	for _, e := range held {
		out = append(out, e.name)
	}

	slices.Sort(out)

	return out
}

// childNames is the spelled names of one key's immediate subkeys.
func (f *fake) childNames(subkey string) []string {
	inside := fold(join(subkey, ""))

	var out []string

	for folded, spelled := range f.keys {
		rest, ok := strings.CutPrefix(folded, inside)
		if !ok || rest == "" || strings.Contains(rest, `\`) {
			continue
		}

		out = append(out, spelled[len(spelled)-len(rest):])
	}

	slices.Sort(out)

	return out
}

func (f *fake) Set(ctx context.Context, subkey, name string, d winreg.Datum) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.refuse(ctx, subkey); err != nil {
		return err
	}

	f.makeKeys(subkey)

	held, ok := f.vals[fold(subkey)]
	if !ok {
		held = map[string]entry{}
		f.vals[fold(subkey)] = held
	}

	spelled := name
	if was, taken := held[fold(name)]; taken {
		spelled = was.name
	}

	held[fold(name)] = entry{name: spelled, d: d}
	f.fire()

	return nil
}

func (f *fake) Create(ctx context.Context, subkey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.refuse(ctx, subkey); err != nil {
		return err
	}

	f.makeKeys(subkey)
	f.fire()

	return nil
}

// makeKeys makes one subkey and every key above it, keeping the spelling whoever
// created each of them first used.
func (f *fake) makeKeys(subkey string) {
	at := ""

	for _, step := range parts(subkey) {
		next := join(at, step)
		if held, ok := f.keys[fold(next)]; ok {
			at = held

			continue
		}

		f.keys[fold(next)] = next
		at = next
	}
}

func (f *fake) DeleteValue(ctx context.Context, subkey, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.refuse(ctx, subkey); err != nil {
		return err
	}

	if f.failDelete[fold(name)] {
		return errFake
	}

	delete(f.vals[fold(subkey)], fold(name))
	f.fire()

	return nil
}

func (f *fake) DeleteKey(ctx context.Context, subkey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.refuse(ctx, subkey); err != nil {
		return err
	}

	at, inside := fold(subkey), fold(join(subkey, ""))

	for folded := range f.keys {
		if folded == at || strings.HasPrefix(folded, inside) {
			delete(f.keys, folded)
			delete(f.vals, folded)
		}
	}

	f.fire()

	return nil
}

// Notify is the [winreg.Notifier] half: it wakes on the next write anywhere in
// this store, and ends when the context does.
func (f *fake) Notify(ctx context.Context) (bool, error) {
	f.mu.Lock()
	wait, lost, ended := f.changed, f.watchFails, f.watchEnds
	f.mu.Unlock()

	if lost {
		return false, errFake
	}

	if ended {
		return false, nil
	}

	select {
	case <-ctx.Done():
		return false, nil
	case <-wait:
		return true, nil
	}
}

// fire wakes every waiting Notify and arms the next one. The lock is already
// held by every caller.
func (f *fake) fire() {
	close(f.changed)
	f.changed = make(chan struct{})
}

// refuse is the staged failure, and the context check every real implementation
// of the seam owes.
func (f *fake) refuse(ctx context.Context, subkey string) error {
	f.answered++

	if err := ctx.Err(); err != nil {
		return err
	}

	if f.failOn[fold(subkey)] {
		return errFake
	}

	if f.cancel != nil {
		f.cancel()
	}

	return nil
}

// calls is how many questions this store has been asked, refused ones included.
func (f *fake) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.answered
}

// contents renders the whole store: one line per subkey, then one per value,
// each sorted and quoted.
//
// It is what [ferrytest.Instance.Contents] hands the golden artefact case, so it
// has to be deterministic and injective over stores. The registry is a tree of
// keys with two namespaces in each and no document of its own, so the rendering
// carries all three things a golden row has to pin: which keys exist, which
// values are in them, and what type each value is stored as. Quoting both halves
// is what buys the injectivity, since a name and a payload can both hold a
// backslash and a newline.
func (f *fake) contents() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()

	var b strings.Builder

	for _, folded := range slices.Sorted(maps.Keys(f.keys)) {
		b.WriteString("key ")
		b.WriteString(strconv.Quote(f.keys[folded]))
		b.WriteString("\n")
	}

	for _, folded := range slices.Sorted(maps.Keys(f.vals)) {
		f.writeValues(&b, folded)
	}

	return []byte(b.String())
}

// writeValues renders one key's values, sorted by name.
func (f *fake) writeValues(b *strings.Builder, folded string) {
	held := f.vals[folded]

	for _, name := range slices.Sorted(maps.Keys(held)) {
		e := held[name]

		b.WriteString("val ")
		b.WriteString(strconv.Quote(f.keys[folded]))
		b.WriteString(" ")
		b.WriteString(strconv.Quote(e.name))
		b.WriteString(" ")
		b.WriteString(e.d.Type.String())
		b.WriteString(" ")
		b.WriteString(strconv.Quote(payload(e.d)))
		b.WriteString("\n")
	}
}

// payload is what one datum holds, whichever of its two fields carries it.
func payload(d winreg.Datum) string {
	if d.Type == winreg.TypeBinary {
		return string(d.Binary)
	}

	return d.Text
}
