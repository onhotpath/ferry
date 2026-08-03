package ferry

import (
	"errors"
	"testing"

	"github.com/onhotpath/ferry/internal/testdata/badtags"
)

// The grammar is asserted through Compile[T], the same as every other compiler
// rule. A name is observed through the address a second-tier refusal is located
// at, which is the only thing that could have named it: the compiler put the
// segment there, and a test that read the segment back out of the schema would
// be asserting against itself.

// nameCase is a tag whose name is the thing under test. Every one of them
// carries required and a default, which contradict, so the refusal lands at the
// address the name built.
type nameCase struct {
	name string
	run  func(...Option) error
	want Path
}

func TestGrammarNames(t *testing.T) {
	t.Parallel()

	cases := []nameCase{{
		name: "a bare name",
		run: Compile[struct {
			F string `ferry:"host,required,default=x"`
		}],
		want: At("host"),
	}, {
		name: "a quoted name holding the grammar's own separator",
		run: Compile[struct {
			F string `ferry:"'a,b',required,default=x"`
		}],
		want: At("a,b"),
	}, {
		name: "a quoted name holding a doubled quote",
		run: Compile[struct {
			F string `ferry:"'a''b',required,default=x"`
		}],
		want: At("a'b"),
	}, {
		name: "a quoted name that is exactly a dash",
		run: Compile[struct {
			F string `ferry:"'-',required,default=x"`
		}],
		want: At("-"),
	}, {
		name: "an apostrophe inside a bare name is just an apostrophe",
		run: Compile[struct {
			F string `ferry:"it's,required,default=x"`
		}],
		want: At("it's"),
	}, {
		name: "a quoted name holding an equals sign",
		run: Compile[struct {
			F string `ferry:"'a=b',required,default=x"`
		}],
		want: At("a=b"),
	}, {
		name: "a bare option word in the name position is a name",
		run: Compile[struct {
			F string `ferry:"required,required,default=x"`
		}],
		want: At("required"),
	}, {
		name: "a quoted name holding the address rendering's punctuation",
		run: Compile[struct {
			F string `ferry:"a/b,required,default=x"`
		}],
		want: At("a/b"),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.check(t)
		})
	}
}

// check reads the name back off the address the refusal is located at.
func (c nameCase) check(t *testing.T) {
	t.Helper()

	e, ok := errors.AsType[*Error](c.run())
	if !ok {
		t.Fatalf("want the contradiction at %s, got no ferry error", c.want)
	}

	if got := e.Address(); got != c.want {
		t.Errorf("named %s, want %s", got, c.want)
	}
}

// TestGrammarDefaults asserts the halves of a default that are observable
// without a plane: that it parses, and that omitzero beside it is a
// contradiction exactly when the text is not the zero value. The text itself is
// echoed by that refusal, which is what makes it readable here.
func TestGrammarDefaults(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "default splits on its first equals sign",
		run: Compile[struct {
			F string `ferry:"f,omitzero,default=a=b"`
		}],
		want:     []string{"omitzero and default=a=b contradict"},
		elements: 1,
	}, {
		name: "a quoted default holding a comma",
		run: Compile[struct {
			F string `ferry:"f,omitzero,default='Hello, world'"`
		}],
		want:     []string{"omitzero and default=Hello, world contradict"},
		elements: 1,
	}, {
		name: "a quoted default holding a doubled quote",
		run: Compile[struct {
			F string `ferry:"f,omitzero,default='it''s'"`
		}],
		want:     []string{"omitzero and default=it's contradict"},
		elements: 1,
	}, {
		name: "a bare default holding a tilde needs no quoting",
		run: Compile[struct {
			F string `ferry:"f,omitzero,default=~/.cache/app"`
		}],
		want:     []string{"omitzero and default=~/.cache/app contradict"},
		elements: 1,
	}})
}

func TestGrammarRefusals(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "an empty tag value is a name left out",
		run: Compile[struct {
			F string `ferry:""`
		}],
		want: []string{
			"/F: the name is empty",
			`every field must name the segment it addresses, or be marked ferry:"-"`,
		},
		elements: 1,
	}, {
		name: "an empty name with an option",
		run: Compile[struct {
			F string `ferry:",required"`
		}],
		want:     []string{"the name is empty"},
		elements: 1,
	}, {
		name: "a dash with an option names no segment",
		run:  Compile[badtags.DashOption],
		want: []string{
			`- names no segment`,
			`write ferry:"-" on its own to leave the field unmapped`,
			`or ferry:"'-',..." to name the segment -`,
		},
		elements: 1,
	}, {
		name: "an unterminated quoted name",
		run: Compile[struct {
			F string `ferry:"'a,b"`
		}],
		want: []string{
			`name "'a,b" is not terminated`,
			"a quoted name ends at a single quote, and a literal quote inside it is doubled",
		},
		elements: 1,
	}, {
		name: "an unterminated quoted value",
		run: Compile[struct {
			F string `ferry:"host,default='abc"`
		}],
		want:     []string{`value "'abc" is not terminated`},
		elements: 1,
	}, {
		name: "text after a closing quote",
		run: Compile[struct {
			F string `ferry:"host,default='abc'def"`
		}],
		want:     []string{`value "'abc'def" has text after the closing quote`},
		elements: 1,
	}, {
		name: "text after a closing quote in a name",
		run: Compile[struct {
			F string `ferry:"'abc'def"`
		}],
		want:     []string{`name "'abc'def" has text after the closing quote`},
		elements: 1,
	}, {
		name: "two commas with nothing between them",
		run: Compile[struct {
			F string `ferry:"host,,required"`
		}],
		want:     []string{"empty option: two commas with nothing between them"},
		elements: 1,
	}, {
		name: "a bare name may not contain an equals sign",
		run: Compile[struct {
			F string `ferry:"default=8080"`
		}],
		want: []string{
			`a name may not contain "="`,
			`"default=8080" looks like the default option with no name in front of it`,
			`write ferry:"<name>,default=8080"`,
		},
		elements: 1,
	}, {
		name: "and the remedy drops a value the option does not take",
		run: Compile[struct {
			F string `ferry:"required=yes"`
		}],
		want:     []string{`write ferry:"<name>,required"`},
		elements: 1,
	}, {
		name: "a bare name that is not an option word either",
		run: Compile[struct {
			F string `ferry:"a=b"`
		}],
		want:     []string{`a name may not contain "=": write ferry:"'a=b'" if the segment really is called that`},
		elements: 1,
	}, {
		name: "the xload migration mistake reports as two loud errors",
		run: Compile[struct {
			F string `ferry:",prefix=DB_"`
		}],
		want:     []string{"the name is empty", `unknown option "prefix"`},
		elements: 2,
	}, {
		name: "an option that takes no value",
		run: Compile[struct {
			F string `ferry:"f,required=yes"`
		}],
		want:     []string{`option "required" takes no value`},
		elements: 1,
	}, {
		name: "an option given twice",
		run: Compile[struct {
			F string `ferry:"f,omitzero,omitzero"`
		}],
		want:     []string{`option "omitzero" is given twice`},
		elements: 1,
	}, {
		name: "a default given twice",
		run: Compile[struct {
			F string `ferry:"f,default=a,default=b"`
		}],
		want:     []string{`option "default" is given twice`},
		elements: 1,
	}, {
		name: "a default with no value at all",
		run: Compile[struct {
			F string `ferry:"f,default"`
		}],
		want: []string{
			`option "default" needs a value`,
			"default= on its own is the empty string",
		},
		elements: 1,
	}, {
		name: "a quoted option is not a word",
		run: Compile[struct {
			F string `ferry:"f,'required'"`
		}],
		want:     []string{`option "'required'" is quoted`, vocabulary},
		elements: 1,
	}, {
		name: "every fault in one tag is reported",
		run: Compile[struct {
			F string `ferry:"f,requird,omitempty"`
		}],
		want:     []string{`unknown option "requird"`, `unknown option "omitempty"`},
		elements: 2,
	}})
}

// TestGrammarWhitespace is its own diagnosis rather than an unknown option,
// because ferry does not trim and `h, required` is a mistake a reader's eye
// slides over.
func TestGrammarWhitespace(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name: "leading",
		run: Compile[struct {
			F string `ferry:"h, required"`
		}],
		want: []string{
			`option " required" has surrounding whitespace`,
			`ferry does not trim, so "required" and " required" are different words`,
		},
		elements: 1,
	}, {
		name: "trailing",
		run: Compile[struct {
			F string `ferry:"h,required "`
		}],
		want:     []string{`option "required " has surrounding whitespace`},
		elements: 1,
	}, {
		name: "around the option word of a default",
		run: Compile[struct {
			F string `ferry:"h,default = x"`
		}],
		want:     []string{`option "default = x" has surrounding whitespace`},
		elements: 1,
	}, {
		name: "a tab is whitespace too",
		run: Compile[struct {
			F string `ferry:"h,	required"`
		}],
		want:     []string{"has surrounding whitespace"},
		elements: 1,
	}})
}

// TestGrammarWhitespaceInAValue is the other side of the same rule: a token is
// the user's text, so whitespace inside a default is not a mistake and ferry
// does not guess at one.
func TestGrammarWhitespaceInAValue(t *testing.T) {
	t.Parallel()

	if err := Compile[struct {
		F string `ferry:"h,default= x "`
	}](); err != nil {
		t.Fatalf("whitespace in a default is the user's text: %+v", err)
	}
}

func TestGrammarNearMisses(t *testing.T) {
	t.Parallel()

	// The four misspellings ADR-0008 names, none of which json/v2's
	// normalisation would catch, and the words from the neighbourhood that get
	// a sentence rather than a bare refusal.
	misses := []struct {
		option string
		run    func(...Option) error
		want   string
	}{
		{"requird", Compile[struct {
			F string `ferry:"f,requird"`
		}], `did you mean "required"?`},
		{"reqired", Compile[struct {
			F string `ferry:"f,reqired"`
		}], `did you mean "required"?`},

		{"omitzeroo", Compile[struct {
			F string `ferry:"f,omitzeroo"`
		}], `did you mean "omitzero"?`},
		{"Omit_Zero", Compile[struct {
			F string `ferry:"f,Omit_Zero"`
		}], `did you mean "omitzero"?`},
		{"omitempty", Compile[struct {
			F string `ferry:"f,omitempty"`
		}], "ferry has no omitempty; its omission option is omitzero"},
		{"inline", Compile[struct {
			F string `ferry:"f,inline"`
		}], "an embedded field with no tag is already promoted to the parent address"},
		{"squash", Compile[struct {
			F string `ferry:"f,squash"`
		}], "ferry has no squash"},
		{"embed", Compile[struct {
			F string `ferry:"f,embed"`
		}], "ferry has no embed"},
		{"prefix", Compile[struct {
			F string `ferry:"f,prefix=DB_"`
		}], "a nested struct's own name is the prefix"},
		{"delimiter", Compile[struct {
			F string `ferry:"f,delimiter=','"`
		}], "there is nothing to delimit"},
		{"separator", Compile[struct {
			F string `ferry:"f,separator=;"`
		}], "there is nothing to separate"},
		{"case", Compile[struct {
			F string `ferry:"f,case=ignore"`
		}], "core never folds"},
		{"string", Compile[struct {
			F string `ferry:"f,string"`
		}], "a plane's own kind assertion is respected"},
		{"format", Compile[struct {
			F string `ferry:"f,format=RFC3339"`
		}], "a per-field layout is a representation ferry has no row for"},
		{"nodump", Compile[struct {
			F string `ferry:"f,nodump"`
		}], "a field ferry loads and never writes cannot round-trip"},
		{"readonly", Compile[struct {
			F string `ferry:"f,readonly"`
		}], "keeping a secret off a plane is two structs rather than one option"},
		{"codec", Compile[struct {
			F string `ferry:"f,codec=json"`
		}], "a per-field override would be a second selection authority"},
		{"xyzzy", Compile[struct {
			F string `ferry:"f,xyzzy"`
		}], vocabulary},
		{"req", Compile[struct {
			F string `ferry:"f,req"`
		}], vocabulary},
	}

	for _, m := range misses {
		t.Run(m.option, func(t *testing.T) {
			t.Parallel()

			run(t, []compileCase{{
				name:     m.option,
				run:      m.run,
				want:     []string{`unknown option "` + m.option + `"`, m.want},
				elements: 1,
			}})
		})
	}
}

// TestGrammarTransposedDefault is the other two of ADR-0008's four
// misspellings. They live in testdata because they are misspellings a spell
// checker knows, and their subtest names cannot spell what their tags do.
func TestGrammarTransposedDefault(t *testing.T) {
	t.Parallel()

	run(t, []compileCase{{
		name:     "one way round",
		run:      Compile[badtags.Transposed],
		want:     []string{"unknown option", `did you mean "default"?`},
		elements: 1,
	}, {
		name:     "the other",
		run:      Compile[badtags.TransposedAgain],
		want:     []string{"unknown option", `did you mean "default"?`},
		elements: 1,
	}})
}

// TestGrammarCollisionNote fires only where the configured key is one another
// mapper is known to own, which is the difference between a refusal a user can
// act on and one that reads as a bug.
func TestGrammarCollisionNote(t *testing.T) {
	t.Parallel()

	const note = `(the ferry tag key is set to "json", which json also uses; ` +
		`ferry validates its own grammar under whatever key it is told to read)`

	run(t, []compileCase{{
		name:     "pointing the key at json",
		run:      Compile[jsonTagged],
		opts:     []Option{TagKey("json")},
		want:     []string{`unknown option "omitempty"`, note},
		elements: 1,
	}, {
		name:     "and not at a key nobody else owns",
		run:      Compile[jsonTagged],
		opts:     []Option{TagKey("mylib")},
		want:     []string{"carries no mylib tag"},
		elements: 1,
	}})
}

type jsonTagged struct {
	Host string `json:"host,omitempty"`
}
