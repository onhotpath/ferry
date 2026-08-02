package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

type BCred struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

type BConf struct {
	Auth *BCred `ferry:"auth,required"`
}

func b1() {
	dhdr("B1  required admissibility, re-cut on the static/dynamic line")
	for _, c := range []struct {
		decl string
		t    reflect.Type
	}{
		{"Token   string          `required`", reflect.TypeFor[struct {
			V string `ferry:"v,required"`
		}]()},
		{"Port    *int            `required`", reflect.TypeFor[struct {
			V *int `ferry:"v,required"`
		}]()},
		{"Key     []byte          `required`", reflect.TypeFor[struct {
			V []byte `ferry:"v,required"`
		}]()},
		{"Auth    Cred            `required`", reflect.TypeFor[struct {
			V BCred `ferry:"v,required"`
		}]()},
		{"Auth    *Cred           `required`", reflect.TypeFor[struct {
			V *BCred `ferry:"v,required"`
		}]()},
		{"Hosts   [2]string       `required`", reflect.TypeFor[struct {
			V [2]string `ferry:"v,required"`
		}]()},
		{"Origins []string        `required`", reflect.TypeFor[struct {
			V []string `ferry:"v,required"`
		}]()},
		{"Limits  map[string]int  `required`", reflect.TypeFor[struct {
			V map[string]int `ferry:"v,required"`
		}]()},
		{"Ptr     *[]string       `required`", reflect.TypeFor[struct {
			V *[]string `ferry:"v,required"`
		}]()},
	} {
		_, err := compileD(c.t)
		verdict := "COMPILES"
		if err != nil {
			verdict = "REFUSED"
		}
		fmt.Printf("  %-38s %s\n", c.decl, verdict)
	}
	fmt.Println("\n  the refusal message:")
	_, e := compileD(reflect.TypeFor[struct {
		Origins []string `ferry:"origins,required"`
	}]())
	fmt.Printf("  %v\n", e)
}

func b2() {
	dhdr("B2  `Auth *Cred required` behaviour, on YAML and on a flat plane")
	ctx := context.Background()
	s := mustSchema(reflect.TypeFor[BConf]())
	dir, _ := os.MkdirTemp("", "ferryB")
	defer os.RemoveAll(dir)

	fmt.Println("  through the real YAML driver:")
	for _, d := range []struct{ label, body string }{
		{"key absent          ", "other: 1\n"},
		{"auth: null          ", "auth: null\n"},
		{"auth: {}            ", "auth: {}\n"},
		{"auth: {user: u}     ", "auth: {user: u}\n"},
		{"auth: {pass: p}     ", "auth: {pass: p}\n"},
	} {
		f := filepath.Join(dir, "c.yaml")
		_ = os.WriteFile(f, []byte(d.body), 0o644)
		vals, err := planeFromYAML(ctx, f, []Path{addr("auth")})
		if err != nil {
			fmt.Printf("  %s driver error: %v\n", d.label, err)
			continue
		}
		var v BConf
		_, e := loadD(vals, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		verdict := "SATISFIED"
		if e != nil {
			verdict = "refused"
		}
		auth := "nil"
		if v.Auth != nil {
			auth = fmt.Sprintf("&%+v", *v.Auth)
		}
		fmt.Printf("  %s reports %-8s -> %-9s Auth=%s\n", d.label, vals[addr("auth")].GoString(), verdict, auth)
	}

	fmt.Println("\n  through a flat plane (env, query, kv), which has no null:")
	for _, c := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"nothing set        ", map[Path]Value{}},
		{"AUTH_USER=u        ", map[Path]Value{addr("auth", "user"): String("u")}},
	} {
		flat, _ := flatten(c.vals)
		var v BConf
		_, e := loadD(flat, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		verdict := "SATISFIED"
		if e != nil {
			verdict = "refused"
		}
		auth := "nil"
		if v.Auth != nil {
			auth = fmt.Sprintf("&%+v", *v.Auth)
		}
		fmt.Printf("  %s -> %-9s Auth=%s\n", c.label, verdict, auth)
	}
	fmt.Println("\n  Same meaning on both: the plane must supply at least one field under")
	fmt.Println("  /auth. The one row where they differ is `auth: null`, which a flat")
	fmt.Println("  plane cannot express at all, so the difference cannot arise there.")
}

func b3() {
	dhdr("B3  required on a non-pointer struct, which was silently unenforced")
	type C struct {
		Auth BCred `ferry:"auth,required"`
	}
	s := mustSchema(reflect.TypeFor[C]())
	for _, c := range []struct {
		label string
		vals  map[Path]Value
	}{
		{"nothing under /auth", map[Path]Value{}},
		{"/auth/pass present ", map[Path]Value{addr("auth", "pass"): String("p")}},
	} {
		var v C
		_, e := loadD(c.vals, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("  %s -> Auth=%+v err=%v\n", c.label, v.Auth, errOrBlank(e))
	}
}
