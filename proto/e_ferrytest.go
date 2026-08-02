package main

// What `ferrytest` would ship for errors.
//
// Question 9 of the grill made message text non-API and question 3 kept the
// vocabulary at five words, so a test that wants to assert "this exact thing
// went wrong at this exact place" has errors.Is plus an address and nothing
// else. That is affordable only if the precision lives somewhere, and this is
// where: an EXACT-SET diff over (address, class) pairs.
//
// Exact rather than "contains" is the whole point. ADR-0008's three diagnostic
// tiers are a suppression order, and the defect they are most likely to develop
// is firing once too often. A contains-assertion passes straight through that.
//
// This lives in ferrytest and not in the root package. Promotion is a later
// call, and nothing stops a user importing ferrytest from production code
// meanwhile.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Want is one expected element. Class is a ferry sentinel.
type Want struct {
	Address Path
	Class   error
}

// DiffErrors is the primitive: no *testing.T, so it works in a fuzz target, in
// a conformance run over a third-party driver, or anywhere the result is wanted
// as data rather than as a test failure. A diff is also what actually helps -
// "wanted /db/port value, got /db/port missing" is a sentence, and false is not.
func DiffErrors(got error, want ...Want) []string {
	type key struct {
		addr  string
		class string
	}
	k := func(a Path, c error) key { return key{a.String(), className(c)} }

	have := map[key]int{}
	order := []error{}
	for _, e := range Elements(got) {
		var fe *Error
		if !errors.As(e, &fe) {
			have[key{"(not a ferry error)", e.Error()}]++
			order = append(order, e)
			continue
		}
		have[k(fe.loc, fe.class)]++
		order = append(order, e)
	}
	wantCount := map[key]int{}
	for _, w := range want {
		wantCount[k(w.Address, w.Class)]++
	}

	var diffs []string
	all := map[key]bool{}
	for x := range have {
		all[x] = true
	}
	for x := range wantCount {
		all[x] = true
	}
	keys := make([]key, 0, len(all))
	for x := range all {
		keys = append(keys, x)
	}
	slices.SortFunc(keys, func(a, b key) int {
		if c := strings.Compare(a.addr, b.addr); c != 0 {
			return c
		}
		return strings.Compare(a.class, b.class)
	})
	for _, x := range keys {
		h, w := have[x], wantCount[x]
		switch {
		case h == w:
		case w == 0:
			diffs = append(diffs, fmt.Sprintf("unwanted: %s %s (x%d)", x.addr, x.class, h))
		case h == 0:
			diffs = append(diffs, fmt.Sprintf("missing:  %s %s (x%d)", x.addr, x.class, w))
		default:
			diffs = append(diffs, fmt.Sprintf("count:    %s %s got %d, want %d", x.addr, x.class, h, w))
		}
	}
	return diffs
}

// CheckErrors is the *testing.T wrapper, matching the shape ferrytest.RoundTrip
// already has.
type testingT interface {
	Helper()
	Errorf(format string, args ...any)
}

func CheckErrors(t testingT, got error, want ...Want) {
	t.Helper()
	if d := DiffErrors(got, want...); len(d) > 0 {
		t.Errorf("error set mismatch:\n  %s", strings.Join(d, "\n  "))
	}
}
