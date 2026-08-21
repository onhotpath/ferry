package winreg_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// memory is a [winreg.Registry] over two ordinary Go maps: which subkeys exist,
// and what values are in each of them.
//
// It is here so that these examples run on every operating system, and so that a
// reader can see the shape the seam has. A program on Windows passes no
// winreg.Store at all and reaches the machine's own registry.
type memory struct {
	keys map[string]bool
	vals map[string]map[string]winreg.Datum

	// changed is what a watch waits on: every write closes it and puts the next
	// one in its place, which is the shape RegNotifyChangeKeyValue has.
	changed chan struct{}
}

func newMemory() *memory {
	return &memory{
		keys:    map[string]bool{"": true},
		vals:    map[string]map[string]winreg.Datum{},
		changed: make(chan struct{}),
	}
}

func (m *memory) Get(_ context.Context, subkey, name string) (winreg.Datum, bool, error) {
	d, ok := m.vals[subkey][name]

	return d, ok, nil
}

func (m *memory) List(_ context.Context, subkey string) (winreg.Listing, bool, error) {
	if !m.keys[subkey] {
		return winreg.Listing{}, false, nil
	}

	l := winreg.Listing{Values: slices.Sorted(maps.Keys(m.vals[subkey]))}

	for held := range m.keys {
		if rest, ok := strings.CutPrefix(held, join(subkey, "")); ok && rest != "" && !strings.Contains(rest, `\`) {
			l.Keys = append(l.Keys, rest)
		}
	}

	slices.Sort(l.Keys)

	return l, true, nil
}

func (m *memory) Set(ctx context.Context, subkey, name string, d winreg.Datum) error {
	if err := m.Create(ctx, subkey); err != nil {
		return err
	}

	if m.vals[subkey] == nil {
		m.vals[subkey] = map[string]winreg.Datum{}
	}

	m.vals[subkey][name] = d
	m.fire()

	return nil
}

func (m *memory) Create(_ context.Context, subkey string) error {
	at := ""
	for _, step := range parts(subkey) {
		at = join(at, step)
		m.keys[at] = true
	}

	m.fire()

	return nil
}

func (m *memory) DeleteValue(_ context.Context, subkey, name string) error {
	delete(m.vals[subkey], name)
	m.fire()

	return nil
}

func (m *memory) DeleteKey(_ context.Context, subkey string) error {
	for held := range m.keys {
		if held == subkey || strings.HasPrefix(held, join(subkey, "")) {
			delete(m.keys, held)
			delete(m.vals, held)
		}
	}

	m.fire()

	return nil
}

// Arm is the [winreg.Notifier] half, and it is what makes a source over this
// store convertible with Watched: it takes the registration that the next write
// anywhere in the store will signal.
func (m *memory) Arm(context.Context) (winreg.Change, error) {
	return &armedMemory{wait: m.changed}, nil
}

// fire wakes every registration that is waiting and arms the next one.
func (m *memory) fire() {
	close(m.changed)
	m.changed = make(chan struct{})
}

// armedMemory is one registration this store handed out, holding the channel it
// was armed with rather than reading the current one at the wait. That is the
// registration outliving the wait, which is what the real notification does.
type armedMemory struct{ wait chan struct{} }

func (a *armedMemory) Wait(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return false, nil
	case <-a.wait:
		return true, nil
	}
}

func (*armedMemory) Close() error { return nil }

// Example loads an annotated struct out of one registry key.
func Example() {
	type DB struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port,default=5432"`
	}

	type Config struct {
		Name string `ferry:"name"`
		DB   DB     `ferry:"db"`
	}

	store := newMemory()
	_ = store.Set(context.Background(), "", "name", winreg.Datum{Type: winreg.TypeString, Text: "checkout"})
	_ = store.Set(context.Background(), "db", "host", winreg.Datum{Type: winreg.TypeString, Text: "db.internal"})

	src := winreg.NewSource(winreg.LocalMachine, `SOFTWARE\Example`, winreg.Store(store))

	cfg, err := ferry.Load[Config](context.Background(), src)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s %s:%d\n", cfg.Name, cfg.DB.Host, cfg.DB.Port)
	// Output: checkout db.internal:5432
}

// Example_dump writes a struct back into the registry, and shows what the two
// namespaces hold afterwards: a nested struct is a subkey, and a slice is a
// subkey holding one value per position.
func Example_dump() {
	type DB struct {
		Host string `ferry:"host"`
	}

	type Config struct {
		Name string   `ferry:"name"`
		DB   DB       `ferry:"db"`
		Tags []string `ferry:"tags"`
	}

	store := newMemory()
	sink := winreg.NewSink(winreg.CurrentUser, `Software\Example`, winreg.Store(store))

	cfg := Config{Name: "checkout", DB: DB{Host: "db.internal"}, Tags: []string{"eu", "prod"}}
	if err := ferry.Dump(context.Background(), cfg, sink); err != nil {
		panic(err)
	}

	for _, subkey := range slices.Sorted(maps.Keys(store.vals)) {
		for _, name := range slices.Sorted(maps.Keys(store.vals[subkey])) {
			d := store.vals[subkey][name]
			fmt.Printf("%s\t%s\t%s\t%s\n", subkey, name, d.Type, d.Text)
		}
	}
	// Output:
	// 	name	REG_SZ	checkout
	// db	host	REG_SZ	db.internal
	// tags	0	REG_SZ	eu
	// tags	1	REG_SZ	prod
}

// ExampleSource_Watched streams a freshly loaded value every time the key
// changes.
//
// The conversion is argument-free, the stream opens with a load, and one context
// ends the whole of it. The change below is made from inside the range so that
// this example is deterministic; a real program's changes arrive from whatever
// else writes the key.
func ExampleSource_Watched() {
	type Config struct {
		Name string `ferry:"name"`
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newMemory()
	_ = store.Set(ctx, "", "name", winreg.Datum{Type: winreg.TypeString, Text: "checkout"})

	src := winreg.NewSource(winreg.CurrentUser, `Software\Example`, winreg.Store(store))

	wb, err := ferry.BindWatched[Config](src.Watched())
	if err != nil {
		panic(err)
	}

	seq, errf := wb.Watch(ctx)
	for cfg := range seq {
		fmt.Println(cfg.Name)

		if cfg.Name == "payments" {
			break
		}

		_ = store.Set(ctx, "", "name", winreg.Datum{Type: winreg.TypeString, Text: "payments"})
	}

	fmt.Println(errf())
	// Output:
	// checkout
	// payments
	// <nil>
}
