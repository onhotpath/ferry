package ferry

import "strconv"

// Presence is what a plane holds at a container address, and the set is closed
// at three: [PresenceAbsent], [PresencePresent] and [PresenceNull].
//
// It is the container-side counterpart of [VKind]. A container is read one
// child at a time and has no group value of its own, so the only thing there is
// to ask at its own address is whether it is there.
type Presence uint8

const (
	// PresenceAbsent means the plane does not have this address at all. It is
	// presence zero, so the zero [SectionInfo] is absence.
	PresenceAbsent Presence = iota

	// PresencePresent means the plane has this address and holds a container
	// there, which may be an empty one.
	PresencePresent

	// PresenceNull means the plane has this address and holds its own null
	// there. Only a plane whose type system contains a null can produce it.
	PresenceNull

	// PresenceElsewhere means the plane holds a link here, and what this
	// address names lives at the one [SectionInfo.Redirect] hands back. It is
	// not an answer about this address's own contents, and reading it as one is
	// the mistake this presence exists to make impossible.
	PresenceElsewhere
)

// presenceName is the closed presence set made mechanical: one entry per
// presence, in order, and the assertion below stops the package compiling if
// one is added without a name.
var presenceName = [...]string{"absent", "present", "null", "elsewhere"}

var _ [len(presenceName)]struct{} = [int(PresenceElsewhere) + 1]struct{}{}

// String names the presence in lower case: absent, present, null.
func (p Presence) String() string {
	if int(p) < len(presenceName) {
		return presenceName[p]
	}

	return "Presence(" + strconv.Itoa(int(p)) + ")"
}

// SectionInfo is what a plane answers about a container address: information
// about a section, reported by a probe.
//
// Return one of [SectionPresent], [SectionAbsent] or [SectionNull] from
// [Prober.Probe], or [SectionAt] where the plane holds a link and what the
// address names lives somewhere else. The zero SectionInfo is absence, so a
// driver with nothing to report returns ferry.SectionInfo{}.
//
// It is comparable, so a caller may assert on it with ==, and [SectionInfo.Presence]
// reads it as a value to switch on.
type SectionInfo struct {
	presence Presence

	// target is the address a link points at, and it is non-nil exactly where
	// presence is PresenceElsewhere. It is an interface holding one of two
	// sealed struct types, both comparable, so SectionInfo stays comparable and
	// no dynamic type outside this package can reach the field (ADR-0016).
	target Container
}

// _ asserts SectionInfo stays comparable, which is what lets the three
// sentinels be compared with == rather than through an accessor.
var _ map[SectionInfo]struct{}

// The three plain answers are values a driver returns rather than functions it
// calls, which is io.EOF's and fs.ErrNotExist's idiom. A driver's Probe then
// reads as prose:
//
//	switch node := d.at(addr); {
//	case node == nil:   return ferry.SectionAbsent, nil
//	case node.isNull(): return ferry.SectionNull, nil
//	default:            return ferry.SectionPresent, nil
//	}

// SectionAbsent is the plane saying it does not have this address at all.
//
// Being a var, it can be assigned to, and doing so changes only what this name
// reads as: ferry's own paths do not go through it, so a program that reassigns
// it breaks its own comparisons and nothing else. Do not.
var SectionAbsent = sectionAbsent

// SectionPresent is the plane saying the container is there, possibly holding
// nothing. It carries the same reassignability caveat as [SectionAbsent].
var SectionPresent = sectionPresent

// SectionNull is the plane saying the container is there and holds the plane's
// own null. A driver over a plane whose grammar has no null never returns it,
// and it carries the same reassignability caveat as [SectionAbsent].
var SectionNull = sectionNull

// The copies core itself reads. A sentinel that ferry both publishes and reads
// is one assignment away from making ferry.SectionPresent = ferry.SectionNull
// rewrite what every probe means, in a process the assigning package does not
// own, so the exported names are copies handed out once and these are what the
// walk compares against (ADR-0016, ADR-0017).
var (
	sectionAbsent  = SectionInfo{presence: PresenceAbsent}
	sectionPresent = SectionInfo{presence: PresencePresent}
	sectionNull    = SectionInfo{presence: PresenceNull}
)

// SectionAt is the plane saying it holds a link at this address, and that what
// the address names lives at target.
//
// It is the sentence a driver over a plane with aliases says: this section is
// that one. Report one hop and stop. Following the chain, keeping the set of
// addresses already visited and refusing a cycle are all done for you, once,
// so every driver tells the same redirect story and none of them has to write
// the loop.
//
//	switch node := d.at(addr); {
//	case node == nil:    return ferry.SectionAbsent, nil
//	case node.isAlias(): return ferry.SectionAt(d.targetOf(node)), nil
//	default:             return ferry.SectionPresent, nil
//	}
//
// The target is an address you were handed, because nothing outside ferry
// builds one. A link whose target this schema does not name therefore cannot be
// reported at all, and resolving it, or refusing it in your own words, stays
// yours.
//
// The target must be the same kind of place as the address it was reported at.
// What is under a section comes from the type and what is under a composite
// comes from the value, so a section that named a composite would be a link to
// somewhere its own members could not be, and it is refused.
func SectionAt(target Container) SectionInfo {
	return SectionInfo{presence: PresenceElsewhere, target: target}
}

// Presence reports what the plane holds at the address: absent, present, null,
// or elsewhere where the plane holds a link.
//
// It never answers about a link's target, and that is why elsewhere is one of
// the four: an accessor that reported a link as absence would make a populated
// section read as an empty one, silently.
func (i SectionInfo) Presence() Presence { return i.presence }

// Redirect is the address a link points at, and whether this answer is one.
//
// It reports false for every plain answer, so a caller that reads presence
// alone is never wrong about a link, only incomplete.
func (i SectionInfo) Redirect() (Container, bool) { return i.target, i.target != nil }

// GoString renders a SectionInfo for a diff or a test failure: absent, present,
// null, or elsewhere(/primary).
func (i SectionInfo) GoString() string {
	if i.target == nil {
		return i.presence.String()
	}

	return i.presence.String() + "(" + i.target.String() + ")"
}
