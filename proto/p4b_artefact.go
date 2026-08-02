package main

// P4d..f: the three things that actually separate before-kind from after-kind.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"

	"github.com/google/uuid"
)

// hostConf is one ordinary config struct containing the types the ordering
// decides. What it looks like as a real YAML file is the whole argument.
type hostConf struct {
	Listen net.IP     `ferry:"listen"`
	ID     uuid.UUID  `ferry:"id"`
	Level  slog.Level `ferry:"level"`
	Name   string     `ferry:"name"`
}

// growing0 and growing1 differ by ONE exported field. Under after-kind that
// edit silently changes the plane representation of the whole type.
type growing0 struct{ n int }

func (v growing0) MarshalText() ([]byte, error) { return fmt.Appendf(nil, "g%d", v.n), nil }
func (v *growing0) UnmarshalText(b []byte) error {
	_, err := fmt.Sscanf(string(b), "g%d", &v.n)
	return err
}

type growing1 struct {
	N int
}

func (v growing1) MarshalText() ([]byte, error) { return fmt.Appendf(nil, "g%d", v.N), nil }
func (v *growing1) UnmarshalText(b []byte) error {
	_, err := fmt.Sscanf(string(b), "g%d", &v.N)
	return err
}

func p4yaml(v any) string {
	dir, _ := os.MkdirTemp("", "p4")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "c.yaml")
	rv := reflect.ValueOf(v)
	d, err := dump(rv)
	if err != nil {
		return "dump err: " + err.Error()
	}
	as := NewAddressSet(sortedAddrs(d))
	ow, err := (FYAMLSink{Path: p}).Bind(as)
	if err != nil {
		return "bind err: " + err.Error()
	}
	if err := fDump(context.Background(), ow, d, as); err != nil {
		return "write err: " + err.Error()
	}
	b, _ := os.ReadFile(p)
	return string(b)
}

func runArtefact() {
	fmt.Println("\n--- P4d: the artefact, on a real YAML file ---")
	c := hostConf{
		Listen: net.ParseIP("192.0.2.1"),
		ID:     uuid.MustParse("0e37df36-f698-11e6-8dd4-cb9ced3df976"),
		Level:  slog.LevelWarn,
		Name:   "svc",
	}
	for _, m := range []struct {
		label  string
		order  []string
		before bool
	}{
		{"kind only / text after kind", nil, false},
		{"text before kind", []string{"text"}, true},
	} {
		chainOrder, chainBeforeKind = m.order, m.before
		fmt.Printf("\n    %s:\n", m.label)
		for _, line := range splitLines(p4yaml(c)) {
			fmt.Printf("      %s\n", line)
		}
	}
	chainOrder, chainBeforeKind = nil, false
	fmt.Println("    ^ the second is a file a human can edit. ADR-0001 puts legibility")
	fmt.Println("      on the driver's side of the line, so nothing in core's guarantee")
	fmt.Println("      catches the first one. That is why the ordering is a decision and")
	fmt.Println("      not an emergent property.")

	fmt.Println("\n--- P4e: under AFTER-kind, adding one exported field changes the plane ---")
	for _, m := range []struct {
		label  string
		before bool
	}{{"after kind", false}, {"before kind", true}} {
		chainOrder, chainBeforeKind = []string{"text"}, m.before
		a0, _ := compile(reflect.TypeFor[struct{ V growing0 }]())
		a1, _ := compile(reflect.TypeFor[struct{ V growing1 }]())
		d0, _ := dump(reflect.ValueOf(struct{ V growing0 }{growing0{7}}))
		d1, _ := dump(reflect.ValueOf(struct{ V growing1 }{growing1{7}}))
		fmt.Printf("    %-12s no exported field: %-10v %s\n", m.label, a0, fmtVals(d0))
		fmt.Printf("    %-12s one exported field: %-9v %s\n", "", a1, fmtVals(d1))
	}
	chainOrder, chainBeforeKind = nil, false
	fmt.Println("    ^ under after-kind an unrelated edit - exporting a field - silently")
	fmt.Println("      rewrites every stored artefact of that type. That is #28's")
	fmt.Println("      breaking change, triggered by a change nobody would review as one.")

	fmt.Println("\n--- P4f: does ferry INVENT the normalising-MarshalText hazard? ---")
	v := normalisingText("info")
	b, _ := json.Marshal(v)
	var back normalisingText
	_ = json.Unmarshal(b, &back)
	fmt.Printf("    encoding/json (v2 semantics): %v -> %s -> %v   round-trips=%v\n",
		v, b, back, v == back)
	fmt.Println("    ^ the same type is already broken through encoding/json. Under")
	fmt.Println("      before-kind ferry inherits that hazard; under after-kind ferry")
	fmt.Println("      would disagree with encoding/json about a type whose author")
	fmt.Println("      declared one text form, which is two authorities again.")
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
