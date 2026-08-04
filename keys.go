package ferry

import (
	"cmp"
	"errors"
	"strconv"
)

// KeyFunc maps a ferry address to a key in one plane's own key space: the join
// an environment driver spells with _, the dotted path a flat KV uses, the
// hyphen join that spells an HTTP header name.
//
// A driver supplies one to [NewKeys] and gets one back from [Keys.Open], so the
// shape is the same at both ends: an address in, a checked plane key out.
//
// A KeyFunc answers legality and never injectivity. Legality is what it returns
// an error for: whether the plane can name this address at all. An empty
// segment has no environment variable name, and no transformation rescues it.
// Whether the transformation collapses two addresses onto one key is not a
// question one call can answer, because one call cannot see a set; [NewKeys]
// answers that.
//
// A KeyFunc is expected to transform segment text rather than to reject it. An
// environment variable name may not contain a hyphen, so a key function that
// only validates refuses feature-flags, which is an ordinary thing to write in
// a config struct; one that maps the hyphen to _ accepts it and is no less
// safe, because the injectivity check is what catches a transformation that
// merges two addresses.
type KeyFunc func(addr Path) (string, error)

// Keys is a driver's plane keys for one compiled schema, computed once and
// checked once, before any I/O.
//
// A driver builds one with [NewKeys] inside [Source.Bind] or [Sink.Bind], where
// it holds the whole address set and has not yet touched its plane, and calls
// [Keys.Open] once per load or per dump.
//
// The table is written before the value is returned and never again, so reading
// it takes no lock and one binding is safe to use from many goroutines. The
// addresses a value mints live in the open instead.
//
// Nothing obliges a driver to route its lookups through this type, and a driver
// that builds its own map[Path]string gets neither check and no diagnostic
// saying so, because core is not in the call.
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

// NewKeys computes a driver's plane keys for one schema and checks them. It is
// the whole of what a flattening driver has to do with the address set it was
// bound to.
//
//	func (s Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
//	    keys, err := ferry.NewKeys(addrs, "env", s.key)
//	    if err != nil {
//	        return nil, err
//	    }
//	    ...
//	}
//
// It takes the address set, the driver's own short name for its diagnostics,
// and its [KeyFunc]. It returns a binding that serves those keys from a
// precomputed table, or an error naming every address the plane cannot name and
// every pair the key function collapses onto one key. Return that error from
// Bind unchanged; core supplies the rest.
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
// A tree driver calls none of this. It walks the segments, builds no plane key
// at all, and so carries no injectivity obligation.
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

// Open starts one load or one dump over this table and hands back the [KeyFunc]
// for it. Call it from the [OpenFunc] or [OpenWriterFunc], once per load or per
// dump.
//
// An address the type determined is served from the precomputed table. An
// address a value mints - a map key, a sequence index - is minted on demand and
// checked as it is minted, against the table and against everything this open
// has already minted, before the write it belongs to. So a legitimate map key
// is answered rather than refused, which is why core hands back a function and
// not a map: a map invites a driver to treat a miss as an error.
//
// Each call gets a fresh minted set, and nothing an open mints outlives it. Two
// dumps through one binding are not required to be mutually injective, only
// each within itself.
//
// The returned function belongs to the open and is not safe for concurrent use.
// The [Keys] it came from is, because the table behind it never changes.
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
