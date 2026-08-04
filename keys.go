package ferry

import (
	"cmp"
	"errors"
	"strconv"
)

// KeyFunc is a mapping from a ferry address to a key in one plane's own key
// space: the join an environment driver spells with _, the dotted path a flat
// KV uses, the bracket form a query string wants.
//
// Core never produces a plane key, because a separator is plane knowledge and
// producing one would require core to know what the plane is. Flattening is the
// driver's, always (ADR-0003), and this is the type it is spelled in.
//
// # A key function answers legality and never injectivity
//
// Legality is what a key function reports an error for: whether the plane can
// name this address at all. An empty segment has no environment variable name
// and a segment holding a backslash has no Registry name, and no transformation
// rescues either. Injectivity - whether the transformation collapses two
// addresses into one - is not a question one call can answer, because one call
// cannot see a set. That is what [NewKeys] is for, and the two are different
// questions rather than two spellings of one.
//
// # A key function is expected to transform segment text, not to reject it
//
// An environment variable name may not contain a hyphen, so a key function that
// only validates refuses feature-flags, which is an ordinary thing to write in
// a config struct. One that maps the hyphen to _ accepts it and is not thereby
// less safe: a transformation is many-to-one, and a many-to-one map out of the
// address set is precisely what the injectivity check exists to catch. A driver
// that refuses to transform is not safer than one that does, only less useful.
//
// It is also the type core hands back from [Keys.Open], because what a driver
// wants at a lookup is the same shape it supplied at Bind: an address in, a
// checked plane key out.
type KeyFunc func(addr Path) (string, error)

// Keys is a driver's plane keys for one compiled schema, computed once and
// checked once, before any I/O.
//
// A driver builds one inside [Source.Bind] or [Sink.Bind], where it holds the
// whole address set and has not yet touched its plane, and calls [Keys.Open]
// once per load or per dump. Both checks ADR-0003 puts on a driver run at
// construction, over the whole static set with container addresses included:
// two containers rendering to one plane key return one merged subtree from
// Children, which is the same silent merge the rule exists to catch.
//
// # The static table is immutable, and reading it takes no lock
//
// Every key the address set determines is computed before this value is
// returned, and nothing writes to the table afterwards. That is what keeps the
// static tier at the cost ADR-0003 priced it at, a precomputed lookup against a
// bare map lookup, rather than the 109 ns of deriving a key per call, and it is
// what lets one binding be read from many goroutines with no synchronisation.
// The addresses a value mints live in the open instead, so nothing mutable is
// shared here.
//
// # A hand-rolled table opts out of both checks, silently
//
// A key function is ordinary Go and nothing obliges a driver to route its
// lookups through this type. A driver that builds its own map[Path]string
// discharges neither check, and gets no diagnostic saying so, because core is
// not in the call. That is a conformance-suite concern rather than something
// core can prevent: the suite hands a driver an address set its own transform
// folds together and asserts that Bind refuses before any I/O.
type Keys struct {
	// name is what the driver calls itself, and it appears in a refusal so
	// that a schema which is fine on one plane and impossible on another is
	// reported as that plane's problem rather than as ferry's.
	name string
	f    KeyFunc

	// static maps every address the type determined to its plane key, and
	// owner is that map inverted, which is what makes injectivity a map insert
	// rather than a pairwise comparison. Both are written before the value
	// leaves NewKeys and never again.
	static map[Path]string
	owner  map[string]Path
}

// NewKeys computes a driver's plane keys for one schema and checks them, and it
// is the whole of what ADR-0003 asks of a flattening driver.
//
// It takes the address set the driver's Bind was handed, the driver's own short
// name for its diagnostics, and its key function. It returns a value serving
// the static tier from a precomputed table, and an error naming every address
// the plane cannot name and every pair the key function collapses into one key.
// A driver returns that error from Bind unchanged; core supplies the moment and
// leaves the rest alone.
//
// Both refusals land before any I/O, which is what lets a plane-to-plane
// transfer be refused after zero backend calls rather than after reading the
// whole source. They are collected and sorted rather than reported one at a
// time, and each names both offending addresses:
//
//	ferry: 2 errors:
//	  /DB_HOST: env renders this address and /DB/HOST to one plane key, "DB_HOST", ...
//	  /feature_flags: env renders this address and /feature-flags to one plane key, ...
//
// A tree driver calls none of this. It walks the segments and builds no plane
// key at all, so it carries no injectivity obligation and pays nothing for the
// address set (ADR-0004).
func NewKeys(a *AddressSet, name string, f KeyFunc) (*Keys, error) {
	if f == nil {
		return nil, newError(momentBind, ErrPlane, Path{}, "the driver supplied no key function")
	}

	if a == nil {
		a = &AddressSet{}
	}

	k := &Keys{
		name:   cmp.Or(name, "the driver"),
		f:      f,
		static: make(map[Path]string, a.Len()),
		owner:  make(map[string]Path, a.Len()),
	}

	errs := make([]error, 0, a.Len())
	for addr := range a.All() {
		errs = append(errs, k.record(addr))
	}

	if err := join(errs...); err != nil {
		return nil, err
	}

	return k, nil
}

// record computes one static address's key and files it, refusing an address
// the plane cannot name and one whose key another address has already taken.
//
// The set arrives sorted segment-wise, so the address a collision is reported
// against is always the later of the pair and the report is the same on every
// run, which is ADR-0001's determinism invariant applied rather than re-decided.
func (k *Keys) record(addr Path) error {
	key, err := k.f(addr)
	if err != nil {
		// The driver said why, so ferry says where and lets the driver's own
		// text and its own class through (ADR-0011).
		return fromDriver(momentBind, addr, err)
	}

	if other, taken := takenBy(key, k.owner); taken {
		return newError(momentBind, ErrPlane, addr, k.collision(other, key))
	}

	k.static[addr] = key
	k.owner[key] = addr

	return nil
}

// Open starts one load or one dump over this table and hands back the key
// function for it.
//
// The static tier is served from the precomputed table. An address the type did
// not determine - a map key, a sequence index - is minted on demand and checked
// as it is minted, against the static table and against everything this open has
// already minted, before the write it belongs to. A legitimate map key is
// therefore answered rather than refused: core hands back a key function and not
// a map exactly because a map invites a driver to treat a miss as an error, and
// a static set of {/name} then refuses /labels/env for a map nobody got wrong
// (ADR-0004).
//
// # The minted set belongs to the open, and never to the binding
//
// Injectivity is a property of one write. Two writes to one plane at different
// times are not required to be mutually injective, and requiring it produces a
// refusal with no defect behind it: a caller holding one binding and dumping a
// map twice, each dump holding one of two keys the transform folds together, is
// refused on the second and told about an address no plane still holds. The
// retention is unbounded too, measured at 20,000 addresses held across 20,000
// loads through one binding (ADR-0012).
//
// So each call gets a fresh minted set, and nothing an open mints outlives it.
// The returned function is the open's and is not safe for concurrent use; the
// binding it came from is, because the table behind it never changes.
func (k *Keys) Open() KeyFunc {
	minted := map[Path]string{}
	owner := map[string]Path{}

	return func(addr Path) (string, error) {
		if key, ok := k.static[addr]; ok {
			return key, nil
		}

		if key, ok := minted[addr]; ok {
			return key, nil
		}

		return k.mint(addr, minted, owner)
	}
}

// mint issues a plane key for an address that came from a value, and refuses one
// that is not injective against everything already issued in this open.
//
// The refusal is a plain error rather than one of core's, deliberately. It
// travels out through the driver's Get or Set and back into core, which supplies
// the address, the moment and the provenance marker there; minting a *Error here
// would print ferry's prefix twice and claim a moment this function cannot know.
// The address is attached with ErrorAt so that a batch driver, which calls this
// at open with no address in core's hand, is located too.
func (k *Keys) mint(addr Path, minted map[Path]string, owner map[string]Path) (string, error) {
	key, err := k.f(addr)
	if err != nil {
		return "", ErrorAt(addr, err)
	}

	if other, taken := takenBy(key, k.owner, owner); taken {
		return "", ErrorAt(addr, errors.New(k.collision(other, key)))
	}

	minted[addr] = key
	owner[key] = addr

	return key, nil
}

// collision is the one message both tiers refuse with, because one rule covers
// separator collisions, case folding and any normalisation a driver invents:
// all three are the same failure, a non-injective map out of the address set.
//
// It names the plane key, which is safe under ADR-0011's rule against printing
// what the plane supplied: a plane key is computed from the address, and the
// address is already in the line.
func (k *Keys) collision(other Path, key string) string {
	return k.name + " renders this address and " + other.String() +
		" to one plane key, " + strconv.Quote(key) + ", so one of the two would be lost"
}

// takenBy reports which address already holds a key, over the tiers in the order
// they are checked.
func takenBy(key string, tiers ...map[string]Path) (Path, bool) {
	for _, tier := range tiers {
		if addr, ok := tier[key]; ok {
			return addr, true
		}
	}

	return Path{}, false
}
