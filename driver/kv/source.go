package kv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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

	return &Source{client: client, prefix: cfg.prefix, batch: cfg.batch}, nil
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

	return s.opener(keys), nil
}

// opener is the [ferry.OpenFunc] one Bind hands back, and the one place the
// batch-versus-lazy choice is read.
//
// A batch open is one List and a reader that answers every address out of what
// it got back; a lazy open is no call at all and a reader that asks per
// address. Both hand back the same [ferry.Reader] type, because the difference
// is data rather than behaviour and nothing above this function can tell which
// it was given.
func (s *Source) opener(keys *ferry.Keys) ferry.OpenFunc {
	root := rootKey(s.prefix)

	return func(ctx context.Context) (ferry.Reader, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		r := &reader{client: s.client, key: keys.Open()}
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
type reader struct {
	client Client

	// key is this open's key function. It serves the static tier from the table
	// Bind built and mints an address that came from a value - a map key, a
	// sequence index - as it is asked for, checking it against everything this
	// open has already minted. It belongs to the open and nothing it mints
	// outlives one (ADR-0012).
	key ferry.KeyFunc

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
)

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

	key, err := r.key(addr.Path())
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

	return held(value), nil
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
// It is a String and never a Bytes, which is the decision that makes this plane
// round-trip anything at all. The store carries no type information, so the
// kind cannot be recovered from what is stored; core's own rule is that a leaf
// takes its own kind and a String, so a String is the one kind every Go type
// accepts - a number parses it, a bool parses it, and a []byte takes its bytes.
// Answering Bytes instead would refuse every string, integer and duration field
// on the plane (ADR-0004: such a driver returns Absent or a String, and never a
// Null).
func held(value []byte) ferry.Value { return ferry.String(string(value)) }

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

// Probe answers whether the store holds anything below a container's own key.
//
// A key-value store has no null, so a container is present or absent and never
// null: an empty composite has nothing to be stored as here, which is what
// [Sink] refuses at the write rather than storing a zero-length value that would
// be indistinguishable from empty text.
func (r *reader) Probe(ctx context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	under, err := r.folderOf(ctx, addr.Path())
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	pairs, err := r.pairsIn(ctx, under)
	if err != nil {
		return ferry.SectionInfo{}, err
	}

	for key := range pairs {
		if strings.HasPrefix(key, under) {
			return ferry.SectionPresent, nil
		}
	}

	return ferry.SectionAbsent, nil
}

// folderOf is the store folder one container address names, and it is where a
// cancelled context and an address the store cannot name are refused before
// anything is read.
func (r *reader) folderOf(ctx context.Context, at ferry.Path) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	key, err := r.key(at)
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
