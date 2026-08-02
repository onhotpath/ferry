package main

// C3, C4, C5: redaction on dump, which #10 names as the test case and as "the
// one two-way introduces: a one-way loader could never write a credential to
// disk".

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cRedact is the redaction middleware, as small as it can be.
func cRedact(inner FSink, secret func(Path) bool, log *[]string) FSink {
	return cFwdSink{
		inner: inner,
		log:   log,
		rewrite: func(p Path, v Value) Value {
			if secret(p) {
				return String("***")
			}
			return v
		},
	}
}

func runC3() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "c10c")
	defer os.RemoveAll(dir)

	cfg := CConf{Name: "svc", DB: CDB{Host: "db1", Password: "hunter2"}}
	isSecret := func(p Path) bool { return strings.HasSuffix(p.String(), "/password") }

	fmt.Println("(a) it works, and the guarantee is real as far as it goes")
	p := filepath.Join(dir, "app.yaml")
	var log []string
	if err := Dump(ctx, cfg, cRedact(FYAMLSink{Path: p}, isSecret, &log)); err != nil {
		fmt.Println("    dump:", err)
	}
	b, _ := os.ReadFile(p)
	fmt.Printf("    what reached the sink: %v\n", log)
	fmt.Printf("    the file:\n%s", prefixLines(string(b), "      "))
	fmt.Printf("\n    the file contains \"hunter2\": %v\n", strings.Contains(string(b), "hunter2"))
	fmt.Println("    A wrapping Sink sees every Set before the inner sink does, so nothing")
	fmt.Println("    can reach the plane without passing it. That part IS a guarantee, and")
	fmt.Println("    it is a property of ADR-0004's contract rather than of the wrapper.")

	fmt.Println("\n(b) but the wrapper has to be TOLD which addresses are secret, and ferry")
	fmt.Println("    has nowhere to get that from")
	fmt.Println("    `isSecret` above is the caller's predicate. The three places it could")
	fmt.Println("    have come from instead:")
	fmt.Println("      a tag option    - ADR-0008 refused `nodump` and `readonly` by name,")
	fmt.Println("                        because \"a field ferry loads and never writes")
	fmt.Println("                        cannot round-trip\". Redaction is that violation")
	fmt.Println("                        exactly, so the refusal is consistent and it means")
	fmt.Println("                        the type cannot carry the answer.")
	fmt.Println("      a schema view   - does not exist; ADR-0001 left it open and ADR-0010")
	fmt.Println("                        and ADR-0012 both declined to reopen it. Same wall")
	fmt.Println("                        #14 hit.")
	fmt.Println("      a side table    - what this is, and it spells the address set a")
	fmt.Println("                        second time. ADR-0006 measured that failure mode")
	fmt.Println("                        against a Static defaults source: a rename drops it")
	fmt.Println("                        silently.")
	fmt.Println("    Reproduced, with the tag renamed and the predicate not:")
	p2 := filepath.Join(dir, "renamed.yaml")
	var log2 []string
	_ = Dump(ctx, CRenamed{Name: "svc", DB: CRenamedDB{Host: "db1", Secret: "hunter2"}},
		cRedact(FYAMLSink{Path: p2}, isSecret, &log2))
	b2, _ := os.ReadFile(p2)
	fmt.Printf("      the file contains \"hunter2\": %v   <- the redaction silently stopped\n",
		strings.Contains(string(b2), "hunter2"))

	fmt.Println("\n(c) and a DYNAMIC address is not in any set the caller can enumerate")
	fmt.Println("    ADR-0003's second tier: a map key's address comes from the value. So a")
	fmt.Println("    predicate over a fixed address list cannot name /creds/prod/token, and")
	fmt.Println("    the middleware has to match on shape rather than on identity - which")
	fmt.Println("    is a second address language for the user to get wrong.")
	p3 := filepath.Join(dir, "dyn.yaml")
	var log3 []string
	_ = Dump(ctx, CDynConf{Creds: map[string]string{"prod": "s3cr3t", "dev": "public"}},
		cRedact(FYAMLSink{Path: p3}, func(p Path) bool { return p == path("creds", "prod") }, &log3))
	b3, _ := os.ReadFile(p3)
	fmt.Printf("      exact-address predicate: %v\n", log3)
	fmt.Printf("      the file:\n%s\n", prefixLines(string(b3), "        "))

	fmt.Println("\n(d) THE #14 INTERACTION: redaction on the sink does not protect a")
	fmt.Println("    template, because a template does not go through the sink")
	fmt.Println("    #14 measured that an annotated template cannot be emitted through")
	fmt.Println("    ADR-0004's Writer at all - there is no channel for a comment - so a")
	fmt.Println("    template emitter is a plane-specific writer that BYPASSES Sink.")
	fmt.Println("    A redaction middleware wraps a Sink. It is therefore not in the path.")
	tp, _ := tPlanFor[CConf](ctx, tAggregating)
	art := tRealYAML(ctx, tp)
	fmt.Printf("      the emitted template contains \"hunter2\": %v\n", strings.Contains(art, "hunter2"))
	fmt.Println("    So the answer to #10's third question - \"can it guarantee a secret")
	fmt.Println("    never reaches the sink\" - is yes for the sink and no for the plane,")
	fmt.Println("    because the sink is not the only way to the plane.")
	fmt.Println("    And the credential here is a DECLARED DEFAULT, so it is in the type,")
	fmt.Println("    in every compiled schema and in the binary. Nothing at the boundary")
	fmt.Println("    can be the whole answer to that.")
}

func runC4() {
	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "c10d")
	defer os.RemoveAll(dir)
	cfg := CConf{Name: "svc", DB: CDB{Host: "db1", Password: "hunter2"}}

	fmt.Println("(a) the mechanism is symmetric; the OBLIGATIONS are not")
	fmt.Println("    A Source wrapper forwards one optional interface, Enumerator, and its")
	fmt.Println("    failure is loud. A Sink wrapper forwards two, Committer and Releaser,")
	fmt.Println("    and both failures are silent - C2 measured a nil error and an unwritten")
	fmt.Println("    plane. So dump needs strictly more of the wrapper than load does, and")
	fmt.Println("    the extra is exactly where the silence is.")

	fmt.Println("\n(b) a redacting sink breaks value fidelity BY DESIGN, and that is fine")
	fmt.Println("    only because a wrapper is not a driver")
	p := filepath.Join(dir, "a.yaml")
	var log []string
	_ = Dump(ctx, cfg, cRedact(FYAMLSink{Path: p}, func(p Path) bool {
		return strings.HasSuffix(p.String(), "/password")
	}, &log))
	back, err := Load[CConf](ctx, FYAMLSource{Path: p}, WithSched(tAggregating))
	fmt.Printf("    dumped then loaded: %+v err=%v\n", back, errShortW(err))
	fmt.Printf("    round trip equal: %v\n", back == cfg)
	fmt.Println("    ADR-0001 makes value fidelity a hard guarantee and backs driver")
	fmt.Println("    fidelity with a conformance suite over `driver/*`. A wrapper lives")
	fmt.Println("    nowhere near that glob, so nothing runs the suite against it, and a")
	fmt.Println("    middleware that deliberately fails it is not a contradiction.")
	fmt.Println("    That is worth stating rather than assuming, because the same fact")
	fmt.Println("    means nothing checks a wrapper's Committer forwarding either.")

	fmt.Println("\n(c) ADR-0011's two-phase Dump is upstream of the wrapper and unaffected")
	fmt.Println("    It encodes every address before writing any, then the writes go")
	fmt.Println("    through Set - so a wrapper sees the second phase and cannot change the")
	fmt.Println("    first. A middleware therefore cannot make an unencodable value")
	fmt.Println("    encodable, which is the right place for that line.")
	fmt.Println("    But a wrapper that hides Committer moves the sink from interleaved")
	fmt.Println("    aggregation to the encode phase, which ADR-0011 measured as a")
	fmt.Println("    materially worse error set: 4 errors against 2 on the same plane.")
	fmt.Println("    So dropping the assertion changes ferry's ERROR POLICY as well as")
	fmt.Println("    losing the commit, and neither is visible at the call site.")
}

func runC5() {
	fmt.Println("(a) a tag option: refused by ADR-0008, and the refusal is right")
	fmt.Printf("    Compile[CNoDump]() -> %v\n", errShortW(Compile[CNoDump]()))
	fmt.Println("    ADR-0008 lists `nodump` and `readonly` with one reason: \"a field ferry")
	fmt.Println("    loads and never writes cannot round-trip\". Redaction is that same")
	fmt.Println("    violation, so it belongs outside the type by the same argument.")

	fmt.Println("\n(b) a registered codec that encodes to ***: PERMITTED, and this probe")
	fmt.Println("    predicted the opposite")
	reg := NewRegistry()
	err := reg.Register(StringCodec(
		func(s CSecret) string { return "***" },
		func(s string) (CSecret, error) { return CSecret(s), nil },
	))
	fmt.Printf("    Register(a redacting codec) -> %v\n", errShortW(err))
	fmt.Println("    The prediction was that ADR-0009's own dec(enc(zero)) check would")
	fmt.Println("    refuse it, because a redacting codec is not an inverse. It does not,")
	fmt.Println("    and the reason is stated in ADR-0009 rather than being an oversight:")
	fmt.Println("    the check asks whether the round trip ERRORS, and deliberately not")
	fmt.Println("    whether the value comes back equal, \"because equality needs a relation")
	fmt.Println("    and a relation is the registrant's\". \"***\" decodes fine.")
	fmt.Println("    Run through a real dump and load, which is what the check cannot see:")
	regd := NewRegistry()
	_ = regd.Register(StringCodec(
		func(s CSecret) string { return "***" },
		func(s string) (CSecret, error) { return CSecret(s), nil },
	))
	ctx2 := context.Background()
	dir2, _ := os.MkdirTemp("", "c10e")
	defer os.RemoveAll(dir2)
	pp := filepath.Join(dir2, "codec.yaml")
	in := CCodecConf{Name: "svc", Token: CSecret("hunter2")}
	if e := Dump(ctx2, in, FYAMLSink{Path: pp}, WithRegistry(regd)); e != nil {
		fmt.Println("    dump:", e)
	}
	bb, _ := os.ReadFile(pp)
	backc, e2 := Load[CCodecConf](ctx2, FYAMLSource{Path: pp}, WithRegistry(regd), WithSched(tAggregating))
	fmt.Printf("    the file: %s", prefixLines(string(bb), "      "))
	fmt.Printf("\n    loaded back: %+v err=%v   round trip equal: %v\n", backc, errShortW(e2), backc == in)
	fmt.Println("    So redaction IS expressible as a codec, ferry permits it, and")
	fmt.Println("    ADR-0009 already says what that means: \"Registering without proving is")
	fmt.Println("    permitted and forfeits the guarantee.\"")
	fmt.Println("    It is nonetheless the WORSE of the two mechanisms, and the reason is")
	fmt.Println("    visibility rather than correctness. A codec is registered once in some")
	fmt.Println("    package's init and then applies to every use of that type, in both")
	fmt.Println("    directions, in every program that imports it. A middleware appears in")
	fmt.Println("    the Dump call that uses it. #10 should say so rather than claiming the")
	fmt.Println("    codec route is refused, which is what this probe assumed until it ran.")

	fmt.Println("\n(c) so the mechanism is a wrapper, and #10's remaining question is not")
	fmt.Println("    \"which mechanism\" but \"what does core owe a wrapper author\"")
	fmt.Println("    Three candidates, none of which is a new interface:")
	fmt.Println("      1. a conformance case, in ferrytest, asserting that a wrapper")
	fmt.Println("         forwards every optional interface its inner value implements.")
	fmt.Println("         Fits ADR-0002's route (b) exactly: an obligation core imposes")
	fmt.Println("         and cannot compile-check, shipped as the thing that checks it.")
	fmt.Println("      2. a core-supplied embedding helper, so a wrapper composes by")
	fmt.Println("         construction rather than by remembering. C2(d) measured why a")
	fmt.Println("         user cannot write one: embedding an INTERFACE promotes only its")
	fmt.Println("         own method set, and the concrete type is unknown.")
	fmt.Println("      3. making Committer and Releaser required, which ADR-0004 refused")
	fmt.Println("         on driver boilerplate and which would make wrappers correct by")
	fmt.Println("         construction. That is an amendment to ADR-0004 and #10 does not")
	fmt.Println("         get to make it.")
}

// C3 and C5's fixtures.

type CRenamedDB struct {
	Host   string `ferry:"host"`
	Secret string `ferry:"secret,default=hunter2"`
}

type CRenamed struct {
	Name string     `ferry:"name"`
	DB   CRenamedDB `ferry:"db"`
}

type CDynConf struct {
	Creds map[string]string `ferry:"creds"`
}

type CNoDump struct {
	Name     string `ferry:"name"`
	Password string `ferry:"password,nodump"`
}

type CSecret string

type CCodecConf struct {
	Name  string  `ferry:"name"`
	Token CSecret `ferry:"token"`
}
