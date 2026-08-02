package main

// What option 2 means in practice: `required` is admissible only where the
// address it names is always realised.

import (
	"fmt"
	"reflect"
	"time"
)

type O2Cred struct {
	User string `ferry:"user"`
	Pass string `ferry:"pass"`
}

func o2a() {
	dhdr("O2a  what compiles and what does not, one field kind at a time")
	cases := []struct {
		decl string
		t    reflect.Type
	}{
		{"Token   string         `required`", reflect.TypeFor[struct {
			V string `ferry:"v,required"`
		}]()},
		{"Port    int            `required`", reflect.TypeFor[struct {
			V int `ferry:"v,required"`
		}]()},
		{"TO      time.Duration  `required`", reflect.TypeFor[struct {
			V time.Duration `ferry:"v,required"`
		}]()},
		{"Key     []byte         `required`", reflect.TypeFor[struct {
			V []byte `ferry:"v,required"`
		}]()},
		{"UUID    [16]byte       `required`", reflect.TypeFor[struct {
			V [16]byte `ferry:"v,required"`
		}]()},
		{"Port    *int           `required`", reflect.TypeFor[struct {
			V *int `ferry:"v,required"`
		}]()},
		{"Origins []string       `required`", reflect.TypeFor[struct {
			V []string `ferry:"v,required"`
		}]()},
		{"Limits  map[string]int `required`", reflect.TypeFor[struct {
			V map[string]int `ferry:"v,required"`
		}]()},
		{"Hosts   [2]string      `required`", reflect.TypeFor[struct {
			V [2]string `ferry:"v,required"`
		}]()},
		{"Auth    Cred           `required`", reflect.TypeFor[struct {
			V O2Cred `ferry:"v,required"`
		}]()},
		{"Auth    *Cred          `required`", reflect.TypeFor[struct {
			V *O2Cred `ferry:"v,required"`
		}]()},
	}
	for _, c := range cases {
		_, err := compileD(c.t)
		if err == nil {
			fmt.Printf("  %-34s COMPILES\n", c.decl)
			continue
		}
		fmt.Printf("  %-34s REFUSED\n", c.decl)
	}
	fmt.Println("\n  the two refusal messages in full:")
	_, e1 := compileD(reflect.TypeFor[struct {
		Origins []string `ferry:"origins,required"`
	}]())
	_, e2 := compileD(reflect.TypeFor[struct {
		Auth O2Cred `ferry:"auth,required"`
	}]())
	fmt.Printf("  %v\n\n  %v\n", e1, e2)
	fmt.Println("\n  Note []byte and [16]byte COMPILE. They look like collections and are")
	fmt.Println("  leaves in ADR-0005's set, so their address is always realised. The rule")
	fmt.Println("  is about addresses, not about whether a type holds several things.")
}

// ---------------------------------------------------------------------------
// O2b  required on a leaf is completely unchanged.
// ---------------------------------------------------------------------------

type O2Leaf struct {
	Token string `ferry:"token,required"`
	Port  *int   `ferry:"port,required"`
}

func o2b() {
	dhdr("O2b  required on a leaf and a pointer-to-leaf: unchanged")
	s := mustSchema(reflect.TypeFor[O2Leaf]())
	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"Absent", map[Path]Value{}},
		{`String("") (FOO=)`, map[Path]Value{addr("token"): String(""), addr("port"): String("0")}},
		{"Null at /port", map[Path]Value{addr("token"): String("t"), addr("port"): Null()}},
		{"values", map[Path]Value{addr("token"): String("t"), addr("port"): Number("1")}},
	} {
		var v O2Leaf
		_, err := loadD(c.plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		verdict := "SATISFIED"
		if err != nil {
			verdict = "refused"
		}
		fmt.Printf("  %-20s -> %-9s Token=%-4q Port=%-5s %v\n",
			c.label, verdict, v.Token, iptr(v.Port), errOrBlank(err))
	}
}

// ---------------------------------------------------------------------------
// O2c  the three things people wanted required on a composite for.
// ---------------------------------------------------------------------------

type O2Before struct {
	Auth    *O2Cred  `ferry:"auth,required"`
	Origins []string `ferry:"origins,required"`
}

// After: the section becomes a VALUE struct with a required leaf inside it.
type O2AfterCred struct {
	User string `ferry:"user,required"`
	Pass string `ferry:"pass"`
}

type O2After struct {
	Auth    O2AfterCred `ferry:"auth"`
	Origins []string    `ferry:"origins"`
}

func o2c() {
	dhdr("O2c  the migration, for the two things people actually wanted")

	fmt.Println("  (1) \"the auth section must be configured\"")
	_, e := compileD(reflect.TypeFor[O2Before]())
	fmt.Printf("      before: Auth *Cred `required`  ->  %s\n", firstLine(e))
	fmt.Println("      after:  Auth Cred, with Cred.User `required`")
	s := mustSchema(reflect.TypeFor[O2After]())
	for _, c := range []struct {
		label string
		plane map[Path]Value
	}{
		{"empty plane", map[Path]Value{}},
		{"/auth/pass only", map[Path]Value{addr("auth", "pass"): String("p")}},
		{"/auth/user present", map[Path]Value{addr("auth", "user"): String("u")}},
	} {
		var v O2After
		_, err := loadD(c.plane, s, reflect.ValueOf(&v).Elem(), loadOpts{})
		fmt.Printf("      %-20s -> Auth=%+v err=%v\n", c.label, v.Auth, errOrBlank(err))
	}
	fmt.Println("      Same guarantee, better error: it names the field that is missing")
	fmt.Println("      rather than the section, and it works identically on every plane.")

	fmt.Println("\n  (2) \"origins must be non-empty\"")
	fmt.Println("      No ferry answer, and that is the point: it is a constraint on the")
	fmt.Println("      VALUE and not on the plane, so ADR-0001 puts it in the type.")
	var v O2After
	_, _ = loadD(map[Path]Value{}, s, reflect.ValueOf(&v).Elem(), loadOpts{})
	fmt.Printf("      loaded Origins=%v (nil), and the caller decides what that means\n", v.Origins)

	fmt.Println("\n  (3) \"auth is optional but must be MENTIONED if present\"")
	fmt.Println("      Not expressible, before or after. Under the old rule `auth: {}`")
	fmt.Println("      was refused and `auth: null` was not, which is a distinction the")
	fmt.Println("      loaded struct cannot see either. Nothing is lost that worked.")
}

func firstLine(e error) string {
	if e == nil {
		return "compiles"
	}
	s := e.Error()
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// O2d  the behaviour diff, per field kind.
// ---------------------------------------------------------------------------

func o2d() {
	dhdr("O2d  what changes, per field kind")
	rows := []struct{ kind, before, after string }{
		{"string, int, Duration, ...", "presence test at its address", "unchanged"},
		{"[]byte, [N]byte", "presence test at its address", "unchanged"},
		{"*int and other *leaf", "presence test at its address", "unchanged"},
		{"[]T", "refused unless a child or a null", "schema compile error"},
		{"map[K]V", "refused unless a child or a null", "schema compile error"},
		{"*struct", "refused unless a child or a null", "schema compile error"},
		{"[N]T, non-byte", "fired, though an array always exists", "schema compile error"},
		{"struct, non-pointer", "SILENTLY UNENFORCED", "schema compile error"},
	}
	fmt.Printf("  %-27s %-34s %s\n", "field kind", "today", "under option 2")
	for _, r := range rows {
		fmt.Printf("  %-27s %-34s %s\n", r.kind, r.before, r.after)
	}
	fmt.Println("\n  Only the last five rows move, all from a runtime outcome to a compile")
	fmt.Println("  error, and one of them from silence to an error. No load that succeeds")
	fmt.Println("  today starts failing at runtime: what fails now fails earlier.")
}
