package env

import (
	"cmp"
	"errors"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// keyCase is one address and the environment variable name it must render to,
// or the legality refusal it must produce instead.
type keyCase struct {
	addr ferry.Path
	sep  string
	want string
	err  error
}

// TestKey pins the transform, which is what every stored artefact of this plane
// is named by.
//
// The first five rows are ADR-0003's own worked example, read down the env
// column. The rest are the cases that distinguish a transforming key function
// from a validating one: a hyphen and a dot are folded rather than refused,
// which is what makes feature-flags writable at all, and only the two shapes no
// fold can rescue are refused.
func TestKey(t *testing.T) {
	t.Parallel()

	cases := map[string]keyCase{
		"a leaf":                  {addr: ferry.At("name"), want: "NAME"},
		"a nested leaf":           {addr: ferry.At("db", "host"), want: "DB_HOST"},
		"twice nested":            {addr: ferry.At("db", "auth", "user"), want: "DB_AUTH_USER"},
		"a sequence position":     {addr: ferry.At("tags").Elem(0), want: "TAGS_0"},
		"a map key":               {addr: ferry.At("limits", "rps"), want: "LIMITS_RPS"},
		"a hyphen is folded":      {addr: ferry.At("feature-flags"), want: "FEATURE_FLAGS"},
		"a dot is folded":         {addr: ferry.At("db.host"), want: "DB_HOST"},
		"case is folded":          {addr: ferry.At("myKey"), want: "MYKEY"},
		"a multi-byte rune":       {addr: ferry.At("naïve"), want: "NA__VE"},
		"a wider join nests":      {addr: ferry.At("metric", "http", "port"), sep: wider, want: "METRIC__HTTP__PORT"},
		"a wider join stays flat": {addr: ferry.At("metric", "http_port"), sep: wider, want: "METRIC__HTTP_PORT"},

		// The three legality failures, which are the questions no transform
		// answers: there is no fold of nothing, and no shell sets a name that
		// begins with a digit.
		"the empty address": {addr: ferry.Path{}, err: ErrIllegalName},
		"an empty segment":  {addr: ferry.At("labels", ""), err: ErrIllegalName},
		"a leading digit":   {addr: ferry.At("1st"), err: ErrIllegalName},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkKey(t, tc)
		})
	}
}

// wider is the join an operator reaches for when the default collides, and it is
// spelled once because half the tests in this package take it.
const wider = "__"

// checkKey renders one address and holds the result to the row.
func checkKey(t *testing.T, tc keyCase) {
	t.Helper()

	c := defaults()
	c.sep = cmp.Or(tc.sep, DefaultSeparator)

	got, err := c.key(tc.addr)

	if !errors.Is(err, tc.err) {
		t.Fatalf("key(%s) error = %v, want %v", tc.addr, err, tc.err)
	}

	if got != tc.want {
		t.Errorf("key(%s) = %q, want %q", tc.addr, got, tc.want)
	}
}

// TestKeyLegalityIsAPlaneRefusal holds the driver to stating the class it has an
// opinion about, and to leaving its own sentinel reachable underneath.
func TestKeyLegalityIsAPlaneRefusal(t *testing.T) {
	t.Parallel()

	c := defaults()

	_, err := c.key(ferry.At("1st"))

	for _, want := range []error{ferry.ErrPlane, ErrIllegalName} {
		if !errors.Is(err, want) {
			t.Errorf("key error %v does not answer errors.Is against %v", err, want)
		}
	}
}

// The schemas the Bind cases are written against.
//
// They are types rather than lists of addresses, because the three address
// kinds are sealed and the schema compiler is the only thing that mints one
// (ADR-0016). Compiling a real struct is the same door core comes in through,
// so what these cases hand the driver is what a caller's own program would.
type (
	// metricPort and metricCollide are the separator collision: a nested
	// section and a flat leaf that render to one name at the default join.
	metricPort struct {
		Port string `ferry:"port"`
	}
	metricInner struct {
		HTTP     metricPort `ferry:"http"`
		HTTPPort string     `ferry:"http_port"`
	}
	metricCollide struct {
		Metric metricInner `ferry:"metric"`
	}

	// metricWideInner is the same shape one join wider, so that a separator
	// chosen to avoid a collision meets a segment holding that separator.
	metricWideInner struct {
		HTTP        metricPort `ferry:"http"`
		HTTPDblPort string     `ferry:"http__port"`
	}
	metricWideCollide struct {
		Metric metricWideInner `ferry:"metric"`
	}

	// hyphenCollide is the transform folding two legal spellings together.
	hyphenCollide struct {
		Hyphen string `ferry:"feature-flags"`
		Under  string `ferry:"feature_flags"`
	}

	// caseCollide is viper's measured bug turned into an error.
	caseCollide struct {
		Mixed string `ferry:"myKey"`
		Title string `ferry:"MyKey"`
	}

	// hyphenAlone and hyphenBesideTree are the acceptance rows: a hyphen is
	// transformed and accepted, and refused only alongside the twin it folds
	// onto.
	hyphenAlone struct {
		Hyphen string `ferry:"feature-flags"`
	}
	dbHost struct {
		Host string `ferry:"host"`
	}
	hyphenBesideTree struct {
		Hyphen string `ferry:"feature-flags"`
		DB     dbHost `ferry:"db"`
	}

	// distinctTrees is two sections that share a leaf name and collide nowhere.
	dbHostPort struct {
		Host string `ferry:"host"`
		Port string `ferry:"port"`
	}
	distinctTrees struct {
		DB    dbHostPort `ferry:"db"`
		Cache dbHost     `ferry:"cache"`
	}
)

// bindCase is one schema, the options it is bound under, and the pair the
// refusal must name. An empty pair is a set the join keeps distinct.
type bindCase struct {
	set  func(...ferry.Option) (*ferry.AddressSet, error)
	opts []Option
	pair [2]ferry.Path
}

// addrs compiles the case's schema, or fails the test on a schema that does not.
func (tc bindCase) addrs(t *testing.T) *ferry.AddressSet {
	t.Helper()

	set, err := tc.set()
	if err != nil {
		t.Fatalf("compiling the fixture: %v", err)
	}

	return set
}

// TestBindRefusesACollidingSchema is ADR-0003's driver-side rule, and it is what
// makes transforming safe rather than merely convenient.
//
// One rule covers all four rows below, because they are one failure: a
// many-to-one map out of the address set. The separator rows are the collision a
// flat key space creates and cannot see, the hyphen row is the transform folding
// two legal spellings together, and the case row is viper's measured bug turned
// into an error.
func TestBindRefusesACollidingSchema(t *testing.T) {
	t.Parallel()

	metric, flat := ferry.At("metric", "http", "port"), ferry.At("metric", "http_port")
	wide := ferry.At("metric", "http__port")
	hyphen, under := ferry.At("feature-flags"), ferry.At("feature_flags")
	mixed, title := ferry.At("myKey"), ferry.At("MyKey")

	cases := map[string]bindCase{
		"the separator, at the default join": {
			set:  addrsOf[metricCollide],
			pair: [2]ferry.Path{metric, flat},
		},
		"a segment holding the wider join, at the wider join": {
			set:  addrsOf[metricWideCollide],
			opts: []Option{Separator(wider)},
			pair: [2]ferry.Path{metric, wide},
		},
		"a hyphen against its underscore twin": {
			set:  addrsOf[hyphenCollide],
			pair: [2]ferry.Path{hyphen, under},
		},
		"two spellings of one name": {
			set:  addrsOf[caseCollide],
			pair: [2]ferry.Path{mixed, title},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkRefused(t, tc)
		})
	}
}

// checkRefused binds one colliding set and holds the refusal to naming its pair.
func checkRefused(t *testing.T, tc bindCase) {
	t.Helper()

	_, err := New(tc.opts...).Bind(tc.addrs(t))
	if err == nil {
		t.Fatalf("Bind accepted %s and %s, which render to one name", tc.pair[0], tc.pair[1])
	}

	assertPlaneRefusalNaming(t, err, tc.pair)
}

// TestBindAcceptsWhatTheJoinKeepsDistinct is the other half of the same rule,
// and without it the tests above are satisfied by a driver that refuses
// everything.
//
// The first two rows are the acceptance criterion stated exactly: a segment
// holding a hyphen is transformed and accepted alone, and refused only alongside
// the twin it folds onto. The third is the wider join earning its place, over
// the very pair the default refuses.
func TestBindAcceptsWhatTheJoinKeepsDistinct(t *testing.T) {
	t.Parallel()

	cases := map[string]bindCase{
		"a hyphen alone": {
			set: addrsOf[hyphenAlone],
		},
		"a hyphen beside a tree of its own": {
			set: addrsOf[hyphenBesideTree],
		},
		"the separator pair, at the wider join": {
			set:  addrsOf[metricCollide],
			opts: []Option{Separator(wider)},
		},
		"distinct trees at the default join": {
			set: addrsOf[distinctTrees],
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkAccepted(t, tc)
		})
	}
}

// checkAccepted binds one set the join keeps distinct.
func checkAccepted(t *testing.T, tc bindCase) {
	t.Helper()

	if _, err := New(tc.opts...).Bind(tc.addrs(t)); err != nil {
		t.Errorf("Bind refused a set that renders to distinct names: %v", err)
	}
}

// The schema TestBindRefusalReachesTheCaller is written against: a nested
// section and a flat leaf whose addresses render to one environment variable
// name at the default join.
type (
	metricHTTP struct {
		Port string `ferry:"port"`
	}

	metricSection struct {
		HTTP     metricHTTP `ferry:"http"`
		HTTPPort string     `ferry:"http_port"`
	}

	metricConfig struct {
		Metric metricSection `ferry:"metric"`
	}
)

// TestBindRefusalReachesTheCaller is the same refusal seen from where a user
// stands: through ferry.Load, over a real schema, with no plane in sight.
//
// It is the end-to-end statement of "before any I/O": the source is given an
// environment that would answer every address, and the load never reads it.
func TestBindRefusalReachesTheCaller(t *testing.T) {
	t.Parallel()

	read := 0
	environ := func() []string {
		read++

		return []string{"METRIC_HTTP_PORT=8080"}
	}

	_, err := ferry.Load[metricConfig](t.Context(), New(Environ(environ)))
	if err == nil {
		t.Fatal("Load accepted a schema whose two addresses render to one name")
	}

	assertPlaneRefusalNaming(t, err, [2]ferry.Path{
		ferry.At("metric", "http", "port"), ferry.At("metric", "http_port"),
	})

	if read != 0 {
		t.Errorf("the environment was read %d times, want none: a schema this plane cannot hold is refused "+
			"at Bind, which does no I/O", read)
	}
}

// leadingDigit is a schema whose one address no environment variable name can
// spell, whatever it is folded to.
type leadingDigit struct {
	First string `ferry:"1st"`
}

// TestIllegalNameReachesTheCaller holds the legality refusal to the same two
// properties as the collision one: the class the driver stated, and the driver's
// own sentinel still reachable under ferry's wrapper.
func TestIllegalNameReachesTheCaller(t *testing.T) {
	t.Parallel()

	_, err := ferry.Load[leadingDigit](t.Context(), New(Environ(newEnviron().environ)))
	if err == nil {
		t.Fatal("Load accepted an address no environment variable name can spell")
	}

	for _, want := range []error{ferry.ErrPlane, ErrIllegalName} {
		if !errors.Is(err, want) {
			t.Errorf("the load failed with %v, which does not answer errors.Is against %v", err, want)
		}
	}
}

// assertPlaneRefusalNaming holds one refusal to being the plane's own class and
// to naming both addresses of the pair it refused over.
//
// It reads the elements rather than the rendering, because a refusal over more
// than one pair aggregates and an aggregate's one line names one address per
// element (ADR-0011). Naming one is a refusal an author cannot act on: it is the
// other address that says which of the two to move.
//
// A zero pair is a set that must be accepted, and reaching here with one is the
// caller's mistake rather than a case.
func assertPlaneRefusalNaming(t *testing.T, err error, pair [2]ferry.Path) {
	t.Helper()

	if !errors.Is(err, ferry.ErrPlane) {
		t.Errorf("Bind refused with %v, which is not a plane refusal", err)
	}

	for _, e := range ferry.Elements(err) {
		if namesBoth(e.Error(), pair) {
			return
		}
	}

	t.Errorf("the refusal %+v names no element holding both %s and %s", err, pair[0], pair[1])
}

// namesBoth reports whether one element's text holds both addresses of the pair.
func namesBoth(text string, pair [2]ferry.Path) bool {
	return strings.Contains(text, pair[0].String()) && strings.Contains(text, pair[1].String())
}
