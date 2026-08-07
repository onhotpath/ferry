package ferrytest

import (
	"github.com/onhotpath/ferry"
)

// casePrepared is case 18: a dump the plane refuses over an address the value
// minted leaves the plane untouched, for a sink that declared it can be told in
// time (ADR-0004, ADR-0011).
//
// The addresses under a mapping come from the value, so the plane key each of
// them renders to is one no Bind could have checked: there was no value then.
// A plane that folds two of them onto one key therefore learns it during the
// dump, and where it learns it decides what the dump leaves behind - at the
// colliding write, the writes before it have landed and a save that failed has
// still changed the plane.
//
// Two capabilities make an untouched plane an obligation and the case runs for
// either. A [ferry.Committer] stages, so a dump that fails is a Commit that does
// not happen; a [ferry.Preparer] is handed the addresses the value determined
// before the first write, so it can refuse there. A sink declaring neither is
// skipped, out loud: it is refused nothing by the contract and cannot fail a
// case for a claim its author never made.
//
// A plane that takes the dump is skipped too, and that is not the case failing
// to run. It means the plane renders no two of those addresses to one key, which
// is the ordinary answer for a tree plane and for any flat plane whose key
// function folds nothing, and there is then no refusal to ask about.
func (d *driverRun) casePrepared() {
	d.rep.Helper()

	inst := d.plane.Open()
	if inst.Sink == nil || inst.Source == nil {
		return
	}

	if !d.declaresUntouched(inst) {
		d.skip(casePreparedNo, "the plane's sink neither stages nor asks to be handed the addresses a dump "+
			"realised, so nothing obliges it to refuse a folded pair before the writes around it have landed")

		return
	}

	if err := ferry.Dump(inst.ctx(), foldedFixture(), inst.Sink, d.opts...); err == nil {
		d.skip(casePreparedNo, "the plane took both of the two map keys, so it renders no two addresses of "+
			"this dump to one plane key and there is no refusal here to hold it to")

		return
	}

	d.nothingLanded(inst)
}

// declaresUntouched opens the write half once and asks whether it declared
// either capability, which is the same question on the same value core's own
// dump asks: on the writer an open returned, and never on the [ferry.Sink].
func (d *driverRun) declaresUntouched(inst Instance) bool {
	d.rep.Helper()

	w, ok := d.openedWriter(inst)
	if !ok {
		return false
	}

	defer closeIf(w)

	_, stages := w.(ferry.Committer)
	_, prepares := w.(ferry.Preparer)

	return stages || prepares
}

// nothingLanded is the other half: what the plane holds after the refused dump,
// read at the one address of it that did not collide.
//
// It reads back a leaf and not the mapping, so a source that cannot list is
// asked nothing it cannot answer. The leaf is absent on a fresh plane and the
// refused dump is the only thing that could have written it, so holding
// anything there is the dump having landed in part.
func (d *driverRun) nothingLanded(inst Instance) {
	d.rep.Helper()

	got, err := ferry.Load[onlyLeaf](inst.ctx(), inst.Source, d.opts...)
	if err != nil {
		d.skip(casePreparedNo, "what the refused dump left could not be read back, so there is nothing to "+
			"compare: "+err.Error())

		return
	}

	if got.Leaf == "" {
		return
	}

	d.fail(casePreparedNo, "a dump refused over two map keys the plane renders to one key left "+
		got.Leaf+" behind at "+addrLeaf.String()+": a sink that stages, or one that is handed the "+
		"addresses a value determined before the first write, refuses a dump with the plane untouched, "+
		"and a save that failed and changed the plane anyway is the one a caller cannot recover from")
}
