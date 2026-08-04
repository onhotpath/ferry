# ferry/http

Load an HTTP request's query parameters or header fields into a Go struct.

```
go get github.com/onhotpath/ferry/driver/http
```

The package is `ferryhttp`, because `http` is a name every caller already has taken.

## Loading

Build the source once, at start-up.
Pass the request in through the context, per request.

```go
type Filter struct {
	Q     string   `ferry:"q,required"`
	Tags  []string `ferry:"tags"`
	Limit int      `ferry:"limit,default=25"`
}

func Example() {
	src := ferryhttp.NewQuerySource() // once, at start-up

	values, err := url.ParseQuery("q=ferry&tags=go&tags=config")
	if err != nil {
		fmt.Println(err)

		return
	}

	f, err := ferry.Load[Filter](ferryhttp.WithQuery(context.Background(), values), src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", f)
	// Output: {Q:ferry Tags:[go config] Limit:25}
}
```

That is the `Example` in `example_test.go`, imports aside, so `go test` compiles and runs it.
It parses a query string only to be self-contained.
In a handler the two calls are `r.Context()` and `r.URL.Query()`:

```go
mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
	f, err := ferry.Load[Filter](ferryhttp.WithQuery(r.Context(), r.URL.Query()), src)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}
	// ...
})
```

**Parameter names come from the tags.**
Each part contributes its own text and nested fields are joined with `.`, so a field tagged `host` inside one tagged `db` reads `db.host`.
Nothing is folded: a query parameter name is any byte sequence, so the name is exactly what the tag spelled.

**Slices and maps** read what is already there.
`?tags=a&tags=b` fills a `[]string`, and so does `?tags.0=a&tags.1=b`.
`?limits.rps=50` fills a `map[string]string` under the key `rps`.

## Headers, with the same source shape

```go
src := ferryhttp.NewHeaderSource()
t, err := ferry.Load[Tenant](ferryhttp.WithHeaders(r.Context(), r.Header), src)
```

Field names join with `-` instead of `.`, and are matched case-insensitively, the way `net/http` spells them.
Headers nest, and the hyphen is how: `X-Forwarded-For` and `X-Forwarded-Proto` are the registry's own spelling of a nested `x-forwarded` object, and this driver reads them as one.

```go
type Forwarded struct {
	For   string `ferry:"for"`
	Proto string `ferry:"proto"`
}

type Request struct {
	Forwarded Forwarded `ferry:"x-forwarded"`
}
```

A repeated field line is a sequence, exactly as a repeated parameter is: two `Accept-Encoding:` lines fill a `[]string` with two elements.
A single line holding a comma-separated list is one value, because `net/http` does not split it and neither does this driver.

## The context is the only way in

A `Source` holds no request.
It is built once and shared, which is what `net/http` running every handler in a goroutine of its own requires, and the request travels in the context instead.

A load whose context never went through `WithQuery` or `WithHeaders` is refused before anything is read:

```
ferry: opening the plane: plane error: http: no query parameters in the context: put it there with ferryhttp.WithQuery or ferryhttp.WithHeaders
```

That is a bug in the handler and not in the request, so it is loud.
A driver that answered from nothing instead would report every field missing, and a `required` field would fail for a request that supplied it.

`errors.Is(err, ferryhttp.ErrNoQuery)` distinguishes it from a malformed request, which is the difference between a 500 and a 400.

## A repeated name is a sequence, and never the first value

`?sort=name&sort=age` into a `string` field is refused:

```
ferry: /sort: closing the plane: plane error: http: this name occurs more than once: it occurs 2 times, and this field takes one value
```

Nothing quietly takes the first.
Change the field to a `[]string`, or answer 400 and name the parameter.

The refusal arrives when the load closes rather than when the field is read, and that is a property of the plane rather than a late report.
At the moment the field is asked for there is one call at one name, and one occurrence and two are indistinguishable there: only the walk finishing without having enumerated the name says the field was not a sequence after all.
A request with two such parameters reports both, one failure per parameter, each carrying its own address.

## One position cannot be spelled two ways

`?tags=a&tags=b` and `?tags.0=a&tags.1=b` are two spellings of one sequence, and both load.
A request using both for the same position is refused rather than resolved:

```
ferry: /tags: the driver failed: plane error: http: this name carries a sequence in two spellings at once: position 0 is spelled both by the repetition and by a name of its own, so one of the two values would be lost
```

The realistic way to hit it is a form with a hidden `tags.0` beside a checkbox group named `tags`.

Only an overlap is refused.
`?tags=a&tags=b&tags.2=c` extends the sequence and loads as three elements, because position 2 is not claimed twice.

## Set but empty is not the same as absent

`?x=` loads as the empty string.
`x` not being in the query at all is a different observation, and `required` can tell them apart: `ferry:"token,required"` is satisfied by `?token=` and fails when `token` is absent.

## Two fields cannot share a name

Nesting joins with `.`, so a field tagged `db.host` and a nested `db`/`host` both want `db.host`.
When that happens the load fails immediately, before anything is read, and names both:

```
ferry: /db.host: query renders this address and /db/host to one plane key, "db.host", so one of the two would be lost
```

Rename one of the fields, or widen the join: `ferryhttp.Separator("..")` keeps `db..host` and `db.host` apart.
The same option exists on the header source, where the default join is `-`.

## What a header value cannot hold

A header field value may not contain a control character other than a tab, and a leading or trailing space or tab does not survive the wire.
Both are properties of HTTP, measured here against a real `net/http` client and server rather than read off the specification, and they are the one thing the header plane can carry less of than the query plane.
A query parameter has no such limit: any byte sequence survives percent-encoding, in a name and in a value alike.

Neither plane carries type information of its own.
Everything is text, and a `bool` or an `int` field is parsed out of that text by ferry rather than by the plane.

## Refusals never quote what the request held

Everything this driver reads came off the wire.
A refusal names the parameter or field it is about and never the value, so an error may be logged and returned without leaking a token out of a query string or an `Authorization` header.
A parameter name minted by a map is part of the address and does appear, because ferry cannot name the address without it.

## There is no way to write back

This package loads only, so `ferry.Dump` through it does not compile rather than failing at run time.
Building an outbound request is a different job and belongs to a package written for it.

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/http) is the reference for every option above, and the design records behind them are in [`docs/adr/`](../../docs/adr/).
