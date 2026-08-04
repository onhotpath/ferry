# ferry/env

Load configuration from environment variables into a Go struct.

```
go get github.com/onhotpath/ferry/driver/env
```

## Loading

Tag the fields, call `Load`, get a struct back.

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
It sets its own variables only to be self-contained; ordinarily they are already there and `env.New()` reads what it finds.

**Variable names come from the tags.**
Each name is upper-cased, anything an environment variable name cannot hold becomes an underscore, and nested fields join with `_`.
So `name` reads `NAME`, the nested `db.host` reads `DB_HOST`, and a field tagged `feature-flags` reads `FEATURE_FLAGS`.

**Slices and maps** read the names that are already there: `TAGS_0` and `TAGS_1` fill a `[]string`, and `LIMITS_RPS` fills a `map[string]string` under the key `rps`.

**`required`** fails the load when the variable is not set.
**`default=`** supplies a value when it is not set.
Both are per field, in the tag.

## Set but empty is not the same as unset

`FOO=` loads as the empty string.
`FOO` not being set at all is different, and `required` can tell them apart: `ferry:"token,required"` is satisfied by `TOKEN=` and fails when `TOKEN` is unset.

What no environment variable can say is "clear this".
`ferry.LoadOver` loads over a struct that already has values, and there an unset variable leaves the field as it was rather than blanking it.

## Using `env:` tags instead of `ferry:`

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

`ferry.TagKey` changes only which tag is read.
What goes inside it - the name, `required`, `default=` - is unchanged.
It applies to every struct in that call, so pass it everywhere you load that type, or a call that omits it will look for `ferry:` tags and find none.

## Two variables cannot share a name

Nesting joins with `_`, and `.` and `-` become `_` too, so two different fields can end up wanting the same variable.
A field tagged `db_host` and a nested `db.host` both want `DB_HOST`.
When that happens the load fails immediately, before reading anything, and names both:

```
ferry: /db_host: env renders this address and /db/host to one plane key, "DB_HOST", so one of the two would be lost
```

Rename one of the fields, or change the separator: `env.Separator("__")` joins nested fields with `__`, so `DB__HOST` and `DB_HOST` stay apart.

## Map keys come back lower-case

`LIMITS_HTTP` fills a `map[string]string` under the key `http`, not `HTTP`, because the name was upper-cased on the way in and there is no way to know which it started as.
`env.Canonical(env.Upper)` chooses upper-case instead, for configuration whose keys are themselves variable names.

At the default separator a key containing `_` is read as nesting: `LIMITS_RPS_BURST` fills `limits.rps.burst` rather than a key `rps_burst`.
Use `env.Separator("__")` if your keys contain underscores.

## There is no way to write back

This package loads only.
Setting the running process's own environment is rarely what anyone wants, and writing a `.env` file is a different job that belongs to a different package.
So `ferry.Dump` with this package does not compile, rather than failing at run time.

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/env) is the reference for every option above, and the design records behind them are in [`docs/adr/`](../../docs/adr/).
