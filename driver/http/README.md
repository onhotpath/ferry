# ferry/http

Load an HTTP request's query parameters or header fields into a Go struct.

```
go get github.com/onhotpath/ferry/driver/http
```

The package is `ferryhttp`, because `http` is a name every caller already has taken.

## In a server: bind once, load per request

`ferry.Bind` compiles the type and hands the source the names it asks for, once.
Everything a request supplies of its own arrives in the context instead, so a handler holds the binding and loads through it.

Here that is a middleware, which is the shape most servers already have a place for.
It is not exported by this package: it is thirty lines a caller writes for the value their own handlers want.

```go
type Filter struct {
	Q     string   `ferry:"q,required"`
	Tags  []string `ferry:"tags"`
	Limit int      `ferry:"limit,default=25"`
}

// filterKey is the middleware's own key for the loaded value, unexported and of
// its own type so that nothing else can read or overwrite what it put there.
type filterKey struct{}

// filterFrom reads back what the middleware loaded. A handler reached through it
// always has one, because a request whose filter did not load never gets there.
func filterFrom(ctx context.Context) Filter {
	f, _ := ctx.Value(filterKey{}).(Filter)

	return f
}

// withFilter is the middleware: it loads a Filter out of every request's query
// parameters and hands it to next in the context.
//
// The binding is built once, by the caller, and this closes over it. Each
// request supplies its own query parameters instead, so the per-request work is
// the load and nothing else.
func withFilter(b *ferry.Binding[Filter], next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := b.Load(ferryhttp.WithQuery(r.Context(), r.URL.Query()))
		if err != nil {
			refuse(w, err)

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), filterKey{}, f)))
	})
}

// refuse answers a request the load would not build a value for, naming the
// parameter it was about and never quoting what that parameter held.
func refuse(w http.ResponseWriter, err error) {
	var located *ferry.Error
	if errors.As(err, &located) {
		http.Error(w, "bad request at "+located.Address().String(), http.StatusBadRequest)

		return
	}

	http.Error(w, "bad request", http.StatusBadRequest)
}

func Example_middleware() {
	b, err := ferry.Bind[Filter](ferryhttp.NewQuerySource()) // once, at start-up
	if err != nil {
		fmt.Println(err)

		return
	}

	h := withFilter(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%+v", filterFrom(r.Context()))
	}))

	for _, target := range []string{"/search?q=ferry&tags=go&tags=config", "/search?tags=go"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))

		fmt.Println(w.Code, strings.TrimSpace(w.Body.String()))
	}
	// Output:
	// 200 {Q:ferry Tags:[go config] Limit:25}
	// 400 bad request at /q
}
```

That is `Example_middleware` in `example_test.go`, imports aside, so `go test` compiles and runs it.
The second request is the failure path: `q` is `required` and is not there, so the wrapped handler is never reached and the refusal carries `/q` rather than a zero value nobody asked for.

## One load, with no binding held

`ferry.Load` takes the same source and does the bind again on each call, which is the shape for a one-off: a URL parsed in a script, a test, a single request outside a serving loop.

```go
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

That is the `Example` in the same file.
It parses a query string only to be self-contained; in a handler the two calls are `r.Context()` and `r.URL.Query()`.

**Parameter names come from the tags.**
Each part contributes its own text and nested fields are joined with `.`, so a field tagged `host` inside one tagged `db` reads `db.host`.
Nothing is folded: a query parameter name is any byte sequence, so the name is exactly what the tag spelled.

**Slices and maps** read what is already there.
`?tags=a&tags=b` fills a `[]string`, and so does `?tags.0=a&tags.1=b`.
`?limits.rps=50` fills a `map[string]string` under the key `rps`.

## Headers, with the same source shape

```go
b, err := ferry.Bind[Tenant](ferryhttp.NewHeaderSource()) // once, at start-up
t, err := b.Load(ferryhttp.WithHeaders(r.Context(), r.Header))
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
ferry: /sort: the driver failed: plane error: http: this name occurs more than once: it occurs 2 times, and this field takes one value
```

Nothing quietly takes the first.
Change the field to a `[]string`, or answer 400 and name the parameter.

The refusal arrives while that field is read, so it carries the field's address and a `required` field pointed at a repeated name reports one failure rather than two.
Which reading a name gets is decided by what the field is, not by what the request holds: `?tags=a&tags=b` is two elements into a `[]string` and a refusal into a `string`, and the same is true one level down, where `?limits.rps=1&limits.rps=2` is a refusal into a `map[string]string` and two elements into a `map[string][]string`.
A request with two such parameters reports both, one failure per parameter, each carrying its own address.

The positions are the request's own order, because position *n* is the *n*th value the name carries.

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
ferry: /db.host: query gives this and /db/host the same name, "db.host", so one of the two would be lost
```

Rename one of the fields, or widen the join: `ferryhttp.Separator("..")` keeps `db..host` and `db.host` apart.
The same option exists on the header source, where the default join is `-`.

## What a header value cannot hold

A header field value may not contain a control character other than a tab, and a leading or trailing space or tab does not survive the wire.
Both are properties of HTTP, measured here against a real `net/http` client and server rather than read off the specification, and they are the one thing the header plane can carry less of than the query plane.
A query parameter has no such limit: any byte sequence survives percent-encoding, in a name and in a value alike.

Neither plane carries type information of its own.
Everything is text, and a `bool` or an `int` field is parsed out of that text by ferry rather than by the plane.

## A plane can carry payloads instead of text

A `[]byte` field takes the bytes of the text that arrived, which is what a request holding text means.
`ferryhttp.BytesAs` says the plane carries payloads instead, and how they are spelled:

```go
spelling := ferry.With(ferryhttp.Base64(), ferryhttp.Gzip(), ferryhttp.MaxSize(4<<10))
src := ferryhttp.NewQuerySource(ferryhttp.BytesAs(spelling))

// What a client would have put in the query string.
spelled, err := spelling.Render([]byte("-----BEGIN CERTIFICATE-----"))
if err != nil {
	fmt.Println(err)

	return
}

ctx := ferryhttp.WithQuery(context.Background(), url.Values{"cert": {spelled}})

c, err := ferry.Load[Certificate](ctx, src)
if err != nil {
	fmt.Println(err)

	return
}

fmt.Printf("%s\n", c.Cert)
// Output: -----BEGIN CERTIFICATE-----
```

`ferryhttp.Base64` is the spelling, and `ferryhttp.Gzip` and `ferryhttp.MaxSize` are payload steps stacked under it.
The step written last is closest to the payload and runs first on the way out, so that source caps the payload, compresses it and spells the result as base64.
A load undoes exactly that, and `ferryhttp.MaxSize` refuses in both directions - on the way out before anything is written, and on the way in once the payload is decompressed.

A spelling is a fact about the whole plane, because a request carries no type information for a driver to consult.
Declare one and every value that source reads is a payload, so a `string` or an `int` field over it is then a value the field cannot take.
Give the fields that are not payloads a source of their own.

## Refusals never quote what the request held

Everything this driver reads came off the wire.
A refusal names the parameter or field it is about and never the value, so an error may be logged and returned without leaking a token out of a query string or an `Authorization` header.
A parameter name minted by a map is part of the address and does appear, because ferry cannot name the address without it.

## There is no way to write back

This package loads only, so `ferry.Dump` through it does not compile rather than failing at run time.
Building an outbound request is a different job and belongs to a package written for it.

## More

The [package documentation](https://pkg.go.dev/github.com/onhotpath/ferry/driver/http) is the reference for every option above, and the design records behind them are in [`docs/adr/`](../../docs/adr/).
