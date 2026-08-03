package ferry

import (
	"errors"
	"testing"
)

// TestTagKeyReadsFerrysGrammarUnderAnotherKey is the case the Option exists
// for: a library built on ferry, whose users write that library's tag. The key
// names where to look and never what the content means, so nothing about the
// grammar or the strictness travels with the word "ferry".
func TestTagKeyReadsFerrysGrammarUnderAnotherKey(t *testing.T) {
	t.Parallel()

	if err := Compile[mylib](TagKey("mylib")); err != nil {
		t.Fatalf("ferry's grammar under another key: %+v", err)
	}

	// The same type under the default key, which is the measurement that makes
	// the Option compile-affecting: one reflect.Type, two keys, two answers.
	if err := Compile[mylib](); err == nil {
		t.Fatal("the same type compiled under the default key, so the key changes nothing")
	}
}

// TestTagKeyKeepsTheVocabularyShut is #34's parked question given no silent
// answer: the tag key is an Option and the vocabulary is not, so a word from
// the library that owns the key is still a schema compile error.
func TestTagKeyKeepsTheVocabularyShut(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "an extension option under another key",
		run: Compile[struct {
			Host string `mylib:"host,retry=3"`
		}],
		opts:     []Option{TagKey("mylib")},
		want:     []string{`unknown option "retry"`},
		elements: 1,
	}})
}

// TestTagKeyIsCheckedWhereItIsSupplied holds the key to what a struct tag key
// can be. The refusal is about the call site rather than about the type, so it
// carries no location and it fires whatever type is named.
func TestTagKeyIsCheckedWhereItIsSupplied(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "empty",
		run:      Compile[everything],
		opts:     []Option{TagKey("")},
		want:     []string{"the tag key may not be empty"},
		elements: 1,
	}, {
		name:     "a space",
		run:      Compile[everything],
		opts:     []Option{TagKey("my lib")},
		want:     []string{`the tag key "my lib" contains " ", which cannot appear in a struct tag key`},
		elements: 1,
	}, {
		name:     "a double quote",
		run:      Compile[everything],
		opts:     []Option{TagKey(`my"lib`)},
		want:     []string{`contains "\"", which cannot appear in a struct tag key`},
		elements: 1,
	}, {
		name:     "a colon",
		run:      Compile[everything],
		opts:     []Option{TagKey("my:lib")},
		want:     []string{`contains ":", which cannot appear in a struct tag key`},
		elements: 1,
	}, {
		name:     "a delete byte",
		run:      Compile[everything],
		opts:     []Option{TagKey("my\x7flib")},
		want:     []string{"cannot appear in a struct tag key"},
		elements: 1,
	}, {
		name:     "a list, spelled as one key",
		run:      Compile[everything],
		opts:     []Option{TagKey("ferry,mylib")},
		want:     []string{`the tag key "ferry,mylib" contains a comma`, "ferry reads exactly one"},
		elements: 1,
	}, {
		name: "a list, spelled as two Options",
		run:  Compile[everything],
		opts: []Option{TagKey("mylib"), TagKey("other")},
		want: []string{
			`the tag key is given twice, as "mylib" and "other"`,
			"a list of keys is a precedence ladder nothing in the tag chooses between",
		},
		elements: 1,
	}, {
		name:     "every wrong Option is reported",
		run:      Compile[everything],
		opts:     []Option{TagKey(""), TagKey("my lib")},
		want:     []string{"may not be empty", `"my lib" contains`},
		elements: 2,
	}})
}

// TestTagKeyRefusalPrecedesTheCompile is the placement rule with a
// consequence: a key that could never be read is not a reason to report every
// field of the type as untagged.
func TestTagKeyRefusalPrecedesTheCompile(t *testing.T) {
	t.Parallel()

	err := Compile[mylib](TagKey("my lib"))
	if n := len(Elements(err)); n != 1 {
		t.Fatalf("reported %d elements, want the Option alone: %+v", n, err)
	}
}

// TestTagKeyAcceptsWhatAStructTagCanHold is the accepting half, which is what
// stops the check above from being a check on the word "ferry".
func TestTagKeyAcceptsWhatAStructTagCanHold(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"ferry", "mylib", "my-lib", "my_lib", "MyLib", "l1", "föö"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			if err := checkTagKeyThrough(key); err != nil {
				t.Errorf("refused %q: %v", key, err)
			}
		})
	}
}

// checkTagKeyThrough asks the question through the entry point: a type carrying
// no tag under any key reports the field rule and nothing about the Option, so
// an Option refusal is the one thing that could arrive instead.
func checkTagKeyThrough(key string) error {
	for _, e := range Elements(Compile[untagged](TagKey(key))) {
		if err, ok := errors.AsType[*Error](e); ok && err.Address() == (Path{}) {
			return err
		}
	}

	return nil
}
