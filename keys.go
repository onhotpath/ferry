package ferry

import (
	"cmp"
	"errors"
	"strconv"
	"strings"
)

// KeyFunc maps a ferry address to a key in one plane's own key space: the join
// an environment driver spells with _, the dotted path a flat KV uses, the
// hyphen join that spells an HTTP header name.
//
// A driver supplies one to [NewKeys] and gets one back from [Keys.Open], so the
// shape is the same at both ends: an address in, a checked plane key out. It
// takes the address with its kind dropped, because a plane key is a function of
// the segments and never of the kind: read one off a typed address with
// [Member.Path].
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

	// leaves and containers are owner split by what the key is for, and the
	// split is the check rather than bookkeeping (ADR-0003). A flat driver reads
	// a leaf's key and only ever uses a container's as a prefix, so two
	// addresses of different kinds landing on one key lose nothing, and
	// refusing them refuses a schema the plane can hold.
	leaves     map[string]Path
	containers map[string]Path
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

	k := &Keys{
		name:       cmp.Or(name, "the driver"),
		f:          f,
		static:     make(map[Path]string, a.Len()),
		owner:      make(map[string]Path, a.Len()),
		leaves:     make(map[string]Path, a.Len()),
		containers: make(map[string]Path, a.Len()),
	}

	errs := make([]error, 0, a.Len())
	for m := range a.Seq() {
		errs = append(errs, k.record(m))
	}

	if err := join(errs...); err != nil {
		return nil, err
	}

	if err := k.enumerable(a); err != nil {
		return nil, err
	}

	return k, nil
}

// record computes one static address's key and files it, refusing an address
// the plane cannot name and one whose key another address of its own kind has
// already taken.
//
// The kind is part of the question, because it is part of what the key is for.
// A flat driver reads a value at a leaf's key and never at a container's, and it
// uses a container's key as the prefix its members are named under, so a leaf
// and a container landing on one key are two addresses the plane still tells
// apart. What a container's key does reserve is checked by [Keys.enumerable].
//
// The set arrives sorted segment-wise, so the address a collision is reported
// against is always the later of the pair and the report is the same on every
// run, which is ADR-0001's determinism invariant applied rather than re-decided.
func (k *Keys) record(m Member) error {
	addr := m.Path()

	key, err := k.f(addr)
	if err != nil {
		// The driver said why, so ferry says where and lets the driver's own
		// text and its own class through (ADR-0011).
		return fromDriver(momentBind, addr, err)
	}

	kind := k.namespace(m)

	if other, taken := takenBy(key, kind); taken {
		return newError(momentBind, ErrPlane, addr, k.collision(other, key))
	}

	k.static[addr] = key
	k.owner[key] = addr
	kind[key] = addr

	return nil
}

// namespace is the one-key-per-address table this member's kind belongs to.
func (k *Keys) namespace(m Member) map[string]Path {
	if _, leaf := m.(LeafAddr); leaf {
		return k.leaves
	}

	return k.containers
}

// enumerable refuses an address whose key lies inside the key space a composite
// is enumerated out of.
//
// A composite's members come from the value, so a flat driver has no table to
// check them against: it lists every plane key beginning with the composite's
// own and reads what it finds as a member. An address of this schema that is not
// under the composite and whose key begins with the composite's key would
// therefore be enumerated as one of its members, which is one value read at two
// addresses and the same loss the injectivity check exists to prevent
// (ADR-0003).
//
// A section reserves nothing, because its members come from the type and a
// driver can ask about exactly those.
func (k *Keys) enumerable(a *AddressSet) error {
	scans := k.composites(a)
	if len(scans) == 0 {
		return nil
	}

	errs := make([]error, 0, a.Len())
	for m := range a.Seq() {
		errs = append(errs, k.reachable(m, scans))
	}

	return join(errs...)
}

// scan is one composite's address and the key its members are listed under.
type scan struct {
	at  Path
	key string
}

// composites is every composite in the set with the key it enumerates under. A
// composite whose key is empty names the whole plane and reserves nothing, since
// every key would lie inside it and no schema could be written at all.
func (k *Keys) composites(a *AddressSet) []scan {
	var out []scan

	for m := range a.Seq() {
		if _, ok := m.(CompositeAddr); ok && k.static[m.Path()] != "" {
			out = append(out, scan{at: m.Path(), key: k.static[m.Path()]})
		}
	}

	return out
}

// reachable refuses this member where some composite would enumerate its key.
func (k *Keys) reachable(m Member, scans []scan) error {
	key := k.static[m.Path()]

	for _, s := range scans {
		if s.at == m.Path() || !strings.HasPrefix(key, s.key) || s.at.isPrefixOf(m.Path()) {
			continue
		}

		return newError(momentBind, ErrPlane, m.Path(), k.name+
			" lists the members of "+s.at.String()+" out of every plane key beginning with "+
			strconv.Quote(s.key)+", and this address renders to "+strconv.Quote(key)+
			", so its value would be read back as a member of that composite")
	}

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
