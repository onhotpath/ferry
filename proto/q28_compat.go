package main

// #28: what counts as a breaking change to what ferry writes into a plane.
//
// ADR-0005 pins a representation for every type in core's set and makes it a
// checked column in the round-trip harness. Those strings are not an
// implementation detail: once ferry ships they are in every user's config
// files, KV stores and secret backends, and changing one is not a Go API
// break. Consumer code compiles unchanged, the tests pass, and what breaks is
// every artefact already written.
//
// Run: `Q28=<n|all> GOTOOLCHAIN=go1.27rc2 go run .` from proto/.

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func runQ28(sel string) {
	all := sel == "all"
	pick := func(n string) bool { return all || sel == n }
	for _, p := range []struct {
		n string
		f func()
	}{
		{"1", q28a}, {"2", q28b}, {"3", q28c}, {"4", q28d},
		{"5", q28e}, {"6", q28f}, {"7", q28g}, {"8", q28h}, {"9", q28i},
	} {
		if pick(p.n) {
			p.f()
		}
	}
}

// ---------------------------------------------------------------------------
// Q28=1  The tool Go ships for this exact question recommends a patch release.
// ---------------------------------------------------------------------------

func q28a() {
	hdr("Q28=1  what the toolchain says about a representation change")

	fmt.Println(`  The ticket's claim is that a representation change is invisible to the
  toolchain. That is checkable rather than rhetorical: Go ships two tools whose
  whole job is to answer "is this release breaking", and both were run against a
  module whose only change is the text it writes.

  Two modules, identical exported API, one line different in a function body:`)

	dir, err := os.MkdirTemp("", "q28")
	if err != nil {
		fmt.Println("  mkdtemp:", err)
		return
	}
	defer os.RemoveAll(dir)

	const src = `package cfg

import (
%s)

// Timeout is a duration a caller stores on a plane.
type Timeout time.Duration

// Encode is what lands in the artefact.
func Encode(t Timeout) string { return %s }

// Decode is its inverse.
func Decode(s string) (Timeout, error) { %s }
`
	v1 := fmt.Sprintf(src,
		"\t\"time\"\n",
		`time.Duration(t).String()`,
		`d, err := time.ParseDuration(s); return Timeout(d), err`)
	v2 := fmt.Sprintf(src,
		"\t\"strconv\"\n\t\"time\"\n",
		`strconv.FormatInt(int64(t), 10)`,
		`n, err := strconv.ParseInt(s, 10, 64); return Timeout(n), err`)

	mk := func(name, body string) string {
		d := filepath.Join(dir, name)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "go.mod"), []byte("module example.com/cfg\n\ngo 1.26\n"), 0o644)
		_ = os.WriteFile(filepath.Join(d, "cfg.go"), []byte(body), 0o644)
		return d
	}
	d1, d2 := mk("v1", v1), mk("v2", v2)

	fmt.Printf("\n    v1: Encode(30s) = %q\n    v2: Encode(30s) = %q\n",
		time.Duration(30*time.Second).String(), strconv.FormatInt(int64(30*time.Second), 10))

	run := func(label, wd, name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = wd
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
		out, _ := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if s == "" {
			s = "(no output: nothing to report)"
		}
		fmt.Printf("\n    %s\n", label)
		for _, line := range strings.Split(s, "\n") {
			fmt.Printf("      %s\n", line)
		}
	}

	// A consumer's own test, which is the round-trip property and nothing else,
	// because that is what a library asks a user to write.
	consumerTest := `package cfg

import "testing"

func TestRoundTrip(t *testing.T) {
	for _, v := range []Timeout{0, 1, 30000000000, -1} {
		got, err := Decode(Encode(v))
		if err != nil || got != v {
			t.Fatalf("%v -> %q -> %v, %v", v, Encode(v), got, err)
		}
	}
}
`
	for _, d := range []string{d1, d2} {
		_ = os.WriteFile(filepath.Join(d, "cfg_test.go"), []byte(consumerTest), 0o644)
	}

	gobin := filepath.Join(os.Getenv("HOME"), "go", "bin")
	run("go build ./... in v2", d2, "go", "build", "./...")
	run("go vet ./... in v2", d2, "go", "vet", "./...")
	run("gofmt -l, on v2", dir, "gofmt", "-l", d2)
	run("go test ./... in v1, the consumer's round-trip test", d1, "go", "test", "./...")
	run("go test ./... in v2, the same test, unchanged", d2, "go", "test", "./...")
	run("apidiff v1 v2  (golang.org/x/exp/cmd/apidiff)", dir, filepath.Join(gobin, "apidiff"), d1, d2)

	fmt.Println(`
  Six clean results. apidiff is the tool the Go team ships to answer "did the
  API change" and is what gorelease reads to recommend a version number; it
  reports nothing, because nothing changed. It is right and it is answering a
  different question from the one that matters.

  The consumer's own test is the sharpest row. It is the round-trip property,
  which is what a library asks a user to write, and it passes on BOTH versions
  because nanoseconds round-trip perfectly. That is ADR-0005's own measurement
  arriving from the consumer's side: a property test cannot see a
  representation, so neither can the user who wrote one.

  So the ticket's premise is confirmed rather than assumed, and the shape of
  the answer follows from it:

      Semver describes the Go API, and the Go API is not the only thing a
      mapper publishes. A representation is a second published interface with
      no tool behind it, so it needs a second promise, stated separately.`)
}

// ---------------------------------------------------------------------------
// Q28=2  Not all representation changes fail the same way, and the dangerous
//        class is the one that still parses.
// ---------------------------------------------------------------------------

type q28Change struct {
	label   string
	oldText string // an artefact written under the old rule
	want    string // what that artefact MEANT
	parse   func(string) (string, error)
}

func q28b() {
	hdr("Q28=2  what an old artefact does under a new rule")

	fmt.Println(`  "A representation change breaks stored data" is true and is not one
  failure. Graded by what happens when the OLD text meets the NEW parser, which
  is what a running program does after the upgrade:`)

	changes := []q28Change{
		{"time.Duration: 30s -> nanoseconds", "30s", "30000000000",
			func(s string) (string, error) { return q28Int(s, 10) }},
		{"time.Time: RFC 3339 -> Unix seconds", "2026-01-15T12:00:00Z", "1768478400",
			func(s string) (string, error) { return q28Int(s, 10) }},
		{"int: base 10 -> base 16", "10", "10",
			func(s string) (string, error) { return q28Int(s, 16) }},
		{"[]byte: raw -> base64, a 2-byte payload", "hi", "hi",
			func(s string) (string, error) { return q28B64(s) }},
		{"[]byte: raw -> base64, a 4-byte payload", "data", "data",
			func(s string) (string, error) { return q28B64(s) }},
		{"float64: shortest -> %.17g", "0.1", "0.1",
			func(s string) (string, error) {
				f, err := strconv.ParseFloat(s, 64)
				return strconv.FormatFloat(f, 'g', -1, 64), err
			}},
		{"bool: true -> 1", "true", "true",
			func(s string) (string, error) {
				b, err := strconv.ParseBool(s)
				return strconv.FormatBool(b), err
			}},
	}

	fmt.Printf("\n  %-42s %-24s %s\n", "the change", "the stored text", "what the new parser does with it")
	loud, wrong, ok := 0, 0, 0
	for _, c := range changes {
		got, err := c.parse(c.oldText)
		var verdict string
		switch {
		case err != nil:
			verdict = "LOUD:    " + shorten2(err.Error(), 40)
			loud++
		case got != c.want:
			verdict = fmt.Sprintf("SILENT, WRONG:      reads as %s", got)
			wrong++
		default:
			verdict = "SILENT, and correct: " + got
			ok++
		}
		fmt.Printf("  %-42s %-24q %s\n", c.label, c.oldText, verdict)
	}
	fmt.Printf("\n  %d loud, %d silently wrong, %d silently compatible, out of %d.\n",
		loud, wrong, ok, len(changes))

	fmt.Println(`
  Three outcomes, not two, and each one wants a different thing from the ADR.

  LOUD is an outage: every process loading a stored artefact fails on the first
  upgrade, at startup, with the address named. Bad, and recoverable, because it
  is visible.

  SILENT AND WRONG is a wrong value in a running system. Base 10 to base 16 is
  the shape of any "we changed how integers are written", and it needs no
  exotic input to bite.

  SILENT AND COMPATIBLE is the trap in the other direction, and the two rows
  here are the ones a maintainer would call safe. They are safe FOR NOW: the new
  parser is a superset of the old spelling by accident rather than by design, so
  nothing stops the next change from narrowing it, and nothing recorded that
  anyone was relying on it. This is the class that most needs a written promise,
  because it is the class where "we tested it and it was fine" is true and
  useless.

  The two base64 rows are the same change, twice, over two payloads, and they
  disagree. A two-byte value is refused because the length is wrong; a four-byte
  value that happens to be in the alphabet decodes into different bytes with no
  error. So even ONE representation change is not one outcome - it is a
  distribution over the data people have.

  That is the deciding measurement for how the promise is stated:

      It cannot be stated over the outcome, because the outcome is a property of
      the pair of versions AND of the stored value. It has to be stated over the
      text.`)
}

func q28Int(s string, base int) (string, error) {
	n, err := strconv.ParseInt(s, base, 64)
	return strconv.FormatInt(n, 10), err
}

func q28B64(s string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	return string(raw), err
}

// ---------------------------------------------------------------------------
// Q28=3  The golden column is the only thing in ferry's CI that sees it, and
//        it is not in the harness that runs through the engine.
// ---------------------------------------------------------------------------

func q28c() {
	hdr("Q28=3  what ferry's own CI sees, and the column that is missing")

	fmt.Println(`  ADR-0005 argues the golden column at its strongest and measures it:
  replacing time.Duration's codec with a nanosecond one, "the round-trip
  property reports zero failures". Re-run through the CURRENT harness, which
  #41 item 6 repointed at Dump and Load:`)

	swap := func(t reflect.Type, c leafCodec, f func()) {
		old := byIdentity[t]
		byIdentity[t] = c
		defer func() { byIdentity[t] = old }()
		f()
	}

	nanoDuration := leafCodec{
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

	var dur Proof
	for _, p := range CoreTypes() {
		if p.Name() == "time.Duration" {
			dur = p
		}
	}
	dir, _ := os.MkdirTemp("", "q28c")
	defer os.RemoveAll(dir)
	planes := []Plane{memoryPlane(), yamlPlane(dir), flatPlane()}

	fmt.Println("\n    with the pinned codec:")
	for _, pl := range planes {
		fmt.Printf("      %-40s %s\n", pl.Name, RoundTrip(pl, dur).summary())
	}
	swap(reflect.TypeFor[time.Duration](), nanoDuration, func() {
		fmt.Println("\n    with the codec ADR-0005 rejects by name, writing nanoseconds:")
		for _, pl := range planes {
			fmt.Printf("      %-40s %s\n", pl.Name, RoundTrip(pl, dur).summary())
		}
		ctx := context.Background()
		out := map[Path]Value{}
		_ = Dump(ctx, struct {
			T time.Duration `ferry:"t"`
		}{30 * time.Second}, MemSink{out})
		fmt.Printf("\n      and what it writes: %s\n", fmtVals(out))
	})

	fmt.Println(`
  Green on all three planes, in both worlds. The property is blind to the
  representation, which is ADR-0005's own finding reproduced against the engine
  rather than against the superseded walk.

  THE FINDING THIS PROBE FOUND. ADR-0005 specifies a proof as a triple -
  values, a relation, and a boundary Value the case must produce - and says
  the third is the only column that pins the representation on purpose. On the
  tip there are TWO proof types:

      harness.go   Type[T](name, eq, values...)              no golden column,
                                                             runs through Dump/Load
      r10_proof.go Prove[T](name, eq, cases...) with Want    HAS a golden column,
                                                             runs through walk.go and a
                                                             map -> map transform

  So the column ADR-0005 calls the whole reason a proof is a triple has never
  run through the engine, and CoreTypes() - the table the completeness check
  checks and the thing standing in for ferrytest - is the property-only one.

  #41 item 6 repointed "the harness" at the entry point and repointed the one
  without the column. That is the audit's own shape once more: a green probe
  over a case its own fixture excluded.

  For this ticket it is the load-bearing fact rather than a note:

      The compatibility promise this ADR states is only checkable where a
      golden row exists, and today core's CI has none.

  The spelling of the merged proof is #35's. That it must be one type with
  three columns, running through the entry point, is this ticket's, because a
  promise nothing executes is prose again within two releases.`)
}

// ---------------------------------------------------------------------------
// Q28=4  A driver can change its spelling in a patch release and nothing in
//        the conformance suite sees it.
// ---------------------------------------------------------------------------

func q28d() {
	hdr("Q28=4  a driver's spelling moves independently, and self-consistency hides it")

	fmt.Println(`  ADR-0005: "Base64 is NOT ferry's business: Bytes carries the bytes, and how
  a plane spells them is the driver's." ADR-0002 versions drivers independently.
  Together those make a driver-side data break expressible with no core change -
  and easy to do by accident.

  The measurement is what a round-trip conformance run says about it. A driver's
  reader and writer are written by one author in one file, so a spelling change
  is applied to both and the pair stays SELF-CONSISTENT:`)

	dir, _ := os.MkdirTemp("", "q28d")
	defer os.RemoveAll(dir)
	ctx := context.Background()

	type blob struct {
		B []byte `ferry:"b"`
	}
	in := blob{[]byte{0x68, 0x69}}

	for _, spelling := range []string{"base64 (as the driver ships)", "hex (a one-line change)"} {
		q28HexBytes = strings.HasPrefix(spelling, "hex")
		p := filepath.Join(dir, "c-"+strconv.FormatBool(q28HexBytes)+".yaml")
		if err := Dump(ctx, in, FYAMLSink{Path: p}); err != nil {
			fmt.Println("    dump:", err)
			continue
		}
		body, _ := os.ReadFile(p)
		out, err := Load[blob](ctx, FYAMLSource{Path: p})
		fmt.Printf("\n    %-30s file: %-28q round-trips: %v\n",
			spelling, strings.TrimSpace(string(body)), err == nil && string(out.B) == string(in.B))
	}
	q28HexBytes = false

	fmt.Println(`
  Both round-trip. ADR-0001's driver fidelity holds, ADR-0005's value fidelity
  holds, the conformance suite as ADR-0004 and ADR-0005 describe it is green,
  and every file the previous version wrote is now garbage.

  That is the same shape ADR-0005 records for its own !!binary defect - "the
  pair was self-consistent and round-tripped; what caught it was gopkg.in
  yaml.v3's emitter refusing to emit invalid !!binary" - except that here
  nothing external refuses, because both spellings are valid YAML.

  A round-trip suite structurally cannot catch this, and it is worth stating
  why rather than adding a case and moving on:

      A round trip tests a function against its own inverse. A spelling is a
      choice of function. Changing both halves together is invisible to any
      test that only composes them.

  So the suite needs an assertion of a different KIND: a fixed value, dumped,
  compared against fixed expected plane contents. That is not ADR-0001's
  rejected byte-level plane fidelity, which is about preserving a USER's
  comments and key order across a Load and Dump cycle. This is the driver's own
  output for one input, which is the golden column at the driver's boundary
  instead of at core's.`)
}

// q28HexBytes is the seam Q28=4 drives: the YAML driver's spelling of Bytes.
var q28HexBytes bool

// ---------------------------------------------------------------------------
// Q28=5  Read-old-write-new, built and costed.
// ---------------------------------------------------------------------------

func q28e() {
	hdr("Q28=5  the dual-read path, built rather than argued about")

	fmt.Println(`  "Accepting a superseded form on Load while writing the current one on Dump
  is the obvious answer", and it directly contradicts ADR-0005's "nothing else
  coerces". It was built, because a coercion that has to meet the same standard
  of argument as the String donor has to be measured first.`)

	dual := leafCodec{
		name: "time.Duration",
		kind: VString,

		enc: byIdentity[reflect.TypeFor[time.Duration]()].enc, // writes 30s, unchanged
		dec: func(val Value, dst reflect.Value) error {
			s, err := val.AsString()
			if err != nil {
				return err
			}
			if d, err := time.ParseDuration(s); err == nil {
				dst.SetInt(int64(d))
				return nil
			}
			// the superseded form
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return err
			}
			dst.SetInt(n)
			return nil
		},
	}

	ctx := context.Background()
	old := byIdentity[reflect.TypeFor[time.Duration]()]
	byIdentity[reflect.TypeFor[time.Duration]()] = dual
	defer func() { byIdentity[reflect.TypeFor[time.Duration]()] = old }()

	dir, _ := os.MkdirTemp("", "q28e")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")

	// An artefact written by the previous version.
	_ = os.WriteFile(p, []byte("t: \"30000000000\"\n"), 0o644)
	type conf struct {
		T time.Duration `ferry:"t"`
	}
	before, _ := os.ReadFile(p)
	got, err := Load[conf](ctx, FYAMLSource{Path: p})
	fmt.Printf("\n  (1) it works.  stored %q -> loads %v, err=%v\n",
		strings.TrimSpace(string(before)), got.T, err)

	_ = Dump(ctx, got, FYAMLSink{Path: p})
	after, _ := os.ReadFile(p)
	fmt.Printf("\n  (2) and the first dump rewrites the artefact:\n      before %q\n      after  %q\n",
		strings.TrimSpace(string(before)), strings.TrimSpace(string(after)))
	fmt.Println(`      Every stored file changes on the first write after the upgrade, so
      the migration happens anyway - unannounced, one file at a time, in
      whatever order processes happen to dump.`)

	fmt.Println(`
  (3) the golden column stops being a column. A proof pins ONE text, and a
      dual-read codec has one on Dump and two on Load, so the triple ADR-0005
      specifies cannot state the decode side at all:`)
	out := map[Path]Value{}
	_ = Dump(ctx, conf{30 * time.Second}, MemSink{out})
	fmt.Printf("      Dump writes exactly one text:  %s\n", fmtVals(out))
	for _, text := range []string{"30s", "30000000000"} {
		v, _ := Load[conf](ctx, MemSource{map[Path]Value{Path{}.Name("t"): String(text)}})
		fmt.Printf("      Load accepts %-14q -> %v\n", text, v.T)
	}

	fmt.Println(`
  (4) and it can never be removed, because nothing can tell you whether any
      plane still holds the old form. ferry's own schema-extraction pattern is
      a Recorder sink, which records what ferry WROTE and not what the plane
      HELD, so a deprecation cycle has no signal to end on:`)
	rec := map[Path]Value{}
	_ = Dump(ctx, conf{time.Second}, MemSink{rec})
	fmt.Printf("      the recording sink saw: %s   <- ferry's text, never the plane's\n", fmtVals(rec))

	fmt.Println(`
  So the four costs, none of which is the one the ticket anticipated:

    - it is a SECOND coercion, and ADR-0005 admits exactly one, on the argument
      that String is what a plane says when it has nothing to say. A superseded
      form is not that; it is ferry second-guessing text a plane asserted.
    - it migrates the data anyway, silently, on the first dump.
    - it makes the golden column unstatable on the Load side, which is the one
      artefact this ADR has to lean on.
    - it is permanent, because ferry cannot observe the plane's remaining old
      forms and so can never learn that the arm is dead.

  What replaces it is Q28=6.`)
}

// ---------------------------------------------------------------------------
// Q28=6  The migration a user writes instead, run.
// ---------------------------------------------------------------------------

func q28f() {
	hdr("Q28=6  the migration that replaces the dual read, written and run")

	fmt.Println(`  ADR-0001 puts plane-to-plane transfer in the Enabled bucket: "falls out of
  the pluggable design for free". A representation migration is a plane-to-plane
  transfer where both planes are the same file and the two ferry versions differ,
  so the capability already exists and this ticket only has to check that it is
  enough.

  The whole program, using nothing that is not already decided:`)

	fmt.Println(`
      // old is the previous release's codec, new is this one's.
      type Conf struct{ T time.Duration ` + "`ferry:\"t\"`" + ` }

      cfg, err := ferry.Load[Conf](ctx, yaml.Source{Path: p}, ferry.WithRegistry(old))
      if err != nil { return err }
      return ferry.Dump(ctx, cfg, yaml.Sink{Path: p}, ferry.WithRegistry(new))

  Five lines, and it is the same five for a driver-side spelling change, with
  the two registries replaced by two driver versions.`)

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "q28f")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "app.yaml")
	_ = os.WriteFile(p, []byte("t: \"30000000000\"\n"), 0o644)

	// Two registries, which is what a migration actually holds: ADR-0010's
	// cache key is per registry, so the two schemas are two schemas and the
	// second Dump is not served the first Load's resolved codec.
	oldReg, newReg := NewRegistry(), NewRegistry()
	_ = oldReg.Register(StringCodec(
		func(t q28Timeout) string { return strconv.FormatInt(int64(t), 10) },
		func(s string) (q28Timeout, error) {
			n, err := strconv.ParseInt(s, 10, 64)
			return q28Timeout(n), err
		}))
	_ = newReg.Register(StringCodec(
		func(t q28Timeout) string { return time.Duration(t).String() },
		func(s string) (q28Timeout, error) {
			d, err := time.ParseDuration(s)
			return q28Timeout(d), err
		}))

	before, _ := os.ReadFile(p)
	cfg, lerr := Load[q28Conf](ctx, FYAMLSource{Path: p}, WithRegistry(oldReg))
	derr := Dump(ctx, cfg, FYAMLSink{Path: p}, WithRegistry(newReg))
	after, _ := os.ReadFile(p)

	fmt.Printf("\n    run: %q -> %v -> %q   (load err=%v, dump err=%v)\n",
		strings.TrimSpace(string(before)), time.Duration(cfg.T),
		strings.TrimSpace(string(after)), lerr, derr)

	// And the same file read back under the NEW registry, which is the check
	// that the migration actually finished rather than only that it wrote.
	back, berr := Load[q28Conf](ctx, FYAMLSource{Path: p}, WithRegistry(newReg))
	fmt.Printf("    verify: reload under the new codec -> %v, err=%v\n", time.Duration(back.T), berr)

	// And what the OLD codec now says about it, which is the deprecation
	// signal a dual read never gets.
	_, oerr := Load[q28Conf](ctx, FYAMLSource{Path: p}, WithRegistry(oldReg))
	fmt.Printf("    and the old codec on the migrated file -> err=%v\n", shorten2(fmt.Sprint(oerr), 70))

	fmt.Println(`
  Three properties the dual read does not have, and they are why this is the
  answer rather than the consolation prize:

    - it is a DECISION, taken once, by a person, at a time they chose.
    - it is OBSERVABLE. It succeeded or it did not, on a file list they hold.
    - it TERMINATES. The old codec is deleted afterwards, and no arm survives
      in core waiting for a plane nobody can inspect.

  What it costs, plainly, and this is the ticket's own objection: a ferry that
  can only read what its own version wrote is a worse migration story than the
  config files it replaced. The answer is not that the cost is small. It is
  that the alternative pays the same cost invisibly and then charges rent:

      A read-old path does not avoid the migration. It performs it silently on
      the first dump, and leaves core carrying the old form forever because
      nothing can prove it is unused.`)
}

// ---------------------------------------------------------------------------
// Q28=7  How much surface the promise actually covers.
// ---------------------------------------------------------------------------

func q28g() {
	hdr("Q28=7  the size of the frozen surface, counted")

	fmt.Println(`  A promise over "the representation" is only as wide as the set that has
  one. Counted rather than described:`)

	proofs := CoreTypes()
	fmt.Printf("\n    rows in CoreTypes()                          %d\n", len(proofs))
	names := make([]string, 0, len(proofs))
	for _, p := range proofs {
		names = append(names, p.Name())
	}
	fmt.Printf("    they are                                     %s\n", strings.Join(names, " "))

	missing := q28MissingProofs()
	fmt.Printf("\n    admitted members with NO row                 %d  %s\n", len(missing), strings.Join(missing, " "))
	fmt.Println(`    (that is #41's D18, red on the tip today, and it is #35's to close.)`)

	fmt.Println(`
    admitted with no row and no possible row:
      category 3, a type admitted by KIND with an unpinned representation
        nobody chose. ADR-0005 names it and lists net.IP and a [16]byte UUID.
      the text arm, which ADR-0007 calls its own weakest point: the set of
        types implementing encoding.TextMarshaler is unbounded and
        unenumerable, so no completeness check can reach it.

  So the promise has three tiers and the ADR has to say which is which:

      PINNED    core's identity table and the admitted kinds, one golden row
                each. This is what a compatibility promise can cover.
      CHOSEN BY SOMEBODY ELSE   a registered codec, whose representation is the
                registrant's and whose promise transfers with the guarantee
                ADR-0001 already transfers.
      CHOSEN BY NOBODY   a type admitted by kind or claimed by the chain with
                no row anywhere. ferry cannot promise what it never chose, and
                saying so is more useful than a promise that quietly excludes
                two thirds of what users actually store.`)
}

func q28MissingProofs() []string {
	have := map[string]bool{}
	for _, p := range CoreTypes() {
		have[p.Name()] = true
	}
	var missing []string
	for _, k := range []string{
		"bool", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64",
	} {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// ---------------------------------------------------------------------------
// Q28=8  Is a registrant told, and by what.
// ---------------------------------------------------------------------------

func q28h() {
	hdr("Q28=8  whether the obligation transfers, and where a registrant hears it")

	fmt.Println(`  ADR-0001 transfers the round-trip guarantee to a registrant. This ticket
  asks whether the STABILITY obligation transfers with it, and whether the
  registrant is told at registration time, which is #19's own mechanism:

      the diagnostic is where the obligation gets communicated, which is the
      point: it is the only moment a registrant is guaranteed to read.

  That mechanism is not available here, and the reason is worth measuring
  rather than asserting. .AsMapKey() works because there is something to
  REFUSE: a map keyed by an unopted type does not compile. A representation
  change has nothing to refuse, because at the moment of registration there is
  no previous version to compare against:`)

	reg := NewRegistry()
	err := reg.Register(StringCodec(
		func(h q28Host) string { return h.Name + ":" + strconv.Itoa(h.Port) },
		func(s string) (q28Host, error) {
			n, p, _ := strings.Cut(s, ":")
			i, _ := strconv.Atoi(p)
			return q28Host{n, i}, nil
		}))
	fmt.Printf("\n    Register(a codec) -> %v\n", err)
	err2 := reg.Register(StringCodec(
		func(h q28Host) string { return h.Name },
		func(s string) (q28Host, error) { return q28Host{s, 0}, nil }))
	fmt.Printf("    Register(a DIFFERENT codec for the same type) -> %v\n", err2)

	fmt.Println(`
  A duplicate is refused, so within one process there is one representation per
  type. Across two RELEASES of the registrant's own package there is nothing to
  compare: Register holds T and the codec, and the previous release is not in
  the room.

  So the answer is that the obligation transfers and the DIAGNOSTIC cannot
  carry it, which is a different answer from #19's and has to be argued rather
  than inherited. What carries it instead is the thing a registrant already has
  to write if they want the guarantee at all:

      A proof with a golden column is a change detector. A registrant who
      pinned string("api:80") and then changes their codec has a red test in
      their own CI, on their own schedule, before they tag.

  That is the same instrument as core's, which is the point: one mechanism,
  two owners, and core does not have to invent a second one. It is also why
  Q28=3's finding is not a detail. The instrument this ADR hands a registrant
  is the column that is currently missing from the harness they would import.`)
}

type q28Host struct {
	Name string
	Port int
}

// q28Timeout is a named type over time.Duration, which is the shape a
// migration actually has: core's own table is a compile-time constant, so the
// two versions in the room are two REGISTRIES and ADR-0010's cache key
// separates them.
type q28Timeout time.Duration

type q28Conf struct {
	T q28Timeout `ferry:"t"`
}

// ---------------------------------------------------------------------------
// Q28=9  Which version bump a consumer can receive without acting.
// ---------------------------------------------------------------------------

func q28i() {
	hdr("Q28=9  the only release a consumer cannot receive by accident")

	fmt.Println(`  The version rule this ADR proposes rests on one property of the Go module
  system, and it is checkable rather than folklore: a major version is a new
  import path, so it cannot arrive through an upgrade. Measured against a real
  module proxy rather than quoted.

  Three published versions of one module, and a consumer at v1.0.0 who runs
  ` + "`go get -u`" + `:`)

	root, err := os.MkdirTemp("", "q28proxy")
	if err != nil {
		fmt.Println("  mkdtemp:", err)
		return
	}
	defer os.RemoveAll(root)
	proxy := filepath.Join(root, "proxy")

	body := func(text string) string {
		return "package cfg\n\n// Encode is what lands in the artefact.\nfunc Encode() string { return \"" + text + "\" }\n"
	}
	if err := q28Publish(proxy, "example.com/cfg", "v1.0.0", "example.com/cfg", body("30s")); err != nil {
		fmt.Println("  publish:", err)
		return
	}
	// A MINOR release that changes the representation: exactly the accident.
	if err := q28Publish(proxy, "example.com/cfg", "v1.1.0", "example.com/cfg", body("30000000000")); err != nil {
		fmt.Println("  publish:", err)
		return
	}
	// A MAJOR release that changes it: a new import path.
	if err := q28Publish(proxy, "example.com/cfg/v2", "v2.0.0", "example.com/cfg/v2", body("30000000000")); err != nil {
		fmt.Println("  publish:", err)
		return
	}

	app := filepath.Join(root, "app")
	_ = os.MkdirAll(app, 0o755)
	_ = os.WriteFile(filepath.Join(app, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.26\n\nrequire example.com/cfg v1.0.0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(app, "main.go"),
		[]byte("package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/cfg\"\n)\n\nfunc main() { fmt.Println(cfg.Encode()) }\n"), 0o644)

	env := append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=mod",
		"GOPROXY=file://"+filepath.ToSlash(proxy),
		"GONOPROXY=",
		"GOPRIVATE=",
		"GONOSUMDB=",
		"GOSUMDB=off",
		"GONOSUMCHECK=1",
		"GOFLAGS=-mod=mod",
		"GdOFLAGS=",
		"GOMODCACHE="+filepath.Join(root, "modcache"),
		"GOFLAGS=-mod=mod")
	run := func(label string, args ...string) string {
		cmd := exec.Command("go", args...)
		cmd.Dir = app
		cmd.Env = env
		out, _ := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if s == "" {
			s = "(no output)"
		}
		fmt.Printf("    %-34s %s\n", label, shorten2(strings.ReplaceAll(s, "\n", " | "), 70))
		return s
	}

	fmt.Println()
	run("at v1.0.0, what it prints:", "run", ".")
	run("go get -u ./...", "get", "-u", "./...")
	run("after the upgrade, it prints:", "run", ".")
	mod, _ := os.ReadFile(filepath.Join(app, "go.mod"))
	for _, line := range strings.Split(strings.TrimSpace(string(mod)), "\n") {
		if strings.Contains(line, "example.com/cfg") {
			fmt.Printf("    %-34s %s\n", "go.mod now says:", strings.TrimSpace(line))
		}
	}
	fmt.Println()
	run("versions visible at that path:", "list", "-m", "-versions", "example.com/cfg")
	run("go get -u again, with v2 published:", "get", "-u", "./...")
	mod2, _ := os.ReadFile(filepath.Join(app, "go.mod"))
	fmt.Printf("    %-34s %v\n", "go.mod mentions /v2:", strings.Contains(string(mod2), "cfg/v2"))

	fmt.Println(`
  The minor release arrived on a ` + "`go get -u`" + ` and changed what the program writes.
  The major release did not arrive at all, and could not, because
  ` + "`example.com/cfg/v2`" + ` is a different import path and nothing upgrades across
  one.

  That is the whole reason this ADR spends a major version on a representation
  change rather than inventing a second version number:

      A major version is the only release in the Go module system that a
      consumer cannot receive without editing a line. A representation change
      is precisely a change that must not arrive without somebody editing a
      line.

  A second, ferry-specific version number would have neither property. Nothing
  in the toolchain reads it, so it could not stop the upgrade; and ferry writes
  no metadata into a plane - that would be a format decision, which the
  plane-agnosticism veto rules out - so it could not be stored beside the data
  either. It would be documentation with a number on it.`)
}

// q28Publish writes one module version into a file-based GOPROXY.
func q28Publish(proxy, modpath, version, pkgpath, src string) error {
	dir := filepath.Join(proxy, modpath, "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	gomod := "module " + modpath + "\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(dir, version+".mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	// The proxy protocol's version list, which is what `go get -u` reads.
	lst := filepath.Join(dir, "list")
	prev, _ := os.ReadFile(lst)
	if err := os.WriteFile(lst, append(prev, []byte(version+"\n")...), 0o644); err != nil {
		return err
	}
	info := `{"Version":"` + version + `","Time":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, version+".info"), []byte(info), 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, version+".zip"))
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	prefix := modpath + "@" + version + "/"
	for name, content := range map[string]string{"go.mod": gomod, "cfg.go": src} {
		w, err := zw.Create(prefix + name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return zw.Close()
}
