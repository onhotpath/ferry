// Package ferry is a bidirectional, struct-first data mapper. One annotated
// struct and one tag grammar drive both directions: Load fills a value from a
// pluggable source, and Dump writes the same value back to a pluggable sink.
//
// Core carries only what no driver can supply for itself - the walk, the schema
// compiler, tag parsing, the codec chain, defaults and zero values - and only
// what core imposes but cannot compile-check, which ships as the thing that
// checks it. Nothing in core knows what a plane is for; planes ship as driver
// modules under driver/ (ADR-0001, ADR-0002).
//
// # Errors
//
// ferry reports every failure that is not a consequence of another failure it
// is already reporting, so a failed call carries a set rather than the first
// thing that went wrong. Range it with [Elements], and match a member with
// errors.Is against [ErrSchema], [ErrMissing], [ErrValue], [ErrPlane],
// [ErrDriver] or [ErrReadOnly]. Read where it happened with
// errors.AsType[*ferry.Error] and [Error.Address]; there is no concrete type to
// switch on, and no enum.
//
// Message text is not API. Match on the sentinels and on the address rather
// than on a string, and get precision from the ferrytest assertions. ferry's own
// text never repeats a value the plane supplied - the cause stays in the chain
// and is never printed - so a plane that holds secrets does not leak them into
// a log through ferry (ADR-0011).
package ferry
