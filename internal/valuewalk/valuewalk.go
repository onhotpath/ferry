// Package valuewalk is the seam between core's reflect.Value-rooted walk and
// the one caller in this module that needs it.
//
// ferry's exported entry points fix their root type at compile time, because
// [ferry.Dump] compiles its schema from its type parameter. ferrytest is handed
// a registry, and what a registry hands back is a reflect.Type, so a suite that
// wants to walk a registrant's own type has no route to one. This package is
// that route, and it is deliberately unimportable from outside the module: the
// question of whether a reflect.Value root is a public capability is #134's,
// and a test harness must not settle it from the side.
//
// It holds one variable and knows nothing about ferry. Package ferry installs a
// value here from its own init, and the caller asserts that value back to an
// interface it declares itself over ferry's real types - so both ends of the
// seam are checked by the compiler's method-set rules, and nothing in between
// has to name a ferry type or this package would be an import cycle.
package valuewalk

// Seam is core's reflect.Value-rooted walk, installed by package ferry.
//
// It is `any` rather than a typed variable because a typed one would have to
// name ferry.Sink, ferry.Source and ferry.Option, and this package is imported
// by ferry. The caller recovers the types with a single assertion against a
// locally declared interface, which is exactly as safe as a typed variable and
// costs one comma-ok.
//
// It is written once, from an init in package ferry, before any importer of
// this package has run a line: a package's dependencies are fully initialised
// before its own variables are. So there is no race and no mutex.
var Seam any
