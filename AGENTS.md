# ferry

`ferry` is a bidirectional, struct-first data mapper for Go.
One annotated struct, one tag grammar, two directions:

- **Load**: pluggable source -> struct (env vars, YAML, HTTP query params, Consul, Vault, anything)
- **Dump**: struct -> pluggable sink (a config struct written back out to a file, a KV store, env)

## Nothing about the API is decided

Treat every one of these as open:

- the source and sink interface signatures
- the tag grammar and its options
- whether keys are flat strings or structured paths
- the encoder interface and its precedence rules
- package layout and how providers are distributed
- the exported verb names

The answer to any of them is "undecided, see `docs/adr/`".
Do not guess, and do not carry another library's answers over as though they were ferry's.

## Prior art

- `github.com/gojekfarm/xtools/xload` is the direct ancestor.
  It covers the Load direction only, via `Loader.Load(ctx, key string) (string, error)`, driven by struct tags such as `env:"HOST"` and `env:",prefix=DB_"`.
  ferry is a new library, not a fork.
  Its ancestry is a starting point for discussion, not an inherited API.
- Blog post on xload's model: https://ajatprabha.in/2024/07/07/xload-ultimate-data-loader-go-structs
- A prior-art sweep of the Go ecosystem found no existing library that drives both directions off one tag grammar over pluggable backends.
  Read `docs/` for the recorded findings rather than re-running that research.

## Where decisions live

Architectural decisions are recorded as ADRs in `docs/adr/`, numbered sequentially (`0001-slug.md`).
Read the relevant ADR before proposing a design change.
If a proposal contradicts an existing ADR, say so explicitly rather than quietly overriding it.
If `docs/adr/` is absent, or has no entry on the topic, that decision has not been made yet.

## Issues

Issues live in this repo's GitHub Issues.
Use the `gh` CLI.

## Standing rules

- Never use an em dash. Use a plain dash instead.
- In Markdown, put each sentence on its own line. Keep normal Markdown structure otherwise.
- Never add a `Co-Authored-By` trailer to commit messages.
- Never put Claude Code session URLs in PR descriptions or commit messages.
- Be concise. Favour quality, simplicity, and long-term maintainability over speed.
