package yaml_test

import (
	"reflect"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// TestAShorterListReplacesTheOneInTheFile is the reported reproduction (#220): a
// three-element list saved over with one element.
//
// Both legs are asserted, because both were silent. The file is what an operator
// opening it sees, and the load is what the caller who dumped gets back the next
// time - and a save that reported success while leaving two elements behind made
// the second of those two disagree with the value that was saved.
func TestAShorterListReplacesTheOneInTheFile(t *testing.T) {
	type config struct {
		Tags []string `ferry:"tags"`
	}

	path := write(t, "tags:\n  - a\n  - b\n  - c\n")

	if err := ferry.Dump(t.Context(), config{Tags: []string{"x"}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "tags:\n  - x\n"; got != want {
		t.Errorf("the plane holds %q, want %q: a save replaces the list it writes rather than overwriting its "+
			"first positions", got, want)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if want := []string{"x"}; !reflect.DeepEqual(got.Tags, want) {
		t.Errorf("the plane loads back as %v, want %v", got.Tags, want)
	}
}

// TestAMappingThatLostAKeyLosesItInTheFile is the same rule at the other
// composite kind (#220).
//
// A mapping is not truncated, so what is asserted is that the key the second
// value no longer holds is gone while the one it does hold stays where the
// operator wrote it.
func TestAMappingThatLostAKeyLosesItInTheFile(t *testing.T) {
	type config struct {
		Limits map[string]int `ferry:"limits"`
	}

	path := write(t, "limits:\n  a: 1\n  b: 2\n")

	if err := ferry.Dump(t.Context(), config{Limits: map[string]int{"a": 5}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "limits:\n  a: 5\n"; got != want {
		t.Errorf("the plane holds %q, want %q: a save replaces the mapping it writes", got, want)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if want := map[string]int{"a": 5}; !reflect.DeepEqual(got.Limits, want) {
		t.Errorf("the plane loads back as %v, want %v", got.Limits, want)
	}
}

// TestAReplacedListKeepsWhatTheOperatorWroteOnWhatStays is the limit of the
// rule, and it is why the members are subtracted at the commit rather than
// cleared when core asks.
//
// The position that stays keeps its comment and its anchor, the key no field
// maps is untouched, and only the positions the second value no longer has are
// gone. Clearing the sequence and writing it again would have been correct about
// the length and lost all three.
func TestAReplacedListKeepsWhatTheOperatorWroteOnWhatStays(t *testing.T) {
	type config struct {
		Tags []string `ferry:"tags"`
	}

	path := write(t, "# the file\ntags:\n  - &first a # keep me\n  - b\n  - c\nnote: untouched\n")

	if err := ferry.Dump(t.Context(), config{Tags: []string{"x"}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	want := "# the file\ntags:\n  - &first x # keep me\nnote: untouched\n"
	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q: what a replaced list keeps is still the operator's", got, want)
	}
}

// TestAListThatLostEveryElementLeavesNothingBehind is the empty arm, where core
// replaces the composite and then writes the plane's null at its own address.
//
// The null is what the reload needs to hand back a nil slice, and the three
// positions under it are what the unset before it takes away.
func TestAListThatLostEveryElementLeavesNothingBehind(t *testing.T) {
	type config struct {
		Tags []string `ferry:"tags"`
	}

	path := write(t, "tags:\n  - a\n  - b\n  - c\n")

	if err := ferry.Dump(t.Context(), config{}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "tags: null\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if got.Tags != nil {
		t.Errorf("the plane loads back as %v, want a nil list", got.Tags)
	}
}

// TestAListOfSectionsShrinks is the composite whose members are not leaves, which
// is where the addresses the walk writes sit below the positions being replaced.
//
// A position stays because something under it was written and not because it was
// named, so a dump that shrinks a list of structs has to leave the file holding
// exactly the structs the value has.
func TestAListOfSectionsShrinks(t *testing.T) {
	type server struct {
		Host string `ferry:"host"`
		Port int    `ferry:"port"`
	}

	type config struct {
		Servers []server `ferry:"servers"`
	}

	path := write(t, "servers:\n  - host: a\n    port: 1\n  - host: b\n    port: 2\n")

	want := config{Servers: []server{{Host: "z", Port: 9}}}
	if err := ferry.Dump(t.Context(), want, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, doc := read(t, path), "servers:\n  - host: z\n    port: 9\n"; got != doc {
		t.Errorf("the plane holds %q, want %q", got, doc)
	}

	got, err := ferry.Load[config](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("loading back what the dump wrote: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the plane loads back as %+v, want %+v", got, want)
	}
}

// TestASectionIsNotReplaced is the rule's boundary: a struct's members come from
// the type, so a field the value leaves out is left where it is.
//
// The list under the section is replaced all the same, which is what says the
// two rules compose rather than one of them winning.
func TestASectionIsNotReplaced(t *testing.T) {
	type db struct {
		Tags []string `ferry:"tags"`
	}

	type config struct {
		DB db `ferry:"db"`
	}

	path := write(t, "db:\n  host: localhost\n  tags:\n    - a\n    - b\n")

	if err := ferry.Dump(t.Context(), config{DB: db{Tags: []string{"x"}}}, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	want := "db:\n  host: localhost\n  tags:\n    - x\n"
	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q: a key no field maps is not a member a save replaces", got, want)
	}
}
