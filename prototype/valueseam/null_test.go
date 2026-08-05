package valueseam_test

import (
	"testing"

	vs "valueseam"
)

// Level is a type whose zero means "unset" and dumps as Null.
type level string

func levelReg() vs.Reg {
	return vs.WithNull(
		vs.StringValue(
			func(l level) (string, error) { return string(l), nil },
			func(s string) (level, error) { return level(s), nil },
		),
		func() (level, error) { return "", nil }, // Null loads as the zero
		func(l level) bool { return l == "" },    // the zero dumps as Null
	)
}

func TestWithNullLoadsAndDumps(t *testing.T) {
	r, err := vs.NewRegistryB(levelReg())
	if err != nil {
		t.Fatal(err)
	}
	// A Null observation becomes the sentinel.
	got, err := vs.DecodeVia[level](r, vs.Null())
	if err != nil || got != "" {
		t.Fatalf("null load: %q, %v", got, err)
	}
	// The sentinel dumps as Null.
	v, err := vs.EncodeVia(r, level(""))
	if err != nil || v.Kind() != vs.KindNull {
		t.Fatalf("null dump: %v, %v", v.Kind(), err)
	}
	// Non-null values keep the ordinary road.
	v, err = vs.EncodeVia(r, level("warn"))
	if err != nil || v.Kind() != vs.KindString {
		t.Fatalf("string dump: %v, %v", v.Kind(), err)
	}
	got, err = vs.DecodeVia[level](r, vs.String("warn"))
	if err != nil || got != "warn" {
		t.Fatalf("string load: %q, %v", got, err)
	}
}

// The policy closure law: isNull(load()) must hold, or a loaded
// sentinel dumps as a plain value and the round trip lies.
func TestNullPolicyClosure(t *testing.T) {
	r, _ := vs.NewRegistryB(levelReg())
	loaded, err := vs.DecodeVia[level](r, vs.Null())
	if err != nil {
		t.Fatal(err)
	}
	v, err := vs.EncodeVia(r, loaded)
	if err != nil || v.Kind() != vs.KindNull {
		t.Fatalf("isNull(load()) must hold: dumped %v", v.Kind())
	}
}
