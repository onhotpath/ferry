package ferry

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/onhotpath/ferry/internal/testdata/badtags"
)

// Every assertion in this file goes through Compile[T], which is the seam for
// every compiler rule. Nothing here reaches the schema, the node tree, the
// scanner or the grammar directly: a rule that cannot be seen through the
// exported entry point is a rule a user cannot rely on.

// case_ is one compile and what it has to report. want is a list of substrings
// every one of which must appear in the report, and elements is how many
// refusals the report holds.
//
// Message text is not API, and asserting on substrings of it here is
// deliberate: these are the diagnoses ADR-0008 spends its argument on, and a
// diagnosis nobody checks is a diagnosis that rots.
type compileCase struct {
	name     string
	run      func(...Option) error
	opts     []Option
	want     []string
	elements int
}

func run(t *testing.T, cases []compileCase) {
	t.Helper()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.check(t)
		})
	}
}

func (c compileCase) check(t *testing.T) {
	t.Helper()

	err := c.run(c.opts...)
	if err == nil {
		t.Fatalf("compiled clean, want %v", c.want)
	}

	report := fmt.Sprintf("%+v", err)
	mustContain(t, report, c.want)

	if n := len(Elements(err)); n != c.elements {
		t.Errorf("report holds %d elements, want %d:\n%s", n, c.elements, report)
	}

	if !errors.Is(err, ErrSchema) {
		t.Errorf("%v is not an ErrSchema", err)
	}
}

func mustContain(t *testing.T, report string, want []string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(report, w) {
			t.Errorf("report\n\t%s\ndoes not contain\n\t%s", report, w)
		}
	}
}

// sub is a nested struct of string leaves, which is this compiler's whole type
// coverage beside the leaf itself.
type sub struct {
	Host string `ferry:"host"`
}

type common struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

// everything is the worked example from ADR-0008, every row of it.
type everything struct {
	common
	Host     string   `ferry:"host,required"`
	Greeting string   `ferry:"greeting,default='Hello, world'"`
	Brokers  string   `ferry:"brokers,default='h1:9092,h2:9092'"`
	Cache    string   `ferry:"cache,default=~/.cache/app"`
	Note     string   `ferry:"note,default=it's here"`
	Odd      string   `ferry:"'a,b'"`
	Dash     string   `ferry:"'-'"`
	Apos     string   `ferry:"'a''b'"`
	Empty    string   `ferry:"empty,default="`
	Omitted  string   `ferry:"omitted,omitzero"`
	Zero     string   `ferry:"zero,omitzero,default="`
	Nested   sub      `ferry:"nested"`
	Skipped  chan int `ferry:"-"`
	hidden   string
}

// Touch is what stops hidden being unused. It is there to be skipped, which is
// a thing only reflect can see.
func (e *everything) Touch() { e.hidden = "" }

// flatNeighbours is the cost ADR-0003 states exactly: /a and /a-b are two flat
// addresses and remain legal, because the prefix relation holds at segment
// boundaries only.
type flatNeighbours struct {
	A  string `ferry:"a"`
	AB string `ferry:"a-b"`
}

// segments are the segment names the grammar has to be able to write, which is
// the obligation ADR-0003 handed the tag grammar.
type segments struct {
	Slash  string `ferry:"a/b"`
	Hash   string `ferry:"a#b"`
	Tilde  string `ferry:"a~b"`
	Space  string `ferry:"a b"`
	Equals string `ferry:"'a=b'"`
	Word   string `ferry:"required"`
}

type ptrRoot struct {
	Host string `ferry:"host"`
}

func TestCompileAccepts(t *testing.T) {
	t.Parallel()

	accepted := []compileCase{
		{name: "every row of the grammar", run: Compile[everything]},
		{name: "a flat neighbour is not a prefix", run: Compile[flatNeighbours]},
		{name: "punctuation the rendering owns", run: Compile[segments]},
		{name: "a pointer at the root", run: Compile[*ptrRoot]},
		{name: "a nested struct", run: Compile[struct {
			S sub `ferry:"s"`
		}]},
		{name: "omitzero on a nested struct", run: Compile[struct {
			S sub `ferry:"s,omitzero"`
		}]},
		{name: "an embedded field under a name", run: Compile[struct {
			common `ferry:"common"`
			Port   string `ferry:"port"`
		}]},
		{name: "and its fields no longer clash with the parent's", run: Compile[struct {
			common `ferry:"common"`
			Name   string `ferry:"name"`
		}]},
		{name: "an embedded field skipped", run: Compile[struct {
			common `ferry:"-"`
			Port   string `ferry:"port"`
		}]},
		{name: "a skipped field of a type ferry does not map", run: Compile[struct {
			Bad  chan int `ferry:"-"`
			Port string   `ferry:"port"`
		}]},
		{name: "a skipped block ferry never walks", run: Compile[struct {
			Bad  untagged `ferry:"-"`
			Port string   `ferry:"port"`
		}]},
		{name: "another library's key", run: Compile[mylib], opts: []Option{TagKey("mylib")}},
	}

	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.run(c.opts...); err != nil {
				t.Fatalf("did not compile: %+v", err)
			}
		})
	}
}

// noAddress is ADR-0005's silent total loss in its smallest form.
type noAddress struct{}

// untagged is a struct that cannot compile, so a field of it that compiles
// clean proves ferry:"-" skipped the block rather than walking it.
type untagged struct {
	Host string
}

type mylib struct {
	Host string   `mylib:"host"`
	Sub  mylibSub `mylib:"sub"`
}

type mylibSub struct {
	Port string `mylib:"port"`
}

func TestCompileFieldRule(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "an exported field with no tag names the field and both remedies",
		run: Compile[struct {
			Debug string
			OK    string `ferry:"ok"`
		}],
		want: []string{
			`/Debug: field Debug carries no ferry tag`,
			`every exported field must name the segment it addresses`,
			`or be marked ferry:"-"`,
		},
		elements: 1,
	}, {
		name:     "the remedies are spelled in the configured key",
		run:      Compile[untagged],
		opts:     []Option{TagKey("mylib")},
		want:     []string{`field Host carries no mylib tag`, `or be marked mylib:"-"`},
		elements: 1,
	}, {
		name: "an unexported field carrying a tag can never do anything",
		run:  Compile[badtags.UnexportedTagged],
		want: []string{
			"/host: field host is unexported",
			"reflect cannot set it and its ferry tag can never do anything",
			"export the field, or delete the tag",
		},
		elements: 1,
	}, {
		name:     "a struct that maps no address does not compile",
		run:      Compile[struct{}],
		want:     []string{"struct {} maps no address", "must contribute at least one"},
		elements: 1,
	}, {
		name: "a nested struct that maps no address does not compile",
		run: Compile[struct {
			S noAddress `ferry:"s"`
		}],
		want:     []string{"/S: ferry.noAddress maps no address"},
		elements: 1,
	}, {
		name: "a type ferry does not map is refused at its address",
		run: Compile[struct {
			C chan int `ferry:"c"`
		}],
		want:     []string{"/c: chan int is not a type ferry maps to an address"},
		elements: 1,
	}})
}

// TestCompileUnexportedIsSkipped is the other half of the field rule, and it
// needs its own test because the rule it asserts is that nothing is reported.
func TestCompileUnexportedIsSkipped(t *testing.T) {
	t.Parallel()

	var (
		v badtags.UnexportedSkipped
		e everything
	)

	v.Touch()
	e.Touch()

	if err := Compile[badtags.UnexportedSkipped](); err != nil {
		t.Fatalf("an unexported field is skipped, and ferry:\"-\" on one is redundant: %+v", err)
	}
}

func TestCompileEmbedding(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "an embedded pointer with no tag has no subtree to materialise",
		run: Compile[struct {
			*common
			Port string `ferry:"port"`
		}],
		want: []string{
			"/common: embedded field common is a pointer",
			"promotion walks the pointed-to struct at the parent address",
			`give it a name, as ferry:"<name>", to nest it, or mark it ferry:"-"`,
		},
		elements: 1,
	}, {
		name: "an embedded field that is not a struct cannot be promoted",
		run: Compile[struct {
			namedString
			Port string `ferry:"port"`
		}],
		want: []string{
			"/namedString: embedded field namedString is a ferry.namedString",
			"only a struct can be promoted to the parent address",
		},
		elements: 1,
	}, {
		name: "a promoted field clashing with a sibling is ADR-0003's rule applied",
		run: Compile[struct {
			common
			Name string `ferry:"name"`
			Port string `ferry:"port"`
		}],
		want:     []string{"/name: addressed by two fields, /common/Name and /Name"},
		elements: 1,
	}, {
		name: "an embedded field with a tag nests under it",
		run: Compile[struct {
			common `ferry:"common"`
			C      string `ferry:"common"`
		}],
		want: []string{
			"/common: a leaf address and a prefix of /common/env",
			"which no tree plane can hold both of: /C and /common/Env",
			"/common: a leaf address and a prefix of /common/name",
		},
		elements: 2,
	}, {
		name: "a promoted field's diagnosis names where the field is",
		run: Compile[struct {
			brokenCommon
			Port string `ferry:"port"`
		}],
		want:     []string{"/brokenCommon/Host: field Host carries no ferry tag"},
		elements: 1,
	}, {
		name: "an embedded field with an empty name and an option is refused",
		run: Compile[struct {
			common `ferry:",required"`
			Port   string `ferry:"port"`
		}],
		want:     []string{"/common: the name is empty"},
		elements: 1,
	}})
}

type namedString string

type brokenCommon struct {
	Host string
}

// unexportedEmbed is considered whether or not its own type is exported,
// because Go's field namespace promotes it and reflect can set through it.
type unexportedEmbed struct {
	Port string `ferry:"port"`
}

func TestCompileEmbedsUnexportedType(t *testing.T) {
	t.Parallel()

	// Promoted, so the two /port addresses collide. Skipping the block would
	// have compiled clean and dropped a mapped field in silence.
	err := Compile[struct {
		unexportedEmbed
		Port string `ferry:"port"`
	}]()
	if err == nil {
		t.Fatal("an embedded unexported type was skipped rather than promoted")
	}

	if want := "addressed by two fields"; !strings.Contains(err.Error(), want) {
		t.Errorf("%v does not contain %q", err, want)
	}
}

func TestCompilePrefixFree(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "two fields at one address",
		run: Compile[struct {
			A string `ferry:"same"`
			B string `ferry:"same"`
		}],
		want:     []string{"/same: addressed by two fields, /A and /B"},
		elements: 1,
	}, {
		name: "a leaf and a subtree sharing one segment",
		run: Compile[struct {
			DB  string `ferry:"db"`
			Sub sub    `ferry:"db"`
		}],
		want: []string{
			"/db: a leaf address and a prefix of /db/host",
			"which no tree plane can hold both of: /DB and /Sub/Host",
		},
		elements: 1,
	}, {
		name: "every clash is listed",
		run: Compile[struct {
			A string `ferry:"same"`
			B string `ferry:"same"`
			C string `ferry:"same"`
		}],
		want:     []string{"/A and /B", "/A and /C", "/B and /C"},
		elements: 3,
	}})
}

func TestCompileRoot(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "a root leaf mints the empty path, which is not an address",
		run:      Compile[int],
		want:     []string{"int is not a struct ferry walks", "wrapping it in one is the whole remedy"},
		elements: 1,
	}, {
		name:     "a root map",
		run:      Compile[map[string]string],
		want:     []string{"map[string]string is not a struct ferry walks"},
		elements: 1,
	}, {
		name:     "a root slice",
		run:      Compile[[]string],
		want:     []string{"[]string is not a struct ferry walks"},
		elements: 1,
	}, {
		name:     "a root interface",
		run:      Compile[any],
		want:     []string{"interface {} is not a struct ferry walks"},
		elements: 1,
	}, {
		name:     "a pointer to a root leaf",
		run:      Compile[*int],
		want:     []string{"int is not a struct ferry walks"},
		elements: 1,
	}})
}

// tiers is ADR-0008's measured fixture, byte for byte: one field of each fault,
// plus a clean one, reporting 1 + 2 + 1 elements.
//
// The tier-two field is a []string rather than a struct, which is the ADR's own
// spelling of it. A struct carries required admissibly now (ADR-0006, and #118
// against the draft that refused it), so it contributes one fault rather than
// two and cannot stand in for the two-fault row.
type tiers struct {
	H  string   `ferry:"h,requird"`
	S  []string `ferry:"s,required,default=v"`
	C  string   `ferry:"c,required,default=x"`
	OK string   `ferry:"ok"`
}

func TestCompileTiers(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "four fields, one fault of each kind, four elements",
		run:  Compile[tiers],
		want: []string{
			`/H: unknown option "requird"`,
			"/s: required is not available on []string",
			"/s: []string is not a leaf",
			"/c: required and default contradict",
		},
		elements: 4,
	}, {
		name: "a tier one fault suppresses everything below it",
		run: Compile[struct {
			H  chan int `ferry:"h,requird"`
			OK string   `ferry:"ok"`
		}],
		want:     []string{`unknown option "requird"`},
		elements: 1,
	}, {
		name: "a tier two fault suppresses the contradiction",
		run: Compile[struct {
			S  sub    `ferry:"s,omitzero,default=x"`
			OK string `ferry:"ok"`
		}],
		want:     []string{"/s: ferry.sub is not a leaf"},
		elements: 1,
	}, {
		name: "omitzero and a non-zero default contradict",
		run: Compile[struct {
			B string `ferry:"b,omitzero,default=8080"`
		}],
		want: []string{
			"/b: omitzero and default=8080 contradict",
			"an explicit zero would be omitted",
		},
		elements: 1,
	}, {
		name:     "a field error suppresses the maps-no-address check",
		run:      Compile[untagged],
		want:     []string{"field Host carries no ferry tag"},
		elements: 1,
	}})
}

// TestCompileIsDeterministic is ADR-0001's determinism invariant, measured the
// way ADR-0003 measured it. It is what makes a CI diff empty when nothing
// changed, and it is the reason the aggregate sorts at construction rather than
// at print time.
func TestCompileIsDeterministic(t *testing.T) {
	t.Parallel()

	const runs = 300

	lines := map[string]int{}
	reports := map[string]int{}

	for range runs {
		err := Compile[tiers]()

		lines[err.Error()]++
		reports[fmt.Sprintf("%+v", err)]++
	}

	if len(lines) != 1 || len(reports) != 1 {
		t.Fatalf("%d compiles produced %d one-line forms and %d reports, want 1 and 1",
			runs, len(lines), len(reports))
	}
}

// TestCompileLocatesTierOneAtTheField is the two spaces one location holds: a
// field whose tag does not parse never named an address, and the whole error is
// that it did not.
func TestCompileLocatesTierOneAtTheField(t *testing.T) {
	t.Parallel()

	locations := []struct {
		name string
		run  func(...Option) error
		want Path
	}{
		{"the Go field path, where the tag did not parse", Compile[struct {
			H string `ferry:"h,requird"`
		}], At("H")},
		{"the address, where it did", Compile[struct {
			H string `ferry:"h,required,default=x"`
		}], At("h")},
	}

	for _, c := range locations {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mustLocate(t, c.run(), c.want)
		})
	}
}

func mustLocate(t *testing.T, err error, want Path) {
	t.Helper()

	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("%v is not a *ferry.Error", err)
	}

	if got := e.Address(); got != want {
		t.Errorf("located at %s, want %s", got, want)
	}
}
