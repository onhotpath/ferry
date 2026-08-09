package protect

import "github.com/onhotpath/ferry"

// This file is the whole of how a decorator keeps the plane it wraps.
//
// A [ferry.Reader] and a [ferry.Writer] are one method each, and everything
// else a driver can do - probing a container, enumerating one, releasing a
// resource, committing, forgetting a composite, naming an address in the
// plane's own spelling, tolerating overlapping calls - is discovered by
// assertion on the instance core was handed (ADR-0004, ADR-0019). Go has no way
// to implement an interface conditionally, so a wrapper is exactly the set of
// methods its type declares, for every plane it is ever put in front of.
//
// Both mistakes are silent, and both are bad in a direction nothing reports:
//
//   - declare less than the plane has, and a capability disappears. A dropped
//     [ferry.Unsetter] is refused at the open for every schema holding a slice
//     or a map, and a dropped [ferry.Enumerator] loads one as empty.
//   - declare more, and the wrapper answers a question the plane cannot. A
//     shell claiming [ferry.Committer] over a sink that has none reports that
//     the sink stages when it does not, and a conformance suite asking a driver
//     what it implements would be asking the wrapper instead.
//
// So the shells are exhaustive: one type per combination, thirty-two on the
// read side and sixty-four on the write side, looked up by a bitmask rather
// than branched through. A table is the one shape that keeps every combination
// visible in one place, and a combination with no entry is a hole somebody can
// see rather than a branch nobody wrote. Five nested conditions is also well
// over the nesting the linter allows, which is the same conclusion arrived at
// from the other end.
//
// The technique is ferrytest's shellWriter, copied rather than shared: ADR-0002
// forbids the internal module that would carry it, and that package's own table
// covers five capabilities for a recorder rather than the six a decorator over
// somebody else's sink has to keep.
//
// Which interfaces a shell carries is always the wrapped plane's answer. Which
// object a call goes to is the front's where the front has the method, so this
// package decorates Get and Set and the plane keeps deciding everything else.

// The one-letter aliases the shells are spelled with. They are here so that a
// combination of seven interfaces is one readable line rather than three, and
// they are aliases rather than named types so that embedding one promotes
// exactly the methods embedding the interface itself would.
type (
	rd = ferry.Reader
	wr = ferry.Writer
	pb = ferry.Prober
	en = ferry.Enumerator
	rl = ferry.Releaser
	nm = ferry.PlaneNamer
	cc = ferry.Concurrent
	cm = ferry.Committer
	es = ferry.Ensurer
	un = ferry.Unsetter
	pp = ferry.Preparer
)

// One bit per optional interface a [ferry.Reader] may have, so that the
// thirty-two combinations are an index into [readerShells].
const (
	rProbe = 1 << iota
	rList
	rRelease
	rName
	rBudget
)

// readerCaps is which of the five the wrapped reader had, and the object each
// call goes to. A nil member is a capability the plane does not have, so the
// shell must not claim it either.
type readerCaps struct {
	probe   ferry.Prober
	list    ferry.Enumerator
	release ferry.Releaser
	name    ferry.PlaneNamer
	budget  ferry.Concurrent
}

// readerCapsOf reads what one wrapped reader can do.
func readerCapsOf(r ferry.Reader) readerCaps {
	var c readerCaps

	c.probe, _ = r.(ferry.Prober)
	c.list, _ = r.(ferry.Enumerator)
	c.release, _ = r.(ferry.Releaser)
	c.name, _ = r.(ferry.PlaneNamer)
	c.budget, _ = r.(ferry.Concurrent)

	return c
}

// combination is which of the five the wrapped reader had, as an index. The
// receiver is a pointer because five interface headers is a wide value to copy
// for a question that only reads them.
func (c *readerCaps) combination() int {
	var i int

	for bit, has := range map[int]bool{
		rProbe:   c.probe != nil,
		rList:    c.list != nil,
		rRelease: c.release != nil,
		rName:    c.name != nil,
		rBudget:  c.budget != nil,
	} {
		if has {
			i |= bit
		}
	}

	return i
}

// shellReader puts front in front of r while keeping the optional interfaces r
// had, and no others.
func shellReader(front, r ferry.Reader) ferry.Reader {
	c := readerCapsOf(r)

	return readerShells[c.combination()](front, c)
}

// One bit per optional interface a [ferry.Writer] may have, so that the
// sixty-four combinations are an index into [writerShells].
const (
	wCommit = 1 << iota
	wRelease
	wEnsure
	wUnset
	wPrepare
	wName
)

// writerCaps is [readerCaps] for the write half: six capabilities rather than
// five, and the same rule about a nil member.
type writerCaps struct {
	commit  ferry.Committer
	release ferry.Releaser
	ensure  ferry.Ensurer
	unset   ferry.Unsetter
	prepare ferry.Preparer
	name    ferry.PlaneNamer
}

// writerCapsOf reads what one wrapped writer can do.
func writerCapsOf(w ferry.Writer) writerCaps {
	var c writerCaps

	c.commit, _ = w.(ferry.Committer)
	c.release, _ = w.(ferry.Releaser)
	c.ensure, _ = w.(ferry.Ensurer)
	c.unset, _ = w.(ferry.Unsetter)
	c.prepare, _ = w.(ferry.Preparer)
	c.name, _ = w.(ferry.PlaneNamer)

	return c
}

// combination is [readerCaps.combination] for the write half.
func (c *writerCaps) combination() int {
	var i int

	for bit, has := range map[int]bool{
		wCommit:  c.commit != nil,
		wRelease: c.release != nil,
		wEnsure:  c.ensure != nil,
		wUnset:   c.unset != nil,
		wPrepare: c.prepare != nil,
		wName:    c.name != nil,
	} {
		if has {
			i |= bit
		}
	}

	return i
}

// shellWriter puts front in front of w while keeping the optional interfaces w
// had, and no others.
func shellWriter(front, w ferry.Writer) ferry.Writer {
	c := writerCapsOf(w)

	return writerShells[c.combination()](front, c)
}

// The thirty-two read-side shells, one per combination of the five optional
// interfaces. The digits in a name are the mask [readerCaps.combination]
// computes, so r00 is a reader with none of them and r31 is one with all five.
type (
	r00 struct{ rd }
	r01 struct {
		rd
		pb
	}
	r02 struct {
		rd
		en
	}
	r03 struct {
		rd
		pb
		en
	}
	r04 struct {
		rd
		rl
	}
	r05 struct {
		rd
		pb
		rl
	}
	r06 struct {
		rd
		en
		rl
	}
	r07 struct {
		rd
		pb
		en
		rl
	}
	r08 struct {
		rd
		nm
	}
	r09 struct {
		rd
		pb
		nm
	}
	r10 struct {
		rd
		en
		nm
	}
	r11 struct {
		rd
		pb
		en
		nm
	}
	r12 struct {
		rd
		rl
		nm
	}
	r13 struct {
		rd
		pb
		rl
		nm
	}
	r14 struct {
		rd
		en
		rl
		nm
	}
	r15 struct {
		rd
		pb
		en
		rl
		nm
	}
	r16 struct {
		rd
		cc
	}
	r17 struct {
		rd
		pb
		cc
	}
	r18 struct {
		rd
		en
		cc
	}
	r19 struct {
		rd
		pb
		en
		cc
	}
	r20 struct {
		rd
		rl
		cc
	}
	r21 struct {
		rd
		pb
		rl
		cc
	}
	r22 struct {
		rd
		en
		rl
		cc
	}
	r23 struct {
		rd
		pb
		en
		rl
		cc
	}
	r24 struct {
		rd
		nm
		cc
	}
	r25 struct {
		rd
		pb
		nm
		cc
	}
	r26 struct {
		rd
		en
		nm
		cc
	}
	r27 struct {
		rd
		pb
		en
		nm
		cc
	}
	r28 struct {
		rd
		rl
		nm
		cc
	}
	r29 struct {
		rd
		pb
		rl
		nm
		cc
	}
	r30 struct {
		rd
		en
		rl
		nm
		cc
	}
	r31 struct {
		rd
		pb
		en
		rl
		nm
		cc
	}
)

// readerShells is one constructor per combination, and every combination has an
// entry: a missing one would be a capability silently dropped from a wrapped
// plane.
var readerShells = [32]func(ferry.Reader, readerCaps) ferry.Reader{
	0:                 func(f rd, _ readerCaps) rd { return r00{f} },
	rProbe:            func(f rd, c readerCaps) rd { return r01{f, c.probe} },
	rList:             func(f rd, c readerCaps) rd { return r02{f, c.list} },
	rProbe | rList:    func(f rd, c readerCaps) rd { return r03{f, c.probe, c.list} },
	rRelease:          func(f rd, c readerCaps) rd { return r04{f, c.release} },
	rProbe | rRelease: func(f rd, c readerCaps) rd { return r05{f, c.probe, c.release} },
	rList | rRelease:  func(f rd, c readerCaps) rd { return r06{f, c.list, c.release} },
	rProbe | rList | rRelease: func(f rd, c readerCaps) rd {
		return r07{f, c.probe, c.list, c.release}
	},
	rName:          func(f rd, c readerCaps) rd { return r08{f, c.name} },
	rProbe | rName: func(f rd, c readerCaps) rd { return r09{f, c.probe, c.name} },
	rList | rName:  func(f rd, c readerCaps) rd { return r10{f, c.list, c.name} },
	rProbe | rList | rName: func(f rd, c readerCaps) rd {
		return r11{f, c.probe, c.list, c.name}
	},
	rRelease | rName: func(f rd, c readerCaps) rd { return r12{f, c.release, c.name} },
	rProbe | rRelease | rName: func(f rd, c readerCaps) rd {
		return r13{f, c.probe, c.release, c.name}
	},
	rList | rRelease | rName: func(f rd, c readerCaps) rd {
		return r14{f, c.list, c.release, c.name}
	},
	rProbe | rList | rRelease | rName: func(f rd, c readerCaps) rd {
		return r15{f, c.probe, c.list, c.release, c.name}
	},
	rBudget:          func(f rd, c readerCaps) rd { return r16{f, c.budget} },
	rProbe | rBudget: func(f rd, c readerCaps) rd { return r17{f, c.probe, c.budget} },
	rList | rBudget:  func(f rd, c readerCaps) rd { return r18{f, c.list, c.budget} },
	rProbe | rList | rBudget: func(f rd, c readerCaps) rd {
		return r19{f, c.probe, c.list, c.budget}
	},
	rRelease | rBudget: func(f rd, c readerCaps) rd { return r20{f, c.release, c.budget} },
	rProbe | rRelease | rBudget: func(f rd, c readerCaps) rd {
		return r21{f, c.probe, c.release, c.budget}
	},
	rList | rRelease | rBudget: func(f rd, c readerCaps) rd {
		return r22{f, c.list, c.release, c.budget}
	},
	rProbe | rList | rRelease | rBudget: func(f rd, c readerCaps) rd {
		return r23{f, c.probe, c.list, c.release, c.budget}
	},
	rName | rBudget: func(f rd, c readerCaps) rd { return r24{f, c.name, c.budget} },
	rProbe | rName | rBudget: func(f rd, c readerCaps) rd {
		return r25{f, c.probe, c.name, c.budget}
	},
	rList | rName | rBudget: func(f rd, c readerCaps) rd {
		return r26{f, c.list, c.name, c.budget}
	},
	rProbe | rList | rName | rBudget: func(f rd, c readerCaps) rd {
		return r27{f, c.probe, c.list, c.name, c.budget}
	},
	rRelease | rName | rBudget: func(f rd, c readerCaps) rd {
		return r28{f, c.release, c.name, c.budget}
	},
	rProbe | rRelease | rName | rBudget: func(f rd, c readerCaps) rd {
		return r29{f, c.probe, c.release, c.name, c.budget}
	},
	rList | rRelease | rName | rBudget: func(f rd, c readerCaps) rd {
		return r30{f, c.list, c.release, c.name, c.budget}
	},
	rProbe | rList | rRelease | rName | rBudget: func(f rd, c readerCaps) rd {
		return r31{f, c.probe, c.list, c.release, c.name, c.budget}
	},
}

// The sixty-four write-side shells, named the way the read-side ones are.
type (
	w00 struct{ wr }
	w01 struct {
		wr
		cm
	}
	w02 struct {
		wr
		rl
	}
	w03 struct {
		wr
		cm
		rl
	}
	w04 struct {
		wr
		es
	}
	w05 struct {
		wr
		cm
		es
	}
	w06 struct {
		wr
		rl
		es
	}
	w07 struct {
		wr
		cm
		rl
		es
	}
	w08 struct {
		wr
		un
	}
	w09 struct {
		wr
		cm
		un
	}
	w10 struct {
		wr
		rl
		un
	}
	w11 struct {
		wr
		cm
		rl
		un
	}
	w12 struct {
		wr
		es
		un
	}
	w13 struct {
		wr
		cm
		es
		un
	}
	w14 struct {
		wr
		rl
		es
		un
	}
	w15 struct {
		wr
		cm
		rl
		es
		un
	}
	w16 struct {
		wr
		pp
	}
	w17 struct {
		wr
		cm
		pp
	}
	w18 struct {
		wr
		rl
		pp
	}
	w19 struct {
		wr
		cm
		rl
		pp
	}
	w20 struct {
		wr
		es
		pp
	}
	w21 struct {
		wr
		cm
		es
		pp
	}
	w22 struct {
		wr
		rl
		es
		pp
	}
	w23 struct {
		wr
		cm
		rl
		es
		pp
	}
	w24 struct {
		wr
		un
		pp
	}
	w25 struct {
		wr
		cm
		un
		pp
	}
	w26 struct {
		wr
		rl
		un
		pp
	}
	w27 struct {
		wr
		cm
		rl
		un
		pp
	}
	w28 struct {
		wr
		es
		un
		pp
	}
	w29 struct {
		wr
		cm
		es
		un
		pp
	}
	w30 struct {
		wr
		rl
		es
		un
		pp
	}
	w31 struct {
		wr
		cm
		rl
		es
		un
		pp
	}
	w32 struct {
		wr
		nm
	}
	w33 struct {
		wr
		cm
		nm
	}
	w34 struct {
		wr
		rl
		nm
	}
	w35 struct {
		wr
		cm
		rl
		nm
	}
	w36 struct {
		wr
		es
		nm
	}
	w37 struct {
		wr
		cm
		es
		nm
	}
	w38 struct {
		wr
		rl
		es
		nm
	}
	w39 struct {
		wr
		cm
		rl
		es
		nm
	}
	w40 struct {
		wr
		un
		nm
	}
	w41 struct {
		wr
		cm
		un
		nm
	}
	w42 struct {
		wr
		rl
		un
		nm
	}
	w43 struct {
		wr
		cm
		rl
		un
		nm
	}
	w44 struct {
		wr
		es
		un
		nm
	}
	w45 struct {
		wr
		cm
		es
		un
		nm
	}
	w46 struct {
		wr
		rl
		es
		un
		nm
	}
	w47 struct {
		wr
		cm
		rl
		es
		un
		nm
	}
	w48 struct {
		wr
		pp
		nm
	}
	w49 struct {
		wr
		cm
		pp
		nm
	}
	w50 struct {
		wr
		rl
		pp
		nm
	}
	w51 struct {
		wr
		cm
		rl
		pp
		nm
	}
	w52 struct {
		wr
		es
		pp
		nm
	}
	w53 struct {
		wr
		cm
		es
		pp
		nm
	}
	w54 struct {
		wr
		rl
		es
		pp
		nm
	}
	w55 struct {
		wr
		cm
		rl
		es
		pp
		nm
	}
	w56 struct {
		wr
		un
		pp
		nm
	}
	w57 struct {
		wr
		cm
		un
		pp
		nm
	}
	w58 struct {
		wr
		rl
		un
		pp
		nm
	}
	w59 struct {
		wr
		cm
		rl
		un
		pp
		nm
	}
	w60 struct {
		wr
		es
		un
		pp
		nm
	}
	w61 struct {
		wr
		cm
		es
		un
		pp
		nm
	}
	w62 struct {
		wr
		rl
		es
		un
		pp
		nm
	}
	w63 struct {
		wr
		cm
		rl
		es
		un
		pp
		nm
	}
)

// writerShells is [readerShells] for the write half.
var writerShells = [64]func(ferry.Writer, writerCaps) ferry.Writer{
	0:                  func(f wr, _ writerCaps) wr { return w00{f} },
	wCommit:            func(f wr, c writerCaps) wr { return w01{f, c.commit} },
	wRelease:           func(f wr, c writerCaps) wr { return w02{f, c.release} },
	wCommit | wRelease: func(f wr, c writerCaps) wr { return w03{f, c.commit, c.release} },
	wEnsure:            func(f wr, c writerCaps) wr { return w04{f, c.ensure} },
	wCommit | wEnsure:  func(f wr, c writerCaps) wr { return w05{f, c.commit, c.ensure} },
	wRelease | wEnsure: func(f wr, c writerCaps) wr { return w06{f, c.release, c.ensure} },
	wCommit | wRelease | wEnsure: func(f wr, c writerCaps) wr {
		return w07{f, c.commit, c.release, c.ensure}
	},
	wUnset:            func(f wr, c writerCaps) wr { return w08{f, c.unset} },
	wCommit | wUnset:  func(f wr, c writerCaps) wr { return w09{f, c.commit, c.unset} },
	wRelease | wUnset: func(f wr, c writerCaps) wr { return w10{f, c.release, c.unset} },
	wCommit | wRelease | wUnset: func(f wr, c writerCaps) wr {
		return w11{f, c.commit, c.release, c.unset}
	},
	wEnsure | wUnset: func(f wr, c writerCaps) wr { return w12{f, c.ensure, c.unset} },
	wCommit | wEnsure | wUnset: func(f wr, c writerCaps) wr {
		return w13{f, c.commit, c.ensure, c.unset}
	},
	wRelease | wEnsure | wUnset: func(f wr, c writerCaps) wr {
		return w14{f, c.release, c.ensure, c.unset}
	},
	wCommit | wRelease | wEnsure | wUnset: func(f wr, c writerCaps) wr {
		return w15{f, c.commit, c.release, c.ensure, c.unset}
	},
	wPrepare:            func(f wr, c writerCaps) wr { return w16{f, c.prepare} },
	wCommit | wPrepare:  func(f wr, c writerCaps) wr { return w17{f, c.commit, c.prepare} },
	wRelease | wPrepare: func(f wr, c writerCaps) wr { return w18{f, c.release, c.prepare} },
	wCommit | wRelease | wPrepare: func(f wr, c writerCaps) wr {
		return w19{f, c.commit, c.release, c.prepare}
	},
	wEnsure | wPrepare: func(f wr, c writerCaps) wr { return w20{f, c.ensure, c.prepare} },
	wCommit | wEnsure | wPrepare: func(f wr, c writerCaps) wr {
		return w21{f, c.commit, c.ensure, c.prepare}
	},
	wRelease | wEnsure | wPrepare: func(f wr, c writerCaps) wr {
		return w22{f, c.release, c.ensure, c.prepare}
	},
	wCommit | wRelease | wEnsure | wPrepare: func(f wr, c writerCaps) wr {
		return w23{f, c.commit, c.release, c.ensure, c.prepare}
	},
	wUnset | wPrepare: func(f wr, c writerCaps) wr { return w24{f, c.unset, c.prepare} },
	wCommit | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w25{f, c.commit, c.unset, c.prepare}
	},
	wRelease | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w26{f, c.release, c.unset, c.prepare}
	},
	wCommit | wRelease | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w27{f, c.commit, c.release, c.unset, c.prepare}
	},
	wEnsure | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w28{f, c.ensure, c.unset, c.prepare}
	},
	wCommit | wEnsure | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w29{f, c.commit, c.ensure, c.unset, c.prepare}
	},
	wRelease | wEnsure | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w30{f, c.release, c.ensure, c.unset, c.prepare}
	},
	wCommit | wRelease | wEnsure | wUnset | wPrepare: func(f wr, c writerCaps) wr {
		return w31{f, c.commit, c.release, c.ensure, c.unset, c.prepare}
	},
	wName:            func(f wr, c writerCaps) wr { return w32{f, c.name} },
	wCommit | wName:  func(f wr, c writerCaps) wr { return w33{f, c.commit, c.name} },
	wRelease | wName: func(f wr, c writerCaps) wr { return w34{f, c.release, c.name} },
	wCommit | wRelease | wName: func(f wr, c writerCaps) wr {
		return w35{f, c.commit, c.release, c.name}
	},
	wEnsure | wName: func(f wr, c writerCaps) wr { return w36{f, c.ensure, c.name} },
	wCommit | wEnsure | wName: func(f wr, c writerCaps) wr {
		return w37{f, c.commit, c.ensure, c.name}
	},
	wRelease | wEnsure | wName: func(f wr, c writerCaps) wr {
		return w38{f, c.release, c.ensure, c.name}
	},
	wCommit | wRelease | wEnsure | wName: func(f wr, c writerCaps) wr {
		return w39{f, c.commit, c.release, c.ensure, c.name}
	},
	wUnset | wName: func(f wr, c writerCaps) wr { return w40{f, c.unset, c.name} },
	wCommit | wUnset | wName: func(f wr, c writerCaps) wr {
		return w41{f, c.commit, c.unset, c.name}
	},
	wRelease | wUnset | wName: func(f wr, c writerCaps) wr {
		return w42{f, c.release, c.unset, c.name}
	},
	wCommit | wRelease | wUnset | wName: func(f wr, c writerCaps) wr {
		return w43{f, c.commit, c.release, c.unset, c.name}
	},
	wEnsure | wUnset | wName: func(f wr, c writerCaps) wr {
		return w44{f, c.ensure, c.unset, c.name}
	},
	wCommit | wEnsure | wUnset | wName: func(f wr, c writerCaps) wr {
		return w45{f, c.commit, c.ensure, c.unset, c.name}
	},
	wRelease | wEnsure | wUnset | wName: func(f wr, c writerCaps) wr {
		return w46{f, c.release, c.ensure, c.unset, c.name}
	},
	wCommit | wRelease | wEnsure | wUnset | wName: func(f wr, c writerCaps) wr {
		return w47{f, c.commit, c.release, c.ensure, c.unset, c.name}
	},
	wPrepare | wName: func(f wr, c writerCaps) wr { return w48{f, c.prepare, c.name} },
	wCommit | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w49{f, c.commit, c.prepare, c.name}
	},
	wRelease | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w50{f, c.release, c.prepare, c.name}
	},
	wCommit | wRelease | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w51{f, c.commit, c.release, c.prepare, c.name}
	},
	wEnsure | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w52{f, c.ensure, c.prepare, c.name}
	},
	wCommit | wEnsure | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w53{f, c.commit, c.ensure, c.prepare, c.name}
	},
	wRelease | wEnsure | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w54{f, c.release, c.ensure, c.prepare, c.name}
	},
	wCommit | wRelease | wEnsure | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w55{f, c.commit, c.release, c.ensure, c.prepare, c.name}
	},
	wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w56{f, c.unset, c.prepare, c.name}
	},
	wCommit | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w57{f, c.commit, c.unset, c.prepare, c.name}
	},
	wRelease | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w58{f, c.release, c.unset, c.prepare, c.name}
	},
	wCommit | wRelease | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w59{f, c.commit, c.release, c.unset, c.prepare, c.name}
	},
	wEnsure | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w60{f, c.ensure, c.unset, c.prepare, c.name}
	},
	wCommit | wEnsure | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w61{f, c.commit, c.ensure, c.unset, c.prepare, c.name}
	},
	wRelease | wEnsure | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w62{f, c.release, c.ensure, c.unset, c.prepare, c.name}
	},
	wCommit | wRelease | wEnsure | wUnset | wPrepare | wName: func(f wr, c writerCaps) wr {
		return w63{f, c.commit, c.release, c.ensure, c.unset, c.prepare, c.name}
	},
}
