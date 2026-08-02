package main

// E7: two things handed to #16 by name that have nothing else in common.
//
//   (a) Whether a root leaf is a legal address. ADR-0007 handed it over:
//       "a chain-admitted type at the root mints the empty path, which
//       ADR-0003 says an address may not be. This is pre-existing - a root
//       int does the same - and belongs to #16's entry point, but the chain
//       enlarges the set of types that can sit there." ADR-0009 repeated it
//       for a registered type.
//
//   (b) The rule for what may become part of the cache key, which ADR-0006
//       opened and nobody closed.

import (
	"context"
	"fmt"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
)

type E7Wrapped struct {
	V netip.Addr `ferry:"v"`
}

func schemaFingerprint(s *schema) string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprint(s.addrs, s.leaves)
}

func runE7() {
	ctx := context.Background()
	reg := NewRegistry()
	mustReg(reg, TextCodec[big.Int](VString))

	fmt.Println("--- E7a: what sits at the root, and what each does to the address ---")
	type row struct {
		name string
		t    reflect.Type
		o    []Option
	}
	rows := []row{
		{"struct{...}                 ordinary", reflect.TypeFor[E7Wrapped](), nil},
		{"int                         kind leaf", reflect.TypeFor[int](), nil},
		{"time.Duration               identity leaf", reflect.TypeFor[durAlias](), nil},
		{"netip.Addr                  a STRUCT the chain claims", reflect.TypeFor[netip.Addr](), nil},
		{"big.Int                     a STRUCT a registration claims", reflect.TypeFor[big.Int](), []Option{WithRegistry(reg)}},
		{"map[string]int              dynamic composite", reflect.TypeFor[map[string]int](), nil},
		{"[]string                    dynamic composite", reflect.TypeFor[[]string](), nil},
		{"[2]string                   static composite", reflect.TypeFor[[2]string](), nil},
		{"*E7Wrapped                  pointer to a struct", reflect.TypeFor[*E7Wrapped](), nil},
	}
	for _, r := range rows {
		o := defaultOpts()
		for _, op := range r.o {
			op.apply(&o)
		}
		done := o.reg.install()
		s, err := compileSchema2(r.t, o)
		done()
		if err != nil {
			fmt.Printf("  %-52s REFUSED  %s\n", r.name, trunc(err))
			continue
		}
		fmt.Printf("  %-52s addrs=%v\n", r.name, s.addrs)
	}

	fmt.Println("\n  Read the rows rather than the summary. `netip.Addr` and `big.Int` are")
	fmt.Println("  STRUCTS, so a rule written as \"the root must be a struct\" admits both,")
	fmt.Println("  and ADR-0007's chain and ADR-0009's table then collapse them to a leaf")
	fmt.Println("  at the empty path. So the rule is not about the Go kind:")
	fmt.Println()
	fmt.Println("    The root may not be a LEAF, and whether it is one is decided after")
	fmt.Println("    the chain and the registry have been asked, not before.")
	fmt.Println()
	fmt.Println("  That is a rule the entry point cannot enforce and only the compiler can,")
	fmt.Println("  which is why it lands here rather than at Load's signature.")

	fmt.Println("\n--- E7b: what a root leaf actually does, with the check removed ---")
	fmt.Println("  Not a panic and not a wrong value: an address that is the EMPTY path,")
	fmt.Println("  which every driver then has to name. Run rather than reasoned about,")
	fmt.Println("  by compiling the node by hand and dumping it through the real drivers:")
	lc, _ := resolveLeaf(reflect.TypeFor[int]())
	rootLeaf := &node{kind: nLeaf, typ: reflect.TypeFor[int](), shape: Path{}, codec: lc}
	rootOut := map[Path]Value{}
	rw := &walker{dir: dumpDir(rootOut), sch: serial, ctx: ctx}
	_, _ = rw.walk(rootLeaf, reflect.ValueOf(8080), Path{})
	for p, v := range rootOut {
		fmt.Printf("    the address it mints        : %q (IsRoot=%v), value %s\n", p.String(), p.IsRoot(), v.GoString())
	}
	dir, _ := os.MkdirTemp("", "e7")
	defer os.RemoveAll(dir)
	yp := filepath.Join(dir, "root.yaml")
	as := NewAddressSet(sortedAddrs(rootOut))
	ow, berr := FYAMLSink{Path: yp}.Bind(as)
	if berr != nil {
		fmt.Printf("    YAML sink Bind              : %v\n", berr)
	} else {
		derr := fDump(ctx, ow, rootOut, as)
		b, _ := os.ReadFile(yp)
		fmt.Printf("    YAML sink Dump              : err=%v, wrote %q\n", derr, string(b))
	}
	fmt.Println("    env-shaped key function     : an empty segment sequence has no")
	fmt.Println("                                  environment variable name at all")
	fmt.Println()
	fmt.Println("  The YAML row is the finding. It is not a refusal and it is not a bad")
	fmt.Println("  document: the sink writes an EMPTY MAPPING and returns a nil error, so")
	fmt.Println("  the value is silently and totally lost. That is ADR-0005's maps-no-")
	fmt.Println("  address class arriving at the one address ADR-0003 forgot to protect,")
	fmt.Println("  and it is what makes this a refusal rather than a documented sharp edge.")
	fmt.Println("  ADR-0003 fixed that an address is a NON-EMPTY sequence of segments, so")
	fmt.Println("  this is not a new rule. It is the existing one reaching the one place")
	fmt.Println("  the type system could still produce a violation, and the refusal is at")
	fmt.Println("  schema compile so no driver ever has to have an opinion about it.")

	fmt.Println("\n--- E7c: a root map and a root slice are legal, and that is deliberate ---")
	fmt.Println("  Both mint non-empty addresses, so ADR-0003 has no objection, and both")
	fmt.Println("  are things people dump. What they cost:")
	m, err := dumpTo(ctx, map[string]int{"a": 1, "b": 2})
	fmt.Printf("    Dump(ctx, map[string]int{...}) -> %v err=%v\n", sortedAddrs(m), err)
	back, err := loadFrom(ctx, map[string]int(nil), m)
	fmt.Printf("    and back                       -> %v err=%v\n", back, err)
	fmt.Println("  A root map has NO static address at all, so the set handed to Bind is")
	fmt.Println("  empty and the driver's injectivity check runs over nothing. That is")
	fmt.Println("  ADR-0003's dynamic tier doing exactly what it says, and it means a root")
	fmt.Println("  map is loadable only from a source implementing Enumerator - which is")
	fmt.Println("  ADR-0004's asymmetry, at the root, where it is most visible.")
	fmt.Println("  Refusing them was considered and is the WRONG direction here: refusing")
	fmt.Println("  is reversible only when nobody depends on the refusal, and ADR-0001's")
	fmt.Println("  plane-to-plane transfer is exactly the caller who would.")

	fmt.Println("\n--- E7d: the rule for what may enter the cache key ---")
	fmt.Println("  ADR-0006 opened this and left it: \"a compile-affecting Option becomes")
	fmt.Println("  part of whatever keys the schema cache\", measured at the bad end as")
	fmt.Println("  `hash of unhashable type main.LoadOption`. Two Options exist today and")
	fmt.Println("  both are compile-affecting, from two ADRs that did not coordinate.")
	fmt.Println()
	fmt.Println("    An Option is compile-affecting if one reflect.Type yields two")
	fmt.Println("    different schemas under two values of it. A compile-affecting Option")
	fmt.Println("    is part of the cache key, and its value must be comparable.")
	fmt.Println("    An Option that is not compile-affecting must NOT be in the key.")
	fmt.Println()
	fmt.Println("  Measured on the three Options this prototype has:")
	type oRow struct {
		name string
		t    reflect.Type
		a, b Option
	}
	// Each row uses a type on which that Option could possibly matter. A row
	// tested against a type it cannot affect proves nothing, which is the
	// shape of null probe #11 recorded shipping.
	regged := NewRegistry()
	mustReg(regged, e5Codec())
	for _, r := range []oRow{
		{"TagKey", reflect.TypeFor[E2Conf](), TagKey("ferry"), TagKey("mylib")},
		{"WithRegistry", reflect.TypeFor[E5Conf](), WithRegistry(NewRegistry()), WithRegistry(regged)},
		{"Observe", reflect.TypeFor[E2Conf](), Observe(nil), Observe(func(Path, Value) {})},
	} {
		t := r.t
		oa, ob := defaultOpts(), defaultOpts()
		r.a.apply(&oa)
		r.b.apply(&ob)
		da := oa.reg.install()
		sa, ea := compileSchema2(t, oa)
		da()
		db := ob.reg.install()
		sb, eb := compileSchema2(t, ob)
		db()
		same := fmt.Sprint(schemaFingerprint(sa), ea) == fmt.Sprint(schemaFingerprint(sb), eb)
		verdict := "compile-affecting -> IN the key"
		if same {
			verdict = "load-affecting    -> NOT in the key"
		}
		fmt.Printf("    %-14s two values give the same schema: %-6v  %s\n", r.name, same, verdict)
	}
	fmt.Println()
	fmt.Println("  The rule needs a mechanism or it is prose. The mechanism is one line,")
	fmt.Println("  and E2c measured that it is a BUILD error and not a runtime one:")
	fmt.Println("      type schemaKey struct { typ reflect.Type; tagKey string }")
	fmt.Println("      var _ = map[schemaKey]struct{}{}")
	fmt.Println("  A named key struct is what makes adding a field to it a deliberate act,")
	fmt.Println("  and the plain-map assertion is what makes an uncomparable one fail to")
	fmt.Println("  compile rather than to panic on the first Load in production.")
	fmt.Println("  Note the sync.Map the cache actually uses would NOT catch it: it takes")
	fmt.Println("  `any` keys, which is how ADR-0006's measurement is a panic at all.")
}
