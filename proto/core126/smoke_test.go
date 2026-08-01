package addr

import "testing"

func TestCoreEligible(t *testing.T) {
	p := path("db", "auth").Name("a/b")
	if got := p.String(); got != "/db/auth/a~1b" {
		t.Fatalf("canon = %q", got)
	}
	if p.Segments()[2].Text != "a/b" {
		t.Fatal("round trip")
	}
	m := map[Path]int{p: 1}
	if m[path("db", "auth").Name("a/b")] != 1 {
		t.Fatal("not comparable as a map key")
	}
}
