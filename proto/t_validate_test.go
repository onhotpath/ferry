package main

import "testing"

// The entry point the ticket's own comment asks for, exercised the way a user
// would: a unit test, no value in hand, no plane reachable.
func TestFerryTagsAreValid(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check func() error
		want  bool
	}{
		{"a fully annotated struct", Validate[namedB], false},
		{"a struct with an untagged exported field", Validate[namedA], true},
		{"omitzero with a non-zero default", Validate[ref5], true},
		{"a misspelled option", Validate[struct {
			H string `ferry:"h,requird"`
		}], true},
		{"an option from another mapper", Validate[struct {
			H string `ferry:"h,omitempty"`
		}], true},
		{"a tag reflect.StructTag.Get silently truncates", Validate[struct {
			H string `ferry:"h,default=["v"]"`
		}], true},
		{"a field carrying two ferry tags", Validate[struct {
			H string `ferry:"first" ferry:"second"`
		}], true},
	} {
		err := tc.check()
		if (err != nil) != tc.want {
			t.Errorf("%s: got err=%v, wanted an error: %v", tc.name, err, tc.want)
			continue
		}
		t.Logf("%-46s %v", tc.name, err)
	}
}
