package concwalk

import (
	"errors"
	"strings"
	"testing"
)

// One panicking codec becomes one addressed error; every other leaf still
// loads; the report carries the panic next to an ordinary refusal.
func TestCodecPanicBecomesAddressedError(t *testing.T) {
	plane := map[string]string{"a": "1", "b": "2", "c": "3"}
	codecs := map[string]func(string) (string, error){
		"a": func(s string) (string, error) { return "A" + s, nil },
		"b": func(string) (string, error) { panic("nil map write in user codec") },
		"c": func(string) (string, error) { return "", errors.New("ordinary refusal") },
	}
	out, errs := loadLeaves(plane, codecs)
	if out["a"] != "A1" {
		t.Fatalf("healthy leaf lost: %v", out)
	}
	if len(errs) != 2 {
		t.Fatalf("want panic error + ordinary error, got %v", errs)
	}
	var cp *errCodecPanic
	if !errors.As(errors.Join(errs...), &cp) {
		t.Fatal("panic not surfaced as a typed, addressed error")
	}
	if cp.addr != "b" || !strings.Contains(cp.Error(), "nil map write") {
		t.Fatalf("panic error lost its address or value: %v", cp)
	}
}

// The fence is the user-code call, not the walk: a panic OUTSIDE the guarded
// call - ferry's own logic - still crashes, so ferry bugs stay loud.
func TestFerryOwnPanicStaysAPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a panic outside the fence must keep unwinding")
		}
	}()
	_ = guarded("x", func() error { return nil }) // fence opens and closes cleanly
	panic("ferry's own bug")                      // outside any fence
}

// The full protocol on a panic path: fence converts, aggregation carries,
// deferred release closes without Commit.
func TestPanicPathEndToEnd(t *testing.T) {
	p := &plane{}
	err := entryDeferred(p, func() error {
		return guarded("bad/leaf", func() error { panic("boom") })
	})
	if err == nil || !strings.Contains(err.Error(), "bad/leaf") {
		t.Fatalf("panic not in the chain: %v", err)
	}
	if p.committed || !p.closed {
		t.Fatalf("protocol broken on recovered panic: %+v", p)
	}
}
