package perschema

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/onhotpath/ferry"
)

// ErrDeclaration reports a per-schema declaration this schema does not satisfy.
var ErrDeclaration = errors.New("header: the schema does not satisfy a declaration this Source carries")

// ErrRepeated reports a name the plane holds a sequence at that nothing read as
// one.
var ErrRepeated = errors.New("header: a declared sequence was read at an address that is not one")

// ErrIllegal reports an address this plane cannot name.
var ErrIllegal = errors.New("header: address has no field name on this plane")

// ErrRequired reports a declared-required name the plane holds nothing at.
var ErrRequired = errors.New("header: the plane holds nothing at a name this Source declared required")

// Option is one piece of a Source's configuration.
//
// Five of them, spanning the two axes the proposed rule conflates. The
// "checkable" column is whether a declaration that is wrong for the schema can
// be detected at Bind from the AddressSet alone.
//
//	option        asserts                              checkable   wrong is
//	Repeatable    the Go type behind a name is a seq   no          silent
//	Audited       (Repeatable, reported at Close)      no          loud
//	Alias         which plane key an address reads     yes         silent
//	Required      the plane must hold this name        yes         loud
//	Fallback      a value to supply where absent       yes         silent
type Option func(*config)

// Repeatable declares that the address rendering to this plane key carries a
// sequence, so Get at it answers Absent and Children mints one position per
// value the plane holds.
//
// This is #210's mechanism verbatim, and the configuration the rule was written
// to exclude.
func Repeatable(keys ...string) Option {
	return func(c *config) {
		for _, k := range keys {
			c.repeatable[k] = true
			c.order = append(c.order, k)
		}
	}
}

// Audited makes every declaration-induced Absent that nothing enumerated a
// refusal at Close.
//
// It changes nothing about what is checkable at Bind. It changes only whether a
// declaration that is wrong for this schema is observable at all.
func Audited() Option { return func(c *config) { c.audited = true } }

// Alias declares that the address rendering to name is read from the plane key
// planeKey instead.
//
// It asserts nothing about the Go type. It goes through the KeyFunc, so
// ferry.NewKeys sees the renamed key space and checks it.
func Alias(name, planeKey string) Option {
	return func(c *config) {
		c.alias[canon(name)] = canon(planeKey)
		c.order = append(c.order, name)
	}
}

// Required declares that the plane must hold a value at this name, and the
// reader refuses where it does not.
func Required(keys ...string) Option {
	return func(c *config) {
		for _, k := range keys {
			c.required[canon(k)] = true
			c.order = append(c.order, k)
		}
	}
}

// Fallback declares a value to supply at this name where the plane holds none.
func Fallback(name, text string) Option {
	return func(c *config) {
		c.fallback[canon(name)] = text
		c.order = append(c.order, name)
	}
}

// Prefix is ORDINARY PLANE CONFIGURATION, of exactly the kind ADR-0004's
// lifetime table blesses: every address reads from the plane key with this
// prefix on it. It names no schema and knows of none.
//
// It is here as the control. If it lands in the same cell of the experiment as
// a per-schema declaration, then the cell is not a defect criterion.
func Prefix(p string) Option { return func(c *config) { c.prefix = canon(p) } }

// PinSchema makes the Source refuse any address set that is not the first one
// it was bound to, which is the only self-enforcement a Source can attempt: it
// cannot count Binds, because a one-shot ferry.Load binds on every call.
func PinSchema() Option { return func(c *config) { c.pin = true } }

// CheckNames turns on the Bind-time check that every declared name is one this
// schema has an address for, which is the whole of what the proposed rule calls
// checkable.
func CheckNames() Option { return func(c *config) { c.checkNames = true } }

// Trace collects every boundary call in order.
func Trace(into *[]string) Option { return func(c *config) { c.trace = into } }

// config is a Source's whole per-schema configuration.
type config struct {
	repeatable map[string]bool
	alias      map[string]string
	required   map[string]bool
	fallback   map[string]string

	// order is every declared name in the order it was given, so a Bind-time
	// refusal names what it did not find, deterministically.
	order []string

	prefix string

	audited    bool
	checkNames bool
	pin        bool

	trace *[]string
}

func newConfig(opts []Option) config {
	c := config{
		repeatable: map[string]bool{},
		alias:      map[string]string{},
		required:   map[string]bool{},
		fallback:   map[string]string{},
	}

	for _, o := range opts {
		o(&c)
	}

	return c
}

// declared is every name this configuration mentions, canonicalised, sorted.
func (c config) declared() []string {
	set := map[string]bool{}

	for k := range c.repeatable {
		set[canon(k)] = true
	}

	for k := range c.alias {
		set[k] = true
	}

	maps.Copy(set, c.required)

	for k := range c.fallback {
		set[k] = true
	}

	out := slices.Collect(maps.Keys(set))
	sort.Strings(out)

	return out
}

// checkAgainst is the whole of what a driver can check at Bind: every declared
// name is one this schema has an address for.
//
// This is the "name-exists half" the proposed rule calls checkable. What it
// cannot check is what the declaration asserts about that address, because an
// AddressSet is a set of Paths and a Path carries no arity and no Go kind.
func (c config) checkAgainst(static map[string]ferry.Path) error {
	if !c.checkNames {
		return nil
	}

	for _, n := range c.declared() {
		if _, ok := static[n]; !ok {
			return fmt.Errorf("%w: %w: no address in this schema renders to %q",
				ferry.ErrPlane, ErrDeclaration, n)
		}
	}

	return nil
}
