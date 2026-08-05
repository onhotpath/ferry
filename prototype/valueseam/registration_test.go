package valueseam_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	vs "valueseam"
)

type flag bool

// ── the emit-check class, unwritable ─────────────────────────────────

func TestEncodeKindCorrectByConstruction(t *testing.T) {
	reg := vs.BoolValue(
		func(f flag) (bool, error) { return bool(f), nil },
		func(b bool) (flag, error) { return flag(b), nil },
	)
	r, err := vs.NewRegistryB(reg)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vs.EncodeVia(r, flag(true))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind() != vs.KindBool {
		t.Fatalf("kind is %v — but note there is no code path that could have made it wrong", v.Kind())
	}
	// The old defect: enc returned a Value, and could return Number("1")
	// under a Bool declaration — the emit check caught it at run time.
	// Here enc returns a bool; the wrong-kind Value has no spelling.
}

func TestDecodeRoadIsTyped(t *testing.T) {
	reg := vs.NumberValue(
		func(n int64) (string, error) { return strconv.FormatInt(n, 10), nil },
		func(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) },
	)
	r, err := vs.NewRegistryB(reg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vs.DecodeVia[int64](r, vs.Number("42"))
	if err != nil || got != 42 {
		t.Fatalf("decode: %v, %v", got, err)
	}
	// A wrong-kind observation refuses inside the payload accessor:
	if _, err := vs.DecodeVia[int64](r, vs.String("42")); err == nil {
		t.Fatal("a String observation must refuse for a Number codec")
	}
}

// ── nil halves refuse at the composition site ────────────────────────

func TestNilHalfPanicsAtConstruction(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !errors.Is(r.(error), vs.ErrNilHalf) {
			t.Fatalf("nil half must panic with ErrNilHalf, got %v", r)
		}
	}()
	vs.BoolValue[flag](nil, nil)
}

// ── map-key eligibility is structural ────────────────────────────────

func TestKeyEligibilityIsStructural(t *testing.T) {
	k := vs.StringKey(
		func(s string) (string, error) { return s, nil },
		func(s string) (string, error) { return s, nil },
	).AsMapKey()
	r, err := vs.NewRegistryB(k)
	if err != nil {
		t.Fatal(err)
	}
	_ = r
	// vs.BytesValue(...).AsMapKey() does not compile: BytesValue
	// returns Reg, and AsMapKey exists only on KeyableReg — the
	// bytes-keyed map is unwritable, not refused. This comment is
	// where the non-compiling line would go, per the D2 pattern.
}

// ── duplicates refuse loudly at construction (G6: construction IS the freeze) ──

func TestDuplicateRegistrationRefuses(t *testing.T) {
	mk := func() vs.Reg {
		return vs.BoolValue(
			func(f flag) (bool, error) { return bool(f), nil },
			func(b bool) (flag, error) { return flag(b), nil },
		)
	}
	_, err := vs.NewRegistryB(mk(), mk())
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate must refuse: %v", err)
	}
}
