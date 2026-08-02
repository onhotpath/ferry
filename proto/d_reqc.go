package main

// required at a container address, examined properly. ADR-0005 measured that
// a plane cannot report present-and-empty at a container address, so this is
// about what `required` can and cannot mean on a composite.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

type RCred struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

type RConf struct {
	Origins []string       `ferry:"origins,required"`
	Limits  map[string]int `ferry:"limits,required"`
	Auth    *RCred         `ferry:"auth,required"`
}

// planeFromYAML builds the plane map the way a real Load does: Get at every
// address the walk will ask about, and Children under each container, rather
// than querying the "/x/*" shape address, which is not a plane address.
func planeFromYAML(ctx context.Context, path string, containers []Path) (map[Path]Value, error) {
	open, err := (FYAMLSource{Path: path}).Bind(NewAddressSet(containers))
	if err != nil {
		return nil, err
	}
	r, err := open(ctx)
	if err != nil {
		return nil, err
	}
	if rel, ok := r.(FReleaser); ok {
		defer rel.Close()
	}
	out := map[Path]Value{}
	var visit func(Path)
	visit = func(p Path) {
		v, err := r.Get(ctx, p)
		if err == nil {
			out[p] = v
		}
		en, ok := r.(FEnumerator)
		if !ok {
			return
		}
		kids, err := en.Children(ctx, p)
		if err != nil {
			return
		}
		for _, k := range kids {
			visit(k)
		}
	}
	for _, c := range containers {
		visit(c)
	}
	return out, nil
}

func reqc() {
	allowRequiredOnComposite = true
	defer func() { allowRequiredOnComposite = false }()

	dhdr("R1  required at a container address, through the real YAML driver")
	ctx := context.Background()
	s := mustSchema(reflect.TypeFor[RConf]())
	dir, _ := os.MkdirTemp("", "ferryR")
	defer os.RemoveAll(dir)
	containers := []Path{addr("origins"), addr("limits"), addr("auth")}

	docs := []struct{ label, body string }{
		{"key absent      ", "other: 1\n"},
		{"empty sequence  ", "origins: []\nlimits: {}\nauth: {}\n"},
		{"explicit null   ", "origins: null\nlimits: null\nauth: null\n"},
		{"one element     ", "origins: [a]\nlimits: {rps: 1}\nauth: {user: u}\n"},
	}
	fmt.Printf("  %-17s %-28s %s\n", "document", "what the plane reports at /origins", "required")
	for _, d := range docs {
		f := filepath.Join(dir, "c.yaml")
		_ = os.WriteFile(f, []byte(d.body), 0o644)
		vals, err := planeFromYAML(ctx, f, containers)
		if err != nil {
			fmt.Printf("  %-17s read error: %v\n", d.label, err)
			continue
		}
		kids := children(vals, addr("origins"))
		obs := fmt.Sprintf("%s, %d children", vals[addr("origins")].GoString(), len(kids))
		var v RConf
		_, e := loadD(vals, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		verdict := "SATISFIED"
		if e != nil {
			verdict = "refused: " + e.Error()
		}
		fmt.Printf("  %-17s %-28s %s\n", d.label, obs, verdict)
	}

	fmt.Println("\n  and what each document actually loads to, ignoring required:")
	var s2 = mustSchema(reflect.TypeFor[struct {
		Origins []string       `ferry:"origins"`
		Limits  map[string]int `ferry:"limits"`
		Auth    *RCred         `ferry:"auth"`
	}]())
	for _, d := range docs {
		f := filepath.Join(dir, "c.yaml")
		_ = os.WriteFile(f, []byte(d.body), 0o644)
		vals, _ := planeFromYAML(ctx, f, containers)
		var v struct {
			Origins []string       `ferry:"origins"`
			Limits  map[string]int `ferry:"limits"`
			Auth    *RCred         `ferry:"auth"`
		}
		_, _ = loadD(vals, s2, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("  %-17s Origins=%v(nil=%v) Limits=%v Auth=%v\n",
			d.label, v.Origins, v.Origins == nil, v.Limits, v.Auth != nil)
	}
}

// ---------------------------------------------------------------------------
// R2  the workaround, on a plane that has no null
// ---------------------------------------------------------------------------

func reqc2() {
	allowRequiredOnComposite = true
	defer func() { allowRequiredOnComposite = false }()

	dhdr("R2  the `null` workaround on a plane that cannot produce one")
	s := mustSchema(reflect.TypeFor[RConf]())
	fmt.Println("  ADR-0004's table: env, query params, TOML and opaque KV cannot")
	fmt.Println("  produce Null at all. So on those planes a required composite has")
	fmt.Println("  exactly one satisfying document.")
	for _, c := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"ORIGINS_0 unset", map[Path]Value{}},
		{"ORIGINS_0=a", map[Path]Value{
			addr("origins").Index(0): String("a"), addr("limits", "rps"): String("1"),
			addr("auth", "user"): String("u")}},
	} {
		flat, _ := flatten(c.vals)
		var v RConf
		_, e := loadD(flat, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		verdict := "SATISFIED"
		if e != nil {
			verdict = "refused"
		}
		fmt.Printf("  %-18s -> %s\n", c.label, verdict)
	}
	fmt.Println("  There is no document on a flat plane that satisfies a required")
	fmt.Println("  composite while leaving it empty. On YAML there is one, `null`.")
}

// ---------------------------------------------------------------------------
// R3  where `required` is meaningful at all, by address
// ---------------------------------------------------------------------------

func reqc3() {
	allowRequiredOnComposite = true
	defer func() { allowRequiredOnComposite = false }()

	dhdr("R3  is the address `required` names even in the address set?")
	type T struct {
		Leaf    string         `ferry:"leaf,required"`
		PtrLeaf *int           `ferry:"ptrleaf,required"`
		Slice   []string       `ferry:"slice,required"`
		Map     map[string]int `ferry:"map,required"`
		PtrSt   *RCred         `ferry:"ptrst,required"`
		Struct  RCred          `ferry:"struct,required"`
		Array   [2]string      `ferry:"array,required"`
	}
	s := mustSchema(reflect.TypeFor[T]())
	inSet := map[string]bool{}
	for _, p := range s.addrs {
		inSet[p.String()] = true
	}
	for _, n := range []string{"leaf", "ptrleaf", "slice", "map", "ptrst", "struct", "array"} {
		p := addr(n)
		fmt.Printf("  %-9s own address in the static set: %-5v   realised when: %s\n",
			"/"+n, inSet[p.String()], realisedWhen(n))
	}
	fmt.Println("  A leaf's address is always realised, so `required` there is a plain")
	fmt.Println("  presence test. A composite's OWN address is realised only when it is")
	fmt.Println("  nil or empty (ADR-0005: the two address shapes are never simultaneously")
	fmt.Println("  realised). A non-pointer struct and an array have no own address at all.")
}

func realisedWhen(n string) string {
	switch n {
	case "leaf", "ptrleaf":
		return "always"
	case "slice", "map", "ptrst":
		return "only when nil or empty; otherwise its children are"
	default:
		return "never: it contributes addresses and has none of its own"
	}
}

// ---------------------------------------------------------------------------
// R4  what happens TODAY at the two composites with no own address, and
//     whether "required on a leaf inside" is a usable replacement.
// ---------------------------------------------------------------------------

type R4Inner struct {
	User string `ferry:"user,required"`
}

type R4Conf struct {
	Struct R4Inner   `ferry:"struct,required"` // required on a non-pointer struct
	Array  [2]string `ferry:"array,required"`  // required on an array
}

type R4Alt struct {
	Value R4Inner  `ferry:"value"` // required on a LEAF inside a value struct
	Opt   *R4Inner `ferry:"opt"`   // required on a leaf inside an OPTIONAL section
}

func reqc4() {
	allowRequiredOnComposite = true
	defer func() { allowRequiredOnComposite = false }()

	dhdr("R4  the two composites with no own address, and the replacement")
	s := mustSchema(reflect.TypeFor[R4Conf]())
	var v R4Conf
	_, e := loadD(map[Path]Value{}, s, reflect.ValueOf(&v).Elem(), loadOpts{})
	fmt.Printf("  required on a non-pointer struct and an array, empty plane -> err=%v\n", e)
	fmt.Println("  The struct's required is accepted at compile and enforced by NOTHING,")
	fmt.Println("  because a non-pointer struct has no address to be absent at. The array's")
	fmt.Println("  fires, but an array always exists in Go, so it is asserting something")
	fmt.Println("  about the plane that the Go value contradicts.")

	fmt.Println("\n  the proposed replacement: required on a LEAF inside")
	s2 := mustSchema(reflect.TypeFor[R4Alt]())
	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"empty plane", map[Path]Value{}},
		{"/value/user present", map[Path]Value{addr("value", "user"): String("u")}},
		{"/value/user and /opt/user", map[Path]Value{
			addr("value", "user"): String("u"), addr("opt", "user"): String("o")}},
	} {
		var a R4Alt
		_, err := loadD(c.plane, s2, reflect.ValueOf(&a).Elem(), loadOpts{})
		fmt.Printf("  %-26s -> Opt=%v err=%v\n", c.label, a.Opt != nil, err)
	}
	fmt.Println("  A required leaf inside a VALUE struct works: the fields are always")
	fmt.Println("  walked, so it is a plain presence test at a leaf.")
	fmt.Println("  A required leaf inside an OPTIONAL section makes the section")
	fmt.Println("  mandatory, which is a contradiction the grammar should refuse.")
}
