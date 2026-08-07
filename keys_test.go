package ferry_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/onhotpath/ferry"
)

// This file is the driver's side of ADR-0003, asserted from outside the seam.
//
// Keys is a pure value with no engine behind it - no first-party driver ships
// yet - and it is the surface a driver author writes against, so it is exercised
// directly as well as through Dump. The three key functions below are ADR-0003's
// own table columns, and the fourth is the transforming driver its prose
// measures; the difference between them is the point of the whole section, so it
// is spelled out where they are defined rather than left to be inferred.

// flatKey is what a flattening driver does: one plane key per address, segments
// joined by a separator, each passed through the driver's own transform.
//
// An empty segment is refused rather than transformed, and that is the legality
// half of the rule: nothing on these planes is named by nothing, and no
// transformation rescues it.
func flatKey(addr ferry.Path, sep string, transform func(string) string) (string, error) {
	out := make([]string, 0, 4)

	for s := range addr.Segments() {
		if s.Text() == "" {
			return "", errors.New("an empty segment names nothing on this plane")
		}

		out = append(out, transform(s.Text()))
	}

	return strings.Join(out, sep), nil
}

func asWritten(s string) string { return s }

// envUpper is ADR-0003's first column: segments joined with _, uppercased, and
// no character transform of any kind.
func envUpper(addr ferry.Path) (string, error) { return flatKey(addr, "_", strings.ToUpper) }

// envExact is its second column: the same join, without the case fold.
func envExact(addr ferry.Path) (string, error) { return flatKey(addr, "_", asWritten) }

// dotted is its third: segments joined with a dot, folding nothing.
func dotted(addr ferry.Path) (string, error) { return flatKey(addr, ".", asWritten) }

// envTransform is the transforming driver of the same ADR's prose, and it is a
// fourth key function rather than a fourth column. It maps every byte an
// environment variable name may not carry to _, so it accepts feature-flags
// where a validating driver would reject it - and, unlike the table's two env
// columns, it folds a dot into the separator too, which is what makes
// /limits/http.port and /limits/http_port one plane key.
func envTransform(addr ferry.Path) (string, error) { return flatKey(addr, "_", scrub) }

func scrub(s string) string {
	b := []byte(strings.ToUpper(s))

	for i, c := range b {
		if !legalEnvByte(c) {
			b[i] = '_'
		}
	}

	return string(b)
}

func legalEnvByte(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// TestTheADR0003Table is that ADR's four address sets against its three key
// functions, which is the whole of "no key function is universally right, which
// is why the rule is stated over the schema rather than over the key function".
func TestTheADR0003Table(t *testing.T) {
	t.Parallel()

	sets := []struct {
		name string
		set  *ferry.AddressSet
	}{
		{"/DB/HOST, /DB_HOST", ferry.LeafSet(ferry.At("DB", "HOST"), ferry.At("DB_HOST"))},
		{"/myKey, /MyKey, /MYKEY", ferry.LeafSet(ferry.At("myKey"), ferry.At("MyKey"), ferry.At("MYKEY"))},
		{"/db.host, /db/host", ferry.LeafSet(ferry.At("db.host"), ferry.At("db", "host"))},
		{
			"/db/host, /db/port, /cache/host",
			ferry.LeafSet(ferry.At("db", "host"), ferry.At("db", "port"), ferry.At("cache", "host")),
		},
	}

	funcs := []struct {
		name string
		f    ferry.KeyFunc
	}{
		{"env, uppercase and _", envUpper},
		{"env, no fold and _", envExact},
		{"dotted, no fold", dotted},
	}

	// One row per address set and one column per key function, in the ADR's own
	// two words.
	table := [][]func(t *testing.T, keys *ferry.Keys, err error){
		{rejected, rejected, accepted},
		{rejected, accepted, accepted},
		{accepted, accepted, rejected},
		{accepted, accepted, accepted},
	}

	for i, set := range sets {
		for j, fn := range funcs {
			t.Run(set.name+" / "+fn.name, func(t *testing.T) {
				t.Parallel()

				keys, err := ferry.NewKeys(set.set, "env", fn.f)
				table[i][j](t, keys, err)
			})
		}
	}
}

func rejected(t *testing.T, keys *ferry.Keys, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("the key function is not injective over this set and NewKeys accepted it")
	}

	if keys != nil {
		t.Error("a refused NewKeys returned a table as well as an error")
	}
}

func accepted(t *testing.T, keys *ferry.Keys, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("the key function is injective over this set and NewKeys refused it: %+v", err)
	}

	if keys == nil {
		t.Error("NewKeys returned neither a table nor an error")
	}
}

// TestARefusalNamesBothAddresses is ADR-0003's first row read as a diagnostic
// rather than as a verdict: the collision a flat key space creates and cannot
// see, appearing before any I/O and naming both of the addresses that made it.
//
// It asserts on ferry's own text, which is the one place that is allowed: the
// rule that message text is not API is about what a caller may depend on, and
// the criterion here is that both addresses are in the line.
func TestARefusalNamesBothAddresses(t *testing.T) {
	t.Parallel()

	set := ferry.LeafSet(ferry.At("DB", "HOST"), ferry.At("DB_HOST"))

	_, err := ferry.NewKeys(set, "env", envUpper)
	if err == nil {
		t.Fatal("DB_HOST is both addresses' plane key and NewKeys accepted the set")
	}

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("a refusal of the address set is not ErrPlane: %+v", err)
	}

	// The refusal is core's own and not the driver's: the key function
	// answered, and it is core that saw the set.
	if errors.Is(err, ferry.ErrDriver) {
		t.Errorf("an injectivity refusal is core's own and carries no driver provenance: %+v", err)
	}

	// Sorted segment-wise, /DB/HOST precedes /DB_HOST, so the later of the two
	// is where the refusal lands and the earlier one is named in the line.
	mustLocate(t, err, ferry.At("DB_HOST"))
	mustName(t, err, "/DB/HOST", `"DB_HOST"`)
}

// TestLegalityIsTheDriversQuestion is the first of the two checks a driver runs.
// It asks whether the plane can name an address at all, which no transformation
// rescues, and the answer is the driver's, so the refusal carries the driver's
// provenance where an injectivity refusal does not.
func TestLegalityIsTheDriversQuestion(t *testing.T) {
	t.Parallel()

	_, err := ferry.NewKeys(ferry.LeafSet(ferry.At("")), "env", envUpper)
	if err == nil {
		t.Fatal("an empty segment has no environment variable name and NewKeys accepted it")
	}

	if !errors.Is(err, ferry.ErrPlane) || !errors.Is(err, ferry.ErrDriver) {
		t.Errorf("the driver's own refusal lost its class or its provenance: %+v", err)
	}

	mustLocate(t, err, ferry.At(""))
}

// TestATransformingKeyFunctionIsSafeBecauseOfInjectivity is the change the
// prototype made to ADR-0003's answer: a driver is expected to transform segment
// text rather than to reject it, because a validating driver refuses
// feature-flags, which is an ordinary thing to write in a config struct.
//
// The transform is many-to-one, and the second half is what makes that safe.
func TestATransformingKeyFunctionIsSafeBecauseOfInjectivity(t *testing.T) {
	t.Parallel()

	if _, err := ferry.NewKeys(ferry.LeafSet(ferry.At("feature-flags")), "env", envTransform); err != nil {
		t.Errorf("feature-flags alone is ordinary and the driver refused it: %+v", err)
	}

	set := ferry.LeafSet(ferry.At("feature-flags"), ferry.At("feature_flags"))

	_, err := ferry.NewKeys(set, "env", envTransform)
	if err == nil {
		t.Fatal("feature-flags and feature_flags are one plane key and NewKeys accepted them")
	}

	mustLocate(t, err, ferry.At("feature_flags"))
	mustName(t, err, "/feature-flags", `"FEATURE_FLAGS"`)
}

// TestNewKeysRefusesEveryOffendingAddress is ADR-0011 at the bind moment: the
// refusals are collected rather than reported one at a time, and they are sorted.
func TestNewKeysRefusesEveryOffendingAddress(t *testing.T) {
	t.Parallel()

	set := ferry.LeafSet(
		ferry.At("a-one"), ferry.At("a_one"),
		ferry.At("b-two"), ferry.At("b_two"),
	)

	_, err := ferry.NewKeys(set, "env", envTransform)

	got := addressesOf(t, ferry.Elements(err))
	if want := []string{"/a_one", "/b_two"}; !slices.Equal(got, want) {
		t.Errorf("the refusal reported %v, want one element per collision, sorted: %v", got, want)
	}
}

// TestNewKeysWithoutAKeyFunction covers the two arguments a driver can get
// wrong. ferry itself never panics, so both are errors (ADR-0011).
func TestNewKeysWithoutAKeyFunction(t *testing.T) {
	t.Parallel()

	if _, err := ferry.NewKeys(ferry.LeafSet(ferry.At("a")), "env", nil); err == nil {
		t.Error("NewKeys accepted a nil key function")
	}

	keys, err := ferry.NewKeys(nil, "", envUpper)
	if err != nil {
		t.Fatalf("an empty address set is a set: %+v", err)
	}

	mustMint(t, keys.Open(), ferry.At("late"), "LATE")
}

// TestAMintedAddressIsCheckedAgainstBothTiers is the dynamic half of the rule at
// the helper: an address that came from a value is checked against the static
// table and against everything this open has already minted, and an address the
// table simply does not hold is answered rather than refused.
func TestAMintedAddressIsCheckedAgainstBothTiers(t *testing.T) {
	t.Parallel()

	keys := mustBind(t, ferry.LeafSet(ferry.At("labels"), ferry.At("name")), envTransform)
	key := keys.Open()

	mustMint(t, key, ferry.At("labels", "env"), "LABELS_ENV")
	mustMint(t, key, ferry.At("labels", "env"), "LABELS_ENV")
	mustMint(t, key, ferry.At("labels", "http-port"), "LABELS_HTTP_PORT")

	// Everything already issued is both tiers: the address above, which this
	// open minted, and /name, which the table held before the open began.
	mustNotMint(t, key, ferry.At("labels", "http_port"), "two map keys folding to one plane key")
	mustNotMint(t, key, ferry.At("Name"), "a minted address folding onto a static one")

	// Legality is asked of a minted address as well, and no transformation
	// rescues it either.
	mustNotMint(t, key, ferry.At("labels", ""), "a map key naming nothing on this plane")
}

// TestNoAddressIsRetainedAcrossOpens is ADR-0012's amendment stated as a test.
// It runs on the write side, because that is where the retention refuses a legal
// write: on Load two loads' minted addresses come out of one plane's own key
// space, so the growth is visible there and the refusal is not.
func TestNoAddressIsRetainedAcrossOpens(t *testing.T) {
	t.Parallel()

	keys := mustBind(t, ferry.LeafSet(ferry.At("labels")), envTransform)

	mustMint(t, keys.Open(), ferry.At("labels", "http-port"), "LABELS_HTTP_PORT")
	mustMint(t, keys.Open(), ferry.At("labels", "http_port"), "LABELS_HTTP_PORT")
}

// TestTheStaticTableTakesNoLock asserts the shape rather than the timing: a
// table written once before it is returned and never again needs no
// synchronisation to read, where holding a mutex over both tiers is the measured
// 20.0 ns against 8.8 (ADR-0004).
//
// The concurrent half is what makes it more than an assertion about fields: 64
// goroutines share one binding, read the static tier and mint into their own
// opens, and -race is the assertion.
func TestTheStaticTableTakesNoLock(t *testing.T) {
	t.Parallel()

	checkNoLockField(t)

	keys := mustBind(t, ferry.LeafSet(ferry.At("labels"), ferry.At("name")), envTransform)

	var wg sync.WaitGroup

	for i := range 64 {
		wg.Go(func() { readAndMint(t, keys, i) })
	}

	wg.Wait()
}

func checkNoLockField(t *testing.T) {
	t.Helper()

	typ := reflect.TypeFor[ferry.Keys]()

	for i := range typ.NumField() {
		if f := typ.Field(i); strings.HasPrefix(f.Type.String(), "sync.") {
			t.Errorf("Keys holds a %s, so its read path takes a lock", f.Type)
		}
	}
}

func readAndMint(t *testing.T, keys *ferry.Keys, i int) {
	t.Helper()

	key := keys.Open()

	mustMint(t, key, ferry.At("name"), "NAME")

	// Two goroutines mint the same address text, which is safe exactly because
	// the set it is checked against is the open's own.
	mustMint(t, key, ferry.At("labels", "tenant"+strconv.Itoa(i%2)),
		"LABELS_TENANT"+strconv.Itoa(i%2))
}

func mustBind(t *testing.T, set *ferry.AddressSet, f ferry.KeyFunc) *ferry.Keys {
	t.Helper()

	keys, err := ferry.NewKeys(set, "env", f)
	if err != nil {
		t.Fatalf("bind: %+v", err)
	}

	return keys
}

func mustMint(t *testing.T, key ferry.KeyFunc, addr ferry.Path, want string) {
	t.Helper()

	got, err := key(addr)
	if err != nil {
		t.Errorf("%v: %+v", addr, err)
	}

	if got != want {
		t.Errorf("%v has the plane key %q, want %q", addr, got, want)
	}
}

func mustNotMint(t *testing.T, key ferry.KeyFunc, addr ferry.Path, why string) {
	t.Helper()

	if _, err := key(addr); err == nil {
		t.Errorf("%v was accepted, and it is %s", addr, why)
	}
}

func addressesOf(t *testing.T, errs []error) []string {
	t.Helper()

	out := make([]string, 0, len(errs))

	for _, err := range errs {
		out = append(out, addressOf(t, err).String())
	}

	return out
}

func addressOf(t *testing.T, err error) ferry.Path {
	t.Helper()

	e, ok := errors.AsType[*ferry.Error](err)
	if !ok {
		t.Fatalf("%v is not a ferry error", err)
	}

	return e.Address()
}

func mustLocate(t *testing.T, err error, want ferry.Path) {
	t.Helper()

	if got := addressOf(t, err); got != want {
		t.Errorf("the refusal is located at %v, want %v", got, want)
	}
}

func mustName(t *testing.T, err error, want ...string) {
	t.Helper()

	line := fmt.Sprintf("%+v", err)
	for _, w := range want {
		if !strings.Contains(line, w) {
			t.Errorf("the refusal does not name %s: %s", w, line)
		}
	}
}

// flatSink is a driver that produces a plane key, so it carries the injectivity
// obligation, and it binds once and opens many the way a long-lived binding
// would (ADR-0012). It is a Sink alone, because the amendment its retained-set
// variant violates is a write-side one.
type flatSink struct {
	f ferry.KeyFunc
	// retain is the variant ADR-0012 refuses: one key function for the whole
	// binding, so the set it minted outlives the open that filled it.
	retain bool

	// prepares is ADR-0004's Preparer: the writer is handed the addresses the
	// dump realised from the value before the first write of that dump, which
	// is the only phase in which a flat sink that does not stage can refuse a
	// folded pair with the plane still untouched.
	prepares bool

	keys  *ferry.Keys
	held  ferry.KeyFunc
	opens int
	plane map[string]ferry.Value
}

func newFlatSink(f ferry.KeyFunc) *flatSink {
	return &flatSink{f: f, plane: map[string]ferry.Value{}}
}

func (s *flatSink) Bind(a *ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	if s.keys == nil {
		k, err := ferry.NewKeys(a, "env", s.f)
		if err != nil {
			return nil, err
		}

		s.keys, s.held = k, k.Open()
	}

	return s.open, nil
}

func (s *flatSink) open(context.Context) (ferry.Writer, error) {
	s.opens++

	key := s.keys.Open()
	if s.retain {
		key = s.held
	}

	if s.prepares {
		return preparingWriter{flatWriter{s: s, key: key}}, nil
	}

	return flatWriter{s: s, key: key}, nil
}

// preparingWriter is [flatWriter] with ADR-0004's Preparer, written the way the
// capability is meant to be taken: the open's own key function, run over the
// realised set before the first write, so a fold between two minted addresses is
// found where the plane is still untouched.
//
// It mints through the same function the writes go through, and that is not a
// second check. A key function serves an address it has already minted from its
// own table, so the write that follows finds the key rather than issuing it
// again.
type preparingWriter struct{ flatWriter }

func (w preparingWriter) Prepare(_ context.Context, addrs []ferry.Path) error {
	errs := make([]error, 0, len(addrs))

	for _, addr := range addrs {
		if _, err := w.key(addr); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (s *flatSink) written() []string { return slices.Sorted(maps.Keys(s.plane)) }

type flatWriter struct {
	s   *flatSink
	key ferry.KeyFunc
}

func (w flatWriter) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	return w.write(addr.Path(), v)
}

// Ensure routes a container's own answer through the same key function, which
// is the whole point of the check: two containers rendering to one plane key
// merge, so the container addresses are in the set NewKeys sees.
func (w flatWriter) Ensure(_ context.Context, addr ferry.Container, p ferry.Presence) error {
	if p != ferry.PresenceNull {
		return nil
	}

	return w.write(addr.Path(), ferry.Null)
}

// Unset forgets every key under the composite's own key, which is what makes a
// dump of this fixture a replacement and what lets the open accept a schema
// holding a map.
func (w flatWriter) Unset(_ context.Context, addr ferry.CompositeAddr) error {
	key, err := w.key(addr.Path())
	if err != nil {
		return err
	}

	for held := range w.s.plane {
		if strings.HasPrefix(held, key) {
			delete(w.s.plane, held)
		}
	}

	return nil
}

func (w flatWriter) write(addr ferry.Path, v ferry.Value) error {
	key, err := w.key(addr)
	if err != nil {
		return err
	}

	w.s.plane[key] = v

	return nil
}

type flagsConf struct {
	Fresh  map[string]string `ferry:"feature-flags"`
	Legacy map[string]string `ferry:"feature_flags"`
	Beta   map[string]string `ferry:"beta-flags"`
	Old    map[string]string `ferry:"beta_flags"`
}

// TestContainerAddressesAreCheckedBeforeAnyIO is the clarification ADR-0003
// added under #56: the rule runs over the whole static set, container addresses
// included, because two containers rendering to one plane key return one merged
// subtree from Children.
//
// Every address in this schema is a container address - a map contributes its
// own and nothing else until there is a value - so a check over the leaves alone
// would find nothing here to refuse.
func TestContainerAddressesAreCheckedBeforeAnyIO(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envTransform)

	err := ferry.Dump(t.Context(), flagsConf{Fresh: map[string]string{"a": "1"}}, sink)
	if err == nil {
		t.Fatal("two container addresses render to one plane key and the dump succeeded")
	}

	if sink.opens != 0 {
		t.Errorf("the driver refused the address set and the plane was opened %d times", sink.opens)
	}

	got := addressesOf(t, ferry.Elements(err))
	if want := []string{"/beta_flags", "/feature_flags"}; !slices.Equal(got, want) {
		t.Errorf("the refusal reported %v, want one element per collision, sorted: %v", got, want)
	}

	mustName(t, err, "/beta-flags", "/feature-flags", `"FEATURE_FLAGS"`, `"BETA_FLAGS"`)
}

// forgedMember is a [ferry.Member] core never minted.
//
// Go seals a type and not an interface, so embedding one of the three address
// types promotes the unexported method and the interface is satisfied from
// outside. There is no way to stop that; what core owes is to treat the result
// as what it is.
type forgedMember struct{ ferry.SectionAddr }

// TestAnAddressCoreDidNotMintIsInNoSet is what that promotion is worth.
//
// A forged member equals no address the compiler made, so a set answers false
// for it rather than mistaking it for the kind whose arm it would otherwise fall
// through to. The path it carries is one the set does hold, because a forgery at
// a path nothing names is refused by the address alone and says nothing about
// the kind.
func TestAnAddressCoreDidNotMintIsInNoSet(t *testing.T) {
	t.Parallel()

	set := ferry.LeafSet(ferry.At("a"), ferry.At("b"))

	if set.Has(forgedMember{SectionAddr: ferry.Section(ferry.At("a"))}) {
		t.Error("a set holds an address core never minted, so a forged member compares equal to a real one")
	}

	if !set.Has(ferry.Leaf(ferry.At("a"))) {
		t.Error("the set does not hold the address the forgery copied, so the case above proves nothing")
	}
}

// The two schemas the kind half of ADR-0003's injectivity rule is read through.
type (
	// foldedKinds renders a section and a leaf to one plane key. A flat driver
	// reads the leaf at that key and only ever uses the section's as a prefix,
	// so nothing is lost and nothing is refused.
	foldedKinds struct {
		Section hostOnly `ferry:"a"`
		Leaf    string   `ferry:"A"`
	}

	// reservedSpace puts a leaf inside the key space a composite is enumerated
	// out of, which is the collision the kind half of the rule leaves behind.
	reservedSpace struct {
		Home  map[string]string `ferry:"home"`
		HomeX string            `ferry:"home_x"`
	}

	hostOnly struct {
		Host string `ferry:"host"`
	}
)

// TestTwoKindsAtOnePlaneKeyAreNotACollision is the kind half of the injectivity
// rule.
//
// A section and a leaf rendering to one plane key are two addresses a flat plane
// still tells apart: the value is read at the key and the section's members are
// read under it, so neither is lost and refusing the pair refuses a schema the
// plane can hold.
func TestTwoKindsAtOnePlaneKeyAreNotACollision(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envUpper)

	v := foldedKinds{Section: hostOnly{Host: "h"}, Leaf: "v"}
	if err := ferry.Dump(t.Context(), v, sink); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	if got, want := sink.written(), []string{"A", "A_HOST"}; !slices.Equal(got, want) {
		t.Errorf("the plane holds %v, want %v: the leaf is at the key and the section's member is under it",
			got, want)
	}
}

// TestAnAddressInsideACompositesKeySpaceIsRefused is the collision the kind half
// leaves behind, and the one a check over the keys alone never saw.
//
// A composite's members come from the value, so a flat driver lists every plane
// key beginning with the composite's own and reads what it finds as a member.
// HOME_X is this schema's own leaf and would be enumerated as a member of the
// map at HOME, which is one value at two addresses.
func TestAnAddressInsideACompositesKeySpaceIsRefused(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envUpper)

	v := reservedSpace{Home: map[string]string{"a": "1"}, HomeX: "x"}

	err := ferry.Dump(t.Context(), v, sink)
	if err == nil {
		t.Fatal("a leaf renders into the key space a composite is enumerated out of and the dump succeeded")
	}

	if sink.opens != 0 {
		t.Errorf("the driver refused the address set and the plane was opened %d times", sink.opens)
	}

	mustName(t, err, "/home", `"HOME"`, `"HOME_X"`)
}

type labelsConf struct {
	Name   string            `ferry:"name"`
	Labels map[string]string `ferry:"labels"`
}

// TestALegitimateMapKeyIsNotADriverError is ADR-0004's own measurement made a
// test: with a static set of {/labels, /name}, dumping a map[string]string used
// to return "address not in the opened set: /labels/env", because a precomputed
// map is a closed set and a miss looked like a driver error. A key function
// mints it instead.
func TestALegitimateMapKeyIsNotADriverError(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envTransform)
	v := labelsConf{Name: "svc", Labels: map[string]string{"env": "prod", "feature-flags": "on"}}

	if err := ferry.Dump(t.Context(), v, sink); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	want := []string{"LABELS_ENV", "LABELS_FEATURE_FLAGS", "NAME"}
	if got := sink.written(); !slices.Equal(got, want) {
		t.Errorf("the plane holds %v, want %v", got, want)
	}
}

// foldedKeys is one value whose two map keys a transforming driver renders to
// one plane key, beside a leaf the type determined.
//
// The leaf is the observable half: it is written by the same dump and is not the
// address that collides, so what the plane holds at it says whether the refusal
// arrived before the writes or among them.
func foldedKeys() labelsConf {
	return labelsConf{Name: "svc", Labels: map[string]string{"a-b": "1", "a_b": "2"}}
}

// TestADynamicCollisionOnAPreparingSinkWritesNothing is #135's fix through the
// engine.
//
// The addresses under a map come from the value, so the plane key each renders
// to is one the driver cannot compute until there is a value. A sink that asks
// for the realised set is handed it after every value is encoded and before the
// first write, which is the one moment at which the whole of what the dump will
// say is known and the plane still holds nothing.
func TestADynamicCollisionOnAPreparingSinkWritesNothing(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envTransform)
	sink.prepares = true

	err := ferry.Dump(t.Context(), foldedKeys(), sink)
	if err == nil {
		t.Fatal("two map keys render to one plane key and the dump succeeded")
	}

	if got := sink.written(); len(got) != 0 {
		t.Errorf("the plane holds %v after a refused dump, want nothing: a sink handed the realised addresses "+
			"before the first write refuses with the plane untouched", got)
	}

	mustLocate(t, err, ferry.At("labels", "a_b"))
	mustName(t, err, "/labels/a-b", `"LABELS_A_B"`)
}

// TestADynamicCollisionWithoutAPreparerLandsAtTheWrite is the same dump against
// the same key function on a sink that neither stages nor prepares, and it pins
// what such a sink still does.
//
// The refusal arrives inside the write that carries the second of the folded
// pair, so the first of them has landed and the addresses after it land too:
// the plane keeps a value at one of two addresses that are one address to it,
// which is the loss the injectivity rule exists to prevent, and the dump reports
// it rather than hiding it. That is the behaviour the capability exists to let a
// driver opt out of, and it is unchanged for one that does not.
func TestADynamicCollisionWithoutAPreparerLandsAtTheWrite(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envTransform)

	err := ferry.Dump(t.Context(), foldedKeys(), sink)
	if err == nil {
		t.Fatal("two map keys render to one plane key and the dump succeeded")
	}

	if got, want := sink.written(), []string{"LABELS_A_B", "NAME"}; !slices.Equal(got, want) {
		t.Errorf("the plane holds %v, want %v: a sink that neither stages nor prepares learns of the fold at "+
			"the colliding write, and the writes around it have landed", got, want)
	}

	mustLocate(t, err, ferry.At("labels", "a_b"))
}

// TestTwoDumpsThroughOneBinding is ADR-0012's amendment through the engine, and
// its second case is the variant the amendment refuses, written as a fixture so
// that the defect is shown failing rather than only the fix shown passing.
//
// Neither value collides with itself. The two map keys are one plane key under
// the driver's transform and they are written by two different dumps, which is
// the case a retained minted set refuses with no defect behind it.
func TestTwoDumpsThroughOneBinding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		sink  *flatSink
		check func(t *testing.T, err error)
	}{
		{name: "the minted set on the open", sink: newFlatSink(envTransform), check: checkSecondDumpWrote},
		{name: "the minted set retained on the binding", sink: retaining(), check: checkSecondDumpRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, twoDumps(t, tc.sink))
		})
	}
}

func retaining() *flatSink {
	s := newFlatSink(envTransform)
	s.retain = true

	return s
}

func twoDumps(t *testing.T, sink *flatSink) error {
	t.Helper()

	first := labelsConf{Name: "a", Labels: map[string]string{"http-port": "1"}}
	if err := ferry.Dump(t.Context(), first, sink); err != nil {
		t.Fatalf("the first dump: %+v", err)
	}

	second := labelsConf{Name: "b", Labels: map[string]string{"http_port": "2"}}

	return ferry.Dump(t.Context(), second, sink)
}

func checkSecondDumpWrote(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("the second dump held one of the two keys and was refused: %+v", err)
	}
}

func checkSecondDumpRefused(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("the retained minted set accepted the second of two writes it holds as one key")
	}

	mustLocate(t, err, ferry.At("labels", "http_port"))
	mustName(t, err, "/labels/http-port", `"LABELS_HTTP_PORT"`)
}

// treeStore is a plane with no key space: a tree of nested mappings, which is
// what a YAML or a Registry driver walks the segments into.
type treeStore struct{ root map[string]any }

func newTreeStore() *treeStore { return &treeStore{root: map[string]any{}} }

func childOf(node map[string]any, name string) map[string]any {
	kid, ok := node[name].(map[string]any)
	if !ok {
		kid = map[string]any{}
		node[name] = kid
	}

	return kid
}

func (s *treeStore) at(addr ferry.Path) any {
	var cur any = s.root

	for seg := range addr.Segments() {
		node, ok := cur.(map[string]any)
		if !ok {
			return nil
		}

		cur = node[seg.Text()]
	}

	return cur
}

// treeSink and treeSource are one plane in two types, because one type cannot
// have two Bind methods (ADR-0004). Neither calls ferry.NewKeys, and neither has
// a key function to call it with.
type treeSink struct{ s *treeStore }

func (t treeSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return t, nil }, nil
}

func (t treeSink) Set(_ context.Context, addr ferry.LeafAddr, v ferry.Value) error {
	return t.write(addr.Path(), v)
}

// Ensure walks the segments exactly as Set does. A tree plane spells a null at
// a container's own address as a node holding one, which is what a reload reads
// back.
func (t treeSink) Ensure(_ context.Context, addr ferry.Container, p ferry.Presence) error {
	if p != ferry.PresenceNull {
		return nil
	}

	return t.write(addr.Path(), ferry.Null)
}

func (t treeSink) write(addr ferry.Path, v ferry.Value) error {
	segs := slices.Collect(addr.Segments())

	node := t.s.root
	for _, seg := range segs[:len(segs)-1] {
		node = childOf(node, seg.Text())
	}

	node[segs[len(segs)-1].Text()] = v

	return nil
}

type treeSource struct{ s *treeStore }

func (t treeSource) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return t, nil }, nil
}

func (t treeSource) Get(_ context.Context, addr ferry.LeafAddr) (ferry.Value, error) {
	v, ok := t.s.at(addr.Path()).(ferry.Value)
	if !ok {
		return ferry.Value{}, nil
	}

	return v, nil
}

// Probe answers at a container's own address out of the same tree, which is
// what makes a section this plane holds visible to a load.
func (t treeSource) Probe(_ context.Context, addr ferry.Container) (ferry.SectionInfo, error) {
	switch node := t.s.at(addr.Path()).(type) {
	case nil:
		return ferry.SectionAbsent, nil
	case ferry.Value:
		if node.Kind() == ferry.KindNull {
			return ferry.SectionNull, nil
		}

		return ferry.SectionPresent, nil
	default:
		return ferry.SectionPresent, nil
	}
}

type treeDB struct {
	Host string `ferry:"HOST"`
}

type treeConf struct {
	DB     treeDB `ferry:"DB"`
	Legacy string `ferry:"DB_HOST"`
}

func treeFilled() treeConf { return treeConf{DB: treeDB{Host: "db1"}, Legacy: "legacy"} }

// TestAFlatDriverRefusesTheSetATreeDriverCarries is the first row of ADR-0003's
// table arriving through the engine, and it is the near half of the asymmetry
// the next test states: this schema is impossible on env and ordinary on YAML,
// and it is reported as env's problem rather than as ferry's.
func TestAFlatDriverRefusesTheSetATreeDriverCarries(t *testing.T) {
	t.Parallel()

	sink := newFlatSink(envUpper)

	err := ferry.Dump(t.Context(), treeFilled(), sink)
	if err == nil {
		t.Fatal("/DB/HOST and /DB_HOST are one environment variable and the dump succeeded")
	}

	if sink.opens != 0 {
		t.Errorf("the plane was opened %d times for a set refused at Bind", sink.opens)
	}

	mustLocate(t, err, ferry.At("DB_HOST"))
	mustName(t, err, "/DB/HOST", `"DB_HOST"`)
}

// TestATreeDriverCarriesNoInjectivityObligation is ADR-0004's asymmetry: a tree
// plane pays nothing for the address set, because it never builds a plane key.
// It walks the segments, so it calls the helper not at all, and it has no key
// function for the helper to check.
func TestATreeDriverCarriesNoInjectivityObligation(t *testing.T) {
	t.Parallel()

	want := treeFilled()
	store := newTreeStore()

	if err := ferry.Dump(t.Context(), want, treeSink{s: store}); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	checkTreeShape(t, store)

	got, err := ferry.Load[treeConf](t.Context(), treeSource{s: store})
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	if got != want {
		t.Errorf("round tripped to %+v, want %+v", got, want)
	}
}

// checkTreeShape is why the tree driver carries no obligation, shown rather than
// asserted: the two addresses a flat key space folds into one are two places
// here, and no string was ever joined to reach either.
func checkTreeShape(t *testing.T, store *treeStore) {
	t.Helper()

	if n := len(store.root); n != 2 {
		t.Fatalf("the plane holds %d members at its root, want 2", n)
	}

	if db, ok := store.root["DB"].(map[string]any); !ok || len(db) != 1 {
		t.Errorf("/DB/HOST did not land under a mapping at DB: %v", store.root["DB"])
	}

	if _, ok := store.root["DB_HOST"].(ferry.Value); !ok {
		t.Errorf("/DB_HOST did not land as a value at the root: %v", store.root["DB_HOST"])
	}
}
