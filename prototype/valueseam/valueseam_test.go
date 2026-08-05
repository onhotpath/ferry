package valueseam_test

// Black-box on purpose: everything asserted here uses only the
// exported surface, which is also why Decl{k: 3} cannot even be
// written in this file — the D2 sealing claim, enforced by the
// compiler on this very package.

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"

	vs "valueseam"
)

// ── V1: layout claims ────────────────────────────────────────────────

func TestValueIs24Bytes(t *testing.T) {
	if got := unsafe.Sizeof(vs.Value{}); got != 24 {
		t.Fatalf("Value is %d bytes, the L1 claim is 24", got)
	}
}

func TestValueIsComparableAndMapKeyable(t *testing.T) {
	m := map[vs.Value]int{
		vs.Bool(true):                1,
		vs.Number("31"):              2,
		vs.String("31"):              3, // same text, different kind: distinct keys
		vs.Bytes([]byte{0xFF, 0x00}): 4,
		vs.Null():                    5,
	}
	if len(m) != 5 {
		t.Fatalf("expected 5 distinct keys, got %d", len(m))
	}
	if vs.Number("31") == vs.String("31") {
		t.Fatal("kind must participate in equality")
	}
	if vs.Bool(true) != vs.Bool(true) {
		t.Fatal("equal payloads must compare equal")
	}
}

func TestZeroValueIsAbsent(t *testing.T) {
	var v vs.Value
	if v.Kind() != vs.KindAbsent {
		t.Fatalf("zero Value is %s, want absent", v.Kind())
	}
}

// ── V2: accessors answer from payloads or refuse ─────────────────────

func TestAsBoolNeverGuesses(t *testing.T) {
	if b, err := vs.Bool(true).AsBool(); err != nil || !b {
		t.Fatalf("Bool(true).AsBool() = %v, %v", b, err)
	}
	// The #223 defect is unwritable: no constructor pairs KindBool
	// with text, so the old smuggling path does not exist. The only
	// wrong-kind route left refuses loudly:
	if _, err := vs.Number("true").AsBool(); err == nil {
		t.Fatal("AsBool on a Number must refuse, not guess")
	}
	if _, err := vs.String("true").AsBool(); err == nil {
		t.Fatal("AsBool on a String must refuse, not guess")
	}
}

func TestBytesCopiesBothWays(t *testing.T) {
	src := []byte("secret")
	v := vs.Bytes(src)
	src[0] = 'X' // caller mutates after construction
	got, err := v.AsBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Fatalf("payload aliased the caller's slice: %q", got)
	}
	got[0] = 'Y' // caller mutates the returned slice
	again, _ := v.AsBytes()
	if string(again) != "secret" {
		t.Fatalf("returned slice aliased the payload: %q", again)
	}
}

// ── the six kinds, end to end over fake planes ───────────────────────

func TestSixKindsEndToEnd(t *testing.T) {
	// A flat, string-carrier plane (env-like) with declared spellings.
	boolSp, err := vs.BoolWords("on", "off", "true", "false")
	if err != nil {
		t.Fatal(err)
	}
	numSp := vs.YAMLishNumber()

	flat := map[string]string{
		"debug": "on",
		"rate":  "0x1F",
		"name":  "ferry",
		// "retries" deliberately unset: the Absent case
	}

	// Absent: a missing key is the zero Value, no constructor.
	if _, ok := flat["retries"]; ok {
		t.Fatal("test setup: retries must be unset")
	}
	var absent vs.Value
	if absent.Kind() != vs.KindAbsent {
		t.Fatal("missing key must observe as Absent")
	}

	// Bool through the spelling: "on" → payload true → back to "on".
	b, err := boolSp.Parse(flat["debug"])
	if err != nil {
		t.Fatal(err)
	}
	v := vs.Bool(b)
	got, err := v.AsBool()
	if err != nil || !got {
		t.Fatalf("bool road: %v, %v", got, err)
	}
	back, err := boolSp.Render(got)
	if err != nil || back != "on" {
		t.Fatalf("bool renders %q, want on", back)
	}

	// Number: plane spelling 0x1F canonicalises to 31; AsInt parses
	// canonical text only — #259 stays in the driver, never in core.
	canon, err := numSp.Parse(flat["rate"])
	if err != nil {
		t.Fatal(err)
	}
	n := vs.Number(canon)
	i, err := n.AsInt()
	if err != nil || i != 31 {
		t.Fatalf("number road: %d, %v", i, err)
	}

	// String: passthrough.
	s, err := vs.String(flat["name"]).AsString()
	if err != nil || s != "ferry" {
		t.Fatalf("string road: %q, %v", s, err)
	}

	// Null: a tree-like plane can hold one; flat planes cannot spell it.
	tree := map[string]any{"debug": nil}
	if tree["debug"] != nil {
		t.Fatal("test setup")
	}
	if vs.Null().Kind() != vs.KindNull {
		t.Fatal("null must observe as Null")
	}

	// Bytes over a raw carrier (kv-like), and over a text carrier with
	// stacked transforms (http-like).
	kv := map[string][]byte{"cert": []byte{0x00, 0xFF, 0x10}}
	raw, err := vs.Raw().Parse(kv["cert"])
	if err != nil {
		t.Fatal(err)
	}
	bv := vs.Bytes(raw)
	payload, err := bv.AsBytes()
	if err != nil || !bytes.Equal(payload, kv["cert"]) {
		t.Fatalf("bytes road: %v, %v", payload, err)
	}
}

// ── K3: closure laws, including composed spellings ───────────────────

func TestClosureLawBool(t *testing.T) {
	sp, err := vs.BoolWords("on", "off", "true", "false", "1", "0")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []bool{true, false} {
		text, err := sp.Render(v)
		if err != nil {
			t.Fatal(err)
		}
		// Law 3: deterministic.
		text2, _ := sp.Render(v)
		if text != text2 {
			t.Fatalf("render is not deterministic: %q vs %q", text, text2)
		}
		// Law 1: parse(render(v)) == v.
		got, err := sp.Parse(text)
		if err != nil || got != v {
			t.Fatalf("closure broken: render(%v)=%q, parse=%v, %v", v, text, got, err)
		}
	}
	// Law 2: accept is wider than the write form.
	if got, err := sp.Parse("true"); err != nil || !got {
		t.Fatalf("accept set must include %q", "true")
	}
	if text, _ := sp.Render(true); text != "on" {
		t.Fatalf("write form must be canonical: got %q", text)
	}
}

func TestClosureLawComposedBytes(t *testing.T) {
	sp := vs.With(vs.Base64(), vs.Gzip(), vs.MaxSize(1<<10))
	payload := []byte(strings.Repeat("ferry", 100))
	carrier, err := sp.Render(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sp.Parse(carrier)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("composed closure broken: %v", err)
	}
}

func TestApplyRefusesPreWrite(t *testing.T) {
	sp := vs.With(vs.Base64(), vs.Gzip(), vs.MaxSize(4))
	if _, err := sp.Render([]byte("longer than four")); err == nil {
		t.Fatal("MaxSize must refuse on the way out")
	}
}

func TestInvertRefusesCorruptData(t *testing.T) {
	sp := vs.With(vs.Base64(), vs.Gzip())
	// Valid base64 of bytes that are not a gzip stream.
	if _, err := sp.Parse("Z2FyYmFnZQ=="); err == nil {
		t.Fatal("gunzip of garbage must refuse")
	}
}

func TestTransformOrder(t *testing.T) {
	// With(spelling, t1, t2): the LAST transform touches the raw
	// payload first on render — cap, then gzip, then base64.
	capped := vs.With(vs.Base64(), vs.Gzip(), vs.MaxSize(4))
	if _, err := capped.Render([]byte("12345")); err == nil {
		t.Fatal("cap must run before gzip: 5 raw bytes exceed 4")
	}
	// If the cap ran after gzip it would measure the compressed size,
	// which for this input is far larger than 4 anyway — so prove the
	// converse too: 4 raw bytes pass even though gzip output is bigger.
	if _, err := capped.Render([]byte("1234")); err != nil {
		t.Fatalf("cap must measure the raw payload, not the gzip output: %v", err)
	}
}

// ── polarity: the DISABLE_* case ─────────────────────────────────────

func TestNegatedPolarity(t *testing.T) {
	words, err := vs.BoolWords("on", "off")
	if err != nil {
		t.Fatal(err)
	}
	sp := vs.With(words, vs.Negated())
	// DISABLE_TLS=on → field false.
	got, err := sp.Parse("on")
	if err != nil || got != false {
		t.Fatalf("negated parse: %v, %v", got, err)
	}
	// field false → "on" on the way back: closure holds through polarity.
	text, err := sp.Render(false)
	if err != nil || text != "on" {
		t.Fatalf("negated render: %q, %v", text, err)
	}
}

// ── V3: the sealed declaration ───────────────────────────────────────

func TestDeclSealed(t *testing.T) {
	// The four package values register clean.
	for _, d := range []vs.Decl{vs.DeclBool, vs.DeclNumber, vs.DeclString, vs.DeclBytes} {
		if err := vs.Register(d); err != nil {
			t.Fatalf("legitimate declaration refused: %v", err)
		}
	}
	// The one residual forgeable value — the zero — refuses loudly.
	var zero vs.Decl
	if err := vs.Register(zero); err == nil {
		t.Fatal("the zero Decl must refuse at Register")
	}
	// Decl{k: 3} and Decl(3) do not compile from this package, which
	// is the sealing claim; this comment is where they would go.
}

// ── the lean boundary: the driver memo, in miniature ─────────────────

func TestSpellingMemoRestoresOriginal(t *testing.T) {
	// Core carries the canonical payload only. A driver that wants to
	// preserve the operator's spelling memoizes it, address-keyed, and
	// restores it when the value is unchanged — the routing-table
	// backpropagation, smallest possible version.
	numSp := vs.YAMLishNumber()
	memo := map[string]string{} // address → original spelling

	load := func(addr, text string) (vs.Value, error) {
		canon, err := numSp.Parse(text)
		if err != nil {
			return vs.Value{}, err
		}
		memo[addr] = text
		return vs.Number(canon), nil
	}
	dump := func(addr string, v vs.Value) (string, error) {
		canon, err := v.AsNumber()
		if err != nil {
			return "", err
		}
		if orig, ok := memo[addr]; ok {
			if reparsed, err := numSp.Parse(orig); err == nil && reparsed == canon {
				return orig, nil // value unchanged: restore the spelling
			}
		}
		return numSp.Render(canon)
	}

	v, err := load("/rate", "0x1F")
	if err != nil {
		t.Fatal(err)
	}
	// Unchanged value dumps with the operator's original spelling.
	out, err := dump("/rate", v)
	if err != nil || out != "0x1F" {
		t.Fatalf("memo restore: %q, %v", out, err)
	}
	// A changed value dumps canonically.
	out, err = dump("/rate", vs.Number("42"))
	if err != nil || out != "42" {
		t.Fatalf("changed value: %q, %v", out, err)
	}
}
