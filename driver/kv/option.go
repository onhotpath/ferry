package kv

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Option is a setting handed to [NewSource] or [NewSink]. The set is closed at
// two: [WithPrefix] and [WithBatch].
type Option func(*config) error

// config is the resolved Option set one source or sink runs under.
type config struct {
	// prefix is the segments every address is placed under, and prefixSet is
	// what makes a second WithPrefix a refusal rather than a silent last-wins.
	prefix    []string
	prefixSet bool

	// batch is the whole of the batch-versus-lazy choice: one bool, read once
	// per open, which ferry never sees (ADR-0004).
	batch bool
}

// newConfig resolves an Option list, reporting every Option that was wrong
// rather than the first one.
func newConfig(opts []Option) (config, error) {
	var c config

	errs := make([]error, 0, len(opts))
	for _, o := range opts {
		errs = append(errs, apply(&c, o))
	}

	return c, errors.Join(errs...)
}

// apply runs one Option, refusing a nil one rather than panicking inside a
// constructor that was handed a list somebody built in a loop.
func apply(c *config, o Option) error {
	if o == nil {
		return errors.New("kv: the Option list holds a nil Option")
	}

	return o(c)
}

// WithPrefix places every key this driver reaches under these segments, so that
// a db.host field with WithPrefix("app", "cfg") reads the key "app/cfg/db/host".
//
// It takes one argument per level and never a path. WithPrefix("app/cfg") is
// refused up front, so a prefix cannot smuggle in a level you did not mean, and
// there is no way to spell a prefix that runs into the first key without a
// separator between them.
//
// It may be given once. Two prefixes are two key spaces and nothing in the call
// says which is meant, so a second one is refused rather than quietly winning.
func WithPrefix(segments ...string) Option {
	return func(c *config) error {
		switch {
		case len(segments) == 0:
			return errors.New("kv: WithPrefix was given no segments: omit the Option to place addresses at the " +
				"root of the store")
		case c.prefixSet:
			return errors.New("kv: the prefix is given twice: this driver places addresses under exactly one, " +
				"because two prefixes are two key spaces and nothing here chooses between them")
		}

		if err := prefixSegments(segments); err != nil {
			return err
		}

		// Cloned, so a later write into the caller's own slice cannot reach a
		// driver that has already been built from it.
		c.prefix, c.prefixSet = slices.Clone(segments), true

		return nil
	}
}

// prefixSegments holds a prefix to what a segment is, which is the same rule
// [nameable] holds an address's own segments to and one more: a prefix is the
// driver author's configuration rather than a user's data, so where an address
// segment holding a separator is a refusal the author cannot act on, a prefix
// segment holding one has an obvious repair and the message names it.
func prefixSegments(segments []string) error {
	for _, s := range segments {
		switch {
		case s == "":
			return errors.New("kv: a prefix segment is empty, and a key-value store has no name for one")
		case strings.Contains(s, separator):
			return fmt.Errorf("kv: the prefix segment %q contains %q: a prefix prepends a segment and never "+
				"concatenates text, so pass one argument per level instead of spelling the separator", s, separator)
		}
	}

	return nil
}

// WithBatch fetches the whole prefix in one call when the load starts, instead
// of one call per field as each is asked for.
//
//	src, err := kv.NewSource(store, kv.WithPrefix("app"), kv.WithBatch())
//
// Pick by what your store costs you. Batch is one round trip and a snapshot that
// cannot change under the load; per-key reads only the fields your struct
// actually names, which is cheaper against a store whose prefix holds far more
// than you want. The struct you get back is identical either way.
//
// It is a load-time Option. [NewSink] refuses it rather than ignoring it,
// because a save stages every write and commits them together, so there is no
// per-key half of it to choose against.
func WithBatch() Option {
	return func(c *config) error {
		c.batch = true

		return nil
	}
}
