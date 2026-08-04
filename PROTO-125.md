# What `.` in an address actually does, and what it does to `driver/env`

Prototype for [#125](https://github.com/onhotpath/ferry/issues/125), on branch `proto/125-env-keys`, based on `integ/83-base`.
Every block below is a real program in `proto125/`, and every output block under it is that program's actual stdout, pasted unedited.
Run them with `go run ./proto125/stepN`.

The three key functions are in `proto125/kf/kf.go`, and everything routes through core's own `ferry.NewKeys` from #81 rather than through a simulation of it.

---

## 1. What a ferry address is

An address is a sequence of segments, each carrying a kind and a text.
It is not a string with separators in it.
`/db/host` and `/db.host` are the two shapes this whole issue turns on, and they differ in segment count, not in punctuation.

```go
for _, addr := range []ferry.Path{
	ferry.At("db", "host"),
	ferry.At("db.host"),
	ferry.At("limits", "extra", "http.port"),
	ferry.At("limits", "extra", "http_port"),
	ferry.At("tags").Elem(0),
} {
	fmt.Printf("%-28s  %d segment(s)\n", addr.String(), count(addr))

	i := 0
	for seg := range addr.Segments() {
		fmt.Printf("      [%d] kind=%-5s text=%q\n", i, seg.Kind(), seg.Text())
		i++
	}
}
```

Output (`go run ./proto125/step1`):

```
/db/host                      2 segment(s)
      [0] kind=Name  text="db"
      [1] kind=Name  text="host"
/db.host                      1 segment(s)
      [0] kind=Name  text="db.host"
/limits/extra/http.port       3 segment(s)
      [0] kind=Name  text="limits"
      [1] kind=Name  text="extra"
      [2] kind=Name  text="http.port"
/limits/extra/http_port       3 segment(s)
      [0] kind=Name  text="limits"
      [1] kind=Name  text="extra"
      [2] kind=Name  text="http_port"
/tags#0                       2 segment(s)
      [0] kind=Name  text="tags"
      [1] kind=Index text="0"

At("db","host") == At("db.host")  -> false
At("db","host") == At("db","host") -> true
```

Read that again on the second and third rows.
The `.` in `/db.host` is inside one segment's text.
The `.` in `/limits/extra/http.port` is inside the third segment's text, and `_` in `/limits/extra/http_port` is inside its third segment's text.

**In core, `.` is not a separator and it is not special in any way.**
It is a byte in a segment's text, exactly as `_`, `-`, and a space are.
Core never joins segments and never parses a plane key back, so no ambiguity exists here at all.
The problem only starts when a driver flattens.

---

## 2. How does a `.` get into an address in the first place?

Five candidate routes.
Three are real, one is closed, one is closed only for the built-in case.

```go
type tagged struct {
	Host  string `ferry:"db.host"`
	Other string `ferry:"db_host"`
}

type dynamic struct {
	Labels map[string]string `ferry:"labels"`
}

type indexed struct {
	Tags []string `ferry:"tags"`
}

type keyed struct {
	Quantiles map[float64]string `ferry:"quantiles"`
}

type peered struct {
	Peers map[netip.Addr]string `ferry:"peers"`
}
```

Output (`go run ./proto125/step2`):

```
== route 1: a dot written in a struct tag ==
Compile[tagged]() -> <nil>
static address set handed to Bind:
      /db.host
      /db_host

== route 2: a dot arriving from a map key ==
Compile[dynamic]() -> <nil>
static address set handed to Bind:
      /labels
addresses a Dump of map[app.name:ferry team:core] actually asked the sink to Set:
      /labels/app.name
      /labels/team

== route 3: a slice index ==
      /tags#0
      /tags#1

== route 4: a non-string map key whose text form carries a dot ==
Compile[keyed]() -> ferry: /quantiles: float64 is not usable as a map key: two distinct NaN payloads both format as NaN, so its text is not injective over the type and two distinct keys collapse into one address - key the map by a type that is injective, or convert the key yourself
      (dump reported: ferry: /quantiles: float64 is not usable as a map key: two distinct NaN payloads both format as NaN, so its text is not injective over the type and two distinct keys collapse into one address - key the map by a type that is injective, or convert the key yourself )

== route 5: a map keyed by a registered type whose text is dotted ==
Compile[peered]() -> <nil>
      /peers/10.0.0.1
      /peers/10.0.0.2
```

Route by route.

- **Route 1, a struct tag, is real.**
  `ferry:"db.host"` compiles with no diagnostic at all, and the static address set handed to `Bind` contains `/db.host`.
  This follows from the tag grammar in `grammar.go`, where a name is a token and a bare token is "any byte except a comma, not beginning with a quote".
  The dot is unremarkable text there.
  So **a user can write a dot in a tag today**, and nothing in core stops them.

- **Route 2, a map key, is real** and it is the one that arrives from runtime data.
  The static set is only `/labels`.
  The dotted address `/labels/app.name` is minted during the walk, from the map's contents, and is never seen by `Bind`.

- **Route 3, a slice index, cannot carry a dot.**
  `Path.Elem` takes a `uint` and renders canonical base-10, so an Index segment's text is digits and nothing else.

- **Route 4, a `float64` map key, is closed.**
  Core refuses the type outright, for an unrelated reason (NaN payloads), so the obvious "numeric key with a dot in its text" route does not exist.

- **Route 5, a map keyed by a type somebody registered a codec for, is real.**
  `map[netip.Addr]string` with `ferry.TextCodec[netip.Addr](ferry.KindString).AsMapKey()` produces `/peers/10.0.0.1`.
  The segment text is the codec's output, and no rule constrains what characters a codec may emit.

**Conclusion for this section.**
The framing "this is a runtime-data problem" is only half right.
It is *both*: a dot is writable in a tag (statically visible at `Bind`), and it arrives from map keys and registered key codecs (only visible mid-walk).
That matters because the two are caught at different moments, which section 6 measures.

---

## 3. What an env key function has to do, and why `.` is a problem at all

An environment variable name is, by POSIX, letters, digits and underscore.
A dot is not in that set, and neither is a hyphen.
So a key function that joins segments with `_` has to decide what to do with a segment text that carries either.

Three plausible answers, all three in `proto125/kf/kf.go`:

```go
// A: uppercase, join with "_", no character transform.
func EnvUpper(addr ferry.Path) (string, error) { return Join(addr, "_", strings.ToUpper) }

// B: uppercase, join with "_", map every illegal byte to "_".
func EnvScrub(addr ferry.Path) (string, error) {
	return Join(addr, "_", func(s string) string {
		b := []byte(strings.ToUpper(s))
		for i := range b {
			if !LegalEnvByte(b[i]) {
				b[i] = '_'
			}
		}

		return string(b)
	})
}

// C: uppercase, join with "_", refuse an illegal byte outright.
func EnvValidate(addr ferry.Path) (string, error) { /* returns an error naming the byte */ }
```

Output (`go run ./proto125/step3`):

```
A: env, uppercase, join with _, no character transform
B: env, uppercase, join with _, illegal character -> _
C: env, uppercase, join with _, refuse an illegal character

address                     A                         B                         C
/db/host                    DB_HOST                   DB_HOST                   DB_HOST
/db.host                    DB.HOST                   DB_HOST                   REFUSED: segment "db.host" carries ".", which an environment variable name may not hold
/limits/extra/http.port     LIMITS_EXTRA_HTTP.PORT    LIMITS_EXTRA_HTTP_PORT    REFUSED: segment "http.port" carries ".", which an environment variable name may not hold
/limits/extra/http_port     LIMITS_EXTRA_HTTP_PORT    LIMITS_EXTRA_HTTP_PORT    LIMITS_EXTRA_HTTP_PORT
/feature-flags              FEATURE-FLAGS             FEATURE_FLAGS             REFUSED: segment "feature-flags" carries "-", which an environment variable name may not hold
/labels/app.name            LABELS_APP.NAME           LABELS_APP_NAME           REFUSED: segment "app.name" carries ".", which an environment variable name may not hold
/tags#0                     TAGS_0                    TAGS_0                    TAGS_0
```

The whole issue is visible in one cell.
Under **A**, `/db.host` renders `DB.HOST` and `/db/host` renders `DB_HOST`, and they are distinct.
Under **B**, both render `DB_HOST`, and they are not.

**A produces names the platform cannot actually carry**, which is measured in section 6a and is the strongest argument against it.

---

## 4. Where the collision actually bites

Same key functions, now through the real `ferry.NewKeys`, over address sets that mix a dot with a slash or an underscore.

```go
keys, err := ferry.NewKeys(ferry.NewAddressSet(s.addrs...), "env", f.F)
```

Output (`go run ./proto125/step4`), abridged to the sets that matter:

```
### /db.host beside /db/host
    address: /db.host
    address: /db/host
  env, uppercase + _, no char transform  Bind ok: /db.host->DB.HOST /db/host->DB_HOST
  env, uppercase + _, illegal -> _       Bind REFUSED
      ferry: /db.host: env renders this address and /db/host to one plane key, "DB_HOST", so one of the two would be lost
  env, uppercase + _, validating         Bind REFUSED
      ferry: /db.host: the driver refused the address set: segment "db.host" carries ".", which an environment variable name may not hold

### ADR-0003's own pair
    address: /limits/extra/http.port
    address: /limits/extra/http_port
  env, uppercase + _, no char transform  Bind ok: /limits/extra/http.port->LIMITS_EXTRA_HTTP.PORT /limits/extra/http_port->LIMITS_EXTRA_HTTP_PORT
  env, uppercase + _, illegal -> _       Bind REFUSED
      ferry: /limits/extra/http_port: env renders this address and /limits/extra/http.port to one plane key, "LIMITS_EXTRA_HTTP_PORT", so one of the two would be lost
  env, uppercase + _, validating         Bind REFUSED
      ferry: /limits/extra/http.port: the driver refused the address set: segment "http.port" carries ".", which an environment variable name may not hold

### /db.host alone
    address: /db.host
  env, uppercase + _, no char transform  Bind ok: /db.host->DB.HOST
  env, uppercase + _, illegal -> _       Bind ok: /db.host->DB_HOST
  env, uppercase + _, validating         Bind REFUSED
      ferry: /db.host: the driver refused the address set: segment "db.host" carries ".", which an environment variable name may not hold

### /feature-flags alone
    address: /feature-flags
  env, uppercase + _, no char transform  Bind ok: /feature-flags->FEATURE-FLAGS
  env, uppercase + _, illegal -> _       Bind ok: /feature-flags->FEATURE_FLAGS
  env, uppercase + _, validating         Bind REFUSED
      ferry: /feature-flags: the driver refused the address set: segment "feature-flags" carries "-", which an environment variable name may not hold

### /feature-flags beside /feature_flags
    address: /feature-flags
    address: /feature_flags
  env, uppercase + _, no char transform  Bind ok: /feature-flags->FEATURE-FLAGS /feature_flags->FEATURE_FLAGS
  env, uppercase + _, illegal -> _       Bind REFUSED
      ferry: /feature_flags: env renders this address and /feature-flags to one plane key, "FEATURE_FLAGS", so one of the two would be lost
  env, uppercase + _, validating         Bind REFUSED
      ferry: /feature-flags: the driver refused the address set: segment "feature-flags" carries "-", which an environment variable name may not hold
```

Three things to read out of that.

1. **The collision refusal names both addresses and the key**, before any I/O.
   `env renders this address and /db/host to one plane key, "DB_HOST", so one of the two would be lost`.
   That message is `Keys.collision` in `keys.go` and is already shipped by #81.
2. **B and C refuse in different circumstances.**
   `/db.host` *alone* is fine under B and refused under C.
   C is strictly more refusing: it dislikes the character, not the collision.
3. **`/feature-flags` alone is the case that kills C.**
   `feature-flags` is an ordinary thing to write in a config struct, and C refuses it, while B renders `FEATURE_FLAGS` and only refuses if `/feature_flags` is also present.
   This is exactly ADR-0003's own argument, and it reproduces.

---

## 5. Reproducing ADR-0003's published table

The table at `docs/adr/0003-how-a-leaf-addresses-a-plane.md:287`, all four address sets against the three published columns, plus the transforming function as a fourth column.

```go
cols := []kf.Named{
	{Label: "env, uppercase and _", F: kf.EnvUpper},
	{Label: "env, no fold and _", F: kf.EnvExact},
	{Label: "dotted, no fold", F: kf.Dotted},
	{Label: "env, transforming", F: kf.EnvScrub},
}
```

Output (`go run ./proto125/step5`), where `[ADR: x]` is the cell the ADR publishes:

```
Address set                       env, uppercase and _        env, no fold and _          dotted, no fold             env, transforming
/DB/HOST, /DB_HOST                rejected [ADR: rejected]    rejected [ADR: rejected]    ok [ADR: ok]                rejected
/myKey, /MyKey, /MYKEY            rejected [ADR: rejected]    ok [ADR: ok]                ok [ADR: ok]                rejected
/db.host, /db/host                ok [ADR: ok]                ok [ADR: ok]                rejected [ADR: rejected]    rejected
/db/host, /db/port, /cache/host   ok [ADR: ok]                ok [ADR: ok]                ok [ADR: ok]                ok
```

**All twelve published cells reproduce**, under the reading that the two env columns do a case fold and a join and no character transform.
No cell of the table is wrong.

**The transforming function differs from the table's first column in exactly one cell: row 3.**
That is the whole of the disagreement, and it is `/db.host` beside `/db/host`.

Now the other half.
The dynamic-tier measurement at `docs/adr/0003-how-a-leaf-addresses-a-plane.md:178`:

> Measured on one type with two values, both walked with the same **transforming** env driver:
> ```
> value 2  ->  /limits/http_port  /limits/extra/http.port  /limits/extra/http_port
>              refused: "LIMITS_EXTRA_HTTP_PORT" <- /limits/extra/http.port and /limits/extra/http_port
> ```

Step 4's second block reproduces that refusal exactly, and it reproduces it **only** under B.
Under A the same pair binds fine, as `LIMITS_EXTRA_HTTP.PORT` and `LIMITS_EXTRA_HTTP_PORT`.

### Verdict on the issue's framing

**The framing in #125 is correct in substance and slightly overstated in one detail.**

Correct: the table's columns and the measurement's driver are different key functions, and a reader building `driver/env` from one passage gets a different answer than from the other.
The table's first column is non-transforming; the measurement is transforming; row 3 is the cell where they disagree.

Overstated: the ADR is not self-contradictory.
Line 178 says "the same **transforming** env driver" in as many words, so the measurement labels its own function.
What the ADR fails to do is label the *table's* columns, which leaves a reader to assume the transforming driver of the prose and the `env, uppercase and _` of the table are the same thing.
They are not, and #81 was right to carry four key functions.

So this is a documentation defect of omission, not a measurement error, and **nothing in the ADR needs to be re-measured**.
Amending the column headers to say "no character transform" and adding the transforming function as a fourth column is sufficient and makes every passage correct as written.
That is #125's first proposed remedy, and it is the one the evidence supports.

---

## 6. The user-visible consequence

A config struct a person would actually write.

```go
type Config struct {
	DB     DB                `ferry:"db"`
	Labels map[string]string `ferry:"labels"`
}

type DB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}

// The static route: a dot written into a tag, beside the nested struct it
// collides with under a transforming key function.
type Tagged struct {
	Legacy string `ferry:"db.host"`
	DB     DB     `ferry:"db"`
}
```

Output (`go run ./proto125/step6`), in six parts.

### 6a. Can the platform even hold these names?

```
  DB_HOST          os.Setenv err=<nil> os.Getenv="x" | sh DB_HOST=x: ok ""
  LABELS_APP.NAME  os.Setenv err=<nil> os.Getenv="x" | sh LABELS_APP.NAME=x: FAILED (exit status 127) "/bin/sh: 1: LABELS_APP.NAME=x: not found"
  FEATURE-FLAGS    os.Setenv err=<nil> os.Getenv="x" | sh FEATURE-FLAGS=x: FAILED (exit status 127) "/bin/sh: 1: FEATURE-FLAGS=x: not found"
  env(1) can still place LABELS_APP.NAME in a child: "ran"
```

This is the fact that decides the most.
Go's `os.Setenv` accepts `LABELS_APP.NAME` happily, and `os.Getenv` reads it back.
A POSIX shell cannot assign it: `LABELS_APP.NAME=x` parses as a command name and exits 127.
`env(1)` can still place it in a child process, so the name is reachable, just not the ordinary way anybody writes a `.env` file, a Dockerfile `ENV`, a Kubernetes `env:` entry, or a systemd `Environment=`.

**Key function A therefore produces keys that Go can read but that an operator cannot set through the normal mechanisms.**
That is not a theoretical objection; it is what a user hits the first time they try to supply the value.

### 6b. Dump with a dot from a map key

```
  env, uppercase + _, no char transform  wrote map[DB_HOST:db1 DB_PORT:5432 LABELS_APP.NAME:ferry LABELS_TEAM:core]
  env, uppercase + _, illegal -> _       wrote map[DB_HOST:db1 DB_PORT:5432 LABELS_APP_NAME:ferry LABELS_TEAM:core]
  env, uppercase + _, validating         FAILED
      ferry: /labels/app.name: the driver failed: segment "app.name" carries ".", which an environment variable name may not hold
      plane after the failure: map[DB_HOST:db1 DB_PORT:5432 LABELS_TEAM:core]
```

A single dotted label, nothing colliding with it.
A works and writes an unsettable name.
B works and writes a settable one.
C refuses a perfectly ordinary label, which is the cost of validating.

### 6c. Dump with two map keys that fold together

`Labels: map[string]string{"app.name": "dotted", "app_name": "scored"}`.

```
  env, uppercase + _, no char transform  wrote map[DB_HOST:db1 DB_PORT:5432 LABELS_APP.NAME:dotted LABELS_APP_NAME:scored]
  env, uppercase + _, illegal -> _       FAILED
      ferry: /labels/app_name: the driver failed: env renders this address and /labels/app.name to one plane key, "LABELS_APP_NAME", so one of the two would be lost
      plane after the failure: map[DB_HOST:db1 DB_PORT:5432 LABELS_APP_NAME:dotted]
  env, uppercase + _, validating         FAILED
      ferry: /labels/app.name: the driver failed: segment "app.name" carries ".", which an environment variable name may not hold
      plane after the failure: map[DB_HOST:db1 DB_PORT:5432 LABELS_APP_NAME:scored]
```

Under B this is the ADR's measurement, arriving from a map rather than from a tag: the refusal happens mid-walk, at the moment the second address is minted, and it names both.
Note the honest detail: `LABELS_APP_NAME:dotted` is already on the plane when the refusal lands.
The refusal is before the *losing* write, not before all writes.
That is a property of the dynamic tier and it is what ADR-0003 already says ("as each is minted, before the write it belongs to"), but it is worth seeing.

### 6d. Dump with a dot from a struct tag

```
  env, uppercase + _, no char transform  wrote map[DB.HOST:old DB_HOST:db1 DB_PORT:1]
  env, uppercase + _, illegal -> _       FAILED at Bind, before any write
      ferry: /db.host: env renders this address and /db/host to one plane key, "DB_HOST", so one of the two would be lost
  env, uppercase + _, validating         FAILED at Bind, before any write
      ferry: /db.host: the driver refused the address set: segment "db.host" carries ".", which an environment variable name may not hold
```

Because the dot came from a tag, the address is static, so B and C both refuse at `Bind` with nothing written.
That is the difference the two tiers make, and it is why route 1 in section 2 mattered.

### 6e. Round trip: dump then load through the same key function

```
  env, uppercase + _, no char transform  plane [DB_HOST DB_PORT LABELS_APP.NAME LABELS_TEAM]
                                         back as {DB:{Host:db1 Port:5432} Labels:map[APP.NAME:ferry TEAM:core]}
  env, uppercase + _, illegal -> _       plane [DB_HOST DB_PORT LABELS_APP_NAME LABELS_TEAM]
                                         back as {DB:{Host:db1 Port:5432} Labels:map[APP_NAME:ferry TEAM:core]}
  env, uppercase + _, validating         dump FAILED: ferry: /labels/app.name: the driver failed: segment "app.name" carries ".", which an environment variable name may not hold
```

**This is the finding I did not expect and think matters most.**

The map key went out as `app.name` and came back as `APP.NAME` under A and as `APP_NAME` under B.
Neither round-trips.

The dot is not what breaks A's round trip.
The **uppercase** does.
`strings.ToUpper` is many-to-one over segment text, so a dynamic map key's original spelling is unrecoverable on Load under *any* env key function that upper-cases, dot or no dot.
Choosing A to "preserve the dot" buys nothing here, because the case is already gone.

Note also that this round trip involves no collision refusal at all.
`ferry.NewKeys` is injective over the address set it was handed in each direction separately, and it is doing its job.
Non-round-tripping is a property of the key function's inverse, which is a different question that ADR-0003 does not currently address for `Load`.
*(This paragraph is my inference from the measurement, not something the ADR says.)*

### 6f. The same dump through a driver that hand-rolls its table

```
  hand-rolled transforming sink: err=<nil>
  plane: map[DB_HOST:db1 DB_PORT:5432 LABELS_APP_NAME:scored]
```

Same value as 6c, same transform, but the sink computes its key per `Set` and routes through no `Keys`.
No error.
One of the two labels is gone, silently, and which one survives is whichever the walk wrote last.
This is the outcome `ferry.NewKeys` exists to prevent, and it is the reason `driver/env` must route through it whichever key function it picks.

---

## 7. Verdict table

| Key function | What a user gains | What a user loses |
| --- | --- | --- |
| **A: uppercase, join `_`, no character transform** | `/db.host` and `/db/host` stay distinct, so no schema is ever refused for a collision the transform invented | Produces names a POSIX shell cannot assign (`LABELS_APP.NAME`), so an ordinary `feature-flags` field or a dotted label is unsettable by an operator; the value silently never arrives |
| **B: uppercase, join `_`, illegal -> `_`** | Every key it emits is a legal, settable environment variable name, and `feature-flags` just works | `/db.host` beside `/db/host` is refused at Bind, and a dotted map key beside its underscored twin is refused mid-dump; the user must rename one |
| **C: uppercase, join `_`, validating** | Never emits a folded key, so no collision is ever invented by the transform | Refuses `feature-flags` and `app.name` outright, even alone with nothing to collide with; a legal Go config becomes an unloadable one |

---

## The decision in front of the owner

Two options are live for `driver/env`, and the ADR amendment follows from whichever is chosen.

**Option 1: `driver/env` transforms (B), and ADR-0003's table gains a labelled fourth column.**

Commits ferry to: every key `driver/env` emits is a settable environment variable name; `feature-flags` and `app.name` work; a schema holding both `/db.host` and `/db/host`, or a map holding both `app.name` and `app_name`, is a compile-or-walk-time refusal naming both addresses.
Costs: the refusal is user-visible and is a compatibility promise; a user with a genuinely dotted label alongside an underscored twin must rename one.
The ADR change is #125's first remedy: amend the column headers in place to say "no character transform" and add the transforming function as a fourth column with `rejected` on row 3.
No published cell changes, and the prose at ADR-0003's "a driver is expected to transform segment text" becomes correct as written.

**Option 2: `driver/env` does not transform (A), and ADR-0003's dynamic-tier measurement is relabelled or replaced.**

Commits ferry to: `/db.host` and `/db/host` are both loadable and both dumpable, and no schema is ever refused for a transform-induced collision.
Costs: `driver/env` emits `FEATURE-FLAGS` and `LABELS_APP.NAME`, which Go can read but a shell, a Dockerfile, a Kubernetes manifest and a systemd unit cannot set.
That is a driver that appears to work in tests and fails in production deployment, which is the worst failure shape available.
It also contradicts ADR-0003's prose, so the prose would have to be reversed rather than clarified.

**Option 3, worth naming so it can be rejected: C.**
It refuses `feature-flags`, which is not a mistake anyone made, and it is the position ADR-0003 already examined and moved away from on measured grounds.
Section 4 reproduces that.

### Recommendation

**Option 1: `driver/env` transforms, and the ADR's table gains a labelled fourth column.**

Three reasons, in order of weight.

1. **6a is decisive.** A key function that emits names an operator cannot set is not an env driver, it is an env-shaped map. The dot's "preservation" under A is preservation into a place nobody can reach.
2. **The refusal B produces is loud, early and names both addresses.** Compare it against 6f, which is what happens with no check: one label silently gone. The transforming driver's worst case is a refusal a user can act on; the alternative's worst case is a value that never arrives, with no error anywhere.
3. **6e shows the "preserve the dot" argument buys less than it appears to.** Even under A, a dynamic map key does not survive a round trip, because the uppercase fold already destroyed the spelling. So option 2 pays the deployment cost of illegal names to preserve a distinction that Load cannot recover anyway.

The ADR work is small: label the table's two env columns "no character transform", add a fourth column for the transforming function with `rejected` on row 3 and the same verdicts elsewhere, and leave every published number alone.
#81's four-key-function test already encodes exactly that and needs no change.

One thing I would flag as unsettled and out of #125's scope: **ADR-0003 says nothing about the key function's inverse**, which 6e shows is where the real user surprise lives for map-typed fields on `Load` from a flat plane.
Whether `driver/env` should lower-case recovered map keys, or refuse to enumerate maps at all, or document the loss, is a question for #84 rather than for this issue.
