# Tags, defaults and absence

The whole tag grammar, what `default=`, `required` and `omitzero` do, and what a plane saying nothing means to a Go field.

The decision records are [ADR-0008](../adr/0008-the-struct-tag-grammar.md) for the grammar and [ADR-0006](../adr/0006-defaults-and-zero-values.md) for defaults and absence.

## The grammar

Four words, and one of them is punctuation:

```
tag      =  name *( "," option )  /  "-"

name     =  token          ; and may not be empty
option   =  "required"  /  "omitzero"  /  "default" "=" token

token    =  bare  /  quoted
bare     =  *( any byte except "," , and not beginning with "'" )
quoted   =  "'" *( any byte except "'"  /  "''" ) "'"
```

That is ferry's whole tag vocabulary: a name, `-`, and three options.

```go
type Config struct {
	Host     string   `ferry:"host"`
	Port     int      `ferry:"port,default=8080"`
	Token    string   `ferry:"token,required"`
	Comment  string   `ferry:"comment,omitzero"`
	Origins  []string `ferry:"cors-origins"`
	Internal string   `ferry:"-"`
}
```

### Every field must name its segment

**An exported, named struct field with no ferry tag is a schema compile error.**
ferry never invents a segment name.

```
ferry: /Debug: field Debug carries no ferry tag: every exported field must name
       the segment it addresses, or be marked ferry:"-"
```

| the field | what ferry does |
| --- | --- |
| unexported | skipped |
| `ferry:"-"` | skipped, in both directions |
| exported and named, with a tag | mapped at the segment the tag names |
| exported and named, with no tag | **schema compile error** |
| embedded, with no tag | its fields are promoted to the parent address |
| embedded, with a tag | nested under that name |
| embedded **pointer**, with no tag | **schema compile error** |

`ferry:"-"` on an unexported field is redundant and accepted.
Any other tag on an unexported field is an error, because `reflect` cannot set it and the tag can never do anything.

### Embedding needs no word

```go
type Common struct {
	Name string `ferry:"name"`
	Env  string `ferry:"env"`
}

struct{ Common;                  Port int `ferry:"port"` }  ->  /env /name /port
struct{ Common `ferry:"common"`; Port int `ferry:"port"` }  ->  /common/env /common/name /port
struct{ Common `ferry:"-"`;      Port int `ferry:"port"` }  ->  /port
```

An embedded field with no ferry tag is walked at the parent address, so its fields are promoted.
That is what Go's own field namespace already means by embedding, which is why `embed`, `inline` and `squash` are all absent from the vocabulary.

An anonymous field is considered whether or not its own type is exported.
`ferry:",required"` on an embedded field, meaning "promote and require", is refused.

### Quoting

A token is bare, or single-quoted with a literal quote doubled inside it.
There is no escape character, and only a **leading** quote is significant, so an apostrophe inside a bare token is just an apostrophe.

```go
Greeting string `ferry:"greeting,default='Hello, world'"`
Brokers  string `ferry:"brokers,default='h1:9092,h2:9092'"`
Cache    string `ferry:"cache,default=~/.cache/app"`
Note     string `ferry:"note,default=it's here"`
Odd      string `ferry:"'a,b'"`
Dash     string `ferry:"'-'"`
Apos     string `ferry:"'a''b'"`
```

`/`, `#` and `~` need no quoting: they are the address rendering's punctuation, not the grammar's, and the two alphabets are disjoint.

`default=` splits on its first `=`, so `default=a=b` is the text `a=b` and is written bare.
A bare **name** may not contain `=` at all, which is the one place the grammar guesses at intent: `ferry:"default=8080"` is diagnosed as a default with no name in front of it rather than as a segment called `default=8080`.

One wart, recorded rather than hidden: `default=` and `default=''` both mean the empty string, so one value has two spellings.

`ferry:""` is refused, because the empty tag value has to mean something and "you left the name out" is the more useful reading than "the segment is called nothing".

### The one mistake the grammar cannot see

```
ferry:"required"   ->  a segment named "required"
ferry:"omitzero"   ->  a segment named "omitzero"
```

A bare option word in the name position is a name, and ferry cannot know otherwise.

## Each option has one honest direction

The grammar spends no syntax on saying which, because every option has exactly one.

| | Load | Dump | why |
| --- | --- | --- | --- |
| the name | yes | yes | one address, or the round trip is not one |
| `-` | yes | yes | as above |
| `required` | yes | no | it asserts the plane spoke; on Dump ferry is the one speaking |
| `default=` | yes | no | it answers an absence Dump never observes |
| `omitzero` | no | yes | it decides whether an address is written |

## `default=`

**A default is text.**
Schema compile turns it into a `String` value at the field's address, and `Load` applies it when, and only when, the plane reports `Absent` there.

That is the whole design: it is indistinguishable at the boundary from what a flat plane would have reported, so ferry has one conversion authority rather than two.
`"0080"` means 80 in a tag exactly as it does from a plane, and a registered codec's type takes defaults with no codec-side awareness.

Checked at schema compile from the type alone, before any plane is touched:

```
p int           `ferry:"p,default=abc"`
    ferry: /p: default "abc" is not a valid int: strconv.ParseInt: parsing "abc": invalid syntax

b int8          `ferry:"b,default=99999"`
    ferry: /b: default "99999" is not a valid int8: strconv.ParseInt: parsing "99999": value out of range

t time.Duration `ferry:"t,default=30"`
    ferry: /t: default "30" is not a valid time.Duration: time: missing unit in duration "30"

t time.Duration `ferry:"t,default=30s"`   compiles
p int           `ferry:"p,default=0080"`  compiles, and means 80
s string        `ferry:"s,default="`      compiles, and means ""
```

`ferry:"b,default"` with no `=` is refused, so an empty default is distinguishable from no default at all.

The text is decoded fresh on every load rather than cached as a Go value, because a cached one aliases: two independently loaded structs would otherwise share one backing array for a `[]byte` default (ADR-0006).

### `default=aGk=` on a `[]byte` field lands as four bytes

This is the sharp edge, and it is stated here because it is the one people meet:

```go
Secret []byte `ferry:"secret,default=aGk="`
```

```
declared default text   "aGk="
boundary Value          string("aGk=")
lands in the field as   "aGk="   (4 bytes: 0x61 0x47 0x6b 0x3d)
```

**`aGk=` is four bytes and not the decoded `hi`.**

A declared default is text, `String` donates to `Bytes` as a relabel, and base64 is not ferry's business: how a plane spells bytes is the driver's (ADR-0008).
`driver/yaml` spells `Bytes` as `!!binary` base64 and `driver/kv` spells them as the raw bytes, and neither of those spellings is what a tag holds.

A user who wants decoded bytes registers a codec for the field's type, or seeds the value through `LoadOver`.

### What else a default is not

- **A default is leaf-only.**
  One on a composite does not compile: a composite's value lives at many addresses and a tag holds one text, so the remedy is to seed the value through `LoadOver`.
  A pointer to a **leaf** is not a composite and does take one, so a `*int` declaring a default loads as a non-nil pointer from an empty plane.
- **A default is Load-side.**
  `Dump` writes the value in hand and never substitutes.
- **A default is not presence.**
  A `*T` over a composite is materialised exactly where the plane spoke under it, and a declared default beneath it does not count, or no such pointer could ever be nil.
  A default fills a hole in a section and never conjures the section.
- **A seed loses to a declared default.**
  Where a `LoadOver` seed and a declared default both apply to one field, the declared one wins, because ferry cannot tell a seeded value from a zero one.
- **A declaration attaches to the address shape, not to an address.**
  A map key's address and a slice element's index come from the value, so `/servers/a/port` is never in a compiled schema.
  The declaration lives at `/servers/*/port`, written once and applied to every realised member.
- **An array element takes its declarations with nothing on the plane**, because an array element is a static address and is walked either way.
  A slice element in the same position does not exist at all.

## `required`

`required` is a presence test and nothing else, satisfied by any observation other than `Absent`.

So an explicit empty satisfies it, and a `Null` at a `*T` satisfies it while yielding nil, which is the user getting exactly what their type asked for.

It is admissible exactly where an address's children come from the type, so it works on leaves, structs, pointers and arrays.
It is a **schema compile error** on a slice, a map, or a pointer to either.

The reading a user wants for a collection is not writable at all: a missing key and an explicit empty list are one observation at a container address, so the refusal names the remedy, which is to model the distinction as `struct{ Set bool; Items []string }`.

At a composite, `required` means the plane supplied at least one of the address's static children, with one meaning on every plane class.

## `omitzero`

`omitzero` is a comparison against the Go zero value, evaluated before anything converts it, and it is the one option admissible at every type - verified on a leaf, a slice, a map, a pointer, a struct and an array.

It is **not** a comparison against the default.
A field holding its declared default is dumped like any other, because ferry cannot tell "still at its default" from "explicitly set to the same value", and because omitting it would leave the stored artefact under-specified: what it denotes would then be decided by whichever version of the code read it.

## The five refusals at schema compile

Checked from the type alone, with no plane in sight:

1. a default whose text the field's own parser does not accept;
2. a default on a composite;
3. `required` on a dynamic composite;
4. `required` together with a default;
5. `omitzero` together with a default that is not the field's zero value.

A zero default beside `omitzero` compiles, because omitting it and reapplying it land on the same value.

Admissibility is checked before contradictions, so one field's single mistake does not report as three errors:

```go
Origins []string `ferry:"origins,required,default=v"`   // 2 errors, not 3
```

## Absence, and what it means to a Go field

One rule carries all of it:

> **`Absent` means ferry does not write to the field.**

Every other observation, `Null` and the empty string included, is a value the plane holds, and it is handed to the type set, which either accepts it or refuses it loudly.

So a value loaded over an empty plane is unchanged, and an explicit empty beats whatever the field was already carrying, because present beats absent and empty is present.

**`Null` is not a second spelling of absence.**
It means the plane has this address and the value stored there is that plane's own null.
It is presence carrying a value, and the only question is which Go types can hold one.

### Absent and Null, per kind

| Go kind | `Absent` | `Null` |
| --- | --- | --- |
| `bool`, `string`, the integer kinds, the float kinds | does not write | **refused**, wrong kind |
| `time.Duration`, `time.Time` | does not write | **refused**, wrong kind |
| `[]byte` | does not write | nil slice |
| `[N]byte` | does not write | **refused**, an array has no nil |
| `*T` | does not write | nil pointer |
| `[]T`, `map[K]V` | does not write | nil, the zero value |
| `[N]T` | each element is a static address, walked either way | per element, by the element's own row |
| `struct` | per field | a struct has no address of its own, so it is only reachable through `*T` |

The refusal is the recoverable direction, and that is the whole argument for it: a registered codec for its own type can accept a `Null` and return whatever it likes, while zeroing in the walk would happen before any codec is consulted and nothing could recover strictness for a plain `int`.
`encoding/json/v2` zeroes, which is a change it made from v1, and ferry departs from it knowingly (ADR-0006).

### What the difference costs, on a real file

A `port` field declaring `default=8080`, through `driver/yaml`:

| document | the plane reports | the field ends up |
| --- | --- | --- |
| the key deleted | `absent` | **8080** |
| `# port: 9090`, commented out | `absent` | **8080** |
| `port:`, blank | `null` | **refused**, loudly |
| `port: null` | `null` | **refused**, loudly |
| `port: ""` | `string("")` | **refused**, loudly |

Not every plane can produce a `Null` at all.
YAML and JSON can; TOML, environment variables, query parameters and an opaque KV store cannot, because none of their grammars has one.
On a flat plane the distinction never arises, and that is a driver-fidelity property rather than a core rule.

### A struct merges and a composite replaces

Both follow from the one rule.

A struct's fields are separate addresses, so the ones the plane does not have are `Absent` and are left alone.
A composite is a single decision: if the plane has any children under that address then it has said what the composite is, so a slice or a map is **replaced wholesale** rather than merged into.

A `*T` over a composite is materialised exactly where the plane spoke under it, and the walk carries that as a bit per subtree rather than inferring it from the value afterwards.
Comparing the result against a fresh zero value cannot tell a subtree the plane really did set to all zeros from one nothing touched, which is the defect that inference has:

| plane | comparing against a fresh zero | what ferry does |
| --- | --- | --- |
| nothing under `/Opt` | `nil` | `nil` |
| `/Opt/A` and `/Opt/N` present, **both zero** | `nil` | `&{A:"" N:0}` |
| `/Opt/A` present and non-zero | `&{A:"x" N:0}` | `&{A:"x" N:0}` |
| an explicit `Null` at `/Opt` | `nil` | `nil` |

Row two is the point.
A value already in the field is not presence, so an optional section stays optional, and an explicit `Null` at its own address is a nil pointer.

A `*T` at a **leaf** is the one shape that tells an explicit zero from an unset field, and it is worth stating as narrowly as it is true: on `Load` from any plane, because absence is observable everywhere, and on `Dump` only from a plane that has a null, because a null is what a nil pointer writes.

### An omission is not a deletion

ferry never hands a sink an `Absent`.
It is a reader-side kind, so an omitted address gets no `Set` call at all rather than a `Set` carrying nothing.

A replacing sink and a patching sink therefore read one dump differently, and both are correct.

### There is no presence report

Presence survives the walk as an observation of one `Load`, per address and including `Absent`, and it is nothing a field holds: a key deleted from the plane and a key set to zero are one struct and two observations.

Core exports no Option, callback or report for it, because a `Reader` a caller wraps is already handed every address the walk asks about.

## Changing the tag key

`ferry.TagKey("env")` changes which struct tag key ferry reads.
It defaults to `ferry`, ferry reads exactly one key, and the key names **where to look and never what the content means**.

```go
type Service struct {
	Name    string `env:"service,required"`
	Timeout int    `env:"timeout,default=30"`
}

svc, err := ferry.Load[Service](ctx, env.New(), ferry.TagKey("env"))
```

What goes inside the tag - the name, `required`, `default=` - is unchanged.
`mylib:"host,retry=3"` is a schema compile error and correctly so: the key is an Option and the vocabulary is not.

**It applies to every struct in that call**, so pass it everywhere you load that type, or a call that omits it will look for `ferry:` tags and find none.

A list of keys is refused: a list is a precedence question wearing a convenience costume.
The key is validated where the Option is supplied rather than at schema compile, so an empty key, or one containing a space, a quote or a colon, is refused there.

## What is deliberately not in the vocabulary

Each of these is a refusal rather than an omission:

| word | where it comes from | why ferry has none |
| --- | --- | --- |
| `prefix=` | xload | a nested struct's own tag **is** the prefix, and a plane-wide prefix is a driver option |
| `delimiter=`, `separator=` | xload | a composite gets one address per element, so there is nothing to delimit |
| `inline`, `embed`, `squash` | json/v2, mapstructure | promotion is already the default for an embedded field |
| `omitempty` | json | there is no "empty JSON object" on a Consul plane |
| `case:ignore` | json/v2 | core never folds, and which characters fold is plane and locale knowledge |
| `string` | json | a plane's own kind assertion is respected rather than overridden |
| `format:` | json/v2, removed in 1.27 | a per-field layout is a representation the golden table has no row for |
| a codec selector | - | a per-field override is a second selection authority |
| `nodump`, `readonly` | asked for | a field ferry loads and never writes cannot round-trip |
| a validation constraint | viper, validator | the type is the validation |

On `prefix=` specifically, the failure it invites is why it is absent:

```
prefix="DB_"  + key "HOST"  ->  "DB_HOST"
prefix="DB"   + key "HOST"  ->  "DBHOST"
prefix="DB__" + key "HOST"  ->  "DB__HOST"
```

The nested struct's tag is the prefix, which is why the word is not needed rather than not wanted.
