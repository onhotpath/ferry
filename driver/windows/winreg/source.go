package winreg

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/onhotpath/ferry"
)

// Source is the read half of a registry plane.
//
//	src := winreg.NewSource(winreg.LocalMachine, `SOFTWARE\Example`)
//	cfg, err := ferry.Load[Config](ctx, src)
//
// It is a separate type from [Sink], so a round trip names the key twice. The
// repetition buys the refusal being a compile error: code handed only a Source
// cannot save through it.
//
// One Source may be used by many loads at once, from many goroutines, and so may
// a binding it hands back: the keys a binding holds are computed once, at Bind,
// and nothing writes to them afterwards.
type Source struct {
	cfg config
}

var _ ferry.Source = (*Source)(nil)

// NewSource builds a source over one subkey of one hive.
//
//	src := winreg.NewSource(winreg.CurrentUser, `Software\Example`)
//	cfg, err := ferry.Load[Config](ctx, src)
//
// The subkey is a path under the hive and may be empty, which is the hive itself.
// Every address is read at or under it.
//
// With no options it reads the machine's own registry in the view the running
// process would get. Change either with [Store] and [WithView].
//
// It touches nothing, and starts nothing, unless it is given [Watch]. That is the
// one setting that does something before a load: it opens a change notification
// here, on the caller's own goroutine, and watches from a goroutine of its own
// until the context it was given is done. A watch that cannot be opened is
// reported at Bind, because this call returns no error.
func NewSource(hive Hive, subkey string, opts ...Option) *Source {
	s := &Source{cfg: newConfig(hive, subkey, func(c *config) {
		for _, o := range opts {
			o.apply(c)
		}
	})}

	startWatch(opts, &s.cfg)

	return s
}

// Bind computes this schema's registry keys and checks them, and it is where a
// schema this plane cannot hold is refused.
//
// Two things are checked, before anything is read: that every address has a
// registry name at all, and that no two of one kind fold to the same name. The
// registry is case-insensitive, so /Host and /host are one value there and a
// schema naming both is refused here, naming both, rather than silently losing
// one of them.
//
// It does no I/O, so it succeeds whatever the registry holds, and a source built
// with an option it cannot use is refused here rather than at the first read.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	if err := s.cfg.validate(); err != nil {
		return nil, err
	}

	keys, err := ferry.NewKeys(addrs, driverName, key)
	if err != nil {
		return nil, err
	}

	declared := declaredLeaves(addrs)
	cfg := s.cfg

	return func(ctx context.Context) (ferry.Reader, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		return &reader{cfg: cfg, names: keys, key: keys.Open(), declared: declared}, nil
	}, nil
}

// readFailed is the one sentence every failed read is reported under, so that a
// registry that could not be reached reads the same whichever question was
// being asked of it.
const readFailed = "winreg: reading the registry: %w"

// declaredLeaves is the classification ADR-0016 puts at Bind: one range over the
// typed address set, one type switch, and the answer held before any I/O.
//
// Only the leaves are kept, and only whether each is there, because that is the
// one bit this driver branches on later. A leaf the type determined is an address
// the schema declared, and one that is not in this table was minted from the
// registry by [reader.Children]; the two get different answers when the registry
// holds no value at the name, and [reader.Get] says why.
//
// It is built once per Bind and never written to afterwards, which is what lets
// one binding be read from many goroutines with no synchronisation.
func declaredLeaves(addrs *ferry.AddressSet) map[ferry.LeafAddr]bool {
	out := make(map[ferry.LeafAddr]bool, addrs.Len())

	for m := range addrs.Seq() {
		if leaf, ok := m.(ferry.LeafAddr); ok {
			out[leaf] = true
		}
	}

	return out
}

// reader is one open read side.
//
// It implements [ferry.Enumerator] because the registry lists a key's values and
// its subkeys trivially, and [ferry.Prober] because a container here is a subkey
// outright: it is there or it is not, and nothing has to be inferred from what
// lies under it. It implements no [ferry.Releaser], because it holds no resource:
// every call opens the one key it needs and closes it again, which is also what
// makes [ferry.Concurrent] cost nothing but the lock below.
type reader struct {
	cfg config

	// mu guards key and nothing else.
	//
	// A [ferry.KeyFunc] belongs to its open and is not safe for concurrent use:
	// minting an address a value produced writes the open's own minted set, and
	// per open is not per goroutine (ADR-0012). Declaring [ferry.Concurrent] is a
	// promise about everything the instance reaches, so the lock is this driver's
	// obligation rather than core's (ADR-0019).
	mu sync.Mutex

	// names is the binding's checked key table, held for the reports rather than
	// for the reads: it answers what this plane calls an address without minting
	// anything, so it needs no lock and is outside everything mu guards
	// (ADR-0011, ADR-0019).
	names *ferry.Keys

	// key is this open's key function, and everything it mints belongs to this
	// open (ADR-0012). Every call to it goes through [reader.check], and every
	// read goes through one of those, which is what puts a minted map key under
	// the injectivity check rather than beside it.
	key ferry.KeyFunc

	declared map[ferry.LeafAddr]bool
}

var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Prober     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
	_ ferry.Concurrent = (*reader)(nil)
	_ ferry.PlaneNamer = (*reader)(nil)
)

// PlaneName is the registry key an address is read from, hive and subkey
// included, which is what a report opens with in place of the address: /db/host
// prints as HKEY_LOCAL_MACHINE\SOFTWARE\Example\db\host.
//
// It goes through the table and never through this open's key function, so it
// records nothing, needs no lock and cannot refuse (ADR-0011). The name is in the
// folded spelling the key function produced, which is a name regedit finds
// whatever case the value was first written in.
func (r *reader) PlaneName(addr ferry.Path) (string, bool) {
	k, ok := r.names.PlaneName(addr)
	if !ok {
		return "", false
	}

	return r.cfg.name(k), true
}

// MaxConcurrent reports that this reader tolerates overlapping calls and imposes
// no bound of its own, so a caller's [ferry.MaxConcurrency] stands alone.
//
// Zero rather than a number, because the bound that exists is not this package's
// to name: every read is a key opened, queried and closed, and how many of those
// the machine will take at once is a fact about the machine.
//
// What it commits this package to is that everything an open reaches is safe from
// many goroutines at once. No registry handle is shared between them, the
// declared table is written before the reader exists, the key function is
// serialised in [reader.check], and what is left is the [Registry] - which this
// package already requires to be safe for use from many goroutines.
func (*reader) MaxConcurrent() int { return 0 }

// check runs one address through this open's key function, and it is the one
// place that function is entered.
//
// The key it computes is discarded, because the subkey and the value name a read
// needs come from the address's own segments rather than from the folded key.
// What the call is for is the check: an address a value minted is checked against
// the static table and against everything this open has already minted as it is
// minted, and a driver that computed its own pair from the path would get no
// check at all (ADR-0003, ADR-0012).
//
// The lock is what declaring [ferry.Concurrent] costs, and it is held over the
// call and never over the registry, so nothing waits here for I/O (ADR-0019).
func (r *reader) check(at ferry.Path) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.key(at)

	return err
}

// Get answers with the value the registry holds at one leaf.
//
// A leaf is a value under a subkey, so /db/host is the value host under db and
// nothing about a subkey named host says anything about it: the registry keeps
// values and subkeys in two namespaces and this driver keeps them apart.
//
// The kinds a value comes back as are what the registry recorded: REG_SZ and
// REG_EXPAND_SZ are text, REG_DWORD and REG_QWORD are numbers, and REG_BINARY is
// bytes. REG_EXPAND_SZ is read exactly as it is stored and its %VARIABLES% are
// never expanded. REG_MULTI_SZ is refused, because it spells a sequence inside
// one value and ferry addresses each element of a sequence in its own right.
//
// A value the registry does not hold is Absent, with one exception it refuses
// instead: an address this driver minted out of the registry, where the value is
// not there and a subkey of that name is, is the registry holding a group of
// values where the field takes a single one. Answering Absent there would fill
// the field with the Go zero and drop what the registry actually held.
func (r *reader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if err := ctx.Err(); err != nil {
		return ferry.Value{}, err
	}

	if err := r.check(addr.Path()); err != nil {
		return ferry.Value{}, err
	}

	at := placeOf(addr.Path())

	d, found, err := r.cfg.store.Get(ctx, at.subkey, at.name)
	if err != nil {
		return ferry.Value{}, fmt.Errorf(readFailed, err)
	}

	if !found {
		return r.absent(ctx, addr)
	}

	return held(d)
}

// absent is the answer for a value the registry does not hold, and the one place
// a minted address is told apart from a declared one.
//
// The scoping is the rule rather than a convenience. At an address the schema
// declared, a subkey that merely shares the name is an unrelated subkey and not
// this schema's business. At a minted one the driver chose the address by listing
// the registry, so a subkey there and no value is the driver having invented a
// member over something it was about to drop.
func (r *reader) absent(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if r.declared[addr] {
		return ferry.Value{}, nil
	}

	_, found, err := r.cfg.store.List(ctx, subkeyOf(addr.Path()))
	if err != nil {
		return ferry.Value{}, fmt.Errorf(readFailed, err)
	}

	if found {
		return ferry.Value{}, deeperThanLeaf()
	}

	return ferry.Value{}, nil
}

// ErrDeeperThanLeaf reports a subkey the registry holds where the schema maps a
// single value.
//
// A map[string]string over a key holding the subkey http, with no value of that
// name beside it, is the case: the members of a container are whatever the key
// holds, and one that is a subkey is a group of values rather than a value.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] returned.
var ErrDeeperThanLeaf = errors.New("winreg: the registry holds a subkey where this address takes a value")

// deeperThanLeaf names no name, for the reason [nameable] gives.
func deeperThanLeaf() error {
	return fmt.Errorf("%w: %w: the registry holds no value at this name and holds a subkey of it, so it holds a "+
		"group of values where the field takes a single one: the members of a container are what the key holds, "+
		"and a subkey is not one of them", ferry.ErrPlane, ErrDeeperThanLeaf)
}

// Probe answers whether the registry holds the subkey a container's own address
// names.
//
// A container here is a subkey outright, so this is exact rather than inferred: a
// section is present when its subkey is there and absent when it is not, and a
// value that merely shares the name says nothing either way. A subkey that exists
// and holds nothing is present, which is how a section that is there and empty
// survives a round trip.
//
// It is never null. A subkey exists or it does not, and this driver spends
// "exists and holds nothing" on present, so there is nothing left for a null to
// be spelled as. [writer.Ensure] refuses one for the same reason.
func (r *reader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	found, err := r.holds(ctx, addr.Path())
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	if found {
		return ferry.SectionPresent, nil
	}

	return ferry.SectionAbsent, nil
}

// holds reports whether the subkey one container address names is there, and it
// is where a cancelled context and an address the registry cannot name are
// refused before anything is read.
func (r *reader) holds(ctx context.Context, at ferry.Path) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if err := r.check(at); err != nil {
		return false, err
	}

	_, found, err := r.cfg.store.List(ctx, subkeyOf(at))
	if err != nil {
		return false, fmt.Errorf(readFailed, err)
	}

	return found, nil
}

// Children lists what the registry holds immediately under a container whose
// members come from the value, which is how a map-typed or slice-typed field is
// loaded from this plane at all: its members are in no compiled address set, and
// only enumeration can reveal them.
//
// Both namespaces are listed, because a member may be a value - a []string is a
// run of values named 0, 1, 2 - or a subkey, which is what a map of structs is.
// A value and a subkey of one name are one member, since one member is one
// address, and [reader.Get] settles which of the two holds its value.
//
// A member's own spelling is the registry's, which is the case whoever wrote it
// first used. Nothing is folded on the way out: the fold is the key function's
// and it exists for the check at Bind.
//
// The result is sorted, so it is 0 1 2 ... 11 rather than the 0 1 10 11 2 that
// sorting text gives, and a caller asserting on it is not asserting on the order
// one machine's registry happened to enumerate in.
func (r *reader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := r.check(addr.Path()); err != nil {
		return nil, err
	}

	listing, found, err := r.cfg.store.List(ctx, subkeyOf(addr.Path()))
	if err != nil {
		return nil, fmt.Errorf(readFailed, err)
	}

	if !found {
		return nil, nil
	}

	return members(listing), nil
}
