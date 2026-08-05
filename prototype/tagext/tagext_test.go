package tagext

import (
	"strings"
	"testing"
)

func mustDecl(t *testing.T) Decl {
	t.Helper()
	d, err := Declare(
		Extension{Namespace: "mylib", Words: []Word{{Name: "retry", TakesVal: true}, {Name: "secret"}}},
		Extension{Namespace: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Claim 3, inert: ferry's parsed fields are byte-identical with and
// without the extension; the extension's values land in the table only.
func TestExtensionIsInert(t *testing.T) {
	d := mustDecl(t)
	table := ExtTable{}
	withExt, err := ParseTag("/host", "host,required,mylib.retry=3,docs.desc=the host", d, table)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ParseTag("/host", "host,required", Decl{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if withExt != plain {
		t.Fatalf("extension changed ferry's own reading: %+v vs %+v", withExt, plain)
	}
	if table["/host"]["mylib.retry"] != "3" || table["/host"]["docs.desc"] != "the host" {
		t.Fatalf("table wrong: %v", table)
	}
}

// The undeclared word still refuses - and the near-miss table now covers
// extension words, so a misspelled one gets a real remedy (#34 item 6).
func TestDiagnosticsCoverExtensionWords(t *testing.T) {
	d := mustDecl(t)
	_, err := ParseTag("/host", "host,mylib.rerty=3", d, ExtTable{})
	if err == nil || !strings.Contains(err.Error(), `did you mean "mylib.retry"`) {
		t.Fatalf("near-miss missed the extension word: %v", err)
	}
	_, err = ParseTag("/host", "host,requird", d, ExtTable{})
	if err == nil || !strings.Contains(err.Error(), `did you mean "required"`) {
		t.Fatalf("ferry's own near-miss regressed: %v", err)
	}
	_, err = ParseTag("/host", "host,mylib.retry=3", Decl{}, ExtTable{})
	if err == nil {
		t.Fatal("an undeclared extension word must refuse - TestTagKeyKeepsTheVocabularyShut's rule survives")
	}
}

// Claim 4: collisions refuse at Declare, once, before any tag parses.
func TestCollisionsRefuseAtDeclaration(t *testing.T) {
	if _, err := Declare(
		Extension{Namespace: "mylib", Words: []Word{{Name: "retry"}}},
		Extension{Namespace: "mylib", Words: []Word{{Name: "other"}}},
	); err == nil {
		t.Fatal("two claims on one namespace must refuse")
	}
	if err := DeclareBare("secret"); err == nil {
		t.Fatal("a bare word is ferry's - reserved even if unused today")
	}
	if _, err := Declare(Extension{Namespace: "my.lib", Words: []Word{{Name: "x"}}}); err == nil {
		t.Fatal("a namespace containing punctuation must refuse")
	}
}

// Claims 1: the Decl is canonical (declaration order does not mint a
// second cache entry) and comparable (build-asserted; == here).
func TestDeclIsCanonicalAndComparable(t *testing.T) {
	a, _ := Declare(
		Extension{Namespace: "mylib", Words: []Word{{Name: "retry", TakesVal: true}, {Name: "secret"}}},
		Extension{Namespace: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
	)
	b, _ := Declare(
		Extension{Namespace: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
		Extension{Namespace: "mylib", Words: []Word{{Name: "secret"}, {Name: "retry", TakesVal: true}}},
	)
	if a != b {
		t.Fatalf("same declaration, two cache keys: %q vs %q", a.canon, b.canon)
	}
	cache := map[Decl]string{a: "one"}
	if cache[b] != "one" {
		t.Fatal("cache lookup through the second spelling missed")
	}
}

// A value-taking word used bare, and a bare word given a value, both
// refuse - the typed declaration is checkable, a callback would not be.
func TestValueShapeIsChecked(t *testing.T) {
	d := mustDecl(t)
	if _, err := ParseTag("/h", "h,mylib.retry", d, ExtTable{}); err == nil {
		t.Fatal("declared-with-value used bare must refuse")
	}
	if _, err := ParseTag("/h", "h,mylib.secret=x", d, ExtTable{}); err == nil {
		t.Fatal("declared-bare given a value must refuse")
	}
}
