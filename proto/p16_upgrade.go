package main

// P16: the exposure nobody asked about.
//
// If the chain runs before kind, then a type acquiring a text pair changes
// its plane representation. That method can be added by a dependency, in a
// release the consumer did not read, to a type the consumer embeds in their
// config struct. Measure it rather than assert it.

import (
	"fmt"
	"net/url"
	"reflect"
)

// v1 and v2 are the same type before and after an upstream release adds
// MarshalText/UnmarshalText. Nothing in the consumer's code changes.
type depTypeV1 struct {
	Host string
	Port int
}

type depTypeV2 struct {
	Host string
	Port int
}

func (v depTypeV2) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%s:%d", v.Host, v.Port), nil
}
func (v *depTypeV2) UnmarshalText(b []byte) error {
	_, err := fmt.Sscanf(string(b), "%s:%d", &v.Host, &v.Port)
	return err
}

func runUpgrade() {
	fmt.Println("\n--- P16a: a dependency adds MarshalText; the consumer changes nothing ---")
	for _, mode := range []struct {
		label  string
		before bool
	}{{"chain BEFORE kind", true}, {"chain AFTER kind", false}} {
		chainOrder, chainBeforeKind = []string{"text"}, mode.before
		fmt.Printf("\n    %s\n", mode.label)
		for _, r := range []struct {
			label string
			t     reflect.Type
			v     any
		}{
			{"dep v1 (no text pair)", reflect.TypeFor[depTypeV1](), depTypeV1{"h", 80}},
			{"dep v2 (text pair added)", reflect.TypeFor[depTypeV2](), depTypeV2{"h", 80}},
		} {
			h := reflect.New(reflect.StructOf([]reflect.StructField{
				{Name: "Backend", Type: r.t, Tag: `ferry:"backend"`},
			})).Elem()
			h.Field(0).Set(reflect.ValueOf(r.v))
			addrs, err := compile(h.Type())
			d, _ := dump(h)
			fmt.Printf("      %-26s addresses=%-24v plane=%s err=%v\n",
				r.label, fmt.Sprint(addrs), fmtVals(d), err)
		}
	}
	chainOrder, chainBeforeKind = nil, false
	fmt.Println("\n    ^ under before-kind the address set and every stored artefact change")
	fmt.Println("      on a dependency bump. Under after-kind they do not, because kind")
	fmt.Println("      admission already had an answer and keeps it.")
	fmt.Println("      The mirror case, a type that was REFUSED acquiring a text pair, is")
	fmt.Println("      benign under both: a compile error becomes a working field.")

	fmt.Println("\n--- P16b: what url.URL actually does under each option ---")
	u := mustU("https://user:pw@example.com/a/b?q=1#f")
	type conf struct {
		Endpoint url.URL `ferry:"endpoint"`
	}
	c := conf{u}

	for _, mode := range []struct {
		label string
		order []string
	}{
		{"text only (proposed)", []string{"text"}},
		{"text then binary", []string{"text", "binary"}},
	} {
		chainOrder, chainBeforeKind = mode.order, true
		addrs, err := compile(reflect.TypeFor[conf]())
		fmt.Printf("\n    %s\n", mode.label)
		if err != nil {
			fmt.Printf("      compile: %s\n", firstLine(err.Error()))
			continue
		}
		fmt.Printf("      compile: %v\n", addrs)
		fmt.Println("      as a real YAML file:")
		for _, l := range splitLines(p4yaml(c)) {
			fmt.Printf("        %s\n", l)
		}
		var back conf
		d, _ := dump(reflect.ValueOf(c))
		fmt.Printf("      round-trips: %v\n", load(d, reflect.ValueOf(&back).Elem()) == nil &&
			back.Endpoint.String() == u.String())
	}
	chainOrder, chainBeforeKind = nil, false

	fmt.Println("\n    and with a registered codec, which is what the ADR proposes instead:")
	fmt.Println("    (the whole codec, verbatim:)")
	fmt.Println(`      register(`)
	fmt.Println(`          func(u url.URL) (Value, error) { return String(u.String()), nil },`)
	fmt.Println(`          func(v Value) (url.URL, error) {`)
	fmt.Println(`              s, err := v.AsString(); if err != nil { return url.URL{}, err }`)
	fmt.Println(`              p, err := url.Parse(s); if err != nil { return url.URL{}, err }`)
	fmt.Println(`              return *p, nil`)
	fmt.Println(`          })`)
	register(
		func(u url.URL) (Value, error) { return String(u.String()), nil },
		func(v Value) (url.URL, error) {
			s, err := v.AsString()
			if err != nil {
				return url.URL{}, err
			}
			p, err := url.Parse(s)
			if err != nil {
				return url.URL{}, err
			}
			return *p, nil
		})
	defer delete(byIdentity, reflect.TypeFor[url.URL]())
	addrs, err := compile(reflect.TypeFor[conf]())
	fmt.Printf("      compile: %v err=%v\n", addrs, err)
	for _, l := range splitLines(p4yaml(c)) {
		fmt.Printf("        %s\n", l)
	}
	d, _ := dump(reflect.ValueOf(c))
	var back conf
	fmt.Printf("      round-trips: %v\n", load(d, reflect.ValueOf(&back).Elem()) == nil &&
		back.Endpoint.String() == u.String())
}
