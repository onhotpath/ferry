# proto/184-keyform

A throwaway prototype for [#184](https://github.com/onhotpath/ferry/issues/184).

**This never merges.**
It lives on `proto/184-key-form`, outside the root `go.work`, in a module of its own.
`go build ./...` and `make check` at the repository root do not see it.

## What it is

Both candidate key functions for an HTTP query-parameter and header driver, built for real and run through the shipped machinery:

- `ferry.NewKeys`, so ADR-0003's legality and injectivity checks are the real ones.
- `ferry.Load` and `ferry.Dump` over working `Source` and `Sink` implementations for two planes, `url.Values` and `http.Header`, both taken from the context.
- `ferrytest.Driver`, the shipped conformance suite, over both planes in both forms.
- `net/url` and a real `httptest` client and server, for what actually goes on the wire.

Four forms are built, on two independent axes:

| | transforms (ADR-0003's stated posture, `driver/env`'s) | rejects (`driver/kv`'s) |
| --- | --- | --- |
| bracket, `db[host]` | `Bracket` | `BracketStrict` |
| flat join, `db.host` / `Db-Host` | `Flat` | `FlatStrict` |

`HeaderDepth1` is a fifth control: a header plane that refuses to nest at all.

## How to run it

```
cd proto/184-keyform
go test -v ./...
```

The suite is green. Where a form fails the conformance suite, the report is captured with a `ferrytest.T` recorder and asserted, because that failure is the finding.

## What it showed

1. **On the query plane the two forms are equivalent**, and the choice there is a convention, not a correctness question.
   Both pass `ferrytest.Driver`. Both round trip a three-level struct, a slice and a map. Each refuses exactly the schemas containing its own structural byte, and each silently loses exactly the map keys containing it. The residues are the same size and mirror images.

2. **On the header plane the bracket form is impossible.**
   `[` and `]` are not `tchar`, so they are not HTTP field-name characters. `net/http` refuses to send `db[host]`: `net/http: invalid header field name`. `textproto.CanonicalMIMEHeaderKey` leaves it alone rather than canonicalising it. The bracket form therefore refuses every address of depth two or more on the header plane, and fails `ferrytest.Driver` with three cases.

3. **The header plane does nest, and the hyphen is how.**
   `X-Forwarded-For` and `X-Forwarded-Proto` are the registry's own spelling of a nested `x-forwarded` object. The flat hyphen join produces them exactly; the bracket form and the depth-1 control both refuse them.

4. **The header join must transform rather than reject.**
   A strict hyphen join refuses `x-request-id`, which is the most ordinary header a config struct names.

5. **Go's parser gives brackets no meaning at all.**
   `?tags[0]=a&tags[1]=b` parses to two keys with one value each, `?tags=a&tags=b` to one key with two. `Values.Encode` percent-encodes a bracket to `%5B`/`%5D`, which round trips.

6. **Neither form reads what a plain client actually sends for a slice.**
   `?tags=a&tags=b` is refused under both, because `ferry.KeyFunc` returns a string and cannot address the second dimension of a `map[string][]string`.
