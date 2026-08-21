package ferrytest

// T is what a suite in this package reports to. Pass *testing.T; it satisfies
// this for free, with no adapter and no wrapper, and so do *testing.B and
// *testing.F.
//
// It is two methods rather than *testing.T so that a suite is runnable from a
// probe or a main, and so that a caller who wants to assert a driver fails a
// case can capture the report instead of failing their own run.
//
// Helper is required rather than optional. Without it every failure a suite
// reports is attributed to a line inside this package, and a driver author
// reading their own CI output learns nothing about which of their cases went
// red.
type T interface {
	Errorf(format string, args ...any)
	Helper()
}

// reporter is [T] under a name a generic can still see.
//
// [Type], [Case] and typeProof all take a type parameter spelled T, which
// shadows the interface inside every one of their methods - and the run a proof
// performs is a method on typeProof, because that is the only place its cases
// are typed. An alias resolves here, at package scope, where T is still the
// interface.
type reporter = T

// logTo is where everything a suite says that is not a failure goes.
//
// [T] is two methods and neither of them is a log, deliberately: it is what
// *testing.T satisfies for free and what a probe can implement in four lines.
// So a skip is written where the reporter can carry one and is otherwise the
// silence it already was - which is why the reason is in each case's own
// documentation as well, where it cannot be lost (ADR-0014).
func logTo(rep reporter, format string, args ...any) {
	rep.Helper()

	l, ok := rep.(interface {
		Logf(format string, args ...any)
	})
	if !ok {
		return
	}

	l.Logf(format, args...)
}
