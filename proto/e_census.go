package main

// E1. The census.
//
// ADRs 0001 through 0009 defer "the error types every refusal here produces" to
// #9, and every one of them is written as though #9's convention already
// exists. Before anything else, #9 has to enumerate that union and check ONE
// model against every row, rather than answering the ticket body alone and
// discovering the misfits later.
//
// A row FITS if the model expresses it with the four things it carries -
// location, moment, class, cause - and nothing more. A row that does not fit is
// printed as a misfit rather than smoothed over.

import (
	"fmt"
	"slices"
	"strings"
)

type refusal struct {
	src    string // the ADR that produced it
	what   string
	mom    moment
	class  error // nil = no ferry class
	driver bool
	hasLoc bool
	fits   bool
	note   string
}

// The union. Every row is a refusal an accepted ADR either measured or
// required, cited by the ADR that owns it.
var census = []refusal{
	// ---- registration, ADR-0009 -------------------------------------------
	{src: "0009", what: "type already in the identity table", mom: mRegister, class: ErrSchema, fits: true},
	{src: "0009", what: "registration after the registry froze", mom: mRegister, class: ErrSchema, fits: true},
	{src: "0009", what: "key codec registered without opting in", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0009", what: "half a codec pair", mom: mNone, fits: false,
		note: "a BUILD error, not an error value: generic inference refuses it. Out of the model's scope, and that is a result rather than a gap"},

	// ---- the tag, ADR-0008 -------------------------------------------------
	{src: "0008", what: "raw tag not in conventional key:\"value\" form", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "tag value is not a valid Go quoted string", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "unterminated quoted tag value", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "field carries two ferry tags", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "exported field carries no ferry tag", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true,
		note: "location is the GO FIELD PATH: the field never named a segment, so it has no address"},
	{src: "0008", what: "tag on an unexported field", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "empty name", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "name contains =, looks like an option with no name", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "unterminated quoted token", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "text after the closing quote", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "empty option, two commas", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "-,required: - names no segment", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "unknown option, with a near-miss suggestion", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "whitespace around an option", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "the tag-key Option is not a legal struct tag key", mom: mNone, class: ErrSchema, fits: false,
		note: "validated WHERE THE OPTION IS SUPPLIED, not at schema compile, so it belongs to no moment in the list"},

	// ---- defaults and options, ADR-0006 -----------------------------------
	{src: "0006", what: "required inadmissible at this type", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0006", what: "default on a composite", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0006", what: "default text does not parse for the leaf type", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0006", what: "required and default contradict", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0006", what: "omitzero and a non-zero default contradict", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},

	// ---- the type set, ADR-0005 -------------------------------------------
	{src: "0005", what: "unsupported type, by kind", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0005", what: "struct maps no address", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0005", what: "recursive type", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0005", what: "map key type not admissible", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},

	// ---- the codec chain, ADR-0007 ----------------------------------------
	{src: "0007", what: "text pair: encoder without decoder", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0007", what: "text pair: decoder without encoder", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0007", what: "UnmarshalText on a value receiver", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},

	// ---- embedding, ADR-0008 ----------------------------------------------
	{src: "0008", what: "promoted embedded pointer", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},
	{src: "0008", what: "embedded field that is not a walkable struct", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true},

	// ---- the address model, ADR-0003 --------------------------------------
	{src: "0003", what: "prefix-free violation, two fields at one address", mom: mCompile, class: ErrSchema, hasLoc: true, fits: true,
		note: "names BOTH offending addresses, so one error carries two locations"},
	{src: "0003", what: "driver key function not injective over the set", mom: mBind, class: ErrPlane, hasLoc: true, fits: true,
		note: "CORE produces it, per ADR-0004's key-function helper, so it carries no Driver marker"},
	{src: "0003", what: "segment illegal on this plane", mom: mBind, class: ErrPlane, driver: true, hasLoc: true, fits: true,
		note: "the DRIVER produces it, and ErrorAt is what lets it name the address"},

	// ---- open, ADR-0004 / ADR-0001 ----------------------------------------
	{src: "0004", what: "plane unreachable at Open", mom: mOpen, class: ErrPlane, driver: true, fits: true},
	{src: "0001", what: "malformed document at Open (5.11)", mom: mOpen, class: ErrValue, driver: true, fits: true,
		note: "the DRIVER declares Value rather than Plane: it is the operator's file, not the infrastructure"},
	{src: "0004", what: "ErrReadOnly at OpenWriter", mom: mOpen, class: ErrPlane, driver: true, fits: true},

	// ---- the walk ----------------------------------------------------------
	{src: "0006", what: "required, and the plane was silent", mom: mWalk, class: ErrMissing, hasLoc: true, fits: true},
	{src: "0006", what: "required at a composite, nothing under it", mom: mWalk, class: ErrMissing, hasLoc: true, fits: true},
	{src: "0006", what: "Null at a leaf that has no null", mom: mWalk, class: ErrValue, hasLoc: true, fits: true},
	{src: "0005", what: "wrong kind at a leaf", mom: mWalk, class: ErrValue, hasLoc: true, fits: true},
	{src: "0005", what: "leaf text does not parse", mom: mWalk, class: ErrValue, hasLoc: true, fits: true,
		note: "the cause is strconv's and stays REACHABLE, but is never printed: NumError quotes its input"},
	{src: "0005", what: "plane has index N and the array holds M", mom: mWalk, class: ErrValue, hasLoc: true, fits: true},
	{src: "0003", what: "dynamic address collision, minted from a map key", mom: mWalk, class: ErrValue, hasLoc: true, fits: true,
		note: "same rule as the prefix-free check, other tier, other class: it is the OPERATOR's map that collided"},
	{src: "0004", what: "map field loaded from a non-enumerating source", mom: mWalk, class: ErrPlane, hasLoc: true, fits: true,
		note: "not Schema: Validate[T]() cannot catch it, because it needs the source"},
	{src: "0004", what: "enumerator returned a non-Index child under a sequence", mom: mWalk, class: ErrPlane, driver: true, hasLoc: true, fits: true},
	{src: "0004", what: "driver Get failed", mom: mWalk, class: ErrPlane, driver: true, hasLoc: true, fits: true},
	{src: "0004", what: "driver Set failed", mom: mWalk, class: ErrPlane, driver: true, hasLoc: true, fits: true},
	{src: "0007", what: "a codec's own decode error", mom: mWalk, class: ErrValue, hasLoc: true, fits: true},
	{src: "0005", what: "encode failed (time.Time year out of range)", mom: mWalk, class: ErrValue, hasLoc: true, fits: true},
	{src: "0004", what: "context cancelled mid-walk", mom: mWalk, fits: true,
		note: "NO ferry class: errors.Is(err, context.Canceled) is the match, and a ferry class would be a second spelling of a stdlib one"},

	// ---- the end of a dump, ADR-0004 --------------------------------------
	{src: "0004", what: "Commit failed", mom: mCommit, class: ErrPlane, driver: true, fits: true},
	{src: "0004", what: "Close failed", mom: mClose, class: ErrPlane, driver: true, fits: true,
		note: "arrives ALONGSIDE walk errors, and is what forced the moment into the sort key"},
}

func runCensus() {
	hdr("E1  the census: every refusal ADRs 0001-0009 deferred here")

	byMoment := map[moment]int{}
	byClass := map[string]int{}
	bySrc := map[string]int{}
	drivers, located, misfits := 0, 0, 0
	for _, r := range census {
		byMoment[r.mom]++
		byClass[className(r.class)]++
		bySrc[r.src]++
		if r.driver {
			drivers++
		}
		if r.hasLoc {
			located++
		}
		if !r.fits {
			misfits++
		}
	}

	fmt.Printf("%d refusals enumerated, from %d ADRs\n\n", len(census), len(bySrc))

	fmt.Println("by moment:")
	for m := mRegister; m <= mNone; m++ {
		if byMoment[m] > 0 {
			fmt.Printf("  %-9s %3d\n", m, byMoment[m])
		}
	}

	fmt.Println("\nby class:")
	for _, c := range []error{ErrSchema, ErrMissing, ErrValue, ErrPlane} {
		fmt.Printf("  %-9s %3d\n", c.Error(), byClass[c.Error()])
	}
	fmt.Printf("  %-9s %3d\n", "(none)", byClass["(none)"])
	fmt.Printf("\n  carrying the Driver marker: %d\n", drivers)
	fmt.Printf("  carrying a location:        %d of %d\n", located, len(census))

	fmt.Println("\nby the ADR that deferred it:")
	srcs := slices.Sorted(maps_Keys(bySrc))
	for _, s := range srcs {
		fmt.Printf("  ADR-%s  %3d\n", s, bySrc[s])
	}

	fmt.Printf("\nrows the four carried things do NOT express: %d\n", misfits)
	for _, r := range census {
		if r.fits {
			continue
		}
		fmt.Printf("  - %s (ADR-%s)\n      %s\n", r.what, r.src, wrapNote(r.note))
	}

	fmt.Println("\nrows that fit but carry a qualification worth the ADR saying out loud:")
	for _, r := range census {
		if !r.fits || r.note == "" {
			continue
		}
		fmt.Printf("  - %s\n      %s\n", r.what, wrapNote(r.note))
	}
}

func className(c error) string {
	if c == nil {
		return "(none)"
	}
	return c.Error()
}

func wrapNote(s string) string {
	const width = 82
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > width {
			out = append(out, line)
			line = ""
		}
		if line == "" {
			line = w
		} else {
			line += " " + w
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return strings.Join(out, "\n      ")
}

func maps_Keys(m map[string]int) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
