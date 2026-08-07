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
)

// presenceName is the closed presence set made mechanical: one entry per
// presence, in order, and the assertion below stops the package compiling if
// one is added without a name.
var presenceName = [...]string{"absent", "present", "null"}

var _ [len(presenceName)]struct{} = [int(PresenceNull) + 1]struct{}{}

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
// [Prober.Probe]. The zero SectionInfo is absence, so a driver with nothing to
// report returns ferry.SectionInfo{}.
//
// It is comparable, so a caller may assert on it with ==, and [SectionInfo.Presence]
// reads it as a value to switch on.
type SectionInfo struct {
	presence Presence
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

// Presence reports what the plane holds at the address: absent, present or
// null.
func (i SectionInfo) Presence() Presence { return i.presence }

// GoString renders a SectionInfo for a diff or a test failure: absent, present,
// null.
func (i SectionInfo) GoString() string { return i.presence.String() }
