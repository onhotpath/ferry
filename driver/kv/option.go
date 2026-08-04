package kv

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Option is a setting handed to [NewSource] or [NewSink].
//
// The set is closed, because the config it writes to is unexported: every
// Option is one this driver decided on, and a caller who needs a different key
// space writes a [Client] that has one rather than a fourth Option here.
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

// WithPrefix places every address this driver reaches under these segments, so
// that /db/host with WithPrefix("app", "cfg") is the key "app/cfg/db/host".
//
// It takes segments and never a key, and that is ADR-0003's rule rather than a
// signature preference:
//
//	Under a structured address a prefix can only prepend a segment.
//
// xload's prefix is text concatenation onto a flat key, so prefix "DB_" with
// key "HOST" gives DB_HOST, prefix "DB" gives DBHOST, and prefix "DB_" with key
// "_HOST" gives DB__HOST. All three are legal and two are typos nothing can
// detect, because the separator is not part of the model. Here it is: a segment
// spelling the separator is refused at this call, so the concatenated form is
// not merely discouraged but unexpressible, and a two-level prefix is spelled
// as two arguments.
//
// It may be given once. Two prefixes are a precedence question wearing a
// convenience costume, and nothing in the call says which is meant.
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

// WithBatch fetches the whole plane in one call when a reader is opened,
// instead of one call per address as each is asked for.
//
// It is the batch-versus-lazy choice ADR-0004 keeps inside the driver, and it
// is one bool: Bind was handed the whole address set before any I/O, so an open
// may fetch everything or fetch nothing, and ferry never learns which. Measured
// on a three-address schema: three backend calls lazily and one in batch, with
// identical results.
//
// Which one to want is a property of the plane and not of ferry. Batch is one
// round trip and a snapshot that cannot change under the walk; lazy reads only
// the addresses the walk actually reaches, which is the cheaper of the two
// against a store whose prefix holds far more than this schema names.
//
// It is a source's Option. [NewSink] refuses it, because a sink stages every
// write and commits them together, so there is no lazy half for it to choose
// between.
func WithBatch() Option {
	return func(c *config) error {
		c.batch = true

		return nil
	}
}
