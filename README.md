# ferry

[![CI](https://github.com/onhotpath/ferry/actions/workflows/ci.yml/badge.svg)](https://github.com/onhotpath/ferry/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/onhotpath/ferry/graph/badge.svg?token=A33mpdyMRa)](https://codecov.io/gh/onhotpath/ferry)

Two-way data mapping for Go structs: load from any source, dump to any sink.

One annotated struct, one tag grammar, two directions, over a plane the library has no opinion about.

```go
type DB struct {
    Host string `ferry:"host,required"`
    Port int    `ferry:"port,default=5432"`
}

type Config struct {
    Name string   `ferry:"name"`
    DB   DB       `ferry:"db"`
    Tags []string `ferry:"tags"`
}

cfg, err := ferry.Load[Config](ctx, yaml.Source{Path: "app.yaml"})
err = ferry.Dump(ctx, cfg, yaml.Sink{Path: "app.yaml"})
```

The same struct loads from environment variables, from a Consul-shaped KV, and from anything anybody writes two methods for.

## Status

Under construction, and not yet usable.

The design is finished and the implementation is not.
Fourteen architectural decision records in [`docs/adr/`](docs/adr/) settle the address model, the source and sink contract, the type set, defaults and absence, the codec chain, the tag grammar, registration, the entry point and schema cache, the error model, the caller-held binding, plane compatibility and the conformance package.
Every one of them is Accepted.

The build that turns them into code is tracked in [#63](https://github.com/onhotpath/ferry/issues/63).

## Reading the design

The ADRs are the specification, and they are meant to be read rather than summarised.
Where this README and an ADR disagree, the ADR wins and this README is wrong.

Start with [ADR-0001](docs/adr/0001-what-ferry-supports.md) for what ferry supports and what is ruled out, then [ADR-0010](docs/adr/0010-the-entry-point-and-the-schema-cache.md) for the shape a caller sees.

## Contributing

`make help` lists the developer targets.
`make check` is what to run before pushing.

Documentation proper is [#87](https://github.com/onhotpath/ferry/issues/87), and this file will be replaced by it.
