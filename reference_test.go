package ferry

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
)

// A plane that reports links rather than resolving them, and what core does
// with what it reports.
//
// A driver has two ways to serve a plane with aliases. It can resolve them
// itself and report nothing, which is what driver/yaml does with a YAML anchor;
// or it can report one hop and let core follow the chain, keep the set of
// addresses already asked and refuse a cycle. This file is the second, which is
// the one that exists so that cycle discipline is written once instead of once
// per driver (ADR-0016).

// refSection is what every alias in these fixtures points at: one section with
// one leaf under it, so a link is a link to a place and not to a value.
type refSection struct {
	Host string `ferry:"host"`
}

// refChain is the shape the two-hop case needs: three optional sections, so
// that /secondary can name /primary and /primary can name /defaults.
type refChain struct {
	Defaults  *refSection `ferry:"defaults"`
	Primary   *refSection `ferry:"primary"`
	Secondary *refSection `ferry:"secondary"`
}

// refPair is two aliases of one target, which is the shape the write side turns
// on: the caller mutates one of them and the dump has to decide.
type refPair struct {
	Target *refSection `ferry:"t"`
	A      *refSection `ferry:"a"`
	B      *refSection `ferry:"b"`
}

// refLeaves is the leaf arm on its own, with no section above it to resolve.
type refLeaves struct {
	Here  string `ferry:"here"`
	There string `ferry:"there"`
}

// refPlane holds contents and the links over them.
//
// links is by section address, and a link at a section is a link to everything
// under it, which is what an alias means: /a naming /t makes /a/host the same
// place as /t/host. Both arms of the resolution are driven from the one map,
// because a plane with two independent notions of a link is not a plane anyone
// has.
type refPlane struct {
	values map[Path]Value
	links  map[Path]Path

	// bound is the set Bind was handed, and it is what the target of a reported
	// link is looked up in. A driver cannot mint an address, so the only
	// targets it can report are ones it was given, and looking them up here
	// rather than building them is what makes this plane behave like one.
	bound *AddressSet

	// foreign makes the plane report a target from another schema's set, which
	// is the one wrong target a driver can still produce and the only thing
	// left for core to check.
	foreign bool

	// lost makes the plane report a link and name nowhere, which is what a
	// driver helper returning (Container, bool) produces when its second result
	// is not checked.
	lost bool

	got  []Path
	set  []Path
	kept []Path
}

func newRefPlane(values map[Path]Value, links map[Path]Path) *refPlane {
	return &refPlane{values: maps.Clone(values), links: maps.Clone(links)}
}

// linkFor is the section link that covers an address: the address itself where
// it is a linked section, or the section above it where the address is a leaf
// beneath one.
func (p *refPlane) linkFor(at Path) (from, to Path, linked bool) {
	if to, ok := p.links[at]; ok {
		return at, to, true
	}

	for from, to := range p.links {
		if from.isPrefixOf(at) {
			return from, to.concat(at.below(from)), true
		}
	}

	return Path{}, Path{}, false
}

// section finds the target's own address in the set Bind was handed, at the
// kind it has to be.
func (p *refPlane) section(at Path) (Container, bool) {
	if p.lost {
		return nil, true
	}

	if p.foreign {
		return sectionOf(at), true
	}

	for m := range p.bound.Seq() {
		if c, ok := m.(Container); ok && c.Path() == at {
			return c, true
		}
	}

	return nil, false
}

// leaf finds a leaf target the same way.
func (p *refPlane) leaf(at Path) (LeafAddr, bool) {
	for m := range p.bound.Seq() {
		if l, ok := m.(LeafAddr); ok && l.Path() == at {
			return l, true
		}
	}

	return LeafAddr{}, false
}

// Get answers with what is held, or reports the link that says where the value
// lives. It never follows one itself.
func (p *refPlane) Get(_ context.Context, addr LeafAddr) (Value, error) {
	at := addr.Path()
	p.got = append(p.got, at)

	if _, to, linked := p.linkFor(at); linked {
		if target, ok := p.leaf(to); ok {
			return Value{}, &LeafRedirect{Target: target}
		}
	}

	return p.values[at], nil
}

// Probe answers about a container, reporting a link where the plane holds one.
func (p *refPlane) Probe(_ context.Context, addr Container) (SectionInfo, error) {
	at := addr.Path()

	if _, to, linked := p.linkFor(at); linked {
		if target, ok := p.section(to); ok {
			return SectionAt(target), nil
		}
	}

	return p.presence(at), nil
}

// presence is what the plane holds at an address it holds no link at: present
// where anything sits under it, absent otherwise.
func (p *refPlane) presence(at Path) SectionInfo {
	for held := range p.values {
		if at.isPrefixOf(held) && at != held {
			return SectionPresent
		}
	}

	return SectionAbsent
}

// Set writes, under the divergence rule.
//
// An address beneath a section the plane holds a link at is not written where
// the value is what the link already carries: the link is what says so, and
// rewriting it at the alias's own address would break the linkage for a value
// that did not change. A value that differs materialises the whole section at
// its own address and drops the link, leaving the target and every other alias
// exactly as they were (ADR-0016).
func (p *refPlane) Set(_ context.Context, addr LeafAddr, v Value) error {
	at := addr.Path()
	p.set = append(p.set, at)

	from, to, linked := p.linkFor(at)
	if !linked {
		p.values[at] = v

		return nil
	}

	if p.values[to] == v {
		p.kept = append(p.kept, at)

		return nil
	}

	p.materialise(from)
	p.values[at] = v

	return nil
}

// materialise copies what the link carried to the alias's own address and drops
// the link, which is what makes a diverged section stop being one.
func (p *refPlane) materialise(from Path) {
	to, linked := p.links[from]
	if !linked {
		return
	}

	delete(p.links, from)

	for held, v := range p.values {
		if to.isPrefixOf(held) && to != held {
			p.values[from.concat(held.below(to))] = v
		}
	}
}

// Ensure records a container's own answer, which these fixtures reach whenever
// a section is nil.
func (p *refPlane) Ensure(_ context.Context, addr Container, held Presence) error {
	if held == PresenceNull {
		delete(p.values, addr.Path())
	}

	return nil
}

// held is the plane's contents as sorted "address=value" lines, which is what a
// write-side assertion compares.
func (p *refPlane) held() []string {
	out := make([]string, 0, len(p.values))
	for at, v := range p.values {
		out = append(out, at.String()+"="+v.GoString())
	}

	slices.Sort(out)

	return out
}

// linksHeld is the links the plane still holds, sorted.
func (p *refPlane) linksHeld() []string {
	out := make([]string, 0, len(p.links))
	for from, to := range p.links {
		out = append(out, from.String()+"->"+to.String())
	}

	slices.Sort(out)

	return out
}

// refText is every member of a failure as one string, because a walk that
// refuses two sibling addresses reports two elements and the summary line names
// only where they were.
func refText(err error) string {
	out := make([]string, 0, len(Elements(err)))
	for _, e := range Elements(err) {
		out = append(out, e.Error())
	}

	return strings.Join(out, "\n")
}

type refSource struct{ p *refPlane }

func (s refSource) Bind(addrs *AddressSet) (OpenFunc, error) {
	s.p.bound = addrs

	return func(context.Context) (Reader, error) { return s.p, nil }, nil
}

type refSink struct{ p *refPlane }

func (s refSink) Bind(addrs *AddressSet) (OpenWriterFunc, error) {
	s.p.bound = addrs

	return func(context.Context) (Writer, error) { return s.p, nil }, nil
}

// TestALinkResolvesThroughTheChain is the two-hop case: /secondary names
// /primary, /primary names /defaults, and only /defaults holds anything.
//
// It fails to express on the branch before this one, where a driver had no way
// to report a link at all: the whole vocabulary is new, and a plane that held
// its values only at /defaults loaded /secondary as absent.
func TestALinkResolvesThroughTheChain(t *testing.T) {
	t.Parallel()

	p := newRefPlane(
		map[Path]Value{At("defaults", "host"): String("x")},
		map[Path]Path{At("secondary"): At("primary"), At("primary"): At("defaults")},
	)

	got, err := Load[refChain](t.Context(), refSource{p: p})
	if err != nil {
		t.Fatalf("loading through a chain of links: %v", err)
	}

	if got.Secondary == nil {
		t.Fatal("the section at the end of a chain of links loaded as nil, so the chain was not followed")
	}

	if got.Secondary.Host != "x" {
		t.Errorf("the linked section holds %q, want %q", got.Secondary.Host, "x")
	}

	if got.Defaults == nil || got.Defaults.Host != "x" {
		t.Errorf("the target itself loaded as %#v, and following a link changes nothing about it", got.Defaults)
	}
}

// TestALinkCycleIsRefusedInCore is the discipline the reported link buys: /a
// names /b, /b names /a, and the refusal is made once in core rather than in
// every driver that ever grows an alias.
func TestALinkCycleIsRefusedInCore(t *testing.T) {
	t.Parallel()

	p := newRefPlane(nil, map[Path]Path{At("a"): At("b"), At("b"): At("a")})

	_, err := Load[refPair](t.Context(), refSource{p: p})
	if err == nil {
		t.Fatal("a cycle of links loaded cleanly, so the walk followed it until something else stopped it")
	}

	if !strings.Contains(refText(err), "reference cycle through") {
		t.Errorf("the refusal reads %q, and it has to name the cycle it closed through", refText(err))
	}

	if !errors.Is(err, ErrPlane) {
		t.Error("the refusal is not an ErrPlane, and a plane whose links close a loop is the plane's fault")
	}
}

// TestALinkToAnAddressTheSchemaDoesNotNameIsRefused is the boundary case, and
// the sealing is most of the answer to it.
//
// A driver cannot mint an address, so a link whose target the schema does not
// name has nothing to report it with: that case stays the driver's own, to
// resolve internally or to refuse in its own words. What is left for core is a
// target the driver got from somewhere else, which is what this stages.
func TestALinkToAnAddressTheSchemaDoesNotNameIsRefused(t *testing.T) {
	t.Parallel()

	p := newRefPlane(nil, map[Path]Path{At("a"): At("unmapped")})
	p.foreign = true

	_, err := Load[refPair](t.Context(), refSource{p: p})
	if err == nil {
		t.Fatal("a link to an address outside the schema loaded cleanly, so core followed it somewhere")
	}

	if !strings.Contains(refText(err), "the schema does not address") {
		t.Errorf("the refusal reads %q, and it has to say the schema does not address the target", refText(err))
	}

	if !strings.Contains(refText(err), "resolve or to refuse") {
		t.Errorf("the refusal reads %q, and it has to name whose job the case is", refText(err))
	}
}

// TestALinkThatNamesNowhereIsRefused is the inconsistent answer the design says
// cannot exist, built by the obvious driver bug.
//
// A helper returning (Container, bool) whose second result is not checked hands
// SectionAt a nil, and the answer that comes out says the container lives
// elsewhere and names nowhere. Read as a plain answer it means three different
// things at three shapes - a nil pointer stays nil, an empty composite is
// zeroed, an unlistable one is refused - so it is refused as what it is.
func TestALinkThatNamesNowhereIsRefused(t *testing.T) {
	t.Parallel()

	p := newRefPlane(nil, map[Path]Path{At("a"): At("t")})
	p.lost = true

	_, err := Load[refPair](t.Context(), refSource{p: p})
	if err == nil {
		t.Fatal("a probe answering elsewhere with no target loaded cleanly, so an answer about no address at " +
			"all was read as an answer about this one")
	}

	if !strings.Contains(refText(err), "named no target") {
		t.Errorf("the refusal reads %q, and it has to say the answer named no target", refText(err))
	}

	if !strings.Contains(refText(err), "refPlane") {
		t.Errorf("the refusal reads %q, and it has to name the driver that answered", refText(err))
	}

	if !errors.Is(err, ErrPlane) {
		t.Error("the refusal is not an ErrPlane, and an answer no plane should have given is the plane's fault")
	}
}

// TestALeafLinkIsAControlAnswerAndNotAKind is the leaf arm: the value lives at
// another leaf, reported as an error and meaning nothing failed.
//
// It is a control error rather than a seventh kind precisely so that the six a
// value can be stay six, and a codec never has to handle one that means "look
// over there".
func TestALeafLinkIsAControlAnswerAndNotAKind(t *testing.T) {
	t.Parallel()

	p := newRefPlane(
		map[Path]Value{At("there"): String("v")},
		map[Path]Path{At("here"): At("there")},
	)

	got, err := Load[refLeaves](t.Context(), refSource{p: p})
	if err != nil {
		t.Fatalf("loading through a leaf link: %v", err)
	}

	if got.Here != "v" {
		t.Errorf("the linked leaf holds %q, want %q", got.Here, "v")
	}

	if !slices.Contains(p.got, At("there")) {
		t.Errorf("the plane was asked %v, and following a link means asking the target", p.got)
	}
}

// TestALeafLinkCycleIsRefused is the leaf arm's half of the same discipline,
// and it is the same loop written once.
func TestALeafLinkCycleIsRefused(t *testing.T) {
	t.Parallel()

	p := newRefPlane(nil, map[Path]Path{At("here"): At("there"), At("there"): At("here")})

	_, err := Load[refLeaves](t.Context(), refSource{p: p})
	if err == nil {
		t.Fatal("a cycle of leaf links loaded cleanly, so the walk followed it forever or gave up silently")
	}

	if !strings.Contains(refText(err), "reference cycle through") {
		t.Errorf("the refusal reads %q, and it has to name the cycle it closed through", refText(err))
	}
}

// TestADivergedAliasMaterialisesAndTheOthersAreUntouched is the write side.
//
// Two aliases of one target, and the caller mutates one of them. Writing
// through the link would move the target and every other alias with it, which
// is what a YAML anchor means and is dangerous precisely because it is silent.
// The rule taken instead is that an unchanged alias keeps its link and a
// diverged one materialises at its own address.
func TestADivergedAliasMaterialisesAndTheOthersAreUntouched(t *testing.T) {
	t.Parallel()

	p := newRefPlane(
		map[Path]Value{At("t", "host"): String("x")},
		map[Path]Path{At("a"): At("t"), At("b"): At("t")},
	)

	got, err := Load[refPair](t.Context(), refSource{p: p})
	if err != nil {
		t.Fatalf("loading two aliases of one target: %v", err)
	}

	if got.A == nil || got.B == nil || got.A.Host != "x" || got.B.Host != "x" {
		t.Fatalf("the two aliases loaded as %#v and %#v, and both name a section holding x", got.A, got.B)
	}

	got.A.Host = "y"

	if err := Dump(t.Context(), got, refSink{p: p}); err != nil {
		t.Fatalf("dumping one diverged alias: %v", err)
	}

	want := []string{"/a/host=string(\"y\")", "/t/host=string(\"x\")"}
	if held := p.held(); !slices.Equal(held, want) {
		t.Errorf("the plane holds %v, want %v: a diverged alias materialises at its own address and the "+
			"target is untouched", held, want)
	}

	if links := p.linksHeld(); !slices.Equal(links, []string{"/b->/t"}) {
		t.Errorf("the plane holds the links %v, want [/b->/t]: the alias that did not change keeps its link "+
			"and the one that did stops being one", links)
	}
}

// TestAnUnchangedAliasWritesNothingAtItsOwnAddress is the other half, and it is
// what makes the rule a rule rather than a special case: a dump that changed
// nothing leaves a plane with links in it exactly as it found it.
func TestAnUnchangedAliasWritesNothingAtItsOwnAddress(t *testing.T) {
	t.Parallel()

	p := newRefPlane(
		map[Path]Value{At("t", "host"): String("x")},
		map[Path]Path{At("a"): At("t"), At("b"): At("t")},
	)

	got, err := Load[refPair](t.Context(), refSource{p: p})
	if err != nil {
		t.Fatalf("loading two aliases of one target: %v", err)
	}

	if err := Dump(t.Context(), got, refSink{p: p}); err != nil {
		t.Fatalf("dumping the value back unchanged: %v", err)
	}

	want := []string{"/t/host=string(\"x\")"}
	if held := p.held(); !slices.Equal(held, want) {
		t.Errorf("the plane holds %v, want %v: what the plane said is preserved until the value says "+
			"otherwise", held, want)
	}

	if links := p.linksHeld(); !slices.Equal(links, []string{"/a->/t", "/b->/t"}) {
		t.Errorf("the plane holds the links %v, want both: neither alias changed", links)
	}

	if !slices.Contains(p.kept, At("a", "host")) {
		t.Error("the write at the unchanged alias was not recognised as one the link already carried")
	}
}

// TestASectionMayNotLinkToAComposite is the one rule the kinds put on a link.
//
// What is under a section comes from the type and what is under a composite
// comes from the value, so a link between them names a place its own members
// could not be. The driver is handed both kinds and nothing stops it reporting
// the wrong one, so core is where the refusal lands.
func TestASectionMayNotLinkToAComposite(t *testing.T) {
	t.Parallel()

	p := newRefPlane(nil, map[Path]Path{At("sect"): At("comp")})

	_, err := Load[struct {
		Sect *refSection       `ferry:"sect"`
		Comp map[string]string `ferry:"comp"`
	}](t.Context(), refSource{p: p})
	if err == nil {
		t.Fatal("a section linked to a composite loaded cleanly, so the walk descended into it either way")
	}

	if !strings.Contains(refText(err), "not the same kind of place") {
		t.Errorf("the refusal reads %q, and it has to say the two are not the same kind of place", refText(err))
	}
}
