package ferrytest

// T is what a suite in this package reports to, and it is deliberately two
// methods rather than *testing.T.
//
// *testing.T satisfies it for free, with no adapter and no wrapper at the call
// site, and so do *testing.B and *testing.F.
//
// ADR-0011 reached the same shape from the other end for the error primitive -
// it returns []string and takes no *testing.T, because the conformance suite
// runs against third-party drivers and wants the result as data - and this
// generalises that rather than adding a second convention.
//
// Three things it buys, and the third is the one that decides it.
//
//   - A suite is runnable from a probe or a main, not only from a test.
//   - A caller who wants to assert that a driver *fails* a case, which is what
//     a negative conformance test needs, can capture the report instead of
//     failing their own run.
//   - This package's own tests can assert on what its suites say. ferrytest is
//     authority under ADR-0002: it ships from the same place as the rules
//     because it is the rules in executable form, and capturing its own output
//     is the only way a package that is authority can be held to them.
//
// Helper is on the interface rather than optional because without it every
// failure a suite reports is attributed to a line inside this package, and a
// driver author reading their own CI output learns nothing about which of their
// cases went red.
//
// It is an interface of two methods rather than a struct of two funcs for a
// reason survey item 5.14 supplies: a caller passes *testing.T and the question
// of value receivers against pointer returns never arises.
type T interface {
	Errorf(format string, args ...any)
	Helper()
}
