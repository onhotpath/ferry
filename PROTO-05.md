# Prototype: session 05, watch/reload (#13) and tag-grammar extension (#34)

Two modules, both deliberately out of the root `go.work`.

## `prototype/watcher` - #13's A0 evidence

Runs against the REAL shipped ferry module via its own nested `go.work` (no `replace`, root workspace untouched):

```sh
cd prototype/watcher
go test -race .
```

What it proves, 4 tests green under `-race`:

1. **A watcher builds entirely outside core.**
   Bind once, reload per signal, publish a fresh value - ~30 lines over the shipped surface, zero core changes.
   The change signal is the driver's own (a channel here; fsnotify or a Consul watch plan in real drivers), exactly ADR-0001's split.
2. **Both candidate delivery shapes implemented side by side** and exercised on the same failure:
   `Watch` is jba's `(iter.Seq[T], func() error)` - the compiler warns on a discarded `errf`;
   `Watch2` is `iter.Seq2[T, error]` with adonovan's four questions answered in its doc comment - and the test shows the second range variable can be silently dropped.
3. **A held value never mutates when the plane changes** - reload is replacement, ADR-0006 upheld through the shipped surface.
4. **The LoadOver-as-reload sharp edges, pinned as facts**: an address the plane lost keeps its stale value (the ADR-0006 leak), and a map composite is replaced wholesale, not merged.
   A fresh `Load` does neither.
   Any watch guide must say this out loud.

## `prototype/tagext` - #34's opening-shape evidence

Self-contained miniature of core's parse path (`GOWORK=off go test .`), 5 tests green:

1. **Inert, structurally**: ferry's parsed tag is byte-identical with and without a declared extension; extension values land only in an address-keyed table handed back to the declarer (#156's consumer shape).
2. **Namespace-prefixed words** (`mylib.retry=3`): no future ferry word can collide with a declared one; bare words stay reserved for ferry even when unused.
3. **A typed declaration reduces to a canonical, comparable `Decl`** - build-time hashability asserted with core's own `map[...]struct{}{}` trick; declaration order does not mint a second cache entry.
4. **Diagnostics stay first-class**: the near-miss table covers declared words (`mylib.rerty` → `did you mean "mylib.retry"?`) without degrading ferry's own; an UNDECLARED extension word still refuses - `TestTagKeyKeepsTheVocabularyShut`'s rule survives opening.
5. **Collisions refuse at Declare** - once, before any tag parses: duplicate namespaces, punctuated namespaces, value-shape mismatches.
