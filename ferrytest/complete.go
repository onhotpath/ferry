package ferrytest

import (
	"reflect"
	"slices"
	"time"

	"github.com/onhotpath/ferry"
)

// Complete reports every type ferry carries that the supplied proofs do not
// discharge.
//
//	for _, s := range ferrytest.Complete(nil, ferrytest.CoreTypes()...) {
//	    t.Errorf("core type set: %s", s)
//	}
//
//	for _, s := range ferrytest.Complete(reg, append(ferrytest.CoreTypes(), mine...)...) {
//	    t.Errorf("registry: %s", s)
//	}
//
// It joins over the union of three tables - core's identity table, one
// representative type per kind core admits as a leaf, and reg - and returns
// data rather than asserting, so a registrant appends their own proofs and asks
// the same question core asks about its own set. A nil registry means core's
// two tables alone.
//
// ADR-0013 is why this is not housekeeping. The compatibility promise ferry
// makes about what a plane holds is exactly as wide as the proof table, so an
// admitted member with no row is outside the promise by accident rather than by
// decision. Run for the first time against the table it replaced, this reported
// seven admitted members with no proof, and they were the integer widths nobody
// would think to doubt.
//
// The join is by reflect.Type and never by name. Name is a label for a report:
// two proofs may share one and mean different types, and a check joining on it
// needs a hand-written special case so that a proof named "[]byte" can discharge
// a fixed-size array member (ADR-0014).
//
// The result is sorted, so a report is one string over repeated runs
// (ADR-0011).
func Complete(reg *ferry.Registry, proofs ...Proof) []string {
	have := make(map[reflect.Type]bool, len(proofs))
	for _, p := range proofs {
		have[p.Type()] = true
	}

	var (
		out  []string
		seen = map[reflect.Type]bool{}
	)

	for _, m := range members(reg) {
		if have[m.typ] || seen[m.typ] {
			continue
		}

		seen[m.typ] = true

		out = append(out, m.typ.String()+" "+m.table+" and has no proof")
	}

	slices.Sort(out)

	return out
}

// member is one type the check joins on, with the clause naming the table it
// came from: a report that says only "no proof" leaves a reader working out
// which of three obligations they are looking at.
type member struct {
	typ   reflect.Type
	table string
}

// members is the union of the three tables, in table order. A type appearing in
// two of them is reported once, under the first, which is [Complete]'s doing
// rather than this function's.
func members(reg *ferry.Registry) []member {
	out := make([]member, 0, len(identityTable)+len(admittedKinds))

	for _, t := range identityTable {
		out = append(out, member{typ: t, table: "is in core's identity table"})
	}

	for _, k := range admittedKinds {
		out = append(out, member{typ: representative(k), table: "is core's representative for kind " + k.String()})
	}

	for _, t := range reg.Types() {
		out = append(out, member{typ: t, table: "has a registered codec"})
	}

	return out
}

// identityTable is the two leaves ferry owns by type identity rather than by
// kind.
//
// It is written out here rather than read off the engine, and that is the same
// decision [CoreTypes] takes for the same reason: a census taken from the
// compiler is a census of whatever the compiler currently tolerates, and the
// point of the check is that the promise is complete from the ticket that
// writes it.
var identityTable = []reflect.Type{
	reflect.TypeFor[time.Duration](),
	reflect.TypeFor[time.Time](),
}

// admittedKinds is every reflect.Kind core admits as a leaf: bool, string, the
// five signed and five unsigned integer widths, both float widths, and the two
// shapes of byte sequence.
var admittedKinds = []reflect.Kind{
	reflect.Bool, reflect.String,
	reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
	reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
	reflect.Float32, reflect.Float64,
	reflect.Slice, reflect.Array,
}

// kindRepresentative is the one type per admitted kind that the join names.
//
// A kind is not a type, so the union needs a canonical member and core picks it
// rather than leaving every proof author to guess which fixed-size array is the
// one they have to discharge.
var kindRepresentative = map[reflect.Kind]reflect.Type{
	reflect.Bool:    reflect.TypeFor[bool](),
	reflect.String:  reflect.TypeFor[string](),
	reflect.Int:     reflect.TypeFor[int](),
	reflect.Int8:    reflect.TypeFor[int8](),
	reflect.Int16:   reflect.TypeFor[int16](),
	reflect.Int32:   reflect.TypeFor[int32](),
	reflect.Int64:   reflect.TypeFor[int64](),
	reflect.Uint:    reflect.TypeFor[uint](),
	reflect.Uint8:   reflect.TypeFor[uint8](),
	reflect.Uint16:  reflect.TypeFor[uint16](),
	reflect.Uint32:  reflect.TypeFor[uint32](),
	reflect.Uint64:  reflect.TypeFor[uint64](),
	reflect.Float32: reflect.TypeFor[float32](),
	reflect.Float64: reflect.TypeFor[float64](),
	reflect.Slice:   reflect.TypeFor[[]byte](),
	reflect.Array:   reflect.TypeFor[[3]byte](),
}

// representative is one admitted kind's canonical member, and it panics for a
// kind that has none.
//
// The panic is the whole point rather than defensiveness. This check exists to
// catch drift between what ferry admits and what the table proves, so a kind
// added to the list and to no representative would be silently skipped by the
// one mechanism that would have reported it - which is the failure mode, not a
// smaller version of it.
func representative(k reflect.Kind) reflect.Type {
	return lookUpRepresentative(k, kindRepresentative)
}

// lookUpRepresentative takes the table as an argument so that the panic has a
// test, which a package-level lookup that can never miss does not.
func lookUpRepresentative(k reflect.Kind, table map[reflect.Kind]reflect.Type) reflect.Type {
	t, ok := table[k]
	if !ok {
		panic("ferrytest: " + k.String() + " is admitted as a leaf kind and has no representative type: " +
			"the completeness check names one member per kind, so a kind with none is skipped by the check " +
			"that exists to catch exactly that")
	}

	return t
}
