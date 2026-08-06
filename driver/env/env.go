package env

import (
	"context"
	"errors"
	"fmt"
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

	declared, err := declaredLeaves(addrs, keys)
	if err != nil {
		return nil, err
	}

	cfg := s.cfg

	return func(context.Context) (ferry.Reader, error) {
		return &reader{cfg: cfg, keys: keys.Open(), declared: declared, env: environMap(cfg.environ())}, nil
	}, nil
}

// declaredLeaves is the classification ADR-0016 puts at Bind: one range over the
// typed address set, one type switch, and the answer held before any I/O.
//
// Only the leaves are kept, because they are the only kind whose bit this driver
// branches on later. A leaf the type determined is an address the schema
// declared, and one that is not in this table was minted from the value by
// [reader.Children]; the two get different answers when the environment holds
// nothing at the name, and [reader.Get] says why.
//
// It is built once per Bind and never written to afterwards, which is what lets
// one binding be read from many goroutines with no synchronisation.
func declaredLeaves(addrs *ferry.AddressSet, keys *ferry.Keys) (map[ferry.LeafAddr]string, error) {
	out := make(map[ferry.LeafAddr]string, addrs.Len())
	name := keys.Open()

	for m := range addrs.Seq() {
		leaf, ok := m.(ferry.LeafAddr)
		if !ok {
			continue
		}

		key, err := name(leaf.Path())
		if err != nil {
			// Unreachable: NewKeys computed a name for every address in this
			// set already, and the table answers a declared address without
			// consulting the key function again. It is returned rather than
			// ignored because a driver that swallows an error here would be
			// deciding that core was wrong.
			return nil, err
		}

		out[leaf] = key
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
	cfg      config
	keys     ferry.KeyFunc
	declared map[ferry.LeafAddr]string
	env      map[string]string
}

// The optional interfaces this reader carries. Enumeration is one of them
// because listing the environment is trivial, and it is what makes a map-typed
// or slice-typed field loadable from this plane at all (ADR-0004). Probing is
// the other, and it is what lets a section be reported present without a value
// ever being read at its own name (ADR-0016). There is no [ferry.Releaser],
// because a map holds no resource and a Close that returns nil is
// indistinguishable in the source from a release somebody forgot.
var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Prober     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
)

// Get answers with what the environment holds at one leaf.
//
// The answer is a String or an Absent and never a Null, because FOO= is a
// zero-length string and not a null: the plane has no type information of its
// own, so every value it holds is text, and the one distinction it does carry -
// set against unset - is the one a required field tests.
//
// It refuses one shape rather than answering it. A leaf this driver minted from
// the environment, whose own name holds nothing while names below it do, is the
// plane holding a section where the schema wants a value. Answering Absent there
// would fill the field with the Go zero and drop what the environment actually
// held, so the address is refused instead.
func (r *reader) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	key, declared := r.declared[addr]
	if !declared {
		var err error
		if key, err = r.keys(addr.Path()); err != nil {
			return ferry.Value{}, err
		}
	}

	if text, ok := r.env[key]; ok {
		return ferry.String(text), nil
	}

	if !declared && r.holdsBelow(key) {
		return ferry.Value{}, deeperThanLeaf()
	}

	return ferry.Value{}, nil
}

// holdsBelow reports whether the environment holds any name strictly below this
// one, which is what makes a minted leaf a section the schema has no room for.
func (r *reader) holdsBelow(key string) bool {
	below := key + r.cfg.sep

	for name := range r.env {
		if strings.HasPrefix(name, below) {
			return true
		}
	}

	return false
}

// deeperThanLeaf is the refusal #235 needed and could not have while a container
// address and a leaf address were the same question.
//
// It fires only at a minted address, and that scoping is the rule rather than a
// convenience. At an address the schema declared, a variable that merely shares
// a prefix is an unrelated variable and not this schema's business, and refusing
// there would fail a legal load over the ambient environment - which is the #219
// class in a new place. At a minted one the driver chose the address by reading
// the environment, so a name below it and nothing at it is the driver having
// invented a child over a value it was about to drop.
//
// It names no name. ADR-0011 keeps a value the plane supplied out of ferry's
// message text, and the tail of the offending variable is a dynamic segment,
// which is the caller's value; core attaches the address, which is structure.
func deeperThanLeaf() error {
	return fmt.Errorf("%w: %w: the environment holds nothing at this name and holds names below it, "+
		"so the plane carries a section where the schema carries a value: the addresses under a container "+
		"come from the environment, and one that reaches deeper than an element is not an element",
		ferry.ErrPlane, ErrDeeperThanLeaf)
}

// ErrDeeperThanLeaf reports a value the environment holds below an address the
// schema maps to a single value.
//
// LIMITS_HTTP_PORT under a map[string]string at limits is the case: there is no
// LIMITS_HTTP to read, so the only honest answers are this refusal or a map
// entry holding the Go zero with the real value dropped.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrDeeperThanLeaf = errors.New("env: the environment reaches deeper than this address")

// Probe answers whether the environment holds anything under a container's own
// name.
//
// A section is present when any variable lies below its name and absent
// otherwise, which is the only distinction a flat plane carries: nothing is ever
// written at a container's own name, so its presence is the presence of its
// members. This plane has no null, so a container is never reported null.
func (r *reader) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	key, err := r.keys(addr.Path())
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	if r.holdsBelow(key) {
		return ferry.SectionPresent, nil
	}

	return ferry.SectionAbsent, nil
}
