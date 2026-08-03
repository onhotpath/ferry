package main

// #35: the four files that get signed off.
//
// ADR-0009 put "what a consumer writes" first because every decision below is
// a decision about that file. `ferrytest` has four consumers, not one, and
// three of them are outside this repository. Each is written here as a real
// compiling function, so a claim that a surface "reads well" is a claim about
// code that exists.

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

// ===========================================================================
// CONSUMER 1: core's own test. It is the only one that runs CoreTypes(), and
// it is the reason CoreTypes() is exported at all - a registrant needs it to
// append to, and a driver needs it to run.
// ===========================================================================

func zCoreTest(t ZT) {
	dir, _ := os.MkdirTemp("", "z35core")
	defer os.RemoveAll(dir)

	for _, p := range []ZPlane{zMemoryPlane(), zYAMLPlane(dir), zFlatPlane()} {
		ZRoundTrip(t, p, ZCoreTypes())
	}
	for _, s := range ZComplete(zCoreRegistry(), ZCoreTypes()...) {
		t.Errorf("core type set: %s", s)
	}
}

// ===========================================================================
// CONSUMER 2: a driver author's test. This is the whole file, and `driver/*`
// being a CI glob means it has to be one call.
// ===========================================================================

func zDriverTest(t ZT, dir string) {
	ZDriver(t, ZPlane{
		Name:  "yaml",
		Kinds: []VKind{VAbsent, VNull, VBool, VNumber, VString, VBytes},
		Open: func() (FSource, FSink) {
			p := filepath.Join(dir, fmt.Sprintf("d%d.yaml", zSeq()))
			return FYAMLSource{Path: p}, FYAMLSink{Path: p}
		},
		Golden: []ZArtefact{
			{Value: struct {
				B []byte `ferry:"b"`
			}{[]byte("hi")}, Want: "b: !!binary aGk=\n"},
			{Value: struct {
				D time.Duration `ferry:"d"`
			}{90 * time.Minute}, Want: "d: \"1h30m0s\"\n"},
		},
	})
}

// ===========================================================================
// CONSUMER 3: a registrant discharging ADR-0001's transferred guarantee.
// ADR-0009 measured that this must be four lines or nobody writes it.
// ===========================================================================

func zRegistrantTest(t ZT) {
	reg := NewRegistry()
	_ = reg.Register(TextCodec[netip.Addr](VString).AsMapKey())

	proofs := []ZProof{
		ZType("netip.Addr", ZEq[netip.Addr],
			ZAt(netip.Addr{}, String("")),
			ZAt(netip.MustParseAddr("192.0.2.1"), String("192.0.2.1")),
			ZAt(netip.MustParseAddr("2001:db8::1"), String("2001:db8::1")),
		),
	}
	ZRoundTrip(t, zMemoryPlane(), proofs, WithRegistry(reg))

	// The key obligation, which #31 makes a separate question from the value
	// proof: do these two values stay two?
	for _, s := range ZInjective(reg,
		netip.MustParseAddr("192.0.2.1"),
		netip.MustParseAddr("::ffff:192.0.2.1"),
		netip.MustParseAddr("fe80::1%eth0"),
		netip.MustParseAddr("fe80::1%eth1"),
		netip.Addr{},
	) {
		t.Errorf("netip.Addr as a key: %s", s)
	}

	// And their own completeness, which is core's check pointed at their table.
	for _, s := range ZComplete(reg, append(ZCoreTypes(), proofs...)...) {
		t.Errorf("registry: %s", s)
	}

	ZCodec(t, reg)
}

// ===========================================================================
// CONSUMER 4: an ordinary user, who is not testing ferry at all. This is the
// audience ADR-0002 had in mind when it admitted the memory plane - "xload
// ships MapLoader and people reach for it constantly" - and it is the reason
// the apparatus and the suites need different stability promises.
// ===========================================================================

type zUserConfig struct {
	Port    int           `ferry:"port"`
	Timeout time.Duration `ferry:"timeout"`
}

func zUserTest(t ZT) {
	ctx := context.Background()
	src := ZStatic(map[Path]Value{
		Path{}.Name("port"):    Number("8080"),
		Path{}.Name("timeout"): String("30s"),
	})
	cfg, err := Load[zUserConfig](ctx, src)
	if err != nil {
		t.Errorf("load: %v", err)
		return
	}
	if cfg.Port != 8080 || cfg.Timeout != 30*time.Second {
		t.Errorf("got %+v", cfg)
	}

	// And the other half of the apparatus: what did my struct actually map to?
	// ADR-0001 puts schema extraction in the Enabled bucket precisely because
	// this pattern needs no core surface beyond a recording sink.
	rec, err := ZRecord(ctx, zUserConfig{Port: 1})
	if err != nil {
		t.Errorf("record: %v", err)
	}
	if _, ok := rec[Path{}.Name("timeout")]; !ok {
		t.Errorf("expected /timeout to be mapped, got %v", rec)
	}
}

// ===========================================================================
// The table. It is a function and not a var because it holds time values, and
// it is the artefact #28 makes a published interface rather than a fixture.
// ===========================================================================

func ZCoreTypes() []ZProof {
	return []ZProof{
		ZType("bool", ZEq[bool], ZAt(true, Bool(true)), ZAt(false, Bool(false))),
		ZType("string", ZEq[string],
			ZAt("", String("")), ZAt("a", String("a")), ZAt("b,c", String("b,c")),
			ZAt("\x00", String("\x00")), ZAt("héllo", String("héllo"))),
		ZType("int", ZEq[int], ZAt(0, Number("0")), ZAt(-1, Number("-1")),
			ZAt(math.MaxInt, Number("9223372036854775807")),
			ZAt(math.MinInt, Number("-9223372036854775808"))),
		ZType("int8", ZEq[int8], ZAt(int8(0), Number("0")),
			ZAt(int8(math.MaxInt8), Number("127")), ZAt(int8(math.MinInt8), Number("-128"))),
		ZType("int16", ZEq[int16], ZAt(int16(0), Number("0")),
			ZAt(int16(math.MaxInt16), Number("32767")), ZAt(int16(math.MinInt16), Number("-32768"))),
		ZType("int32", ZEq[int32], ZAt(int32(0), Number("0")),
			ZAt(int32(math.MaxInt32), Number("2147483647")), ZAt(int32(math.MinInt32), Number("-2147483648"))),
		ZType("int64", ZEq[int64], ZAt(int64(0), Number("0")),
			ZAt(int64(math.MaxInt64), Number("9223372036854775807")),
			ZAt(int64(math.MinInt64), Number("-9223372036854775808"))),
		ZType("uint", ZEq[uint], ZAt(uint(0), Number("0")),
			ZAt(uint(math.MaxUint), Number("18446744073709551615"))),
		ZType("uint8", ZEq[uint8], ZAt(uint8(0), Number("0")), ZAt(uint8(255), Number("255"))),
		ZType("uint16", ZEq[uint16], ZAt(uint16(0), Number("0")), ZAt(uint16(65535), Number("65535"))),
		ZType("uint32", ZEq[uint32], ZAt(uint32(0), Number("0")), ZAt(uint32(4294967295), Number("4294967295"))),
		ZType("uint64", ZEq[uint64], ZAt(uint64(0), Number("0")),
			ZAt(uint64(math.MaxUint64), Number("18446744073709551615"))),
		ZType("float64", zBitEq[float64],
			ZAt(0.0, Number("0")), ZAt(math.Copysign(0, -1), Number("-0")),
			ZAt(0.1, Number("0.1")), ZAt(1.0/3.0, Number("0.3333333333333333")),
			ZAt(math.MaxFloat64, Number("1.7976931348623157e+308")),
			ZAt(math.SmallestNonzeroFloat64, Number("5e-324")),
			ZAt(math.Inf(1), Number("+Inf")), ZAt(math.Inf(-1), Number("-Inf")),
			ZAt(math.NaN(), Number("NaN"))),
		ZType("float32", zBitEq32,
			ZAt(float32(0), Number("0")), ZAt(float32(0.1), Number("0.1")),
			ZAt(float32(math.MaxFloat32), Number("3.4028235e+38"))),
		// The golden column earned its place on the first run. ADR-0005 says
		// "a composite with no elements writes Null at its own address,
		// whether it is nil or empty" - and []byte is a LEAF, admitted at kind
		// Bytes, so that rule does not reach it and nil and empty are two
		// texts. The relation conflates them; the column does not.
		ZType("[]byte", ZSliceEq(ZEq[byte]),
			ZAt([]byte(nil), Null()), ZAt([]byte{}, Bytes([]byte{})),
			ZAt([]byte{0x00, 0xff, 0x41}, Bytes([]byte{0x00, 0xff, 0x41}))),
		ZType("[1]byte", ZEq[[1]byte], ZAt([1]byte{0x41}, Bytes([]byte{0x41}))),
		ZType("time.Duration", ZEq[time.Duration],
			ZAt(time.Duration(0), String("0s")),
			ZAt(time.Second, String("1s")),
			ZAt(90*time.Minute, String("1h30m0s")),
			ZAt(-time.Second, String("-1s"))),
		ZType("time.Time", time.Time.Equal,
			ZAt(time.Time{}, String("0001-01-01T00:00:00Z")),
			ZAt(time.Unix(0, 0).UTC(), String("1970-01-01T00:00:00Z")),
			ZAt(time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), String("2026-08-02T12:00:00Z"))),
		ZType("[]string", ZSliceEq(ZEq[string]), ZAt([]string(nil), Null())),
	}
}

func zBitEq[T ~float64](a, b T) bool {
	return math.Float64bits(float64(a)) == math.Float64bits(float64(b))
}
func zBitEq32(a, b float32) bool {
	return math.Float32bits(a) == math.Float32bits(b)
}

var zSeqN int

func zSeq() int { zSeqN++; return zSeqN }
