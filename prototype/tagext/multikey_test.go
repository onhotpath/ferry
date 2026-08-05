package tagext

import (
	"strings"
	"testing"
)

func mustKeys(t *testing.T) Decl {
	t.Helper()
	d, err := DeclareKeys("ferry",
		KeyExtension{TagKey: "mylib", Words: []Word{{Name: "retry", TakesVal: true}, {Name: "secret"}}},
		KeyExtension{TagKey: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The owner's shape end to end: ferry's tag is parsed by ferry's grammar
// alone (no extension words inside it, ever), declared foreign keys mint
// the same address-keyed table.
func TestMultiKeyShape(t *testing.T) {
	d := mustKeys(t)
	table := ExtTable{}
	raw := `ferry:"host,required" mylib:"retry=3,secret" docs:"desc=the host" json:"host,omitempty"`
	tg, err := ParseField("/host", raw, "ferry", d, table)
	if err != nil {
		t.Fatal(err)
	}
	if tg.name != "host" || !tg.required {
		t.Fatalf("ferry's own reading broken: %+v", tg)
	}
	if table["/host"]["mylib:retry"] != "3" || table["/host"]["docs:desc"] != "the host" {
		t.Fatalf("table wrong: %v", table)
	}
	if _, claimed := table["/host"]["json:host"]; claimed {
		t.Fatal("an undeclared foreign key is another library's - never claimed")
	}
}

// ferry's namespace never opened: an extension word INSIDE ferry's tag is
// still the same refusal as today.
func TestFerryTagStaysClosed(t *testing.T) {
	d := mustKeys(t)
	raw := `ferry:"host,mylib.retry=3"`
	if _, err := ParseField("/host", raw, "ferry", d, ExtTable{}); err == nil {
		t.Fatal("a foreign word inside ferry's own tag must refuse - the namespace stays shut")
	}
}

// Declared-key diagnostics are first-class; a typo inside mylib's tag
// gets mylib's near-miss, not silence and not ferry's vocabulary.
func TestForeignNearMiss(t *testing.T) {
	d := mustKeys(t)
	raw := `ferry:"host" mylib:"rerty=3"`
	_, err := ParseField("/host", raw, "ferry", d, ExtTable{})
	if err == nil || !strings.Contains(err.Error(), `did you mean "retry"`) {
		t.Fatalf("foreign near-miss missed: %v", err)
	}
}

// Collisions refuse at declaration: claiming ferry's own key, or one key twice.
func TestKeyCollisionsRefuse(t *testing.T) {
	if _, err := DeclareKeys("ferry", KeyExtension{TagKey: "ferry", Words: []Word{{Name: "x"}}}); err == nil {
		t.Fatal("claiming ferry's key must refuse")
	}
	if _, err := DeclareKeys("ferry",
		KeyExtension{TagKey: "mylib", Words: []Word{{Name: "a"}}},
		KeyExtension{TagKey: "mylib", Words: []Word{{Name: "b"}}},
	); err == nil {
		t.Fatal("one key declared twice must refuse")
	}
}

// The Decl stays canonical and comparable - same cache-key property.
func TestMultiKeyDeclCanonical(t *testing.T) {
	a, _ := DeclareKeys("ferry",
		KeyExtension{TagKey: "mylib", Words: []Word{{Name: "retry", TakesVal: true}, {Name: "secret"}}},
		KeyExtension{TagKey: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
	)
	b, _ := DeclareKeys("ferry",
		KeyExtension{TagKey: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
		KeyExtension{TagKey: "mylib", Words: []Word{{Name: "secret"}, {Name: "retry", TakesVal: true}}},
	)
	if a != b {
		t.Fatalf("same declaration, two cache keys: %q vs %q", a.canon, b.canon)
	}
}
