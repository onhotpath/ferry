package main

// #35's measurements. Run: `Z35=<n|all> GOTOOLCHAIN=go1.27rc2 go run .`

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// zRec is ZT as a recorder, so a probe can assert on what a suite reports.
// This is the shape ADR-0011 already argued for from the other end.
type zRec struct{ lines []string }

func (r *zRec) Errorf(f string, a ...any) { r.lines = append(r.lines, fmt.Sprintf(f, a...)) }
func (r *zRec) Helper()                   {}

func runZ35(sel string) {
	all := sel == "all"
	pick := func(n string) bool { return all || sel == n }
	for _, p := range []struct {
		n string
		f func()
	}{
		{"1", z35a}, {"2", z35b}, {"3", z35c}, {"4", z35d},
		{"5", z35e}, {"6", z35f}, {"7", z35g},
	} {
		if pick(p.n) {
			p.f()
		}
	}
}

// ---------------------------------------------------------------------------
// Z35=1  The four consumers, run.
// ---------------------------------------------------------------------------

func z35a() {
	hdr("Z35=1  the four consumers, run rather than read")

	fmt.Println(`  ferrytest has four consumers and three of them are outside this
  repository. Each is written as a real function in z35_consumers.go, and this
  is what each reports.`)

	report := func(label string, f func(ZT)) {
		r := &zRec{}
		f(r)
		fmt.Printf("\n  %s\n", label)
		if len(r.lines) == 0 {
			fmt.Println("    (clean)")
			return
		}
		sort.Strings(r.lines)
		for _, l := range r.lines {
			fmt.Println("    " + shorten2(l, 100))
		}
	}

	report("CONSUMER 1, core's own test:", zCoreTest)
	dir, _ := os.MkdirTemp("", "z35d")
	defer os.RemoveAll(dir)
	report("CONSUMER 2, a driver author's test:", func(t ZT) { zDriverTest(t, dir) })
	report("CONSUMER 3, a registrant:", zRegistrantTest)
	report("CONSUMER 4, an ordinary user:", zUserTest)

	fmt.Println(`
  Consumer 1 is the one to read. The golden column is running THROUGH THE ENTRY
  POINT for the first time, and D18's seven kinds now have rows, so the two
  things #28 and #41 hand this ticket are discharged in one table.`)
}

// ---------------------------------------------------------------------------
// Z35=2  RoundTrip's signature: three candidates, at their call sites.
// ---------------------------------------------------------------------------

func z35b() {
	hdr("Z35=2  how a registry reaches the harness")

	fmt.Println(`  ADR-0005 and ADR-0009 specify the same function with different
  signatures, and #35's body names that as the first collision to resolve:

      ADR-0005   RoundTrip(t, Plane, ...Proof)
      ADR-0009   RoundTrip(t, *Registry, Plane, ...Proof)

  Three candidates, written at a call site rather than argued about. All three
  compile; the difference is what they say.`)

	fmt.Println(`
  (a) a *Registry parameter, ADR-0009's own spelling

        ferrytest.RoundTrip(t, reg, plane, proofs...)
        ferrytest.RoundTrip(t, nil, plane, proofs...)      <- the common case

      Every call site pays for the uncommon one, and a caller who wants the tag
      key ADR-0008 put in the same cache key has nowhere to put it.

  (b) a field on Plane

        ferrytest.RoundTrip(t, ferrytest.Plane{..., Registry: reg}, proofs...)

      A registry is not a property of a plane. It decides how a Go value
      becomes a Value, which happens before any plane is reached, and a driver
      author filling in a Plane literal would have to know what to put there.

  (c) OPTIONS, which is what the entry point already takes

        ferrytest.RoundTrip(t, plane, proofs)
        ferrytest.RoundTrip(t, plane, proofs, ferry.WithRegistry(reg))
        ferrytest.RoundTrip(t, plane, proofs, ferry.WithRegistry(reg), ferry.WithTagKey("cfg"))

  (c) is taken, and the deciding argument is not ergonomics.`)

	fmt.Println(`
      A *Registry parameter would be a SECOND way to say what
      ferry.WithRegistry already says.

  That is survey item 5.14's first entry - "two ways to set the loader" - which
  ADR-0004 avoided by construction and ADR-0009 avoided again by making the
  default registry a Registry rather than a second mechanism. A harness with its
  own registry parameter reintroduces it in the one package whose job is to
  hold ferry to its own rules.

  It also settles the thing neither ADR could see alone: ADR-0008 put the tag
  key in the same schema cache key and ADR-0012 added Options of its own, so the
  parameter list would have grown twice more. Options are already the mechanism
  for "everything that keys a schema".

  The cost, stated: the proofs become a SLICE rather than the variadic tail, so
  the one-proof call reads`)

	fmt.Println(`
        ferrytest.RoundTrip(t, plane, []ferrytest.Proof{p})

  rather than`)
	fmt.Println(`
        ferrytest.RoundTrip(t, plane, p)

  and that is the whole price. It is paid once per call site and CoreTypes()
  already returns a slice, which is the call every driver makes.`)

	// Run (c) both ways so the claim that both compile is a fact.
	r := &zRec{}
	ZRoundTrip(r, zMemoryPlane(), ZCoreTypes())
	fmt.Printf("\n    run, no options:      %d failure(s)\n", len(r.lines))
	r2 := &zRec{}
	reg := NewRegistry()
	ZRoundTrip(r2, zMemoryPlane(), ZCoreTypes(), WithRegistry(reg))
	fmt.Printf("    run, WithRegistry:    %d failure(s)\n", len(r2.lines))
}

// ---------------------------------------------------------------------------
// Z35=3  The two proof types, merged, and what the merge costs.
// ---------------------------------------------------------------------------

func z35c() {
	hdr("Z35=3  one proof type, three columns")

	fmt.Println(`  #28 found two proof types on this prototype and neither is what ADR-0005
  specifies:

      harness.go    Type[T](name, eq, values...)           NO golden column,
                                                           runs through Dump/Load
      r10_proof.go  Prove[T](name, eq, cases...) w/ Want   golden column,
                                                           runs through the superseded walk

  The merged one is ZType, and this is what it catches that harness.go's does
  not. The same nanosecond codec ADR-0005 rejects by name:`)

	swapDuration(func() {
		r := &zRec{}
		ZRoundTrip(r, zMemoryPlane(), ZCoreTypes())
		fmt.Printf("\n    merged proof, nanosecond codec:  %d failure(s)\n", len(r.lines))
		for _, l := range r.lines {
			fmt.Println("      " + shorten2(l, 96))
		}
		res := RoundTrip(memoryPlane(), CoreTypes()...)
		fmt.Printf("\n    harness.go's proof, same codec:  %s\n", res.summary())
	})

	fmt.Println(`
  So the merge is not tidying. It is the difference between a harness that can
  see a representation change and one that cannot, which #28 makes ferry's
  second compatibility promise.

  WHAT IT COSTS, counted rather than waved at. Every case now carries a golden
  Value, so the table grew from values to pairs:`)

	n := 0
	for _, p := range ZCoreTypes() {
		n += p.Cases()
	}
	fmt.Printf("\n    rows in the table                 %d\n", len(ZCoreTypes()))
	fmt.Printf("    cases, each carrying a golden     %d\n", n)
	fmt.Printf("    rows in harness.go's table        %d\n", len(CoreTypes()))

	fmt.Println(`
  Sixty-odd golden values written by hand, and that is the point rather than
  the price: ADR-0005 says "a contributor adding a type cannot avoid stating
  what it looks like on a plane".

  A GOLDEN FILE WAS CONSIDERED AND REFUSED. Writing the column to testdata and
  comparing would be the same table with a better diff - a reviewer would see
  the representation change as a file change, which is exactly what #28 wants.
  It is refused because a golden file grows an -update flag within one release,
  and then the change #28 exists to make visible is a flag. ADR-0002's "the
  harness is a table, not a generator" is the same instinct one level up.`)
}

func swapDuration(f func()) {
	t := durationType()
	old := byIdentity[t]
	byIdentity[t] = nanoDurationCodec()
	defer func() { byIdentity[t] = old }()
	f()
}

// ---------------------------------------------------------------------------
// Z35=4  The completeness check, as one function over the union.
// ---------------------------------------------------------------------------

func z35d() {
	hdr("Z35=4  one completeness check, not two")

	fmt.Println(`  ADR-0005's check iterates core's identity table and the admitted kind
  list. ADR-0009's iterates a registry. #35's body calls them "the same check
  over two tables", and they are:`)

	fmt.Printf("\n    ZComplete(empty registry, ZCoreTypes()...)        missing=%v\n",
		ZComplete(NewRegistry(), ZCoreTypes()...))

	reg := NewRegistry()
	_ = reg.Register(TextCodec[zAddr](VString))
	fmt.Printf("    after one registration, no proof added           missing=%v\n",
		ZComplete(reg, ZCoreTypes()...))

	fmt.Printf("    with core's proofs but a shorter table           missing=%v\n",
		shorten2(fmt.Sprint(ZComplete(NewRegistry(), ZCoreTypes()[:3]...)), 96))

	fmt.Println(`
  One function, one union, and the registrant's call is core's call with their
  own proofs appended. Neither ADR could have written it, because ADR-0005 did
  not have a Registry and ADR-0009 did not have the kind list.

  THE ONE THING IT NEEDS FROM A REGISTRY, and it is worth naming because
  ADR-0009 made keeping Reg opaque a design constraint:

      (*Registry).Types() []reflect.Type

  A proof needs nothing from a Reg - it exercises the codec through the
  ordinary walk - and that stays true. Enumerating the registry's TYPES is a
  property of the registry rather than of any registration, so it opens no
  registration and exports no field.

  And it closes #41's D18, which has been red since it was written:`)

	r := &zRec{}
	for _, s := range ZComplete(NewRegistry(), ZCoreTypes()...) {
		r.Errorf("%s", s)
	}
	fmt.Printf("\n    D18, against the merged table: %d missing\n", len(r.lines))
	mustCoreComplete(func(f string, a ...any) {
		fmt.Printf("    D18, against harness.go's table: %s\n", shorten2(fmt.Sprintf(f, a...), 92))
	})
}

// ---------------------------------------------------------------------------
// Z35=5  What the suites contain, counted against the audit's own list.
// ---------------------------------------------------------------------------

func z35e() {
	hdr("Z35=5  the case lists, counted")

	fmt.Println(`  The #41 audit wrote its section 5 as assertions rather than prose
  "because a rule that no suite holds is a rule the next implementation can
  drop the same way". Thirty cases, grouped by which suite owns them. This
  ticket owns the entry points; the count is what the entry points have to
  carry.`)

	groups := []struct {
		name  string
		cases []string
	}{
		{"driver conformance (a Source/Sink pair, per driver)", []string{
			"Bind succeeds against an unreachable plane",
			"Get at a container address is Absent with a nil error, for four shapes",
			"a Get error reaches the caller as an error, never as Absent",
			"Children returns kinded element addresses",
			"Commit only on success, Close always, a Close failure in the error set",
			"a non-injective key function is refused before any I/O, naming both",
			"a key function retains nothing across opens, on the WRITE side",
			"a sink accepts a dynamic address its static table never held",
			"a driver reading its plane from the context refuses at open, with ErrPlane",
			"the declared kinds are honoured, and what they exclude is refused loudly",
			"a GOLDEN ARTEFACT: a fixed value, dumped, compared against fixed contents",
		}},
		{"schema compile (no plane, reflect.TypeFor[T]() only)", []string{
			"a leaf and a subtree at one segment do not compile",
			"two fields addressing one segment do not compile",
			"a promoted embedded pointer does not compile",
			"a tag StructTag.Get would truncate is malformed, not missing",
			"two ferry tags on one field are reported",
			"a quoted option value containing a comma parses",
			"a near miss, a foreign word and surrounding whitespace get three diagnoses",
			"required on a struct, a *struct and an array compiles; on a slice it does not",
			"a registration not total over the zero value is refused AT Register",
			"map[T]V over a key type with no AsMapKey does not compile",
			"every admitted kind compiles, one single-field struct per kind",
			"map[time.Time]V does not compile",
		}},
		{"the walk (memory plane, no driver)", []string{
			"required at a struct and a *struct sees the subtree's presence bit",
			"an index outside an array's length is loud",
			"a Null at each of the six kind classes gives ADR-0006's table",
			"five failing leaves report five errors, Load and Dump alike",
			"a failing Dump writes nothing; a Committer reports both failure kinds",
			"no ferry-authored message contains plane-supplied text",
			"the error set is an exact-set diff over (address, class)",
			"two map keys addressing one segment are refused as the address is minted",
		}},
		{"round-trip property", []string{
			"CoreTypes() runs THROUGH the entry point, not a walk of its own",
			"the completeness check covers the identity table, the kinds and a registry",
			"every case carries its golden Value",
		}},
		{"codec conformance", []string{
			"AppendText and MarshalText agree",
			"a registered INTERFACE codec at its nil zero value, encoding",
			"a registered INTERFACE codec at its nil zero value, decoding",
			"a codec's declared kind matches what it emits",
			"a codec accepts every kind it emits",
			"a key codec is injective under ==, over ferry's own key text",
		}},
	}

	total := 0
	for _, g := range groups {
		fmt.Printf("\n  %s  (%d)\n", g.name, len(g.cases))
		for _, c := range g.cases {
			fmt.Printf("    - %s\n", c)
		}
		total += len(g.cases)
	}
	fmt.Printf("\n  %d cases across five groups and three entry points.\n", total)

	fmt.Println(`
  THREE ENTRY POINTS AND NOT FIVE, and the grouping above is why. The schema
  compile group and the walk group take no driver and no registry: they are
  core's own tests of core, and they belong in core's package rather than in
  ferrytest. ADR-0002's route (b) admits to ferrytest what core CANNOT
  compile-check about somebody ELSE's code, and a schema-compile refusal is
  core checking itself.

  So the split is not by subject, it is by WHOSE code is under test:

      ferrytest.RoundTrip   a registrant's codec, or a driver's plane
      ferrytest.Driver      a driver
      ferrytest.Codec       a registrant's codec
      core's own _test.go   everything above that has no third party in it

  That removes 20 of the 30 from ferrytest's surface and leaves it three verbs.`)
}

// ---------------------------------------------------------------------------
// Z35=6  The exported surface, listed and counted.
// ---------------------------------------------------------------------------

func z35f() {
	hdr("Z35=6  the exported surface, as a list")

	names := zExportedIn("z35_surface.go", "z35_suites.go", "z35_consumers.go")
	fmt.Printf("\n  %d exported names, in one package:\n", len(names))
	groups := map[string][]string{}
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "ZType") || strings.HasPrefix(n, "ZCase") ||
			strings.HasPrefix(n, "ZAt") || strings.HasPrefix(n, "ZProof") ||
			n == "ZEq" || strings.HasSuffix(n, "Eq"):
			groups["the proof"] = append(groups["the proof"], n)
		case n == "ZRoundTrip" || n == "ZDriver" || n == "ZCodec" ||
			n == "ZComplete" || n == "ZInjective":
			groups["the suites"] = append(groups["the suites"], n)
		case n == "ZPlane" || n == "ZArtefact" || n == "ZT":
			groups["what a caller describes"] = append(groups["what a caller describes"], n)
		case n == "ZStatic" || n == "ZRecord":
			groups["the apparatus"] = append(groups["the apparatus"], n)
		default:
			groups["the table"] = append(groups["the table"], n)
		}
	}
	for _, k := range []string{"what a caller describes", "the proof", "the suites", "the apparatus", "the table"} {
		sort.Strings(groups[k])
		fmt.Printf("    %-26s %s\n", k, strings.Join(groups[k], " "))
	}

	fmt.Println(`
  Read the Z off every name; they carry it only so this file can live beside
  the tip's own harness without colliding.

  ONE PACKAGE, and the argument is the import graph rather than taste. Every
  suite needs Plane; the driver suite needs the proof table; the codec suite
  needs the registry and the relations; all three need the assertion sink. A
  split would make a driver import two paths to run one CI job, and ADR-0002
  admitted this package on the line that a rule "is only worth anything when it
  ships from the same place as the rule". Two places is one more than one.`)
}

func zExportedIn(files ...string) []string {
	var out []string
	fset := token.NewFileSet()
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range af.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					out = append(out, d.Name.Name)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						if sp.Name.IsExported() {
							out = append(out, sp.Name.Name)
						}
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							if n.IsExported() {
								out = append(out, n.Name)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Z35=7  The stability promise, and why it needs one.
// ---------------------------------------------------------------------------

func z35g() {
	hdr("Z35=7  adding a conformance case is not a Go API change")

	fmt.Println(`  ADR-0002 makes this package AUTHORITY rather than a convenience, so a
  driver's CI depends on it. #35 asks whether it has a stability promise, and
  the answer turns on one measurable fact.

  A case is a line in a list inside ZDriver. Adding one changes no signature,
  no type, and no exported name:`)

	before := len(zExportedIn("z35_suites.go"))
	fmt.Printf("\n    exported names in the suite file, today          %d\n", before)
	fmt.Println(`    exported names after adding a case              the same
    what apidiff would report                       nothing
    what a driver's CI would report                 a new failure`)

	fmt.Println(`
  That is #28's shape arriving at ferrytest, and it is the THIRD instance in
  this design of a change semver cannot see:

      #28   a golden row moves, and every stored artefact breaks
      #28   a driver's spelling moves, and a round-trip suite stays green
      #35   a conformance case is added, and a driver's CI goes red

  The first two get a major version, because they break DATA. This one does
  not, and the difference is worth stating rather than assuming:

      A new conformance case does not break a driver. It reports that the
      driver was already broken, against a rule an ADR had already landed.

  So the promise is two promises in one package, and they are different:

      THE APPARATUS - the memory plane, the recorder, the relations, the value
      constructors - is ordinary exported API under semver. An ordinary user
      embeds it in tests that are not about ferry at all, and it must not move.

      THE SUITES may gain cases in a minor release, and each new case cites the
      ADR sentence it executes. A case that asserts a rule no ADR states is not
      a case, it is a new rule, and it needs the ADR first.

  That second clause is the whole of ADR-0002's route (b) turned into a release
  policy: the suite IS the rule in executable form, so the suite may only grow
  where the rules already did.`)
}

// zAddr is a stand-in registered type for the completeness probe.
type zAddr struct{ n string }

func (a zAddr) MarshalText() ([]byte, error)  { return []byte(a.n), nil }
func (a *zAddr) UnmarshalText(b []byte) error { a.n = string(b); return nil }

func durationType() reflect.Type { return reflect.TypeFor[time.Duration]() }

func nanoDurationCodec() leafCodec {
	return leafCodec{
		name: "time.Duration",
		kind: VString,
		enc: func(v reflect.Value) (Value, error) {
			return String(strconv.FormatInt(v.Int(), 10)), nil
		},
		dec: func(val Value, dst reflect.Value) error {
			s, err := val.AsString()
			if err != nil {
				return err
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return err
			}
			dst.SetInt(n)
			return nil
		},
	}
}
