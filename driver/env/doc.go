// Package env is ferry's environment-variable plane, and it is a source and
// never a sink.
//
// It ships outside core because environment variables have no honest Dump: the
// target people want is a .env file or an environ slice, and .env is a format,
// which is plane knowledge (ADR-0002). Setting the process's own environment is
// process-global mutation nobody wants, so the absence of a sink here is a
// property of the plane rather than a decision about scope (ADR-0004). That
// absence is carried by the type system rather than by prose: nothing in this
// package implements ferry.Sink, so dumping to env is a compile error at the
// call site and never a runtime refusal or an ErrUnsupported nobody reads.
//
// # What the plane holds
//
// A String or an Absent, and never a Null. FOO= is a zero-length string and not
// a null (ADR-0004), so the distinction between "set to empty" and "not set at
// all" survives a load intact and a required field can tell the two apart. A
// value dumped as Bytes is carried as its bytes, because an environment
// variable is a byte string; nothing else about a value's type survives, since
// the plane holds no type information of its own.
//
// # How an address becomes a name
//
// Segments are folded to upper case, every byte an environment variable name
// cannot hold becomes an underscore, and the segments are joined with a
// separator that is a driver option. The fold is a transform rather than a
// validation, which is what makes a segment such as feature-flags writable at
// all, and injectivity over the schema's whole address set is what makes the
// transform safe: an address set the fold collapses is refused at Bind, before
// any I/O, naming both addresses (ADR-0003).
package env
