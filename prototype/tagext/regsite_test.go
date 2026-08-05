package tagext

import (
	"reflect"
	"testing"
)

// The full pipeline: declare on the registry, compile, table rides the
// AddressSet, the driver reads its own view at Bind with no plumbing.
func TestRegistryToDriverPipeline(t *testing.T) {
	reg, err := NewRegistry(
		KeyExtension{TagKey: "yamlext", Words: []Word{{Name: "node", TakesVal: true}}},
		KeyExtension{TagKey: "docs", Words: []Word{{Name: "desc", TakesVal: true}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	set, err := CompileWith(reg, map[string]string{
		"/wait": `ferry:"wait" yamlext:"node=mycompany:duration" docs:"desc=how long to wait"`,
		"/host": `ferry:"host,required" docs:"desc=the host"`,
	})
	if err != nil {
		t.Fatal(err)
	}

	sink := &yamlSinkMini{}
	if err := sink.Bind(set); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"/wait": "!mycompany:duration"}
	if !reflect.DeepEqual(sink.nodeTags, want) {
		t.Fatalf("driver's view wrong: %v", sink.nodeTags)
	}

	docs := set.Extension("docs")
	if docs["/host"]["desc"] != "the host" || docs["/wait"]["desc"] != "how long to wait" {
		t.Fatalf("out-of-band consumer's view wrong: %v", docs)
	}
	if len(set.Extension("nobody")) != 0 {
		t.Fatal("an undeclared key's view must be empty, not an error")
	}
}

// Two registries, two declarations, two independent tables - the
// registry is the outer cache level, so this is the cache-key story too.
func TestDeclarationIsTheRegistrys(t *testing.T) {
	with, _ := NewRegistry(KeyExtension{TagKey: "yamlext", Words: []Word{{Name: "node", TakesVal: true}}})
	without, _ := NewRegistry()

	fields := map[string]string{"/wait": `ferry:"wait" yamlext:"node=mycompany:duration"`}
	if _, err := CompileWith(with, fields); err != nil {
		t.Fatal(err)
	}
	// Under a registry that declared nothing, the SAME struct still
	// compiles - the foreign key is another library's, per Go convention -
	// but no table entry is minted: undeclared means unread, not illegal.
	set, err := CompileWith(without, fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Extension("yamlext")) != 0 {
		t.Fatal("an undeclared key must mint nothing")
	}
}

// A typo inside a declared key still refuses at compile with its own
// near-miss - declaring a key buys its users first-class diagnostics.
func TestDeclaredKeyTypoRefusesAtCompile(t *testing.T) {
	reg, _ := NewRegistry(KeyExtension{TagKey: "yamlext", Words: []Word{{Name: "node", TakesVal: true}}})
	_, err := CompileWith(reg, map[string]string{
		"/wait": `ferry:"wait" yamlext:"nodee=x"`,
	})
	if err == nil {
		t.Fatal("a typo in a declared key must refuse at compile")
	}
}
