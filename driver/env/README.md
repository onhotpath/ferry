# ferry/env

`env` is ferry's environment-variable plane.
It is a source and never a sink: it loads an annotated struct out of the environment, and it has no `Dump`.

```
go get github.com/onhotpath/ferry/driver/env
```

## Loading

An address becomes a name by folding each segment to upper case, turning every byte an environment variable name cannot hold into an underscore, and joining the segments with `_`.
So the schema below reads `NAME`, `DB_HOST` and `DB_PORT`, and a field tagged `feature-flags` reads `FEATURE_FLAGS`.

```go
type Config struct {
	Name string `ferry:"name,required"`
	DB   DB     `ferry:"db"`
}

type DB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=5432"`
}

func Example() {
	os.Setenv("NAME", "checkout")
	os.Setenv("DB_HOST", "db.internal")
	os.Unsetenv("DB_PORT") // not set at all, so the default applies

	cfg, err := ferry.Load[Config](context.Background(), env.New())
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Name:checkout DB:{Host:db.internal Port:5432}}
}
```

That is the `Example` in `example_test.go`, imports aside, so `go test` compiles and runs it.
It sets its own variables only to be self-contained; ordinarily they are already in the environment and `env.New()` reads what is there.

Composites load from the names the environment already holds: `TAGS_0` and `TAGS_1` fill a `[]string`, and `LIMITS_RPS` fills a `map[string]string` with the key `rps`.

## Reading `env` tags instead of `ferry` tags

`ferry` is the struct tag key by default, and `ferry.TagKey` changes it.
A struct already annotated for an environment loader, or one you would rather read as `env:"..."` because that is what the field means here, needs no rewriting.

```go
type Service struct {
	Name    string `env:"service,required"`
	Timeout int    `env:"timeout,default=30"`
}

func Example_tagKey() {
	os.Setenv("SERVICE", "checkout")
	os.Unsetenv("TIMEOUT") // not set at all, so the default applies

	svc, err := ferry.Load[Service](context.Background(), env.New(), ferry.TagKey("env"))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", svc)
	// Output: {Name:checkout Timeout:30}
}
```

Two things it is worth knowing before reaching for it.

`ferry.TagKey` is core's option and not this driver's, so it renames the key for every type in that call and for every plane, not only for this one.
It names where to look and never what the content means: `required`, `default=` and the rest of the grammar are unchanged, because ADR-0008 specifies the content and this only specifies the key.

A type read under two different keys is two different schemas, so ferry caches them apart and a call that omits the option gets the `ferry` key back.
The fields above carry no `ferry` tag at all, so loading them without the option is a schema refusal naming each field rather than a silent empty struct:

```
ferry: 2 errors:
  /Name: field Name carries no ferry tag: every exported field must name the segment it addresses, or be marked ferry:"-"
  /Timeout: field Timeout carries no ferry tag: every exported field must name the segment it addresses, or be marked ferry:"-"
```

## There is no Dump

Setting the process's own environment is process-global mutation nobody wants, and the target people actually want is a `.env` file, which is a format and belongs to a driver of its own.
Nothing in this package implements `ferry.Sink`, so `ferry.Dump(ctx, cfg, env.New())` is a compile error at the call site rather than a runtime refusal.

## The separator

`_` is the default and `env.Separator("__")` changes it.
No separator is universally safe, because segment text is yours: at `_` the addresses `/db/host` and `/db_host` render one name.
A schema whose addresses collide under the separator in force is refused at `Bind`, before a single variable is read, and the refusal names both:

```
ferry: /db_host: env renders this address and /db/host to one plane key, "DB_HOST", so one of the two would be lost
```

## What the plane holds

A string, or nothing at all.
`FOO=` arrives as the empty string, a variable that is not set is absent, and the two stay distinguishable: `ferry:"token,required"` is satisfied by `TOKEN=` and refused when `TOKEN` is unset.

There is no null.
A value ferry can only express as a null, such as an empty composite, has no representation here at all, so this plane declares that it cannot carry one.
It is refused loudly rather than flattened to empty text, which would make an empty composite and a composite of one empty element the same bytes.

## Dynamic segments come back canonical

A map key or a slice index is not in the compiled schema, so its address comes from the environment, and the fold that produced the name is many-to-one: `http`, `HTTP` and `Http` all render `LIMITS_HTTP`.
`env.Canonical` chooses the spelling such a segment comes back in, `env.Lower` by default and `env.Upper` for a schema whose keys are themselves variable names.

That buys determinism and not totality.
The round-trip guarantee holds over canonical keys, and a key outside them comes back changed: at the default separator `LIMITS_RPS_BURST` reads as the nesting `/limits/rps/burst` rather than as the key `rps_burst`, and a wider separator moves that boundary without removing it.

## Where the decisions live

The package doc carries the full reasoning; this file is the short version of it.
ADR-0003 governs how an address becomes a name and why injectivity is checked over the whole address set, ADR-0004 the source and sink contract and why this plane has no sink, and ADR-0013 what a plane holds.
They are in [`docs/adr/`](../../docs/adr/).
