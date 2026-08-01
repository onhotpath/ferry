package main

import (
	"strings"
	"testing"
)

// P10: the canonical form buys comparability at the cost of an encode on the
// way in and a decode on the way out. Encode happens once per schema and is
// cached; decode happens whenever a driver needs segments. Is either a reason
// to prefer a flat string?

var sinkS string
var sinkP []Segment

func BenchmarkEncodeCanonical(b *testing.B) {
	for b.Loop() {
		sinkS = path("db", "auth", "user").String()
	}
}

func BenchmarkEncodeFlatJoin(b *testing.B) {
	segs := []string{"db", "auth", "user"}
	for b.Loop() {
		sinkS = strings.Join(segs, ".")
	}
}

func BenchmarkDecodeCanonical(b *testing.B) {
	p := path("db", "auth", "user")
	for b.Loop() {
		sinkP = p.Segments()
	}
}

func BenchmarkDecodeFlatSplit(b *testing.B) {
	s := "db.auth.user"
	var out []string
	for b.Loop() {
		out = strings.Split(s, ".")
	}
	_ = out
}

func BenchmarkDriverKeyFunc(b *testing.B) {
	p := path("db", "auth", "user")
	for b.Loop() {
		sinkS = envKey(p)
	}
}

func BenchmarkInjectivityCheck60(b *testing.B) {
	var addrs []Path
	for i := range 20 {
		base := path("svc"+string(rune('a'+i)), "auth")
		addrs = append(addrs, base.Name("user"), base.Name("pass"), base.Parent().Name("host"))
	}
	b.ResetTimer()
	for b.Loop() {
		if err := checkInjective(addrs, envKey); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeCanonicalSeq(b *testing.B) {
	p := path("db", "auth", "user")
	var n int
	for b.Loop() {
		for s := range p.SegmentsSeq() {
			n += len(s.Text)
		}
	}
	_ = n
}

func BenchmarkDriverKeyFuncSeq(b *testing.B) {
	p := path("db", "auth", "user")
	for b.Loop() {
		sinkS = envKeySeq(p)
	}
}

func BenchmarkDriverKeyPrecomputed(b *testing.B) {
	p := path("db", "auth", "user")
	pk := newPlaneKeys([]Path{p}, envKeySeq)
	b.ResetTimer()
	for b.Loop() {
		sinkS = pk.key(p)
	}
}

func BenchmarkFlatKeyDirect(b *testing.B) {
	m := map[string]string{"DB_AUTH_USER": "x"}
	for b.Loop() {
		sinkS = m["DB_AUTH_USER"]
	}
}
