package yaml_test

// Throwaway probe for #116, the half that needs a plane with a serialization
// format. See proto/README-116.md.
//
// Run: cd driver/yaml && go test -v -run TestP7 .

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"unsafe"

	"github.com/onhotpath/ferry"
	yaml "github.com/onhotpath/ferry/driver/yaml"
)

type p7Conf struct {
	P uintptr `ferry:"p"`
	Q uintptr `ferry:"q"`
}

func p7Text(u uintptr) (string, error) { return strconv.FormatUint(uint64(u), 10), nil }

func p7Parse(s string) (uintptr, error) {
	n, err := strconv.ParseUint(s, 10, 64)

	return uintptr(n), err
}

// P7: the memory plane stores the boundary Value itself, so it cannot show
// whether a uintptr survives being written down. A real file can.
func TestP7UintptrThroughARealFile(t *testing.T) {
	reg, err := ferry.NewRegistry(ferry.NumberValue(p7Text, p7Parse))
	if err != nil {
		t.Fatalf("register: %+v", err)
	}

	obj := new([4]int)
	in := p7Conf{P: uintptr(unsafe.Pointer(obj)), Q: ^uintptr(0)}

	path := filepath.Join(t.TempDir(), "p.yaml")
	if err := ferry.Dump(t.Context(), in, yaml.NewSink(path), ferry.WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	t.Logf("the file on disk:\n%s", raw)

	back, err := ferry.Load[p7Conf](t.Context(), yaml.NewSource(path), ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	t.Logf("a real heap address: in=%d out=%d identical=%v", in.P, back.P, in.P == back.P)
	t.Logf("the widest uintptr:  in=%d out=%d identical=%v", in.Q, back.Q, in.Q == back.Q)
}
