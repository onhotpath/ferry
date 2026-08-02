package main

// T6: where a comment's words come from.
//
// #14 asks this as a design question - "Where would comments come from, given
// Go struct tags are a poor place for prose?" - and there are exactly three
// candidate sources. All three are run here rather than weighed.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

func runT6() {
	fmt.Println("(a) the struct tag, which ADR-0008 closed")
	fmt.Println("    ADR-0008's vocabulary is name, `-`, required, omitzero, default=, and")
	fmt.Println("    ADR-0001 freezes it on publication, so `desc=` is a breaking change to")
	fmt.Println("    add later and an amendment to an Accepted ADR to add now. Measured, what")
	fmt.Println("    ferry does with one today:")
	type withDesc struct {
		Name string `ferry:"name,desc=the service name"`
	}
	_ = withDesc{}
	err := Compile[TDescProbe]()
	fmt.Printf("      Compile[...] with ferry:\"name,desc=...\"  ->  %v\n", err)
	fmt.Println("    And ADR-0008 measured the shape of the problem independently: a comma")
	fmt.Println("    appears in 22 of 565 real free-text tag values, so prose in a tag needs")
	fmt.Println("    the quoted form on roughly one value in twenty-five, forever.")

	fmt.Println("\n(b) the Go doc comment, which is where a Go programmer already writes prose")
	docs, err := tDocComments()
	if err != nil {
		fmt.Println("    parse failed:", err)
	} else {
		for _, f := range []string{"Name", "Listen", "TLS", "Limits", "Debug"} {
			d, ok := docs[f]
			if !ok {
				d = "(none)"
			}
			fmt.Printf("      %-8s %s\n", f, d)
		}
		fmt.Println("    That is real text, extracted from this prototype's own source, and it")
		fmt.Println("    is EXACTLY what a template wants. Two facts about how it was obtained:")
		fmt.Printf("      reflect.StructField has a Doc field: %v\n", tHasDocField())
		fmt.Println("      it was read with go/parser, from the .go file on disk.")
		fmt.Println("    So this route is not available at run time at all. A doc comment is")
		fmt.Println("    not in the binary, and asking for it means either shipping the source")
		fmt.Println("    or running at build time.")
	}

	fmt.Println("\n(c) a side table the caller supplies")
	fmt.Println("      map[ferry.Path]string, written by the same person who wrote the tags.")
	fmt.Println("    Costs nothing to ferry and is the one route that works today. Its defect")
	fmt.Println("    is the one ADR-0006 already measured against a Static defaults source:")
	fmt.Println("    it spells the address set a SECOND time, and nothing checks the two")
	fmt.Println("    agree, so a rename silently drops the prose. Reproduced:")
	prose := tProseSideTable()
	renamed := map[Path]string{}
	for k, v := range prose {
		renamed[k] = v
	}
	fmt.Printf("      before a rename : /db/host has prose = %v\n", prose[path("db", "host")] != "")
	fmt.Printf("      after  `ferry:\"host\"` becomes `ferry:\"hostname\"`:\n")
	fmt.Printf("        /db/hostname has prose = %v, and no error from anything\n",
		renamed[path("db", "hostname")] != "")

	fmt.Println("\n(d) the honest reading")
	fmt.Println("    (b) is the source people want and it is a BUILD-TIME source.")
	fmt.Println("    ADR-0002 reserves `cmd/` and says the prefix keeps the root namespace")
	fmt.Println("    free \"because ... #14 may want a command\". This is that sentence coming")
	fmt.Println("    true: the prose question is what makes template generation a generator")
	fmt.Println("    rather than a function, and a generator reads the source, so it also gets")
	fmt.Println("    the tags and the types without a second RUNTIME authority.")
}

// TDescProbe is at package level for the same go1.27rc2 linker reason as
// TWithEmbed.
type TDescProbe struct {
	Name string `ferry:"name,desc=the service name"`
}

// tDocComments reads this prototype's own t_fixture.go and returns the doc
// comment on each field of TConf. It is the (b) route, run rather than
// described.
func tDocComments() (map[string]string, error) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("no caller info")
	}
	src := filepath.Join(filepath.Dir(self), "t_fixture.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "TConf" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, fld := range st.Fields.List {
			if fld.Doc == nil || len(fld.Names) == 0 {
				continue
			}
			out[fld.Names[0].Name] = strings.TrimSpace(fld.Doc.Text())
		}
		return false
	})
	return out, nil
}

// tHasDocField asks reflect whether a doc comment is reachable at run time.
func tHasDocField() bool {
	t := reflect.TypeFor[reflect.StructField]()
	_, ok := t.FieldByName("Doc")
	return ok
}
