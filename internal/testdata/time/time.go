// Package time is not the standard library's, and that is the whole of what it
// is for.
//
// ADR-0005 resolves core's type set by type identity before reflect.Kind, and
// says the mechanism matters as much as the rule: the table is keyed by
// reflect.Type values compared with ==, and it contains no strings. The prior
// art it corrects does the opposite - xload identifies time.Duration by
// comparing Type.String() to "time.Duration" - and a table keyed by name cannot
// tell that apart from any other package called time.
//
// So this package declares the two names core owns, with the same kinds and the
// same renderings. Measured, reflect.TypeFor[Duration]().String() is
// "time.Duration" and reflect.TypeFor[Time]().String() is "time.Time", byte for
// byte what the standard library's produce, while both reflect.Type values
// differ from theirs under ==. A ferry that matched by name would give these
// types the pinned representations, and a ferry that matches by identity gives
// Duration its kind's and refuses Time for mapping no address.
//
// It lives under testdata because the go command never matches a directory
// named testdata against ./... at any depth, so a second package called time is
// never built, vetted or linted with the module while an explicit import still
// resolves it; and the internal element above it means no importer outside
// ferry can reach it.
package time

// Duration is a distinct reflect.Type from time.Duration with the same kind and
// the same rendering, which is the false positive a name comparison cannot see.
type Duration int64

// String renders like the standard library's for the one value the test uses,
// so even a chain that consulted fmt.Stringer would produce the pinned text and
// the assertion would still be about identity rather than about spelling.
func (d Duration) String() string {
	if d == 0 {
		return "0s"
	}

	return "30s"
}

// Time is a distinct reflect.Type from time.Time, with the same shape: a struct
// whose fields are all unexported, so it maps no address and core refuses it.
type Time struct {
	wall uint64
	ext  int64
}

// MarshalText gives it the same text pair the standard library's carries, so
// what refuses it is the identity table's absence and not a missing method.
func (Time) MarshalText() ([]byte, error) { return []byte("2026-08-02T12:00:00Z"), nil }

// UnmarshalText is the inverse half, present for the same reason.
func (t *Time) UnmarshalText([]byte) error {
	t.wall, t.ext = 0, 0

	return nil
}
