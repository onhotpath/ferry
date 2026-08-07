package kv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/onhotpath/ferry"
)

// Source is the read half of a key-value plane.
//
//	src, err := kv.NewSource(store, kv.WithPrefix("app"))
//	cfg, err := ferry.Load[Config](ctx, src)
//
// It is a separate type from [Sink], so a round trip names the store twice.
// The repetition buys the refusal being a compile error: code handed only a
// Source cannot save through it.
//
// One source may be used by many loads at once, from many goroutines. Nothing
// mutable is shared between them.
type Source struct {
	client Client
	prefix []string

	// batch is the whole of ADR-0004's "the difference is one boolean inside
	// the driver", and it is read once, in the open.
	batch bool

	// raw is the spelling [Raw] declared, or nil where this plane's values are
	// text (ADR-0018).
	raw ferry.Spelling[[]byte, []byte]
}

var _ ferry.Source = (*Source)(nil)

// NewSource builds a source reading through client.
//
//	src, err := kv.NewSource(consulClient, kv.WithPrefix("app"), kv.WithBatch())
//	cfg, err := ferry.Load[Config](ctx, src)
//
// It reports every Option that was wrong rather than only the first, and it
// refuses a nil client here rather than at the first read.
func NewSource(client Client, opts ...Option) (*Source, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, errNoClient
	}

	return &Source{client: client, prefix: cfg.prefix, batch: cfg.batch, raw: cfg.raw}, nil
}

// errNoClient is both constructors' refusal of a plane that was never supplied.
var errNoClient = errors.New("kv: the client is nil, so there is no store to reach: assign one, or check the " +
	"error of the constructor that was meant to return it")

// Bind computes this schema's store keys and checks them, and does no I/O.
//
// Two things are checked before anything is read: that every field has a store
// key at all, and that no two fields want the same key. A schema failing either
// is refused here, in one error naming every offending field.
//
// It cannot fail for anything about the store itself. A store that is
// unreachable, or a token that has expired, is reported when the load starts.
func (s *Source) Bind(addrs *ferry.AddressSet) (ferry.OpenFunc, error) {
	keys, err := ferry.NewKeys(addrs, driverName, keyFunc(s.prefix))
	if err != nil {
		return nil, err
	}

	sections, err := declaredSections(addrs, keys)
	if err != nil {
		return nil, err
	}

	return s.opener(keys, sections), nil
}

// sectionScope is what a declared section's presence is decided from: the store
// keys of the leaves the type puts under it, and the folders of the composites
// it puts under it, whose own members come from the store instead.
//
// A section's children come from the type (ADR-0016), so the store can be asked
// about exactly those keys, and a key that merely lies in the same folder stays
// a key of somebody else's. A key-value folder is namespaced by the caller's own
// prefix, so this is a narrower hole than the same one on a process
// environment, and it is the same hole.
type sectionScope struct {
	keys    []string
	folders []string
}

// declaredSections is the presence table Bind builds, one entry per section the
// type determined.
//
// A section a value minted - one under a composite - is in no address set and in
// no entry here, and [reader.Probe] falls back to asking whether the folder
// holds anything. That is exact too: everything under a composite's own folder
// is one of its members by construction, because its members are whatever the
// store holds there.
func declaredSections(addrs *ferry.AddressSet, keys *ferry.Keys) (map[ferry.SectionAddr]sectionScope, error) {
	out := make(map[ferry.SectionAddr]sectionScope, addrs.Len())
	key := keys.Open()

	for m := range addrs.Seq() {
		section, ok := m.(ferry.SectionAddr)
		if !ok {
			continue
		}

		scope, err := scopeOf(addrs, key, section.Path())
		if err != nil {
			return nil, err
		}

		out[section] = scope
	}

	return out, nil
}

// scopeOf collects one section's leaves and composites out of the address set.
//
// It ranges the whole set per section rather than exploiting the set's ordering,
// because this runs once per Bind, before any backend call.
func scopeOf(addrs *ferry.AddressSet, key ferry.KeyFunc, at ferry.Path) (sectionScope, error) {
	var scope sectionScope

	for m := range addrs.Seq() {
		if !under(at, m.Path()) {
			continue
		}

		k, err := key(m.Path())
		if err != nil {
			// Unreachable: NewKeys computed a key for every address in this set
			// already. It is returned rather than ignored because a driver that
			// swallows an error here would be deciding that core was wrong.
			return sectionScope{}, err
		}

		switch m.(type) {
		case ferry.LeafAddr:
			scope.keys = append(scope.keys, k)
		case ferry.CompositeAddr:
			scope.folders = append(scope.folders, folder(k))
		default:
			// A section under a section contributes nothing of its own: its
			// members are in this set too, and they are what the store is asked
			// about.
		}
	}

	return scope, nil
}

// under reports whether p lies strictly below prefix, at a segment boundary.
//
// The canonical renderings decide it. ADR-0003's escaping leaves no bare
// delimiter inside a segment, so a rendering that continues past another one
// continues at a boundary and never in the middle of a segment, which is why /ab
// is not under /a while /a/b and /a#0 both are.
func under(prefix, p ferry.Path) bool {
	rest, ok := strings.CutPrefix(p.String(), prefix.String())

	return ok && rest != "" && (rest[0] == '/' || rest[0] == '#')
}

// opener is the [ferry.OpenFunc] one Bind hands back, and the one place the
// batch-versus-lazy choice is read.
//
// A batch open is one List and a reader that answers every address out of what
// it got back; a lazy open is no call at all and a reader that asks per
// address. Both hand back the same [ferry.Reader] type, because the difference
// is data rather than behaviour and nothing above this function can tell which
// it was given.
//
// The batch is one List and is deliberately not subdivided inside the caller's
// budget. ADR-0019 leaves subdivision to the driver because only the driver has
// the cost model, and this one has the flattest possible answer: a store lists
// a folder in one round trip, so splitting the prefix into several would be
// more requests for the same bytes. A driver whose plane routes across several
// backends is where [ferry.ConcurrencyBudget] earns its keep, and it is read at
// the open rather than here for that reason.
func (s *Source) opener(keys *ferry.Keys, sections map[ferry.SectionAddr]sectionScope) ferry.OpenFunc {
	root := rootKey(s.prefix)

	return func(ctx context.Context) (ferry.Reader, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		r := &reader{client: s.client, key: keys.Open(), sections: sections, raw: s.raw}
		if !s.batch {
			return r, nil
		}

		pairs, err := s.client.List(ctx, folder(root))
		if err != nil {
			return nil, fmt.Errorf("kv: listing the store: %w", err)
		}

		r.pairs, r.batched = pairs, true

		return r, nil
	}
}

// reader is one open read side.
//
// It implements [ferry.Enumerator] because a store lists trivially, and
// [ferry.Prober] for the same reason: a container is present here exactly when
// the store holds something below it. It does not implement [ferry.Releaser],
// because it holds no resource: the client is the source's and outlives every
// open of it.
//
// It implements [ferry.Concurrent], which is what obliges everything below it
// to be safe from many goroutines at once (ADR-0019). Two of the three things
// it holds are safe by construction - the snapshot and the presence table are
// written before the reader exists and only read afterwards - and the third is
// the key function, which is not, so this type is where it is serialised.
type reader struct {
	client Client

	// mu guards key and nothing else.
	//
	// A [ferry.KeyFunc] belongs to its open and is not safe for concurrent use:
	// minting an address a value produced writes the open's own minted set, and
	// per open is not per goroutine (ADR-0012). Declaring [ferry.Concurrent] is
	// a promise about everything the instance reaches, so the lock is this
	// driver's obligation rather than core's, which is what keeps a driver that
	// declares nothing paying nothing (ADR-0019).
	mu sync.Mutex

	// key is this open's key function. It serves the static tier from the table
	// Bind built and mints an address that came from a value - a map key, a
	// sequence index - as it is asked for, checking it against everything this
	// open has already minted. It belongs to the open and nothing it mints
	// outlives one (ADR-0012). Every call to it goes through [reader.keyOf].
	key ferry.KeyFunc

	// sections is the presence table Bind built, and it is read and never
	// written, so one binding's opens share it.
	sections map[ferry.SectionAddr]sectionScope

	// raw is this plane's spelling of a stored value, or nil where the plane
	// carries text. It is read and never written, and its two halves are pure,
	// so it is reachable from every goroutine one open is entered from
	// (ADR-0018, ADR-0019).
	raw ferry.Spelling[[]byte, []byte]

	// pairs is the whole plane, and batched is what tells "fetched and empty"
	// from "not fetched". A store that holds nothing answers List with an empty
	// map, and the two must not be one state.
	pairs   map[string][]byte
	batched bool
}

var (
	_ ferry.Reader     = (*reader)(nil)
	_ ferry.Prober     = (*reader)(nil)
	_ ferry.Enumerator = (*reader)(nil)
	_ ferry.Concurrent = (*reader)(nil)
)

// MaxConcurrent reports that this reader tolerates overlapping calls and
// imposes no bound of its own, so a caller's [ferry.MaxConcurrency] stands
// alone.
//
// Zero rather than a number, because the bound that exists is not this
// package's to name. A lazy open turns every overlapping read into a call to
// your [Client], so how many of those the store behind it will take is a fact
// about your store and your token, and the caller is the one holding both. A
// batch open makes no call here at all, so there is nothing left to bound.
//
// What it commits this package to is that everything an open reaches is safe
// from many goroutines at once. The snapshot and the presence table are written
// before the reader exists, the key function is serialised inside this package,
// and what is left is your [Client] - which this package already requires to be
// safe for use from many goroutines, because one source serves many loads.
func (*reader) MaxConcurrent() int { return 0 }

// keyOf is this open's plane key for one address, and the one place the key
// function is entered.
//
// The lock is what declaring [ferry.Concurrent] costs: a key function belongs
// to its open and is not safe for concurrent use, so the driver that declares
// the tolerance is the one that serialises it (ADR-0012, ADR-0019). It is held
// over the call and never over the store, so nothing waits here for I/O.
func (r *reader) keyOf(at ferry.Path) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.key(at)
}

// Get answers with what the store holds at this address.
//
// A failure reaches the caller as a failure and is never substituted with
// Absent. That is ADR-0014's conformance case 4 and it exists because a survey
// found a real provider discarding its errors and answering with an empty
// result: a read that failed and an address the store does not hold are
// different observations, and only one of them is a configuration that can be
// used.
//
// A cancelled context is answered with the context's own error before the
// client is asked anything. A client that blocks and then reports the
// cancellation itself has its error returned wrapped, so errors.Is reaches
// context.Canceled either way; which of the two a race resolves to is #20's
// question and is not answered here.
//
// It is asked only about a leaf, so a container's own key is never read as a
// value: what is asked there is [reader.Probe].
func (r *reader) Get(ctx context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	if err := ctx.Err(); err != nil {
		return ferry.Value{}, err
	}

	key, err := r.keyOf(addr.Path())
	if err != nil {
		return ferry.Value{}, err
	}

	value, found, err := r.fetch(ctx, key)
	if err != nil {
		return ferry.Value{}, err
	}

	// A key the store does not hold is the zero Value, which is Absent. A key
	// it holds with no bytes is String(""), and the two stay different
	// observations on a plane that has no null to confuse them with.
	if !found {
		return ferry.Value{}, nil
	}

	return r.held(value)
}

// fetch reads one key, out of the snapshot a batch open already has or out of
// the store.
//
// It is the only place the two differ, which is what makes "the difference is
// one boolean inside the driver" true of the code as well as of the prose: a
// batch open makes no call here and a lazy one makes exactly one.
func (r *reader) fetch(ctx context.Context, key string) (value []byte, found bool, err error) {
	if r.batched {
		value, found = r.pairs[key]

		return value, found, nil
	}

	value, found, err = r.client.Get(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("kv: reading the store: %w", err)
	}

	return value, found, nil
}

// held is the one place a stored value becomes a [ferry.Value], so that a batch
// read and a lazy read cannot disagree about what the store said.
//
// It is a String and never a Bytes by default, which is the decision that makes
// this plane round-trip anything at all. The store carries no type information,
// so the kind cannot be recovered from what is stored; core's own rule is that a
// leaf takes its own kind and a String, so a String is the one kind every Go
// type accepts - a number parses it, a bool parses it, and a []byte takes its
// bytes. Answering Bytes instead refuses every string, integer and duration
// field on the plane (ADR-0004: such a driver returns Absent or a String, and
// never a Null).
//
// [Raw] is a caller declaring that this plane holds payloads and accepting
// exactly that consequence, and it is the only way the other answer is given
// (ADR-0018).
func (r *reader) held(value []byte) (ferry.Value, error) {
	if r.raw == nil {
		return ferry.String(string(value)), nil
	}

	payload, err := r.raw.Parse(value)
	if err != nil {
		// Unreachable: this plane's spelling is the identity and refuses
		// nothing, because every byte sequence a store holds is a payload. It
		// is returned rather than dropped because a reader that swallowed a
		// spelling's refusal would answer with a value the spelling refused.
		return ferry.Value{}, err
	}

	return ferry.Bytes(payload), nil
}

// Children lists what the store holds immediately under an address.
//
// It is what makes a map-typed or slice-typed field loadable at all, since
// those addresses come from the value rather than from the type. A batch open
// answers it out of the snapshot it already has and makes no call; a lazy open
// lists the one folder.
//
// # The one thing a key space cannot carry
//
// An address carries its segment kind and a store key does not, so the kind has
// to be recovered from the text here, and canonical base-10 is read as a
// position. That is the limitation ADR-0003 names by name - it is why
// [ferry.SegmentKind] exists - and this driver is where it is unavoidable
// rather than chosen: the store was handed "tags/0" and nothing else.
//
// It is bounded rather than silent. A schema naming both /tags#0 and a map key
// "0" under /tags is refused at Bind, because they are one key and the
// injectivity check sees both. What is left is a map whose key text is a
// position, dumped and then loaded back: the load reports that the plane holds
// /m#0 under a mapping and refuses it, which is core's own check, so the entry
// is never quietly turned into something else.
func (r *reader) Children(ctx context.Context, addr ferry.CompositeAddr) ([]ferry.Segment, error) {
	under, err := r.folderOf(ctx, addr.Path())
	if err != nil {
		return nil, err
	}

	pairs, err := r.pairsIn(ctx, under)
	if err != nil {
		return nil, err
	}

	return children(under, pairs), nil
}

// Probe answers whether the store holds anything this schema addresses below a
// container's own key.
//
// A key-value store has no null, so a container is present or absent and never
// null: an empty composite has nothing to be stored as here, which is what
// [Sink] refuses at the write rather than storing a zero-length value that would
// be indistinguishable from empty text.
//
// The members are what the question is scoped to, and that is the sharp edge. A
// section's members come from the type, so a key that merely lies in the same
// folder and belongs to nothing this schema addresses does not make the section
// present. A composite is the other way round, because its members are whatever
// the store holds below its key, so everything there is one of them.
func (r *reader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	at, err := r.folderOf(ctx, addr.Path())
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	pairs, err := r.pairsIn(ctx, at)
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	if r.holds(addr, at, pairs) {
		return ferry.SectionPresent, nil
	}

	return ferry.SectionAbsent, nil
}

// holds reports whether these pairs hold anything the container owns: exactly
// its declared members where the type determined them, and the whole folder
// where the value does.
func (r *reader) holds(addr ferry.Container, at string, pairs map[string][]byte) bool {
	if section, ok := addr.(ferry.SectionAddr); ok {
		if scope, declared := r.sections[section]; declared {
			return scope.holdsIn(pairs)
		}
	}

	for key := range pairs {
		if strings.HasPrefix(key, at) {
			return true
		}
	}

	return false
}

// holdsIn reports whether the pairs hold one of the section's own members: a
// declared leaf at its key, or anything at all inside a declared composite.
func (s sectionScope) holdsIn(pairs map[string][]byte) bool {
	for _, key := range s.keys {
		if _, ok := pairs[key]; ok {
			return true
		}
	}

	for _, folder := range s.folders {
		if anyUnder(pairs, folder) {
			return true
		}
	}

	return false
}

// anyUnder reports whether the pairs hold anything inside one folder.
func anyUnder(pairs map[string][]byte, folder string) bool {
	for key := range pairs {
		if strings.HasPrefix(key, folder) {
			return true
		}
	}

	return false
}

// folderOf is the store folder one container address names, and it is where a
// cancelled context and an address the store cannot name are refused before
// anything is read.
func (r *reader) folderOf(ctx context.Context, at ferry.Path) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	key, err := r.keyOf(at)
	if err != nil {
		return "", err
	}

	return folder(key), nil
}

// pairsIn is whatever the store holds in one folder: out of the snapshot a
// batch open already has, or out of one List.
//
// It is the one place the two container questions reach the store, so a batch
// open makes no call for either and a lazy one makes exactly one.
func (r *reader) pairsIn(ctx context.Context, under string) (map[string][]byte, error) {
	if r.batched {
		return r.pairs, nil
	}

	pairs, err := r.client.List(ctx, under)
	if err != nil {
		return nil, fmt.Errorf("kv: listing the store: %w", err)
	}

	return pairs, nil
}

// children is the immediate members of one folder, as segments, sorted the way
// core orders the addresses they name.
//
// The sort is not decoration: Go's map iteration is randomised, so an unsorted
// answer would make a test that reads a plane's contents depend on iteration
// order, and ADR-0003 requires the enumeration to be segment-wise rather than
// over the rendering.
func children(under string, pairs map[string][]byte) []ferry.Segment {
	seen := make(map[string]struct{}, len(pairs))
	out := make([]ferry.Segment, 0, len(pairs))

	for key := range pairs {
		name, ok := childName(key, under)
		if !ok {
			continue
		}

		if _, dup := seen[name]; dup {
			continue
		}

		seen[name] = struct{}{}
		out = append(out, segmentOf(name))
	}

	slices.SortFunc(out, compareSegments)

	return out
}

// compareSegments orders two members the way core orders the addresses they
// name: by kind first, and a position numerically rather than as text.
func compareSegments(a, b ferry.Segment) int {
	if a.Kind() != b.Kind() {
		return int(a.Kind()) - int(b.Kind())
	}

	if a.Kind() == ferry.Index && len(a.Text()) != len(b.Text()) {
		return len(a.Text()) - len(b.Text())
	}

	return strings.Compare(a.Text(), b.Text())
}

// childName is the first step of key below under, and whether key lies strictly
// under it at all. A deeper key contributes the folder it lies in, which is what
// makes the answer immediate members and not a subtree.
func childName(key, under string) (string, bool) {
	rest, ok := strings.CutPrefix(key, under)
	if !ok {
		return "", false
	}

	name, _, _ := strings.Cut(rest, separator)

	return name, name != ""
}

// segmentOf builds one member out of the text the store spelled, reading the
// segment kind off the text because the store carries none.
func segmentOf(name string) ferry.Segment {
	if i, ok := position(name); ok {
		return ferry.IndexSegment(i)
	}

	return ferry.NameSegment(name)
}

// position is the sequence index a child name spells, if it spells one.
//
// It accepts exactly what [ferry.Path] renders an Index segment as: canonical
// base-10 with no leading zero. "01" and "" are member names and not positions,
// which keeps this the inverse of the key function rather than a looser parse
// that would read one address as another.
//
// A number too large for the type is not a position either. It is a name this
// plane can still hold, and answering with a wrapped-around index would be the
// one thing worse than refusing it.
func position(name string) (uint, bool) {
	if !canonicalDigits(name) {
		return 0, false
	}

	var n uint

	for i := range len(name) {
		d := uint(name[i] - '0')
		if n > (maxUint-d)/base10 {
			return 0, false
		}

		n = n*base10 + d
	}

	return n, true
}

// canonicalDigits reports whether text is base-10 with no leading zero, which is
// the only spelling ferry renders a position in and therefore the only one that
// may be read back as one.
func canonicalDigits(text string) bool {
	if text == "" || (text[0] == '0' && text != "0") {
		return false
	}

	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}

	return true
}

const (
	// base10 is the only base a position is ever spelled in, which is what makes
	// the rendering of an address unique.
	base10 = 10
	// maxUint is the largest position [ferry.Path.Elem] can take.
	maxUint = ^uint(0)
)
