# 8. The struct tag grammar

Status: Proposed
Date: 2026-08-02
Ticket: [#11](https://github.com/onhotpath/ferry/issues/11)

## Context

ferry is one annotated struct driving two directions.
This ADR is the annotation.

It is the ticket every other ADR deferred a spelling to, so its scope is the union of six deferrals plus its own question, and the first job is to enumerate that union rather than answer the ticket body alone.

[ADR-0001](0001-what-ferry-supports.md) put tag validation **In core** and made it strict: unrecognised or malformed tag content fails schema compilation.
It left "what counts as malformed, near-miss suggestions, and where option contradictions sit" here by name.
Its consequence that ferry's tag vocabulary is frozen once published is the constraint that shapes every call below.

[ADR-0003](0003-how-a-leaf-addresses-a-plane.md) left four things here: how a tag names a segment, how a segment containing the grammar's own punctuation is written, whether prefix and squash exist, and whether a per-field case option exists.
It also flagged that "configurable" and "strict" are compatible only with a stated answer for what happens when the tag key is pointed at a tag somebody else owns.

[ADR-0005](0005-the-supported-type-set.md) left how a field is named, whether a composite's element naming is configurable, whether embedding is spelled `embed`, and the spelling of omission, whose semantics are already fixed as `omitzero` in Go terms.

[ADR-0006](0006-defaults-and-zero-values.md) has a section titled "What this hands #11".
Three options to name, five schema-compile refusals, one diagnostic rule, and one grammar property: an empty default must be expressible and distinguishable from no default.

[ADR-0007](0007-the-codec-chain-and-its-precedence.md) fixed the composed order, so this ADR's tag decides omission, #8's rule decides the value, and the chain converts what survives.
It also left a documentation obligation: `default=aGk=` on a `[]byte` field lands as the four bytes `aGk=`.

[ADR-0002](0002-core-and-sub-modules.md) makes v1 conditional on this grammar surviving real use, so this is the ticket that gates the version number.

The inherited answer is xload's, whose whole vocabulary is `prefix`, `delimiter`, `separator` and `required` at [load.go:219-249](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L219-L249).
A clean break from it is decided, so it is a comparison and not a starting point.

This ADR is written from a throwaway prototype on branch `proto/11-tag-grammar`, which never merges.
It is built on `proto/8-defaults`, so every measurement runs against a real `Path`, a real `Value`, the real type set, #8's real compiled schema, and the real YAML driver over real files.
Thirty-two probes across two rounds.
**Seven overturned an answer this ADR had already reached in draft**, and three of the seven came from one audit round aimed at the case the earlier fixtures did not contain.
One of the seven was a probe that passed while measuring nothing.

## Decision

### What this closes, and what it does not

The ticket asked for four things by name.

| The ticket asked | Closed | Where |
| --- | --- | --- |
| which options are load-only, dump-only, and shared | **yes**, one table | [Direction is a property of the option](#direction-is-a-property-of-the-option-and-the-grammar-does-not-spell-it) |
| how direction is expressed without the grammar becoming confusing | **yes: it is not expressed** | as above |
| whether the tag name stays configurable, as xload's `FieldTagName` allows | **yes: no**, and the reason is strictness rather than cost | [The tag key is `ferry`](#the-tag-key-is-ferry-and-it-is-not-configurable) |
| how prefixing and nesting express themselves under ADR-0003's address model | **yes**: nesting is the mechanism and `prefix` does not exist | [What is not in the vocabulary](#what-is-not-in-the-vocabulary-and-why-each-is-a-refusal-rather-than-an-omission) |

Its two comments asked for two more, and both are this ticket's outright.

| The comments asked | Closed | Where |
| --- | --- | --- |
| a validation entry point a user can call from a test | **yes**, `Validate[T]()`, which is schema compile | [The validation entry point is the compiler](#the-validation-entry-point-is-the-compiler-not-a-second-one) |
| near-miss rejection in json/v2's style | **yes**, and wider than v2's | [Diagnostics](#diagnostics-three-tiers-and-the-vocabulary-of-the-neighbourhood) |

Six ADRs deferred a spelling here.
This is the union, so a reader can check it rather than rediscover it.

| Deferred by | The ask | Where it is answered |
| --- | --- | --- |
| ADR-0001 | what counts as malformed | [What a struct tag can carry](#what-a-struct-tag-can-carry-and-what-it-cannot) |
| ADR-0001 | near-miss suggestions | [Diagnostics](#diagnostics-three-tiers-and-the-vocabulary-of-the-neighbourhood) |
| ADR-0001 | where option contradictions sit | as above, tier three |
| ADR-0003 | how a tag names a segment | [The grammar](#the-grammar) |
| ADR-0003 | how a segment containing the grammar's punctuation is written | [The escape is `~`](#the-escape-is--and-it-follows-the-character) |
| ADR-0003 | whether prefix and squash exist | **neither** |
| ADR-0003 | whether a per-field case option exists | **no** |
| ADR-0003 | whether the struct tag key is configurable | **no** |
| ADR-0005 | how a field is named | [The name is mandatory](#the-name-is-mandatory-and-a-go-field-name-is-not-a-default) |
| ADR-0005 | whether a composite's element naming is configurable | **no**, and there is nothing for a tag to configure |
| ADR-0005 | whether embedding is spelled `embed` | **no word is spent** |
| ADR-0005 | the spelling of omission | `omitzero` |
| ADR-0006 | names for a default, a required marker, and omission | `default=`, `required`, `omitzero` |
| ADR-0006 | the five schema-compile refusals | enforced, re-run through this grammar |
| ADR-0006 | admissibility before contradictions | enforced, and it gains a tier |
| ADR-0006 | an empty default, distinguishable from no default | `default=` against the option's absence |
| ADR-0007 | how omission and defaults are spelled | as above |
| ADR-0007 | documenting `default=aGk=` on `[]byte` | [What a declared default is not](#what-a-declared-default-is-not) |

Seven questions this ADR had to answer that nobody asked.

| Not asked for, answered anyway | Where |
| --- | --- |
| whether ferry may use `reflect.StructTag.Get` at all | [ferry parses the raw struct tag itself](#ferry-parses-the-raw-struct-tag-itself) |
| what an embedded field does, given that a name is mandatory | [`-` skips, and embedding needs no word](#--skips-and-embedding-needs-no-word) |
| whether a tag option may name a codec or a format | [What is not in the vocabulary](#what-is-not-in-the-vocabulary-and-why-each-is-a-refusal-rather-than-an-omission) |
| what a diagnostic tier above ADR-0006's two looks like | [Diagnostics](#diagnostics-three-tiers-and-the-vocabulary-of-the-neighbourhood) |
| what the grammar's own blind spot is | [The one mistake the grammar cannot see](#the-one-mistake-the-grammar-cannot-see) |
| whether the escape rule the address rendering uses can serve both | [The escape is `~`](#the-escape-is--and-it-follows-the-character) |
| whether the walk and the schema compiler may have separate field rules | [`-` skips, and embedding needs no word](#--skips-and-embedding-needs-no-word) |

**Three things this ADR does not close.**

- **A bare option word in the name position is a segment name and ferry cannot know otherwise.**
  Stated as the grammar's one blind spot, with the reason it is not closable.
- **An empty segment name is unwritable from a tag**, deliberately, though the address model permits one.
- **The migration guide from xload** is named in [#1](https://github.com/onhotpath/ferry/issues/1) as unspecified until this grammar exists.
  It exists now, and the guide is still not written.

### The grammar

```
tag      =  name *( "," option )  /  "-"

name     =  1*( plain / escaped )
option   =  "required"  /  "omitzero"  /  "default" "=" text
text     =  *( plain / escaped )

escaped  =  "~" ( "~" / "," / "=" / "-" )
plain    =  any byte except "," and "~"
```

Four words, and one of them is punctuation.
`-` as the whole value means the field is not mapped; `required` and `default=` are Load-side; `omitzero` is Dump-side; the name is shared.

`default=` splits on its first `=`, so `default=a=b` is the text `a=b` and needs no escape.
The `=` escape exists for the name, which is not split on `=` at all and where an unescaped one is refused.

The field rule, which is the other half of the grammar and is where most of the argument is:

| the field | what ferry does |
| --- | --- |
| unexported | skipped, per ADR-0005 |
| `ferry:"-"` | skipped |
| exported and named, with a ferry tag | mapped at the segment the tag names |
| exported and named, **with no ferry tag** | **schema compile error** |
| embedded, with no ferry tag | its fields are promoted to the parent address |
| embedded, with a ferry tag | nested under that name |
| embedded **pointer**, with no ferry tag | **schema compile error**, see below |

### What a struct tag can carry, and what it cannot

This section is first because the grammar has to live inside it, and because what it measures is stronger than the one instance ADR-0006 recorded.

ADR-0006 measured that `reflect.StructTag.Get` truncates `ferry:"origins,default=["value"]"` at the embedded quote and returns `origins,default=[`.
The general question is what else `Get` does silently, and the answer is worse than one case.

Measured, each raw tag written as it would appear between backquotes in a Go source file:

| raw ferry tag | `Lookup("ferry")` | the `json` and `yaml` tags on the same field |
| --- | --- | --- |
| `ferry:"host,required"` | `host,required`, ok | intact |
| `ferry:"origins,default=["value"]"` | `origins,default=[`, ok | **both destroyed** |
| `ferry:"a\,b"` | `""`, **not ok** | intact |
| `ferry:"a\"` | `a\" json:`, ok | **both destroyed** |
| `ferry:"a\\,b"` | `a\,b`, ok | intact |
| `ferry:"first" ferry:"second"` | `first`, ok | intact |

Three separate failure modes, and none of them is visible at run time.

**A bare double quote destroys every other library's tag on the field, not only ferry's.**
That is a materially stronger statement than ADR-0006's, and it is why the grammar may not use `"` for anything.

**An invalid Go escape makes the ferry tag invisible rather than wrong.**
A struct tag's value is unquoted by `strconv.Unquote`, so `\,` is not an escape Go defines, `Unquote` fails, and `Lookup` reports `ok=false`.
The tag is then indistinguishable from a field that carries no ferry tag at all.
**So the grammar may not require any character sequence that is not a valid Go string escape**, which rules out backslash escaping unless spelled `\\`, and rules out any quoting scheme whose inner escape is `\'`.

**A field may carry two ferry tags, and `Get` returns the first.**

`go vet` catches two of the three, and `go test` catches none.
Verified on this prototype: `go vet ./...` reports `not compatible with reflect.StructTag.Get` for the bare quote and for the invalid escape, and says nothing about the duplicate key; `go test ./...` passes clean with all three present, because `structtag` is not in the analyzer subset `go test` runs.
That is ADR-0001's vet-gap claim confirmed for a `ferry` tag specifically, and extended: the gap is not only that vet is not run, it is that vet does not cover the whole class even when it is.

### ferry parses the raw struct tag itself

> Core does not call `reflect.StructTag.Get` or `Lookup`.
> It scans `reflect.StructField.Tag` with its own parser and reports what `Get` answers with a silent empty string.

This costs about forty lines, and it is the difference between the three rows above being diagnosable and being invisible.
Measured, the same six tags through ferry's scanner:

```
ferry:"host,required"                value="host,required"
ferry:"origins,default=["value"]"    struct tag is not in the conventional `key:"value"` form, at "value\"]\"";
                                     the usual cause is a bare double quote inside a ferry tag, which a struct
                                     tag value cannot contain
ferry:"a\,b"                         ferry tag value "a\,b" is not a valid Go quoted string (invalid syntax);
                                     a struct tag value is unquoted by strconv.Unquote, so it may not contain
                                     a bare double quote and may not contain an escape Go does not define
ferry:"a\"                           struct tag key "ferry" has an unterminated quoted value
ferry:"a\\,b"                        value="a\,b"
ferry:"first" ferry:"second"         the field carries two ferry tags, "first" and "second";
                                     reflect.StructTag.Get returns the first and go vet does not check it
json:"host"                          (no ferry key)
```

The scanning loop is `Lookup`'s own, with the error paths kept rather than collapsed into `break`.
A field that genuinely carries no ferry tag is distinguished from one whose tag could not be read, which `Lookup` cannot do.

**The refusal is scoped, because a malformed tag is not always ferry's.**
ferry refuses a struct tag that does not parse only when the text `ferry:"` occurs in it.
A field whose `json` tag is malformed and whose ferry tag was read cleanly is `go vet`'s problem and not ferry's.

### The name is mandatory, and a Go field name is not a default

> An exported, named struct field with no ferry tag is a schema compile error.
> ferry never invents a segment name.

This is the largest call in the ADR and the one with a number behind it.

**Measured, over a corpus of 10,012 non-generated third-party Go files and the whole `go1.27rc2` standard library**, counting every exported struct field that carries a name under `json`, `yaml`, `toml`, `mapstructure`, `env`, `envconfig` or `xml`:

| corpus | named fields | name **is** the Go field name | differs only by case | differs otherwise |
| --- | --- | --- | --- | --- |
| third party | 4835 | 233, **4.8%** | 1942 | 2660 |
| `go1.27rc2` stdlib | 556 | 38, **6.8%** | 387 | 131 |

Broken out by tag key, the two keys whose planes look most like ferry's are the sharpest:

```
yaml            1 of 1580
mapstructure    0 of  808
json          216 of 2320
```

So a Go-field-name default is byte-exactly what the author wanted about one time in twenty.
And ADR-0003 already decided that **core never folds**, so the large "differs only by case" group is not rescued by ferry either: `Host` would be the segment, not `host`.

The population is fields somebody already named, which is stated rather than hidden: fields nobody named are invisible to this measurement.
What it does measure is the right question, which is whether the Go identifier is the name a Go programmer writes for a plane.

**Two arguments beyond the count, and the second is the one that decides it.**

**A silently invented name is a representation nobody chose.**
ADR-0005 named that failure class as its category 3 and accepted it reluctantly for types admitted by kind, and ADR-0007 spent a section shrinking it.
Manufacturing one for every untagged field would grow it back at the layer ferry controls completely.

**Under an explicit name, exporting a field cannot change the plane.**
Measured, the same struct after somebody adds an exported field for an unrelated reason:

```
Go-name default    before  /HTTPPort /Host /TLS
                   after   /Debug /HTTPPort /Host /TLS        the plane gained an address
explicit name      after   ferry: /Debug: field Debug carries no ferry tag: every exported field
                           must name the segment it addresses, or be marked ferry:"-"
```

That is exactly the property ADR-0007 wanted and could not have.
ADR-0007 argued its chain order partly on the ground that "under after-kind, exporting a field silently rewrites the type's plane representation", and had to settle for the weaker claim that both orders drift.
Here the property is available outright, and it is [#28](https://github.com/onhotpath/ferry/issues/28)'s breaking change made unrepresentable rather than merely detected.

**And refusing is the reversible direction**, which is ADR-0006's rule applied to a naming question.
A struct that compiles today still compiles if ferry ever adds a default, because every field already names itself.
A struct that is refused today has nobody depending on the refusal.
The reverse move, defaulting now and requiring later, breaks compiling code.

**The cost is stated plainly: a thirty-field config struct carries thirty tags.**
There is no worked case where that is wrong rather than merely verbose, and the verbosity is mechanical.
The mitigation is the diagnosis, which names the field and gives both remedies.

### The escape is `~`, and it follows the character

ADR-0003's consequences state the obligation:

> The tag grammar inherits a constraint it must now satisfy: a tag has to be able to name a segment whose text contains whatever punctuation the grammar itself uses.

And survey item 5.10's second half is the cautionary case: xload's `parseField` splits the tag on `,` at [load.go:219](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L219), so `env:"K,delimiter=,"` cannot be written at all.
Reproduced: it parses to key `K` and options `["delimiter=", ""]`, and the delimiter is now the empty string.

> `~` introduces an escape.
> `~x` yields `x` for `x` in the grammar's own punctuation, `~ , = -`.
> Anything else after `~` is an error.

Three models were implemented and measured against the same two inputs, rather than argued:

| model | the name `a,b` | a stray comma, `host,,required` |
| --- | --- | --- |
| escape character, `a~,b` | `name="a,b"` | refused: empty option |
| doubling, `a,,b` | `name="a,b"` | **`name="host,required"`, no options, silently** |
| no escaping | unwritable | refused: empty option |

Doubling is the model that needs no escape character and it loses on the row that matters: a typo becomes a wrongly-named address with no diagnostic, which is ADR-0001's prohibition on silently ignoring anything, arriving as silently renaming instead.

**`encoding/json/v2` has this problem too, and Go 1.27 does not solve it**, which was measured rather than assumed after its source suggested otherwise.

v2's `consumeTagOption` carries a single-quoted-string grammar and a comment that states ferry's own constraint independently:

> "The grammar is nearly identical to a double-quoted Go string literal, but uses single quotes as the terminators.
> The reason for a custom grammar is because both backtick and double quotes cannot be used verbatim in a struct tag."
> ([go1.27rc2 `src/encoding/json/v2/fields.go`](https://cs.opensource.google/go/go/+/refs/tags/go1.27rc2:src/encoding/json/v2/fields.go))

Measured on `go1.27rc2`, that machinery is not reachable from the name position:

```
json:"a,b"              marshals as {"a":"v"}          the name is truncated at the comma, silently
json:"'a,b'"            malformed `json` tag: invalid character '\'' at start of option
json:"t,format:'...'"   Go struct field T has unsupported `format` tag option
```

The name is consumed with `allowQuoted=false`, so a quoted name is refused; the only caller that passes `allowQuoted=true` is `format:`, which 1.27 removed pending typed struct tags and which now errors.
So **there is no working quoted-token path in json/v2 today**, and a JSON member name containing a comma is unexpressible in the standard library's newest tag grammar.
There was nothing to copy.

**And the single-quote model would have been the wrong thing to copy anyway**, for a reason the section above already measured.
A `'` inside a single-quoted name is written `\'`, which a struct tag value spells `\\'` because `Unquote` runs first.
Measured:

```
ferry:"'it\\'s'"    Lookup -> "'it\'s'"  ok=true                the correct spelling
ferry:"'it\'s'"     Lookup -> ""         ok=false               one backslash short, and the tag is gone
```

`~` needs no backslash at any depth, so no spelling of it can vanish.

**One escape rule serves the tag and the address rendering**, which is why it is `~` and not something new.
ADR-0003 took RFC 6901's escaping model on the ground that escaping a separator and an escape character is a solved problem, and left the exact byte spelling to the implementation.
Re-running ADR-0003's own property under the follow-the-character rule, over 200,000 fuzzed paths with segment text drawn from `a b / # ~ ~0 ~1 ~2 , = -` plus the empty string, an embedded NUL, non-ASCII and a space: **0 round-trip failures and 0 distinct segment lists sharing a rendering.**
The prototype's `~0 ~1 ~2` spelling satisfies ADR-0003 equally; what the follow-the-character rule buys is that a reader learns one rule instead of two.
That is a recommendation to the implementation and not an amendment to ADR-0003, which never fixed the bytes.

**Measured end to end through the real YAML driver**, a struct whose every segment contains the grammar's own punctuation:

```
tag                        address        segment      the YAML the driver wrote
ferry:"a~,b"               /a,b           "a,b"        a,b: "c"
ferry:"a~=b"               /a=b           "a=b"        a=b: "e"
ferry:"a~~b"               /a~0b          "a~b"        a~b: "t"
ferry:"~-"                 /-             "-"          '-': "d"
ferry:"a/b"                /a~1b          "a/b"        a/b: "s"
ferry:"a#b"                /a~2b          "a#b"        a#b: "h"
ferry:"a b"                /a b           "a b"        a b: "sp"
ferry:"greet,default=Hello~, world"                    greet: ""
```

All eight load back exactly, and with `/greet` deleted from the document the declared default arrives as `Hello, world`.
`/` and `#` need no escape in a **tag**, because they are the address rendering's punctuation and not the grammar's; the two alphabets are disjoint and the rule is shared.

**One thing is deliberately unwritable: an empty segment name.**
`ferry:""` is refused, because the empty tag value has to mean something and "you left the name out" is the more useful reading than "the segment is called nothing".
ADR-0003's address model permits empty segment text, and a driver would refuse it anyway: an empty segment has no environment variable name.

### Direction is a property of the option, and the grammar does not spell it

> Every option has exactly one honest direction, so the grammar spends no syntax on saying which.

| | Load | Dump | why |
| --- | --- | --- | --- |
| the name | yes | yes | one address, or the round trip is not one |
| `-` | yes | yes | as above |
| `required` | yes | no | it asserts the plane spoke; on Dump ferry is the one speaking |
| `default=` | yes | no | it answers an absence Dump never observes |
| `omitzero` | no | yes | it decides whether an address is written |

Measured rather than reasoned, in both directions:

```
dumping a struct whose fields carry `required` and `default=x`
  -> 2 Set calls, /a=string("") /b=string(""),  neither option changed anything

loading a struct whose field carries `omitzero`, from a plane holding both
  -> {A:x B:y},                                 omitzero changed nothing
dumping the same struct at its zero value
  -> 1 Set call,  /b=string("")
```

**A per-direction name was the one candidate that would have needed syntax, and it is a round-trip violation by construction.**
The ask is real: load from `LEGACY_HOST`, dump to `host`.
On one plane that means Dump writes `/host` and Load reads `/legacy_host`, so `Load(Dump(x))` finds nothing, which is exactly the value fidelity ADR-0001 makes a hard guarantee.
The remedy is two structs, or a source that renames, which ADR-0004's combinators already admit.

The same argument disposes of `nodump` and `readonly`, which people will ask for to keep a secret off a plane: a field ferry loads and never writes cannot round-trip.
Both are in the near-miss table below with that sentence as their diagnosis.

**This is the asymmetry ADR-0001 named, applied.**
A word not spent stays available.
If a direction-scoped option ever earns its place, it arrives into a grammar that has not already committed a syntax for direction to the wrong shape.

### `-` skips, and embedding needs no word

`ferry:"-"`, as the whole tag value, means the field is not mapped in either direction.
A segment whose text is literally `-` is `ferry:"~-"`, measured above.
`ferry:"-,required"` is refused rather than read either way, with both remedies in the message.

`ferry:"-"` on an unexported field is redundant and accepted; any other tag on an unexported field is an error, because `reflect` cannot set it and the tag can never do anything.
That is `encoding/json/v2`'s reading of the same case.

**Embedding costs no vocabulary at all**, which is the point.

```
type Common struct { Name string `ferry:"name"`; Env string `ferry:"env"` }

struct{ Common;                  Port int `ferry:"port"` }   ->  /env /name /port
struct{ Common `ferry:"common"`; Port int `ferry:"port"` }   ->  /common/env /common/name /port
struct{ Common `ferry:"-"`;      Port int `ferry:"port"` }   ->  /port
```

An embedded field with no ferry tag is walked at the **parent** address, so its fields are promoted.
That is what Go's own field namespace already means by embedding, and it is why `embed`, `inline` and `squash` are all absent from the vocabulary.

`ferry:",required"` on an embedded field, meaning "promote and require", is refused for the same reason the pointer is: a promoted field has no address of its own for `required` to assert about.



**json/v2 needs the word because it also inlines a named field, and ferry refuses that.**
Verified on `go1.27rc2`:

```
anonymous field, no option      {"Name":"n","Env":"e","port":8080}
NAMED field, json:",embed"      {"Name":"n","Env":"e","port":8080}
NAMED field, json:",inline"     {"C":{"Name":"n","Env":"e"},"port":8080}
```

Two things fall out of that measurement.

**`inline` is a no-op in Go 1.27**, still, which is the ticket's own comment reproduced live: it is not in v2's option set, so it falls through the default arm and is ignored.
That is the roughly 29k-use Kubernetes silent misconfiguration, and it is the strongest single argument for the diagnostics section below.

**And the first version of this probe measured nothing.**
It put `,embed` and `,inline` on an *anonymous* field, where v2 promotes either way, so both rows agreed and the probe looked like a pass.
Only moving the option to a named field made the difference visible.
That is the handoff's own warning about a fixture where every case is well-formed, arriving in a different costume, and it is recorded because it nearly shipped an assertion.

The one hazard promotion introduces is a name clash, and it needs no new rule:

```
struct{ Common; Name string `ferry:"name"`; Port int `ferry:"port"` }
  ->  ferry: two fields address /name
```

That is ADR-0003's prefix-free rule, applied rather than extended.
An embedded field that is not a struct ferry walks is refused, naming both remedies.

**An embedded pointer may not be promoted, and this was found by auditing rather than by designing.**

The T9 fixtures above tested one level of embedding, on a value receiver, with an exported embedded type.
Every one of them passed.
Three cases outside that shape each turned out to be a silent loss.

*A promoted embedded pointer erases the pointer.*
Promotion walks the pointed-to struct **at the parent address**, so the pointer has no address subtree of its own, and ADR-0006's presence bit has nothing to materialise it from.
Measured before the refusal existed:

```
struct{ *Common; Port int `ferry:"port"` }   compiled clean to [/env /name /port]
load /name=string("n")   ->  the pointer is still nil, the value went nowhere, err=nil
dump one whose pointer is nil  ->  2 Set calls
```

A silent total loss, of exactly ADR-0005's maps-no-address class.
The refusal names the remedy, and nesting the pointer works normally: `ferry:"common"` gives `/common/env`, `/common/name`, `/port`, and the pointer is optional at its own address again.

*An embedded field whose type is unexported is promoted, not skipped.*
Go's own field namespace promotes it, and `reflect` can set through it.
Measured:

```
anonymous field "inner": IsExported=false Anonymous=true PkgPath="main"
the embedded value CanSet=false
its promoted exported field Name CanSet=true, and setting it works
```

So the rule is that an **anonymous** field is considered whether or not its own type is exported, which is the same test `encoding/json/v2` applies.
Skipping it would have dropped a mapped field in silence.

*And the walk must share the compiler's field rule.*
With the rule in the compiler alone, the schema for the case above promised `/name` and the walk never visited it, so a load returned a nil error and a zero field.
Two field rules is the two-conversion-authorities defect the survey measured in viper, moved one layer down.
Measured after the walk was made to share it, on a two-level promotion through a real dump and load: 4 addresses, round trip equal.

**A composite's element naming is not configurable, and there is nothing for a tag to configure.**
Under ADR-0003 a slice element mints an `Index` segment whose text is its position, and a map key mints a `Name` segment whose text comes from the key type's own representation, which ADR-0005 restricts and ADR-0007 extends.
A declaration inside a map value or a slice element attaches to the static address shape, `/servers/*/port`, which ADR-0006 decided and this ADR inherits unchanged.

### The tag key is `ferry`, and it is not configurable

ADR-0003 flagged that "configurable" and "strict" are compatible only with a stated answer for the case where the key names a tag somebody else owns.
Here is the answer, measured against a struct annotated for `json`:

```
json:"host,omitempty"   REFUSED  ferry has no omitempty; its omission option is `omitzero`...
json:"port,string"      REFUSED  ferry has no string option; a plane's own type information is respected...
json:"-"                ok       not mapped
json:"name"             ok       name="name"
```

Three of four fields refuse, and the fourth compiles only because `name` happens to carry no option.
So a configurable key is usable against a foreign tag **only if strictness is switched off for it**, which is ADR-0001's rule with an off switch, and ADR-0001 ruled out silently ignoring anything by architecture.

**Cost is not the argument, and it was measured so that it cannot be mistaken for one.**
A per-instance key would become part of [#16](https://github.com/onhotpath/ferry/issues/16)'s schema cache key, and unlike ADR-0006's compile-affecting Option, a string is hashable:

| cache key | ns/op | allocs/op |
| --- | --- | --- |
| `map[reflect.Type]` | 12.0 | 0 |
| `map[struct{reflect.Type; string}]` | 18.0 | 0 |

Six nanoseconds, against ADR-0006's measured `hash of unhashable type main.LoadOption` panic for the other kind of Option.
So the cache can afford it and strictness cannot, and the ADR says which of the two decided it.

**Two smaller reasons, neither load-bearing.**
Merovius' argument in [go.dev/issue/74472](https://go.dev/issue/74472) that tag keys are not namespaced, so two YAML packages both claim `yaml:`, is a reason to pick a distinctive key rather than a reason to make it configurable; `ferry` is distinctive.
And migration from xload's `env:` tag is a written guide, which [ADR-0001](0001-what-ferry-supports.md) already made the plan, rather than a compatibility switch in core.

**Refusing keeps the decision open**, on ADR-0006's rule: nothing depends on a key being fixed, so an Option can add one later, and #16 gets the simplest cache key in the meantime.

### What is not in the vocabulary, and why each is a refusal rather than an omission

Every word below was considered and left unspent.
ADR-0001 freezes the vocabulary on publication, so a word not spent stays available and a word spent is permanent, and that asymmetry decided this whole list.

| word | where it comes from | why ferry has none |
| --- | --- | --- |
| `prefix=` | xload | a nested struct's own name is the prefix, and a plane-wide prefix is ADR-0004's `Under` combinator on the source |
| `delimiter=`, `separator=` | xload | a composite gets one address per element, so there is nothing to delimit; how a driver joins segments is the driver's option |
| `inline`, `embed`, `squash` | json/v2, mapstructure | promotion is the default for an embedded field |
| `omitempty` | json | ADR-0005 rejected it: there is no empty JSON object on a Consul plane |
| `case:ignore` | json/v2 | ADR-0003: core never folds, and which characters fold is plane and locale knowledge |
| `string` | json | ADR-0005: a plane's own kind assertion is respected rather than overridden |
| `format:` | json/v2, removed in 1.27 | ADR-0005 pins `time.Time` to RFC 3339 in the golden column; a per-field layout is a representation the harness has no row for |
| a codec selector | this ADR's own question | ADR-0007 puts selection in the identity table and the text pair; a per-field override is a second selection authority |
| `nodump`, `readonly` | asked for, not inherited | a field ferry loads and never writes cannot round-trip |
| a per-direction name | the ticket | as above |
| a validation constraint | viper, validator | ADR-0001 ruled it out by architecture: the type is the validation |

**`prefix=` is worth its own paragraph, because it is the one the ticket asked about.**
xload's prefix is text concatenation onto a flat key, and ADR-0003 already recorded that all three of these are legal and two are typos nothing can detect:

```
prefix="DB_"  + key "HOST"  ->  "DB_HOST"
prefix="DB"   + key "HOST"  ->  "DBHOST"
prefix="DB__" + key "HOST"  ->  "DB__HOST"
```

Under a structured address there is no concatenation to get wrong.

```go
type App struct {
    DB   DBConf `ferry:"db"`
    Name string `ferry:"name"`
}
```

compiles to `/db/host`, `/db/port`, `/name`, and how those segments become a plane key is the driver's option with ADR-0003's injectivity check behind it.
The nested struct's tag **is** the prefix, which is why the word is not needed rather than not wanted.

### Diagnostics: three tiers, and the vocabulary of the neighbourhood

ADR-0006 required one rule: admissibility is checked before contradictions, or one field's single mistake reports as three errors.
Running the real grammar found a tier above both.

> 1. **Well-formedness.** The raw tag scans and the grammar parses.
> 2. **Admissibility.** Is each option legal at this field's type, on its own?
> 3. **Contradiction.** Do two options that both survived tier 2 conflict?
>
> A tier fires only for a field that cleared the tier above it.
> And a check that depends on a field having contributed is suppressed at any level that already reported a field error.

The last clause is the new one.
Without it, a one-field struct whose tag is misspelled reports both the misspelling and ADR-0005's "maps no address", and the second is the first's consequence.

Measured:

```
tier 1   H string `ferry:"h,requird"`               1 error
tier 2   O []string `ferry:"o,required,default=v"`  2 errors, both inadmissible, no contradiction reported
tier 3   S string `ferry:"s,required,default=x"`    1 error, the contradiction

four fields, one of each                            3 errors, one per field
```

ADR-0006's five refusals all fire, driven by this grammar rather than by the placeholder that stood in for it:

```
p int      `ferry:"p,default=abc"`         default "abc" is not a valid int: strconv.ParseInt: invalid syntax
tags []string `ferry:"tags,default=a"`     []string is a composite, so it has no single address a default
                                           could sit at; seed the value instead
origins []string `ferry:"origins,required"` required is not available on []string: ...
s string   `ferry:"s,required,default=x"`  required and default contradict
b int      `ferry:"b,omitzero,default=8080"` omitzero and default=8080 contradict
c int      `ferry:"c,omitzero,default=0"`  compiles
```

`omitzero` is the only option admissible at every type, verified on a leaf, a slice, a map, a pointer, a struct and an array, because it asks a question about the Go value rather than about an address.

**Near-miss rejection is json/v2's shape, and wider than json/v2's reach.**

v2 normalises an unknown option by lowercasing it and stripping `_`, and rejects it only if the result is one of its six words, with `has invalid appearance of %s tag option; specify %s instead`.
Everything else it ignores, with a source comment saying that is not a promise.
Measured on `go1.27rc2`: `json:"a,omitEmpty"` errors and `json:"a,xyzzy"` marshals clean.

**So ADR-0001's strictness is materially stricter than v2's, and this ADR says how much rather than leaving the two sounding identical.**
ferry rejects every unrecognised option, and adds two layers on top of v2's normalisation:

- **Edit distance**, so `requird`, `reqired`, `defualt` and `deafult` each get the right suggestion, none of which v2's normalisation would catch.
- **A table of the neighbourhood's vocabulary**, so a word from another mapper gets its own sentence rather than a bare refusal.
  This is the `inline` lesson taken seriously: the fix for a 29k-use silent no-op is not only to reject it, it is to say what to write instead.

Measured over 26 misspellings and foreign words: **22 got a specific remedy**, and the four that did not (`req`, `r`, `asString`, `xyzzy`) are not near anything and correctly got the generic message naming the whole vocabulary.

Surrounding whitespace is its own diagnosis rather than an unknown option, because ferry does not trim and `ferry:"h, required"` is a mistake a reader's eye slides over.

Every refusal is per address, and the reports are joined and sorted, which is ADR-0001's determinism invariant applied rather than re-decided.
The error types are [#9](https://github.com/onhotpath/ferry/issues/9)'s convention.

### The validation entry point is the compiler, not a second one

> `func Validate[T any]() error` compiles the schema for `T` and discards it.

Two entry points that could disagree about whether a type is legal would be the viper defect at ferry's own front door, so there is one compiler and this is it.

Exercised from a real `go test`, with no value in hand and no plane reachable:

```
a fully annotated struct                        nil
a struct with an untagged exported field        ferry: /Host: field Host carries no ferry tag: ...
omitzero with a non-zero default                ferry: /b: omitzero and default=8080 contradict: ...
a misspelled option                             ferry: /H: has invalid appearance of "requird" tag option; specify "required" instead
an option from another mapper                   ferry: /H: unknown option "omitempty": ferry has no omitempty; ...
a tag reflect.StructTag.Get silently truncates  ferry: /H: struct tag is not in the conventional `key:"value"` form ...
a field carrying two ferry tags                 ferry: /H: the field carries two ferry tags, "first" and "second" ...
```

The last two are the point.
`go vet` catches the sixth and misses the seventh, and `go test` runs neither.

It takes no arguments, and that is a consequence of the tag key not being configurable rather than a separate decision.
No tag content depends on the call site, so `Validate[T]()` and a later `Load[T]` see the same tags and can share whatever cache #16 builds.
Whether they see the same **codec registry** is [#19](https://github.com/onhotpath/ferry/issues/19)'s, and it is the one thing that could make them disagree.

### What a declared default is not

ADR-0007 left a documentation obligation here and it is discharged with the measurement rather than the sentence.

A declared default is text, and schema compile turns it into a `String` `Value` at the field's address, which ADR-0006 decided.
`String` donates to `Bytes` as a relabel, per ADR-0005's `[]byte` rule.
So:

```
Secret []byte `ferry:"secret,default=aGk="`

declared default text   "aGk="
boundary Value          string("aGk=")
lands in the field as   "aGk="   (4 bytes)
```

**`aGk=` is four bytes and not the decoded `hi`.**
ADR-0007 states explicitly that this is not a case for a second coercion, and this ADR agrees: base64 is not ferry's business, and how a plane spells bytes is the driver's.
A user who wants decoded bytes registers a codec, or seeds the value.

**And ADR-0006's one grammar requirement is satisfied**, measured through a load rather than asserted:

```
a string `ferry:"a,default="`     ->  default string("")
b string `ferry:"b"`              ->  no default
c string `ferry:"c,default=x"`    ->  default string("x")

seeded                       {WithEmpty:seeded WithNone:seeded WithText:seeded}
loaded from an EMPTY plane   {WithEmpty:       WithNone:seeded WithText:x}
```

`default=` wrote the empty string over the seed and the absent option left the field alone, which is the distinction ADR-0006 required and the reason the option's value is not optional.
`ferry:"b,default"` is refused with that sentence in the message.

### The one mistake the grammar cannot see

```
ferry:"required"   ->  a segment named "required"
ferry:"omitzero"   ->  a segment named "omitzero"
```

A bare option word in the name position is a name, and ferry cannot know otherwise.
ADR-0003 decided that core has no opinion about segment text, because it cannot have one that is right for env, YAML, a Registry hive and a query string at once, and a plane key really can be called `required`.
Refusing it would make a legal segment name unwritable, which is the constraint ADR-0003 handed this ticket in the first place.

What ferry does catch is the structural version of the same slip, because `=` in a name is illegal:

```
ferry:"default=8080"   a name may not contain `=`, and "default=8080" looks like the default option
                       with no name in front of it; write ferry:"<name>,default=8080"
ferry:"required=yes"   a name may not contain `=`, and "required=yes" looks like the required option
                       with no name in front of it; write ferry:"<name>,required"
```

That covers the xload-shaped migration mistake, `env:",prefix=DB_"`, which under this grammar is an empty name plus an unknown option and reports as two loud errors.
The bare-word case is left, named, and visible in the artefact: a segment called `required` shows up in the dumped file.

### What this hands the tickets that were waiting

**[#16](https://github.com/onhotpath/ferry/issues/16), the generic entry point and the schema cache.**
The ticket's own comment says the tag-key question determines the cache design and should be resolved jointly, so this is the answer stated as an obligation.
The tag key is fixed, no tag option affects what compiles, and `Validate[T]()` is the compiler, so **nothing in this ADR makes a compiled schema depend on anything but `reflect.Type`**.
That is narrower than "the cache key is `reflect.Type`", and the difference is not this ADR's to close: ADR-0007 makes a registered codec change whether a type compiles at all, so whether the codec registry is part of the key depends on [#19](https://github.com/onhotpath/ferry/issues/19) making it process-wide or per-instance.
What this ADR guarantees is that the **tag** contributes nothing to that key.
If a tag-key Option ever lands it becomes part of that key, at a measured 12 ns against 18 ns and no allocations either way, which is the cheap end of the question ADR-0006 opened.
The expensive end, a compile-affecting Option whose value is unhashable, is untouched by this ADR because no tag option is one.

**[#19](https://github.com/onhotpath/ferry/issues/19), registration.**
No tag option names a codec, in either direction.
ADR-0007 put selection in the identity table and the text pair, and a per-field override would be a second selection authority for one type.
What a registered codec gets from this grammar is what every leaf gets: its type is a leaf, so it takes `default=`, `required` and `omitzero` with no codec-side awareness, which ADR-0007 already measured against `netip.Addr` and `big.Int`.

**[#14](https://github.com/onhotpath/ferry/issues/14), template generation.**
Every mapped address now has a name a human wrote, so a generated template carries the plane's own vocabulary rather than Go identifiers.
That is a direct consequence of the naming rule and it is the strongest ergonomic argument for it.

**[#9](https://github.com/onhotpath/ferry/issues/9), errors.**
This ADR produces more error values than any before it, and defers all of their types.
It does hand #9 one shape it should not have to invent: the three tiers are a suppression order, so an error convention that reports everything it finds would undo them.

### What this ADR does not decide

- **The error types** every refusal here produces: [#9](https://github.com/onhotpath/ferry/issues/9)'s convention, applied rather than invented.
  This ADR joins and sorts, per ADR-0001's determinism invariant.
- **Where the compiled schema is cached, and the generic entry point's name**: [#16](https://github.com/onhotpath/ferry/issues/16).
  `Validate[T]()` is named here because the ticket asked for it; whether it sits beside `Load[T]` or inside a schema type is #16's.
- **The registration API**: [#19](https://github.com/onhotpath/ferry/issues/19), with the one obligation above.
- **Whether a `ferry` `analysis.Analyzer` ships.**
  The research doc offers it as an alternative to a validation entry point or as a complement.
  This ADR ships the entry point, which needs no tooling and runs in a user's own test, and records that an analyzer would catch the same class at build time for users who wire it up.
  It has no ticket, and one is proposed in the resolution comment rather than decided here.
- **Whether the address rendering adopts the follow-the-character escape.**
  ADR-0003 left the bytes to the implementation and this ADR measures that both spellings work.
- **The migration guide from xload**, which [#1](https://github.com/onhotpath/ferry/issues/1) lists as unspecified until this grammar exists.
  It exists now, and the guide is somebody's.
- **Whether the vocabulary ever grows.**
  ADR-0001 froze it at publication and ADR-0002 keeps ferry at v0 until this grammar survives real use, which is the only window in which a word can be taken back.

## Consequences

- ferry's whole tag vocabulary is a name, `-`, and three options.
  Every word in the neighbourhood that ferry does not have is listed with the reason, so the list can be argued against without the reasoning being reinvented.
- **Core does not use `reflect.StructTag.Get`.**
  That is forty lines nobody would write on purpose, and it is what turns three silent failure modes into three diagnoses, one of which `go vet` does not cover and none of which `go test` sees.
  It also means a bare double quote in a ferry tag is reported by ferry rather than only by the `json` tag on the same field mysteriously disappearing.
- **Every exported field carries a name somebody wrote.**
  The cost is thirty tags on a thirty-field struct, and the return is that exporting a field cannot change the plane, that a generated template speaks the plane's vocabulary, and that ferry never invents a representation.
  A Go-field-name default would have been right about one time in twenty, measured.
- The grammar can name any segment a plane can hold, except the empty one, which is deliberate.
  That closes the half of survey item 5.10 ADR-0003 left open, and it is a property `encoding/json/v2` does not have in Go 1.27.
- **ferry is stricter than `encoding/json/v2`, measurably.**
  v2 rejects near-misses of its six options and silently ignores everything else; ferry rejects everything it does not recognise and suggests a remedy for 22 of 26 measured mistakes.
  The cost is ADR-0001's, already priced: the vocabulary is frozen, and a future option is a breaking change for anyone who used that word.
- Direction is nowhere in the syntax, and the option that would have forced it there, a per-direction name, is a round-trip violation.
  So the grammar has no direction concept to get wrong, and no shape committed if one is ever needed.
- Embedding, prefixing and squashing cost no vocabulary between them, because a structured address already expresses all three.
  The one hazard, a promoted name clashing with a sibling, is caught by ADR-0003's existing prefix-free rule with nothing added.
- The tag key is fixed, so a compiled schema is a function of `reflect.Type` alone and #16 gets the simplest cache key available.
  The measured cost of a configurable key is six nanoseconds and the measured cost of strictness under one is three refusals in four fields, and the second is what decided it.
- Diagnostics have three tiers rather than ADR-0006's two, and the suppression rule extends to ADR-0005's "maps no address" check.
  One field's single mistake reports once.
- The three embedding defects the audit found were all outside the shape of the original fixtures, and all three were silent.
  The one that generalises is that **the walk and the schema compiler must share one field rule**, or the schema promises an address the walk never visits.
- **The grammar has one blind spot and it is not closable**: a bare option word in the name position is a name.
  It follows from ADR-0003's decision that core has no opinion about segment text, and the alternative would make a legal name unwritable.
- `Validate[T]()` takes no arguments, which is a consequence of two other decisions rather than a design choice, and it is the same compiler `Load` will use.
- A declared `default=aGk=` on a `[]byte` field is four bytes and not `hi`, documented here as ADR-0007 required.
- ADR-0002's v1 trigger is now something a person can hold: the grammar exists, and what remains is one first-party driver and one external adopter using it.

## Items from the xload survey

The survey is [`docs/research/generics-and-modern-go.md`](../research/generics-and-modern-go.md), section 5, against `github.com/gojekfarm/xtools` at [`a90b3aa`](https://github.com/gojekfarm/xtools/commit/a90b3aad2133248cec50f6b4d6e37b0d9e788adb).

**5.10, composite values are string-splitting, and it is not escapable.**
The half ADR-0003 and ADR-0005 left here is closed, and it is this ticket's outright.
xload's `parseField` splits the tag on `,` at [load.go:219](https://github.com/gojekfarm/xtools/blob/a90b3aad2133248cec50f6b4d6e37b0d9e788adb/xload/load.go#L219), so `env:"K,delimiter=,"` cannot be written; reproduced, it parses to key `K` with the delimiter silently empty.
ferry writes the same intent as `ferry:"K,default=~,"` and gets the comma, and eight hostile segment names round-trip through the real YAML driver.
The measurement that decided the escape model is that the alternative needing no escape character, doubling the separator, silently renames a field on a stray comma.

**5.14** was enumerated rather than assumed, all four items.

- *Two ways to set the loader.*
  Bears on this ADR directly, and is answered rather than avoided.
  There is exactly one way to declare each of the three options and exactly one tag key that carries them, so no field can be configured twice.
  ADR-0006 already answered the neighbouring instance, where a `Static` source under `FirstOf` is a second way to express a default: the combinator stays a user's composition and spells the address set twice, and a declared default cannot drift because it is on the field.
- *The `CanAddr` loop that can only run once.*
  Bears on nothing here.
  This ADR decides what a field is called, not how it is addressed in `reflect`; ADR-0005 and ADR-0007 each carried their half of that defect.
- *The non-deterministic select on a cancelled context.*
  Concurrency, and [#20](https://github.com/onhotpath/ferry/issues/20)'s.
  This ADR neither fixes nor worsens it.
  It adds nothing #20 needs to know: tag parsing happens once per type at schema compile, with no context and no I/O.
- *Value receivers on `Error()` where pointers are returned.*
  Bears on this ADR more than on any before it, since it produces more distinct refusals than any of them.
  Deferred to [#9](https://github.com/onhotpath/ferry/issues/9)'s convention rather than pre-empted, as ADR-0003 through ADR-0007 all did.

**5.5, nondeterministic error output**, is [#9](https://github.com/onhotpath/ferry/issues/9)'s, and this ADR applies it rather than deciding it: every report is per address, joined and sorted.

**5.1, the `Loader` signature cannot express absence**, is ADR-0004's and ADR-0006's, and it surfaces here once, as the reason `required` is a presence test with a one-word spelling rather than a family of options.

The research doc's recommendation 12, "ship your own tag validation, nothing else will do it", is discharged in full: the entry point exists, the near-miss rejection is wider than the one it recommended copying, and the vet gap it cites is re-measured against a `ferry` tag rather than inferred.

The remaining items are unaffected by this ADR.
