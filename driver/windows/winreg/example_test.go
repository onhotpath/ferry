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
}

func newMemory() *memory {
	return &memory{keys: map[string]bool{"": true}, vals: map[string]map[string]winreg.Datum{}}
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

	return nil
}

func (m *memory) Create(_ context.Context, subkey string) error {
	at := ""
	for _, step := range parts(subkey) {
		at = join(at, step)
		m.keys[at] = true
	}

	return nil
}

func (m *memory) DeleteValue(_ context.Context, subkey, name string) error {
	delete(m.vals[subkey], name)

	return nil
}

func (m *memory) DeleteKey(_ context.Context, subkey string) error {
	for held := range m.keys {
		if held == subkey || strings.HasPrefix(held, join(subkey, "")) {
			delete(m.keys, held)
			delete(m.vals, held)
		}
	}

	return nil
}

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
