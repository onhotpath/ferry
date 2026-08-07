package yaml_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/yaml"
)

// tagged is the struct the node-tag tests share: two addresses annotated with a
// tag this plane has no reading of its own for, and one that is not.
//
// The two tags are the two cases #156 names. !mycompany:duration is a tag YAML
// cannot resolve at all, and !!timestamp is one it resolves from the bare text
// of a value this driver still reads as a string.
type tagged struct {
	Wait string `ferry:"wait" yamlext:"node=!mycompany:duration"`
	When string `ferry:"when" yamlext:"node=!!timestamp"`
	Port int    `ferry:"port"`
}

// declared is the registry these tests resolve against: core's own type set,
// plus this driver's tag key.
func declared() ferry.Option {
	return ferry.WithRegistry(ferry.NewRegistry(ferry.WithTagKeys(yaml.Extension())))
}

// TestADeclaredNodeTagSurvivesThreeStages is #156's acceptance bar: a load, a
// dump, and a second load, with the third stage compared against the first.
//
// It is stricter than a round trip on purpose. A read and a write that are
// wrong in the same direction are self-consistent and round-trip, so the file's
// own text is asserted between the stages, and the document it starts from
// carries no tag at either address: what closes the cycle here is the schema's
// annotation minting one, which is the half [carryTag] cannot do because there
// was nothing in the file to carry.
func TestADeclaredNodeTagSurvivesThreeStages(t *testing.T) {
	path := write(t, "wait: 30s\nwhen: '2026-08-04T00:00:00Z'\nport: 5432\n")

	first, err := ferry.Load[tagged](t.Context(), yaml.NewSource(path), declared())
	if err != nil {
		t.Fatalf("the first load: %v", err)
	}

	if want := (tagged{Wait: "30s", When: "2026-08-04T00:00:00Z", Port: 5432}); first != want {
		t.Fatalf("the first load gave %+v, want %+v", first, want)
	}

	if err := ferry.Dump(t.Context(), first, yaml.NewSink(path), declared()); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const want = "wait: !mycompany:duration 30s\nwhen: !!timestamp 2026-08-04T00:00:00Z\nport: 5432\n"

	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q: the tag each field declared is what the address is written under",
			got, want)
	}

	third, err := ferry.Load[tagged](t.Context(), yaml.NewSource(path), declared())
	if err != nil {
		t.Fatalf("the third load: %v", err)
	}

	if third != first {
		t.Errorf("the third stage is %+v and the first was %+v", third, first)
	}
}

// TestADeclaredNodeTagIsWrittenIntoAFileThatDoesNotExistYet is the same
// annotation with no document under it at all.
//
// A save into a path with no file at it is how the first one gets written, and
// it is the case that has no operator's tag to keep: everything the file says
// about these two addresses came from the schema.
func TestADeclaredNodeTagIsWrittenIntoAFileThatDoesNotExistYet(t *testing.T) {
	path := write(t, "")
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the plane: %v", err)
	}

	cfg := tagged{Wait: "30s", When: "2026-08-04T00:00:00Z", Port: 5432}

	if err := ferry.Dump(t.Context(), cfg, yaml.NewSink(path), declared()); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const want = "wait: !mycompany:duration 30s\nwhen: !!timestamp 2026-08-04T00:00:00Z\nport: 5432\n"

	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	back, err := ferry.Load[tagged](t.Context(), yaml.NewSource(path), declared())
	if err != nil {
		t.Fatalf("the load back: %v", err)
	}

	if back != cfg {
		t.Errorf("the load back gave %+v and the dump was given %+v", back, cfg)
	}
}

// TestTheDeclaredNodeTagBeatsTheOneInTheFile is the precedence rule, which is
// the question a driver table and a schema annotation naming one address would
// otherwise leave open.
//
// The schema wins. The tag in the file is kept only where nothing said what the
// address should be written under, and here something did.
func TestTheDeclaredNodeTagBeatsTheOneInTheFile(t *testing.T) {
	path := write(t, "wait: !someone:else 30s\nwhen: '2026-08-04T00:00:00Z'\nport: 5432\n")

	cfg, err := ferry.Load[tagged](t.Context(), yaml.NewSource(path), declared())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := ferry.Dump(t.Context(), cfg, yaml.NewSink(path), declared()); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "wait: !mycompany:duration 30s\n"; !strings.HasPrefix(got, want) {
		t.Errorf("the plane holds %q, want it to start with %q", got, want)
	}
}

// TestAnUndeclaredKeyIsAnotherLibrarysBusiness is core's inertness, seen from
// the driver: the same struct, saved through a call that names no registry.
//
// Nothing declares the key, so nothing reads it, and the save writes exactly
// what it wrote before this mechanism existed. That is what makes the word
// opt-in rather than a change to what every YAML file ferry writes means.
func TestAnUndeclaredKeyIsAnotherLibrarysBusiness(t *testing.T) {
	path := write(t, "wait: 30s\nwhen: '2026-08-04T00:00:00Z'\nport: 5432\n")

	cfg, err := ferry.Load[tagged](t.Context(), yaml.NewSource(path))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := ferry.Dump(t.Context(), cfg, yaml.NewSink(path)); err != nil {
		t.Fatalf("dump: %v", err)
	}

	const want = "wait: 30s\nwhen: \"2026-08-04T00:00:00Z\"\nport: 5432\n"

	if got := read(t, path); got != want {
		t.Errorf("the plane holds %q, want %q: a key no registry declared is nobody's business here", got, want)
	}
}

// TestANullUnderADeclaredNodeTagIsWrittenPlainly is the one value written
// without the tag rather than refused.
//
// There is nothing at a null for a node type to describe, and the address reads
// back null under either spelling, so an optional field that happens to be
// unset does not fail a save.
func TestANullUnderADeclaredNodeTagIsWrittenPlainly(t *testing.T) {
	type config struct {
		Wait *string `ferry:"wait" yamlext:"node=!mycompany:duration"`
	}

	path := write(t, "wait: 30s\n")

	if err := ferry.Dump(t.Context(), config{}, yaml.NewSink(path), declared()); err != nil {
		t.Fatalf("dump: %v", err)
	}

	if got, want := read(t, path), "wait: null\n"; got != want {
		t.Errorf("the plane holds %q, want %q", got, want)
	}

	back, err := ferry.Load[config](t.Context(), yaml.NewSource(path), declared())
	if err != nil {
		t.Fatalf("the load back: %v", err)
	}

	if back.Wait != nil {
		t.Errorf("the load back gave %q, want a nil pointer", *back.Wait)
	}
}

// TestANodeTagOverAValueOfAnotherKindIsRefused is the write-time half of the
// refusals, and the plane is left as it was.
//
// A scalar under a tag this plane has no arm for comes back as a string, so a
// number written under one would not survive the trip. Refusing is louder than
// dropping the tag, which would have left the field annotated and the file
// unchanged with nothing saying why.
func TestANodeTagOverAValueOfAnotherKindIsRefused(t *testing.T) {
	type config struct {
		Port int `ferry:"port" yamlext:"node=!mycompany:duration"`
	}

	const doc = "port: 5432\n"

	path := write(t, doc)

	err := ferry.Dump(t.Context(), config{Port: 8080}, yaml.NewSink(path), declared())
	if !errors.Is(err, ferry.ErrValue) {
		t.Fatalf("dump gave %v, want ferry.ErrValue", err)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: a save that failed leaves the file as it was", got, doc)
	}
}

// TestADeclarationThisDriverCannotHonourIsRefusedAtBind is every refusal a
// declaration alone carries, and each one fires before the file is opened.
//
// The address of a struct is on the list because a node tag names how one value
// is written, and the members of a section are elsewhere; the address of a map
// is on it for the same reason and one more, which is that a composite's
// members are minted from the value and no field of the type stands at them.
func TestADeclarationThisDriverCannotHonourIsRefusedAtBind(t *testing.T) {
	t.Run("no leading bang", func(t *testing.T) {
		type config struct {
			Wait string `ferry:"wait" yamlext:"node=mycompany:duration"`
		}

		refusesDeclaration(t, config{}, "does not start with")
	})

	t.Run("no type after the bang", func(t *testing.T) {
		type config struct {
			Wait string `ferry:"wait" yamlext:"node=!!"`
		}

		refusesDeclaration(t, config{}, "names no type")
	})

	t.Run("a space in the tag", func(t *testing.T) {
		type config struct {
			Wait string `ferry:"wait" yamlext:"node='!my duration'"`
		}

		refusesDeclaration(t, config{}, "holds a space")
	})

	t.Run("a tag this plane spells itself", func(t *testing.T) {
		type config struct {
			Wait string `ferry:"wait" yamlext:"node=!!int"`
		}

		refusesDeclaration(t, config{}, "spells itself")
	})

	t.Run("at a section", func(t *testing.T) {
		type inner struct {
			Wait string `ferry:"wait"`
		}

		type config struct {
			DB inner `ferry:"db" yamlext:"node=!mycompany:section"`
		}

		refusesDeclaration(t, config{}, "struct, a list or a map")
	})

	t.Run("at a composite", func(t *testing.T) {
		type config struct {
			Tags map[string]string `ferry:"tags" yamlext:"node=!mycompany:tags"`
		}

		refusesDeclaration(t, config{}, "struct, a list or a map")
	})
}

// refusesDeclaration dumps v and asserts the save was refused for what its
// declaration says, with the file untouched.
func refusesDeclaration[T any](t *testing.T, v T, want string) {
	t.Helper()

	const doc = "wait: 30s\n"

	path := write(t, doc)

	err := ferry.Dump(t.Context(), v, yaml.NewSink(path), declared())
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("dump gave %v, want ferry.ErrPlane", err)
	}

	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("the refusal reads %q, want it to hold %q", got, want)
	}

	if got := read(t, path); got != doc {
		t.Errorf("the plane holds %q, want %q: a declaration is refused before the file is opened", got, doc)
	}
}

// TestAWordOutsideTheDeclaredVocabularyIsRefused is the diagnostic the typed
// declaration buys, run over this driver's own vocabulary.
//
// It is core's refusal rather than this driver's: nothing here ever sees the
// word, because a tag that does not parse against the declaration never
// compiles into a schema.
func TestAWordOutsideTheDeclaredVocabularyIsRefused(t *testing.T) {
	type config struct {
		Wait string `ferry:"wait" yamlext:"nde=!mycompany:duration"`
	}

	path := write(t, "wait: 30s\n")

	_, err := ferry.Load[config](t.Context(), yaml.NewSource(path), declared())
	if !errors.Is(err, ferry.ErrSchema) {
		t.Fatalf("load gave %v, want ferry.ErrSchema", err)
	}

	if got, want := err.Error(), `did you mean "node"?`; !strings.Contains(got, want) {
		t.Errorf("the refusal reads %q, want it to hold %q", got, want)
	}
}
