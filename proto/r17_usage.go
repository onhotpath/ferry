package main

// R17: the consumer's view.
//
// Everything before this probe measures a decision. None of it shows the API
// the way a library consumer meets it, which is the thing that actually gets
// signed off. This probe writes the call sites, runs them, and prints what a
// user would see, using ADR-0008's tag grammar throughout.
//
// It also answers the five questions the ADR asserted rather than showed:
//   R17a  what a consumer's whole file looks like
//   R17b  when each constructor is the only one that works
//   R17c  the lifetime question as two entry-point spellings
//   R17d  the map-key opt-in, and what a false refusal costs
//   R17e  what dynamic registration would look like, and who would call it

import (
	"fmt"
	"math/big"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"time"
)

// --- R17a: the consumer's file -----------------------------------------------

// PollInterval is the named type over time.Duration that ADR-0005 documents as
// a sharp edge: it misses the identity table, falls to kind int64, and dumps
// nanoseconds unless somebody registers it.
type PollInterval time.Duration

// AppConfig is what the consumer annotates. Tags are ADR-0008's grammar.
type AppConfig struct {
	Listen   netip.AddrPort `ferry:"listen"`
	Upstream url.URL        `ferry:"upstream"`
	Poll     PollInterval   `ferry:"poll"`
	MaxBytes big.Int        `ferry:"max_bytes"`
}

func runR17() {
	fmt.Println("--- R17a: the whole consumer file, top to bottom ---")
	os.Stdout.WriteString(`
    package main

    import (
        "math/big"
        "net/netip"
        "net/url"
        "time"

        "github.com/onhotpath/ferry"
        "github.com/onhotpath/ferry/driver/yaml"
    )

    type PollInterval time.Duration

    type AppConfig struct {
        Listen   netip.AddrPort ` + "`ferry:\"listen\"`" + `
        Upstream url.URL        ` + "`ferry:\"upstream\"`" + `
        Poll     PollInterval   ` + "`ferry:\"poll,default=30s\"`" + `
        MaxBytes big.Int        ` + "`ferry:\"max_bytes\"`" + `
    }

    func init() {
        // Register writes into ferry's default registry. It returns an error
        // rather than panicking, and an init that ignores it is the one thing
        // this API cannot stop.
        err := ferry.Register(
            // 1. The type declares its own inverse. ferry uses it, and the
            //    only thing you supply is the boundary kind.
            ferry.TextCodec[netip.AddrPort](ferry.String),

            // 2. You declare the inverse, as two functions.
            ferry.StringCodec(
                func(u url.URL) string { return u.String() },
                func(s string) (url.URL, error) {
                    u, err := url.Parse(s)
                    if err != nil {
                        return url.URL{}, err
                    }
                    return *u, nil
                }),

            // 3. Core ships this one, because ADR-0005 named the hole.
            ferry.DurationLike[PollInterval](),

            // 4. You declare the inverse AND the kind, because big.Int's text
            //    is a run of digits and a YAML plane will report it as Number.
            ferry.ValueCodec(ferry.Number,
                func(x big.Int) (ferry.Value, error) { return ferry.Num(x.String()), nil },
                func(v ferry.Value) (big.Int, error) {
                    var x big.Int
                    s, err := v.AsNumber()
                    if err != nil {
                        return x, err
                    }
                    if _, ok := x.SetString(s, 10); !ok {
                        return x, fmt.Errorf("not an integer: %q", s)
                    }
                    return x, nil
                }),
        )
        if err != nil {
            panic(err)
        }
    }

    func main() {
        var cfg AppConfig
        if err := ferry.Load(ctx, &cfg, yaml.Source{Path: "app.yaml"}); err != nil {
            log.Fatal(err)
        }
    }`)

	fmt.Println("\n    Run against the real walk and a real YAML plane:")
	reg := NewRegistry()
	if err := reg.Register(
		TextCodec[netip.AddrPort](VString),
		StringCodec(
			func(u url.URL) string { return u.String() },
			func(s string) (url.URL, error) {
				u, err := url.Parse(s)
				if err != nil {
					return url.URL{}, err
				}
				return *u, nil
			}),
		DurationLike[PollInterval](),
		TypeCodec(VNumber,
			func(x big.Int) (Value, error) { return Number(x.String()), nil },
			func(v Value) (big.Int, error) {
				var x big.Int
				s, err := v.AsNumber()
				if err != nil {
					return x, err
				}
				if _, ok := x.SetString(s, 10); !ok {
					return x, fmt.Errorf("not an integer: %q", s)
				}
				return x, nil
			}),
	); err != nil {
		fmt.Println("    Register err:", err)
	}
	withRegistry(reg, func() {
		cfg := AppConfig{
			Listen:   netip.MustParseAddrPort("0.0.0.0:8080"),
			Upstream: mustU("https://api.example.com/v1"),
			Poll:     PollInterval(30 * time.Second),
			MaxBytes: *big.NewInt(1 << 40),
		}
		d, err := dump(reflect.ValueOf(cfg))
		fmt.Printf("\n    dump err=%v\n", err)
		for _, p := range sortedAddrs(d) {
			fmt.Printf("      %-12s %s\n", p, d[p].GoString())
		}
		fmt.Println("\n    the app.yaml this produces:")
		for _, l := range splitLines(p4yaml(cfg)) {
			fmt.Printf("      %s\n", l)
		}
		var back AppConfig
		fmt.Printf("    load back err=%v -> listen=%v poll=%v max=%s\n",
			load(d, reflect.ValueOf(&back).Elem()),
			back.Listen, time.Duration(back.Poll), back.MaxBytes.String())
		fmt.Println("\n    and the same struct through a FLAT plane, which is env or Consul:")
		flat, _ := flatten(d)
		var back2 AppConfig
		fmt.Printf("    load err=%v -> listen=%v poll=%v max=%s\n",
			load(flat, reflect.ValueOf(&back2).Elem()),
			back2.Listen, time.Duration(back2.Poll), back2.MaxBytes.String())
		fmt.Println("    ^ max_bytes declared Number and loads from BOTH, which is the")
		fmt.Println("      whole reason the kind is an argument rather than a default.")
	})

	r17Constructors()
	r17Lifetime()
	r17MapKey()
	r17Dynamic()
}

// --- R17b: which constructor, and when ---------------------------------------

func r17Constructors() {
	fmt.Println("\n--- R17b: the three constructors differ by WHAT YOU HAND OVER ---")
	fmt.Println()
	fmt.Println("    constructor      you supply                    the type must")
	fmt.Println("    ---------------  ---------------------------  --------------------------")
	fmt.Println("    TextCodec[T]     a kind. nothing else.        implement encoding.Text{,Un}Marshaler")
	fmt.Println("    StringCodec      format and parse funcs       nothing")
	fmt.Println("    ValueCodec       kind, encode and decode      nothing")
	fmt.Println("                     funcs over ferry.Value")
	fmt.Println()
	fmt.Println("    So TextCodec takes NO function arguments at all: both halves come")
	fmt.Println("    from the type. That is the difference the two names hide, and it is")
	fmt.Println("    the reason this ADR proposes renaming the general form TypeCodec ->")
	fmt.Println("    ValueCodec: the trio then reads String / Value / Text, named after")
	fmt.Println("    what the two halves speak.")

	fmt.Println("\n    When each is the ONLY one that works, measured:")

	fmt.Println("\n    (1) TextCodec, when the type has a pair and you need another KIND.")
	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()
	type bigconf struct{ B big.Int }
	v := reflect.ValueOf(bigconf{*big.NewInt(1 << 40)})
	d0, _ := dump(v)
	fmt.Printf("        unregistered, ADR-0007 step 2 gives String -> %s\n",
		d0[Path{}.Name("B")].GoString())
	numeric := NewRegistry()
	_ = numeric.Register(TextCodec[big.Int](VNumber))
	withRegistry(numeric, func() {
		d1, _ := dump(v)
		fmt.Printf("        TextCodec[big.Int](Number)                 -> %s\n",
			d1[Path{}.Name("B")].GoString())
		typed := map[Path]Value{Path{}.Name("B"): Number("1099511627776")}
		var back bigconf
		fmt.Printf("        and it now loads from a YAML plane saying Number: err=%v %s\n",
			load(typed, reflect.ValueOf(&back).Elem()), back.B.String())
	})
	fmt.Println("        ^ eleven lines of ValueCodec replaced by one, because the type")
	fmt.Println("          already declares the inverse and only the KIND was wrong.")

	fmt.Println("\n    (2) StringCodec, when the type declares no inverse.")
	fmt.Println("        url.URL has no text pair, so TextCodec cannot be written for it:")
	fmt.Println("          TextCodec[url.URL](String)")
	fmt.Println("          -> url.URL does not satisfy interface{*url.URL;")
	fmt.Println("             UnmarshalText([]byte) error} (missing method UnmarshalText)")
	fmt.Println("        That is a BUILD error, so the wrong constructor is unwritable")
	fmt.Println("        rather than wrong at runtime.")

	fmt.Println("\n    (3) ValueCodec, when the codec must accept a kind it never emits.")
	fmt.Println("        This is ADR-0006's escape hatch and StringCodec cannot express")
	fmt.Println("        it, because its decode half only ever sees a string:")
	strict := NewRegistry()
	_ = strict.Register(StringCodec(
		func(p PollInterval) string { return time.Duration(p).String() },
		func(s string) (PollInterval, error) {
			d, err := time.ParseDuration(s)
			return PollInterval(d), err
		}))
	lenient := NewRegistry()
	_ = lenient.Register(TypeCodec(VString,
		func(p PollInterval) (Value, error) { return String(time.Duration(p).String()), nil },
		func(v Value) (PollInterval, error) {
			if v.Kind() == VNull {
				return 0, nil // "null means no polling"
			}
			s, err := v.AsString()
			if err != nil {
				return 0, err
			}
			d, err := time.ParseDuration(s)
			return PollInterval(d), err
		}))
	type pc struct{ P PollInterval }
	for _, tc := range []struct {
		label string
		r     *Registry
	}{{"StringCodec", strict}, {"ValueCodec ", lenient}} {
		withRegistry(tc.r, func() {
			var out pc
			err := load(map[Path]Value{Path{}.Name("P"): Null()}, reflect.ValueOf(&out).Elem())
			fmt.Printf("        %s, plane holds `poll:` (a YAML null) -> %v\n",
				tc.label, errOr(err, time.Duration(out.P).String()))
		})
	}
}

// --- R17c: the lifetime question, as consumer code ---------------------------

func r17Lifetime() {
	fmt.Println("\n--- R17c: the lifetime question is a question about #16's SIGNATURE ---")
	fmt.Println()
	fmt.Println("    The two spellings a consumer could meet:")
	fmt.Println(`
    (A) implicit registry
        func init() { ferry.Register(...) }
        var cfg AppConfig
        err := ferry.Load(ctx, &cfg, yaml.Source{Path: "app.yaml"})

    (B) explicit registry
        reg := ferry.NewRegistry()
        reg.Register(...)
        cfg, err := ferry.Load[AppConfig](ctx, src, ferry.WithRegistry(reg))`)
	fmt.Println()
	fmt.Println("    This ADR needs BOTH, and says core ships a default registry so (A)")
	fmt.Println("    stays available. Neither spelling is #19's to choose: the entry")
	fmt.Println("    point is #16's, and ADR-0008 has already put its own tag-key Option")
	fmt.Println("    into the same cache key.")
	fmt.Println()
	fmt.Println("    Here is the concrete thing #16 cannot get right without this ADR.")
	fmt.Println("    Every one of the eight type caches in the standard library is a")
	fmt.Println("    sync.Map keyed by reflect.Type, and ADR-0008 just measured that")
	fmt.Println("    shape at 18 ns. It is the obvious thing for #16 to write:")
	fmt.Println()
	fmt.Println("        var schemaCache sync.Map // map[reflect.Type]*schema")
	fmt.Println()
	fmt.Println("    Run it against spelling (B), with two registries in one process:")

	chainOrder, chainBeforeKind = []string{"text"}, true
	defer func() { chainOrder, chainBeforeKind = nil, false }()

	type Conf struct{ B big.Int }
	asText := NewRegistry()
	_ = asText.Register(TextCodec[big.Int](VString))
	asNumber := NewRegistry()
	_ = asNumber.Register(TextCodec[big.Int](VNumber))

	cache := map[reflect.Type]Value{}
	run := func(label string, r *Registry) {
		withRegistry(r, func() {
			t := reflect.TypeFor[Conf]()
			if v, ok := cache[t]; ok {
				fmt.Printf("      %-34s -> %-28s (cache hit)\n", label, v.GoString())
				return
			}
			d, _ := dump(reflect.ValueOf(Conf{*big.NewInt(1 << 40)}))
			v := d[Path{}.Name("B")]
			cache[t] = v
			fmt.Printf("      %-34s -> %s\n", label, v.GoString())
		})
	}
	run("service A wants it as text", asText)
	run("service B wants it as a number", asNumber)
	fmt.Println("    ^ B silently gets A's representation, and writes a quoted string")
	fmt.Println("      into a YAML file where its own codec said Number. No error.")
	fmt.Println("      That is ADR-0004's EnvSource{Sep} defect, one layer up, and it is")
	fmt.Println("      not visible from #16's ticket at all.")
	fmt.Println()
	fmt.Println("    So the split this ADR proposes is:")
	fmt.Println("      #19 owns  - a registration goes into a Registry value")
	fmt.Println("                - a Registry freezes at its first use")
	fmt.Println("                - core ships a default one")
	fmt.Println("      #16 owns  - the entry point's signature, and whether (A) or (B)")
	fmt.Println("                  or both are spelled")
	fmt.Println("                - where the cache lives and what else is in its key")
	fmt.Println("                  (ADR-0008 already put the tag key there)")
	fmt.Println("      the seam  - `once a type has been resolved against a registry,")
	fmt.Println("                   that registry's answer for that type must never")
	fmt.Println("                   change`, and the cache key must distinguish two")
	fmt.Println("                   registries")
	fmt.Println()
	fmt.Println("    ADR-0008 already wrote half of this handoff, in #16's words:")
	fmt.Println("      `whether the codec registry belongs there depends on #19 making")
	fmt.Println("       it process-wide or per-instance`")
	fmt.Println("    This ADR's answer is: per-instance, with a default instance, and")
	fmt.Println("    frozen. So yes, it belongs in the key.")
}

// --- R17d: the map-key opt-in ------------------------------------------------

// injective is the harness check ADR-0005 asks for and never spells: over the
// registrant's own value list, are all the key texts distinct?
func injective[T any](format func(T) string, values ...T) error {
	seen := map[string]int{}
	for i, v := range values {
		s := format(v)
		if j, dup := seen[s]; dup {
			return fmt.Errorf(
				"ferry: key codec for %T is not injective: values[%d] and values[%d] both encode to %q",
				v, j, i, s)
		}
		seen[s] = i
	}
	return nil
}

type R17Host struct {
	Name string
	Port int
}

func r17MapKey() {
	fmt.Println("\n--- R17d: the map-key opt-in, and what the false refusal actually is ---")
	fmt.Println()
	fmt.Println("    A consumer writes:")
	fmt.Println(`
        func init() {
            ferry.Register(ferry.TextCodec[netip.Addr](ferry.String))
        }

        type Config struct {
            Name   string             ` + "`ferry:\"name\"`" + `
            Limits map[netip.Addr]int ` + "`ferry:\"limits\"`" + `
        }`)
	keyOptIn = true
	defer func() { keyOptIn = false }()
	optOut := NewRegistry()
	_ = optOut.Register(TextCodec[netip.Addr](VString))
	type Config struct {
		Name   string             `ferry:"name"`
		Limits map[netip.Addr]int `ferry:"limits"`
	}
	withRegistry(optOut, func() {
		_, err := compile(reflect.TypeFor[Config]())
		fmt.Printf("\n    and gets, at schema compile:\n      %v\n", err)
	})
	fmt.Println("\n    The fix is one method call:")
	fmt.Println("        ferry.TextCodec[netip.Addr](ferry.String).AsMapKey()")
	optIn := NewRegistry()
	_ = optIn.Register(TextCodec[netip.Addr](VString).AsMapKey())
	withRegistry(optIn, func() {
		addrs, err := compile(reflect.TypeFor[Config]())
		fmt.Printf("    -> %v err=%v\n", addrs, err)
	})
	fmt.Println()
	fmt.Println("    THAT IS THE FALSE REFUSAL: netip.Addr's text IS injective, so the")
	fmt.Println("    user is being made to affirm something true. The cost is one line")
	fmt.Println("    and one confused reading of the docs, once per key type.")
	fmt.Println()
	fmt.Println("    What it buys, run against a codec that is NOT injective:")
	nonInj := NewRegistry()
	_ = nonInj.Register(StringCodec(
		func(h R17Host) string { return h.Name },
		func(s string) (R17Host, error) { return R17Host{Name: s}, nil }))
	keyOptIn = false
	withRegistry(nonInj, func() {
		type C struct {
			M map[R17Host]int `ferry:"m"`
		}
		m := map[R17Host]int{{"api", 80}: 1, {"api", 443}: 2}
		d, _ := dump(reflect.ValueOf(C{m}))
		fmt.Printf("      implied rule: Go map has %d keys -> ferry writes %d address(es)\n",
			len(m), len(d))
		for _, p := range sortedAddrs(d) {
			fmt.Printf("        %-6s %s\n", p, d[p].GoString())
		}
	})
	keyOptIn = true
	fmt.Println("      ^ one entry gone, no error, winner decided by map iteration order.")
	fmt.Println()
	fmt.Println("    And the third option, which is additive rather than an alternative:")
	fmt.Println("    ferrytest checks injectivity over the registrant's own value list,")
	fmt.Println("    which is what ADR-0005 says the obligation is discharged over.")
	fmt.Printf("      injective(netip.Addr, 3 values) -> %v\n",
		injective(netip.Addr.String,
			netip.MustParseAddr("10.0.0.1"),
			netip.MustParseAddr("10.0.0.2"),
			netip.MustParseAddr("2001:db8::1")))
	fmt.Printf("      injective(R17Host, 2 values)    -> %v\n",
		injective(func(h R17Host) string { return h.Name },
			R17Host{"api", 80}, R17Host{"api", 443}))
	fmt.Println("    ^ so the two mechanisms answer different questions: AsMapKey asks")
	fmt.Println("      `did you think about it`, and the harness asks `is it true of the")
	fmt.Println("      values you care about`. Neither subsumes the other, and shipping")
	fmt.Println("      only the harness leaves a registrant who writes no proof with the")
	fmt.Println("      silent dropped entry above.")
}

// --- R17e: what dynamic registration would look like -------------------------

func r17Dynamic() {
	fmt.Println("\n--- R17e: `contingent` means the API compiles and nobody can call it ---")
	fmt.Println()
	fmt.Println("    The method, if it shipped:")
	fmt.Println(`
        func (r *ferry.Registry) RegisterType(
            t reflect.Type, kind ferry.VKind,
            enc func(reflect.Value) (ferry.Value, error),
            dec func(ferry.Value, reflect.Value) error,
        ) error`)
	fmt.Println()
	fmt.Println("    It is only reachable if a caller can ASK ferry about a type they")
	fmt.Println("    cannot name, and that is a property of #16's entry point:")
	fmt.Println(`
        ferry.Load[T](ctx, src)          T is written in source. A
                                          reflect.StructOf type can never be T,
                                          so RegisterType is dead code.

        ferry.Load(ctx, v any, src)       xload's own shape. v may hold a value
                                          of a runtime-built type, so
                                          RegisterType becomes reachable.`)
	fmt.Println()
	fmt.Println("    xload's entry point is the second one: Load(ctx, v any, opts...)")
	fmt.Println("    at load.go:37, with two runtime kind checks behind it. The research")
	fmt.Println("    recommends the first, and ADR-0001 calls Load/Dump a working")
	fmt.Println("    assumption rather than a decision. So this is live, not academic.")
	fmt.Println()
	fmt.Println("    Who would actually call it, concretely: a caller whose struct type")
	fmt.Println("    is built at runtime by reflect.StructOf - a plugin host, or a")
	fmt.Println("    schema-driven mapper. Run:")
	dyn := reflect.StructOf([]reflect.StructField{
		{Name: "X", Type: reflect.TypeFor[int](), Tag: `ferry:"x"`},
	})
	fmt.Printf("      reflect.StructOf(...) -> %v\n", dyn)
	fmt.Println("      there is no way to write `ferry.Load[?]` for that type, because")
	fmt.Println("      the type argument would have to be a name and it has none.")
	fmt.Println()
	fmt.Println("    So the recommendation is REFUSE FOR NOW, on reversibility, which is")
	fmt.Println("    ADR-0006's own rule: `a refusal is liftable later with no break and")
	fmt.Println("    a permission is not retractable`. Adding RegisterType after #16")
	fmt.Println("    ships a non-generic entry point is additive. Shipping it now and")
	fmt.Println("    finding nobody can call it is a method ferry maintains forever.")
}
