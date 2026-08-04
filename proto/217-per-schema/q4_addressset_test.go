package perschema_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// captured is a Source that does nothing but keep the AddressSet it was handed,
// which is exactly what a driver holds at Bind.
type captured struct{ set *ferry.AddressSet }

var errNotAPlane = errors.New("probe: this source is not a plane")

func (c *captured) Bind(a *ferry.AddressSet) (ferry.OpenFunc, error) {
	c.set = a

	return func(context.Context) (ferry.Reader, error) { return nil, errNotAPlane }, nil
}

// setFor is the AddressSet core hands a driver for T.
func setFor[T any](t *testing.T) *ferry.AddressSet {
	t.Helper()

	c := &captured{}
	if _, err := ferry.Bind[T](c); err != nil {
		t.Fatalf("bind: %v", err)
	}

	return c.set
}

func render(a *ferry.AddressSet) string {
	out := make([]string, 0, a.Len())
	for p := range a.All() {
		out = append(out, p.String())
	}

	return "[" + strings.Join(out, " ") + "]"
}

// TestQ4WhatTheSetHolds is the ground truth every question below is answered
// against: what core actually puts in the set for each shape of member.
func TestQ4WhatTheSetHolds(t *testing.T) {
	type Array struct {
		Pair [2]string `ferry:"pair"`
	}

	type Ptr struct {
		Sect *struct {
			Host string `ferry:"host"`
		} `ferry:"sect"`
	}

	t.Logf("%-38s  %-4s  %s", "type", "Len", "All()")

	for _, c := range []struct {
		label string
		set   *ferry.AddressSet
	}{
		{"struct{ DB struct{Host,Port} }", setFor[Static](t)},
		{"struct{ Tags []string; Limits map }", setFor[Dynamic](t)},
		{"struct{ Tags string; Limits string }", setFor[Leaf](t)},
		{"struct{ Pair [2]string }", setFor[Array](t)},
		{"struct{ Sect *struct{Host} }", setFor[Ptr](t)},
		{"struct{ Encodings []string }", setFor[Encodings](t)},
		{"struct{ Encoding string }", setFor[Encoding](t)},
	} {
		t.Logf("%-38s  %-4d  %s", c.label, c.set.Len(), render(c.set))
	}
}

// TestQ4WhatItCanAnswer walks every question a driver at Bind might want to ask
// about a name, with the code that answers it or the reason nothing does.
func TestQ4WhatItCanAnswer(t *testing.T) {
	dyn := setFor[Dynamic](t)
	stat := setFor[Static](t)
	leaf := setFor[Leaf](t)

	t.Logf("=== ANSWERABLE ===")
	t.Logf("")
	t.Logf("1. does this address exist in the schema?   a.Has(addr)")
	t.Logf("   Dynamic, /tags        -> %t", dyn.Has(ferry.Path{}.At("tags")))
	t.Logf("   Dynamic, /nope        -> %t", dyn.Has(ferry.Path{}.At("nope")))
	t.Logf("")
	t.Logf("2. does any address render to this plane name?   fold All() through the KeyFunc")
	t.Logf("   Dynamic, \"tags\"       -> %t", rendersTo(dyn, "tags"))
	t.Logf("   Dynamic, \"nope\"       -> %t", rendersTo(dyn, "nope"))
	t.Logf("   (this is the whole of what #210's CheckDeclaration did)")
	t.Logf("")
	t.Logf("3. how many addresses are there?   a.Len()")
	t.Logf("   Dynamic -> %d, Static -> %d, Leaf -> %d", dyn.Len(), stat.Len(), leaf.Len())
	t.Logf("")
	t.Logf("4. has this address any member the TYPE determines?   scan All() for a strict prefix match")
	t.Logf("   Static,  /db          -> %t  (a struct: its fields are in the set)", hasStaticMember(stat, "db"))
	t.Logf("   Dynamic, /tags        -> %t  (a slice: its elements are not)", hasStaticMember(dyn, "tags"))
	t.Logf("   Dynamic, /limits      -> %t  (a map: its members are not)", hasStaticMember(dyn, "limits"))
	t.Logf("   Leaf,    /tags        -> %t", hasStaticMember(leaf, "tags"))
	t.Logf("")
	t.Logf("5. what segment kinds does an address use?   Path.Segments(), Segment.Kind()")

	for _, c := range []struct {
		label string
		set   *ferry.AddressSet
	}{
		{"Dynamic", dyn},
		{"Static ", stat},
	} {
		for p := range c.set.All() {
			t.Logf("   %s %-16s -> %s", c.label, p.String(), kinds(p))
		}
	}

	t.Logf("")
	t.Logf("=== NOT ANSWERABLE ===")
	t.Logf("")
	t.Logf("6. is this address a container?")
	t.Logf("   Dynamic /tags (a []string) and Leaf /tags (a string) are the same member of the")
	t.Logf("   same-shaped set, and no method distinguishes them:")
	t.Logf("     Dynamic /tags  Has=%t  Len(set)=%d  staticMembers=%t  segments=%s",
		dyn.Has(ferry.Path{}.At("tags")), dyn.Len(), hasStaticMember(dyn, "tags"),
		kinds(ferry.Path{}.At("tags")))
	t.Logf("     Leaf    /tags  Has=%t  Len(set)=%d  staticMembers=%t  segments=%s",
		leaf.Has(ferry.Path{}.At("tags")), leaf.Len(), hasStaticMember(leaf, "tags"),
		kinds(ferry.Path{}.At("tags")))
	t.Logf("   the two sets are identical: %t", render(dyn) == render(leaf))
	t.Logf("")
	t.Logf("7. what Go type is behind this address?")
	t.Logf("   AddressSet's whole exported surface is Len, All and Has, over Path.")
	t.Logf("   Path's is At, Elem, String, Segments and Compare. No type crosses.")
	t.Logf("")
	t.Logf("8. is this address required, does it carry a default, what codec does it use?")
	t.Logf("   none of it is in the set, by the same argument as 7.")
	t.Logf("")
	t.Logf("9. which OTHER schemas has this Source been bound to?")
	t.Logf("   the set is one schema's. A driver may keep its own, and #210 measured that")
	t.Logf("   a guard on it breaks the one-shot path: Binds() is 3 for three ferry.Load calls.")
}

// TestQ4ContainerIsHalfAnswerable is the finding that sharpens "is-it-a-container
// is not checkable": half of it is, and which half is exactly whether the
// container's members come from the type or from the value.
func TestQ4ContainerIsHalfAnswerable(t *testing.T) {
	type Array struct {
		Tags [2]string `ferry:"tags"`
	}

	type Slice struct {
		Tags []string `ferry:"tags"`
	}

	type Struct struct {
		Tags struct {
			A string `ferry:"a"`
		} `ferry:"tags"`
	}

	type Scalar struct {
		Tags string `ferry:"tags"`
	}

	type Mapped struct {
		Tags map[string]string `ferry:"tags"`
	}

	t.Logf("%-34s  %-8s  %-14s  %s", "Go type at /tags", "in set", "staticMembers", "All()")

	for _, c := range []struct {
		label string
		set   *ferry.AddressSet
	}{
		{"[2]string   (members from type) ", setFor[Array](t)},
		{"struct{A}   (members from type) ", setFor[Struct](t)},
		{"[]string    (members from value)", setFor[Slice](t)},
		{"map[str]str (members from value)", setFor[Mapped](t)},
		{"string      (no members)        ", setFor[Scalar](t)},
	} {
		t.Logf("%-34s  %-8t  %-14t  %s", c.label,
			c.set.Has(ferry.Path{}.At("tags")), hasStaticMember(c.set, "tags"), render(c.set))
	}

	t.Logf("")
	t.Logf("so a driver at Bind CAN tell a statically-membered container from a leaf,")
	t.Logf("and CANNOT tell a dynamically-membered one from a leaf - which is the only")
	t.Logf("case a multimap driver's Repeatable is ever about.")
}

// rendersTo is question 2's code: fold the set through a KeyFunc and look for the
// name.
func rendersTo(a *ferry.AddressSet, name string) bool {
	for p := range a.All() {
		var b strings.Builder

		first := true

		for seg := range p.Segments() {
			if !first {
				b.WriteString("-")
			}

			first = false

			b.WriteString(seg.Text())
		}

		if strings.EqualFold(b.String(), name) {
			return true
		}
	}

	return false
}

// hasStaticMember is question 4's code: does any address in the set sit strictly
// below this one?
func hasStaticMember(a *ferry.AddressSet, name string) bool {
	at := ferry.Path{}.At(name).String()

	for p := range a.All() {
		if s := p.String(); s != at && strings.HasPrefix(s, at) {
			return true
		}
	}

	return false
}

func kinds(p ferry.Path) string {
	var out []string

	for seg := range p.Segments() {
		k := "Name"
		if seg.Kind() == ferry.Index {
			k = "Index"
		}

		out = append(out, fmt.Sprintf("%s(%s)", k, seg.Text()))
	}

	return strings.Join(slices.Clip(out), " ")
}
