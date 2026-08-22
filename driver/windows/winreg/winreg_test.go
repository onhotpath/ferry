package winreg_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The schemas the behaviour tests are written against.
type (
	// typed is one field per registry type this driver reads.
	typed struct {
		Text   string `ferry:"text"`
		Expand string `ferry:"expand"`
		Count  int    `ferry:"count"`
		Big    uint64 `ferry:"big"`
		Raw    []byte `ferry:"raw"`
	}

	// oneText is the smallest schema there is, and the one the refusals are read
	// through.
	oneText struct {
		Text string `ferry:"text"`
	}

	// requiredHost is what a report's opening line is read off.
	requiredHost struct {
		Host string `ferry:"host,required"`
	}

	// bothNamespaces is a value and a subkey of one name, which the registry
	// keeps apart and this driver must too.
	bothNamespaces struct {
		Leaf    string    `ferry:"a"`
		Section innerHost `ferry:"A"`
	}

	// structMap is a composite whose members are subkeys rather than values,
	// which is what makes the sweep have to descend.
	structMap struct {
		Envs map[string]innerHost `ferry:"envs"`
	}

	// oneBlob is the smallest schema whose one value is bytes.
	oneBlob struct {
		Raw []byte `ferry:"raw"`
	}
)

// TestReadsEveryTypeItStores is the read half of the type table: text, an
// expandable string taken raw, both integer widths, and opaque bytes.
//
// The expand row is the one that matters most. %SystemRoot% reaches the field as
// those twelve characters, because expanding it here and dumping afterwards would
// write the expansion back over what the operator wrote.
func TestReadsEveryTypeItStores(t *testing.T) {
	t.Parallel()

	store := newFake().
		put("", "text", winreg.Datum{Type: winreg.TypeString, Text: "h"}).
		put("", "expand", winreg.Datum{Type: winreg.TypeExpandString, Text: `%SystemRoot%\x`}).
		put("", "count", winreg.Datum{Type: winreg.TypeDWord, Text: "42"}).
		put("", "big", winreg.Datum{Type: winreg.TypeQWord, Text: "18446744073709551615"}).
		put("", "raw", winreg.Datum{Type: winreg.TypeBinary, Binary: []byte{0x00, 0xff, 'A'}})

	got, err := ferry.Load[typed](t.Context(), source(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := typed{
		Text: "h", Expand: `%SystemRoot%\x`, Count: 42, Big: 18446744073709551615,
		Raw: []byte{0x00, 0xff, 'A'},
	}

	if got.Text != want.Text || got.Expand != want.Expand || got.Count != want.Count || got.Big != want.Big {
		t.Errorf("loaded %+v, want %+v", got, want)
	}

	if string(got.Raw) != string(want.Raw) {
		t.Errorf("loaded %q, want %q", got.Raw, want.Raw)
	}
}

// TestRefusesATypeItCannotCarry is the other half of the same table.
//
// REG_MULTI_SZ is a sequence spelled inside one value, and ferry addresses each
// element of a sequence in its own right, so there is no address to read it into
// and the honest answer is a refusal rather than the whole list as one string.
func TestRefusesATypeItCannotCarry(t *testing.T) {
	t.Parallel()

	cases := map[string]winreg.Type{
		"REG_MULTI_SZ":          winreg.TypeMultiString,
		"a type ferry does not": winreg.TypeOther,
	}

	for name, kind := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newFake().put("", "text", winreg.Datum{Type: kind})

			_, err := ferry.Load[oneText](t.Context(), source(store))
			if !errors.Is(err, winreg.ErrValueType) {
				t.Fatalf("Load answered %v, want an error reaching winreg.ErrValueType", err)
			}
		})
	}
}

// TestValuesAndSubkeysAreTwoNamespaces is the whole two-namespace design in one
// load: a value a and a subkey A under one key are two objects, and both are read.
func TestValuesAndSubkeysAreTwoNamespaces(t *testing.T) {
	t.Parallel()

	store := newFake().
		put("", "a", winreg.Datum{Type: winreg.TypeString, Text: "the value"}).
		put("A", "host", winreg.Datum{Type: winreg.TypeString, Text: "the subkey"})

	got, err := ferry.Load[bothNamespaces](t.Context(), source(store))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Leaf != "the value" || got.Section.Host != "the subkey" {
		t.Errorf("loaded %+v, want the value and the subkey read separately", got)
	}
}

// TestASubkeyWhereAMemberTakesAValueIsRefused is the refusal a driver that
// enumerated both namespaces has to make.
//
// A map's members are whatever the key holds, so a subkey there is a member; but a
// map[string]string's member takes a single value, and answering Absent would fill
// the entry with the Go zero and drop what the registry actually held.
func TestASubkeyWhereAMemberTakesAValueIsRefused(t *testing.T) {
	t.Parallel()

	store := newFake().put(`tags\http`, "port", winreg.Datum{Type: winreg.TypeString, Text: "80"})

	_, err := ferry.Load[tagsMap](t.Context(), source(store))
	if !errors.Is(err, winreg.ErrDeeperThanLeaf) {
		t.Fatalf("Load answered %v, want an error reaching winreg.ErrDeeperThanLeaf", err)
	}
}

// TestASaveKeepsAnExpandableStringAndRetypesEverythingElse is the one place this
// driver reads the plane before writing it, and the two rows are why.
//
// REG_EXPAND_SZ survives because retyping it would destroy the expansion for
// every other reader of that key, which is the plane-compatibility break this
// driver already refuses to commit by expanding on read. REG_DWORD does not,
// because REG_SZ is the only type that carries a number's own spelling intact,
// and the data survives either way.
func TestASaveKeepsAnExpandableStringAndRetypesEverythingElse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		held []winreg.Datum
		want string
	}{
		"an expandable string keeps its type": {
			held: []winreg.Datum{{Type: winreg.TypeExpandString, Text: `%SystemRoot%`}},
			want: `val "" "text" REG_EXPAND_SZ "2"`,
		},
		"a number an operator typed by hand does not": {
			held: []winreg.Datum{{Type: winreg.TypeDWord, Text: "1"}},
			want: `val "" "text" REG_SZ "2"`,
		},
		"a value that is not there is a fresh REG_SZ": {
			want: `val "" "text" REG_SZ "2"`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkTypeAfterSave(t, tc.held, tc.want)
		})
	}
}

// checkTypeAfterSave saves one string over whatever the registry already held at
// that address, and holds the result to one rendered row.
func checkTypeAfterSave(t *testing.T, held []winreg.Datum, want string) {
	t.Helper()

	store := newFake()
	for _, d := range held {
		store.put("", "text", d)
	}

	if err := ferry.Dump(t.Context(), oneText{Text: "2"}, sink(store)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	if got := string(store.contents()); !strings.Contains(got, want) {
		t.Errorf("the save left this behind:\n%s\nwant %s", got, want)
	}
}

// TestBytesOverAnExpandableStringAreStillBinary is the other half of the same
// rule: what is kept is the type of a value this dump writes text to, and bytes
// have one type here whatever the address held before.
func TestBytesOverAnExpandableStringAreStillBinary(t *testing.T) {
	t.Parallel()

	store := newFake().put("", "raw", winreg.Datum{Type: winreg.TypeExpandString, Text: `%SystemRoot%`})

	if err := ferry.Dump(t.Context(), oneBlob{Raw: []byte{0x00, 'A'}}, sink(store)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	if held := string(store.contents()); !strings.Contains(held, `val "" "raw" REG_BINARY`) {
		t.Errorf("the save left this behind:\n%s", held)
	}
}

// TestTheSweepTellsAValueFromASubkeyOfTheSameName is the aliasing the staging
// used to have: a value name holding a backslash, which the registry allows and
// which joined to the key of a value one subkey deeper.
//
// A foreign value named a\b under a replaced composite is not something this
// dump wrote, and keeping it because a member one level down happens to be named
// b is the sweep leaving behind exactly what dump-is-replace exists to remove.
//
// The key's own unnamed value is the same aliasing from the other end, and it is
// not a case here because core refuses the schema that would reach it: a leaf
// whose key is a composite's key is refused at Bind, since that composite would
// enumerate it as one of its own members. This driver still does not lean on
// that, because the alias is in its own staging and not in core's table.
func TestTheSweepTellsAValueFromASubkeyOfTheSameName(t *testing.T) {
	t.Parallel()

	store := newFake().put("m", `a\b`, winreg.Datum{Type: winreg.TypeString, Text: "stale"})

	both := mapOfMaps{M: map[string]map[string]string{"a": {"b": "1"}}}
	if err := ferry.Dump(t.Context(), both, sink(store)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	if held := string(store.contents()); strings.Contains(held, `"stale"`) {
		t.Errorf("the sweep kept a value one subkey deeper had the key of:\n%s", held)
	}
}

// TestASaveKeepsTheCallerSpellingAndFoldsOnlyTheKey is what the fold costs and
// what it does not.
//
// The key function folds so that two addresses the registry would merge are
// refused at Bind. The write does not, because the registry keeps whichever
// spelling wrote a name first and there is no reason for this driver to be what
// destroys it.
func TestASaveKeepsTheCallerSpellingAndFoldsOnlyTheKey(t *testing.T) {
	t.Parallel()

	store := newFake()

	if err := ferry.Dump(t.Context(), tagsMap{Tags: map[string]string{"HttpPort": "80"}}, sink(store)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	if held := string(store.contents()); !strings.Contains(held, `"HttpPort"`) {
		t.Errorf("the save folded the caller's own spelling away:\n%s", held)
	}
}

// TestARefusedSaveLeavesTheRegistryAlone is conformance case 18's property, made
// here because the suite skips that case for this plane: its two colliding map
// keys are two names the registry keeps apart, so there is no refusal there to
// hold this driver to. A string REG_SZ cannot spell is a refusal there is.
func TestARefusedSaveLeavesTheRegistryAlone(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a string holding a NUL":     "a\x00b",
		"a string that is not UTF-8": "\xff\xfe",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkSaveRefused(t, text)
		})
	}
}

// checkSaveRefused saves one unspellable string over a registry that already
// holds something, and holds the refusal to leaving that something alone.
func checkSaveRefused(t *testing.T, text string) {
	t.Helper()

	store := newFake().put("", "text", winreg.Datum{Type: winreg.TypeString, Text: "as it was"})

	err := ferry.Dump(t.Context(), oneText{Text: text}, sink(store))
	if !errors.Is(err, winreg.ErrUnspellable) {
		t.Fatalf("Dump answered %v, want an error reaching winreg.ErrUnspellable", err)
	}

	if held := string(store.contents()); !strings.Contains(held, `"as it was"`) {
		t.Errorf("the refused save changed the registry:\n%s", held)
	}
}

// TestASaveReplacesACompositeOfSubkeys is the sweep descending, which is the half
// of dump-is-replace a flat plane never has to do: the members of this composite
// are subkeys, so forgetting one means removing a whole subtree.
func TestASaveReplacesACompositeOfSubkeys(t *testing.T) {
	t.Parallel()

	store := newFake()
	both := structMap{Envs: map[string]innerHost{"prod": {Host: "p"}, "dev": {Host: "d"}}}

	if err := ferry.Dump(t.Context(), both, sink(store)); err != nil {
		t.Fatalf("the first Dump: %v", err)
	}

	one := structMap{Envs: map[string]innerHost{"prod": {Host: "p"}}}
	if err := ferry.Dump(t.Context(), one, sink(store)); err != nil {
		t.Fatalf("the second Dump: %v", err)
	}

	held := string(store.contents())
	if strings.Contains(held, "dev") {
		t.Errorf("the save left the member it replaced behind:\n%s", held)
	}

	if !strings.Contains(held, `val "envs\\prod" "host" REG_SZ "p"`) {
		t.Errorf("the save removed the member it wrote:\n%s", held)
	}
}

// TestAReadFailureReachesTheCaller is conformance case 4 asserted against this
// driver's own seam: a registry that could not be read and a value it does not
// hold are different observations, and only one of them is a configuration.
func TestAReadFailureReachesTheCaller(t *testing.T) {
	t.Parallel()

	_, err := ferry.Load[oneText](t.Context(), source(newFake().failUnder("")))
	if !errors.Is(err, errFake) {
		t.Fatalf("Load answered %v, want the registry's own error", err)
	}
}

// TestASaveRefusesAKeyItMayNotWrite is ADR-0004's placement: not at Bind, which
// does no I/O and so cannot know, and not at the first write, which would already
// have half-written the plane.
func TestASaveRefusesAKeyItMayNotWrite(t *testing.T) {
	t.Parallel()

	store := newFake().failUnder("")

	b, err := ferry.BindSink[oneText](sink(store))
	if err != nil {
		t.Fatalf("BindSink refused a schema it can name: %v", err)
	}

	if err := b.Dump(t.Context(), oneText{Text: "x"}); !errors.Is(err, ferry.ErrReadOnly) {
		t.Fatalf("Dump answered %v, want an error reaching ferry.ErrReadOnly", err)
	}
}

// TestAReportOpensWithTheRegistryPath is [ferry.PlaneNamer] earning its keep: the
// line names the key somebody can go and open in regedit rather than ferry's own
// rendering of the address.
func TestAReportOpensWithTheRegistryPath(t *testing.T) {
	t.Parallel()

	_, err := ferry.Load[requiredHost](t.Context(), source(newFake()))
	if err == nil {
		t.Fatal("Load accepted a required field the registry does not hold")
	}

	if want := `HKEY_CURRENT_USER\Software\Example\host`; !strings.Contains(err.Error(), want) {
		t.Errorf("the report does not open with %s: %v", want, err)
	}
}

// TestOptionRefusalsLandAtBind is where a constructor that returns no error puts
// one.
func TestOptionRefusalsLandAtBind(t *testing.T) {
	t.Parallel()

	cases := map[string]*winreg.Source{
		"a hive nobody chose": winreg.NewSource(0, base, winreg.Store(newFake())),
		"a view outside the three": winreg.NewSource(winreg.CurrentUser, base,
			winreg.Store(newFake()), winreg.WithView(winreg.View(9))),
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := bindOf[oneText](src); !errors.Is(err, winreg.ErrOption) {
				t.Fatalf("Bind answered %v, want an error reaching winreg.ErrOption", err)
			}
		})
	}
}

// TestThereIsNoRegistryOffWindows is the module's own portability statement, and
// it is asserted rather than assumed: the seam is behind a build tag, and every
// platform gets an answer it can act on.
func TestThereIsNoRegistryOffWindows(t *testing.T) {
	t.Parallel()

	err := bindOf[oneText](winreg.NewSource(winreg.CurrentUser, base))

	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("Bind refused the machine's own registry: %v", err)
		}

		return
	}

	if !errors.Is(err, winreg.ErrNoRegistry) {
		t.Fatalf("Bind answered %v, want an error reaching winreg.ErrNoRegistry", err)
	}
}

// quiet is a [winreg.Registry] that is no [winreg.Notifier], which is every store
// that cannot say when it changed.
//
// It forwards rather than embeds, because embedding *fake would promote its Notify
// and make this exactly the thing it is not.
type quiet struct{ reg *fake }

func (q quiet) Get(ctx context.Context, subkey, name string) (winreg.Datum, bool, error) {
	return q.reg.Get(ctx, subkey, name)
}

func (q quiet) List(ctx context.Context, subkey string) (winreg.Listing, bool, error) {
	return q.reg.List(ctx, subkey)
}

func (q quiet) Set(ctx context.Context, subkey, name string, d winreg.Datum) error {
	return q.reg.Set(ctx, subkey, name, d)
}

func (q quiet) Create(ctx context.Context, subkey string) error { return q.reg.Create(ctx, subkey) }

func (q quiet) DeleteValue(ctx context.Context, subkey, name string) error {
	return q.reg.DeleteValue(ctx, subkey, name)
}

func (q quiet) DeleteKey(ctx context.Context, subkey string) error {
	return q.reg.DeleteKey(ctx, subkey)
}

var _ winreg.Registry = quiet{}
