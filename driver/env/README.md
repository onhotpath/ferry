# ferry/env

Load configuration from environment variables and `.env` files into a Go struct, and write a `.env` file back.

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
ferry: /db_host: env gives this and /db/host the same name, "DB_HOST", so one of the two would be lost
```

Rename one of the fields, or change the separator: `env.Separator("__")` joins nested fields with `__`, so `DB__HOST` and `DB_HOST` stay apart.

## Map keys come back lower-case

`LIMITS_HTTP` fills a `map[string]string` under the key `http`, not `HTTP`, because the name was upper-cased on the way in and there is no way to know which it started as.
`env.Canonical(env.Upper)` chooses upper-case instead, for configuration whose keys are themselves variable names.

At the default separator a key containing `_` is read as nesting: `LIMITS_RPS_BURST` fills `limits.rps.burst` rather than a key `rps_burst`.
Use `env.Separator("__")` if your keys contain underscores.

## A bool is true or false, unless you say otherwise

An environment variable is text, so a `bool` field reads the text a bool's own parser reads: `true` and `false`.
`ENABLED=on` is refused, because ferry does not guess at what a word was meant to mean.

`env.BoolWords("on", "off", "true", "false")` says which words this environment spells a boolean with.
All four are then accepted, and `on` is the one a `true` is written as.

The words are consulted where the schema wants a bool and nowhere else, so a `string` field over `FEATURE=on` loads the text `on`.
The sharp edge is what that means for two programs reading one environment: a variable holding a declared word is a boolean where the schema wants one and text where it does not, so one variable can be read two ways, and which way is the schema's business rather than the environment's.

## A schema that is one value needs a name for it

`ferry.Load[int]` names one address, the root, and the root carries no segment to fold a name out of.
Name the variable and it loads:

```go
func ExampleRootVar() {
	environ := func() []string { return []string{"APP_PORT=8080"} }

	port, err := ferry.Load[int](context.Background(), env.New(env.Environ(environ), env.RootVar("APP_PORT")))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(port)
	// Output: 8080
}
```

That is `ExampleRootVar` in `example_test.go`, which supplies its own environment so that it runs the same everywhere.

Without `env.RootVar` that load is refused at Bind, before anything is read.
It is the only route to a root value, because the fold turns every byte outside `A-Z`, `0-9` and `_` into `_`, so no field or map key could ever produce the name.

The variable being unset is ordinary absence: the root has no tag to carry `required` or `default=`, so `ferry.Load` gives back the zero value and `ferry.LoadOver` gives back the seed.

## `.env` files layer under the process environment

The process environment and a `.env` file share one namespace and one name-folding rule, so they are one plane here rather than two.
The process is the anchor and always wins.
Files are optional layers underneath it, in the order you name them.

```go
func ExampleDotEnv() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("# the box the database is on\nDB_HOST=db.internal\nDB_PORT=6543\n"), 0o600)

	environ := func() []string { return []string{"NAME=checkout", "DB_PORT=5432"} }

	cfg, err := ferry.Load[Config](context.Background(), env.New(env.Environ(environ), env.DotEnv(path)))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Name:checkout DB:{Host:db.internal Port:5432}}
}
```

That is `ExampleDotEnv` in `example_test.go`.
`DB_PORT` comes from the process because the process wins; `DB_HOST` comes from the file because the process does not say.

`env.DotEnv()` with no paths reads `.env`.
`env.DotEnv("base.env", "local.env")` reads both, and `local.env` wins over `base.env`.
A file that is not there is an empty layer, so every field takes its default and a `required` field fails.
A file that is there and does not parse is a refusal, not an empty load.

## Writing a `.env` file back

`env.NewDotEnvSink(path)` is the write half, and a save is a merge into whatever file is already at the path.

```go
func ExampleNewDotEnvSink() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("# the box the database is on\nexport DB_HOST=old\nUNRELATED=keep me\n"), 0o600)

	cfg := Config{Name: "checkout", DB: DB{Host: "db.internal", Port: 5432}}

	if err := ferry.Dump(context.Background(), cfg, env.NewDotEnvSink(path)); err != nil {
		fmt.Println(err)

		return
	}

	saved, _ := os.ReadFile(path)
	fmt.Print(string(saved))
	// Output:
	// # the box the database is on
	// export DB_HOST=db.internal
	// DB_PORT=5432
	// UNRELATED=keep me
	// NAME=checkout
}
```

That is `ExampleNewDotEnvSink` in `example_test.go`.
The comment, the `export`, the key order and the variable no field maps all survive.
A new variable lands beside the ones whose names it is most like, which is what keeps a slice's elements together.

A slice or a map is the one place the merge stops, because it is replaced whole: a three-element slice saved over with one element leaves one variable, and the comment written directly above each removed line goes with it.

The write is atomic, the file is read before it is written, and a save whose file somebody else edited in between refuses rather than discarding their edit.

**Give both halves the same naming options.**
`env.Separator`, `env.RootVar` and `env.BoolWords` apply to a source and a sink alike, and nothing checks that the two agree - a sink writing `TAGS_0` and a source reading `TAGS__0` never meet.

### Quoting

A value is written in the narrowest quoting that holds it, and in the quoting the line already used where the new value permits it.

| value | written |
| --- | --- |
| `db.internal` | `db.internal` |
| `""` | `''` |
| `" padded "` | `' padded '` |
| `"# not a comment"` | `'# not a comment'` |
| `"$HOME"` | `'$HOME'` |
| `say "hi"` | `'say "hi"'` |
| `"it's"` | `"it's"` |
| `"a\nb"` | `"a\nb"`, one physical line |
| `"\xff\xfe"` | raw bytes inside double quotes |
| `"a\x00b"` | **refused** |

A value holding a NUL is the one thing this plane cannot hold, because the environment block is handed to a new process as NUL-terminated strings.

A value this driver writes is one `sh` reads back identically when the file is sourced, with two exceptions, both in the double-quoted case: `sh` does not read `\n`, `\r` or `\t` as the byte meant by them, and a value holding a single quote together with a `$` or a backtick has to be double-quoted and is then expanded.
Both round trip through ferry exactly.

## A save can look as though it did nothing

The process environment is above the file, so a save that writes only the file leaves the next load reading the old value.

```go
sink := env.NewDotEnvSink(".env", env.Setenv(nil))
```

`env.Setenv` makes a save apply itself to the running process as well, which is what keeps the two halves of the plane in agreement.
It is off by default, because changing a process's environment is visible to every goroutine in it and to every child it starts.
It also unsets what a save swept, which is what makes a shortened slice actually shorter on the next load.
`env.Setenv(nil)` names the running process; anything else is where the writes go instead, which is what a test supplies.

## Reloading when a file changes

```go
src := env.New(env.Environ(func() []string { return nil }), env.DotEnv(path)).Watched()

wb, err := ferry.BindWatched[Config](src)

seq, errf := wb.Watch(ctx)
for cfg := range seq {
	publish(cfg) // replace the pointer, never mutate the old value
}
```

That is `ExampleSource_Watched` in [`example_test.go`](example_test.go), trimmed of its setup.

`env.Source.Watched` converts a source into one that can be watched, and it takes no arguments: the files are the ones `env.DotEnv` already named.
The stream opens with a load and yields a freshly loaded value for every change afterwards, so there is no first load to write and no change that landed before the range started to lose.
Cancelling `ctx` ends the watch and the stream together, and it is the only ending a caller asks for.

The directory is what is watched rather than the file, because an editor and this package's own sink both replace a file by renaming another over it.
A burst of events from one save is one reload, and the `.env.ferry-*` files a save stages are ignored.
A dump through `env.DotEnvSink` over the same path is a change like any other, so a process that both watches and saves its own configuration hears its own writes.

A source naming no file is refused at `ferry.BindWatched`, under `ferry.ErrPlane` with `env.ErrWatch` reachable underneath, because a watch that never fires is the failure that refusal exists to avoid.
Whatever the operating system has an opinion about - a directory that is not there, or one removed or moved out from under a running stream - ends the stream under `ferry.ErrWatchLost` instead, so a process holding stale configuration always has something to tell it so.

## The sharp edges

Every one of them is the same fact: the process environment is above the files, and it is not yours.

- **A save can look as though it did nothing.** Without `env.Setenv`, the file holds the new value and the process still exports the old one.
- **A save replaces the file, not the union.** Clearing a slice clears it from the file; the process goes on serving what it holds.
- **An ambient variable can invent a map key.** `TAGS_5` exported by somebody else adds a sixth element to a slice the file gives two of.
- **Ambient names collide with short field names.** A field tagged `path` reads `PATH`, one tagged `home` reads `HOME`.
- **A report names the variable, and editing the file may not fix it.** The name is a function of the address and the driver's settings, so it cannot say the process is shadowing the file.
- **`export ` is kept and never added.** A line that had it keeps it; a new line does not get one.
- **The sink writes the file a symlink names.** Swapping the link between two saves sends them to two different files.

The way out of the first five is `env.Environ(func() []string { return nil })`, which makes the files the whole plane.

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/env) is the reference for every option above, and the design records behind them are in [`docs/adr/`](../../docs/adr/).
