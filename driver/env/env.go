package env

import (
	"context"
	"strings"

	"github.com/onhotpath/ferry"
)

// Source is the environment as a ferry plane, read side.
//
//	cfg, err := ferry.Load[Config](ctx, env.New())
//
// There is no env.Sink beside it. This package loads only, so [ferry.Dump]
// through it is a compile error at the call site rather than a failure at run
// time.
//
// A Source is safe for use from many goroutines, and so is a binding it hands
// back: the names a binding holds are computed once, at Bind, and nothing writes
// to them afterwards.
//
// The zero Source has no environment to read and no separator to join with, so
// it refuses at Bind rather than guessing. Build one with [New].
type Source struct {
	cfg config
}

// Source is the whole of what this package implements, and the absence of
// [ferry.Sink] beside it is the point rather than an omission.
var _ ferry.Source = (*Source)(nil)

// New builds a [Source] over the process environment.
//
//	cfg, err := ferry.Load[Config](ctx, env.New())
//
// With no options it joins nested fields with [DefaultSeparator], returns map
// keys in [Lower] case, and reads the process environment. Change any of the
// three with [Separator], [Canonical] and [Environ].
func New(opts ...Option) *Source {
	c := defaults()
	for _, o := range opts {
		o.apply(&c)
	}

	return &Source{cfg: c}
}

// Bind computes this schema's environment variable names and checks them, and it
// is where a schema this plane cannot hold is refused.
//
// Two things are checked, before any environment is read: that every field has
// an environment variable name at all, and that no two fields fold to the same
// name. A schema failing either is refused here, in one error that names every
// offending field along with the one it collided with.
//
// It does no I/O, so it succeeds whatever the environment holds, and a Source
// built with an option it cannot use is refused here rather than at the first
// read.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if err := s.cfg.validate(); err != nil {
		return nil, err
	}

	keys, err := ferry.NewKeys(addrs, driverName, s.cfg.key)
	if err != nil {
		return nil, err
	}

	static, err := staticNames(addrs, keys)
	if err != nil {
		return nil, err
	}

	cfg := s.cfg

	return func(context.Context) (ferry.Reader, error) {
		return &reader{cfg: cfg, keys: keys.Open(), static: static, env: environMap(cfg.environ())}, nil
	}, nil
}

// staticNames is the precomputed table read backwards: every name the type
// determined, mapped to the address that determined it.
//
// It is what makes the static tier of enumeration exact. This driver's key
// function is many-to-one over segment text, so a name cannot be parsed back
// into an address in general - but an address the schema determined is in the
// set, so matching a name against this table recovers the segment's own
// spelling rather than a fold of it. Only what the value mints has to fall back
// on [Canonical].
//
// It is built once per Bind and never written to afterwards, which is what lets
// one binding be read from many goroutines with no synchronisation.
func staticNames(addrs *ferry.AddressSet, keys *ferry.Keys) (map[string]ferry.Path, error) {
	if addrs == nil {
		return map[string]ferry.Path{}, nil
	}

	out := make(map[string]ferry.Path, addrs.Len())
	name := keys.Open()

	for addr := range addrs.All() {
		key, err := name(addr)
		if err != nil {
			// Unreachable: NewKeys computed a name for every address in this
			// set already, and the table answers a static address without
			// consulting the key function again. It is returned rather than
			// ignored because a driver that swallows an error here would be
			// deciding that core was wrong.
			return nil, err
		}

		out[key] = addr
	}

	return out, nil
}

// environMap turns an environ slice into the lookup one open reads from.
//
// An entry with no "=" is not a variable and is skipped, and so is one with an
// empty name: Windows carries entries such as "=C:=C:\\" in its environment
// block, and neither is a name any address renders to.
func environMap(environ []string) map[string]string {
	out := make(map[string]string, len(environ))

	for _, entry := range environ {
		if name, value, ok := strings.Cut(entry, "="); ok && name != "" {
			out[name] = value
		}
	}

	return out
}

// reader is one open over one snapshot of the environment.
//
// The snapshot is taken at open rather than per lookup, so one load sees one
// consistent environment: a variable that changes half way through a walk would
// otherwise land in some fields and not others, with nothing saying so.
type reader struct {
	cfg    config
	keys   ferry.KeyFunc
	static map[string]ferry.Path
	env    map[string]string
}

// The optional interfaces this reader carries. Enumeration is one of them
// because listing the environment is trivial, and it is what makes a map-typed
// or slice-typed field loadable from this plane at all (ADR-0004). There is no
// [ferry.Releaser], because a map holds no resource and a Close that returns nil
// is indistinguishable in the source from a release somebody forgot.
var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
)

// Get answers with what the environment holds at an address.
//
// The answer is a String or an Absent and never a Null, because FOO= is a
// zero-length string and not a null (ADR-0004): the plane has no type
// information of its own, so every value it holds is text, and the one
// distinction it does carry - set against unset - is the one a required field
// tests.
//
// At a container address that is ordinarily Absent, since a composite is stored
// one element at a time and nothing is written at the container's own name.
func (r *reader) Get(_ context.Context, addr ferry.Path) (ferry.Value, error) {
	key, err := r.keys(addr)
	if err != nil {
		return ferry.Value{}, err
	}

	text, ok := r.env[key]
	if !ok {
		return ferry.Value{}, nil
	}

	return ferry.String(text), nil
}
