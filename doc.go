// Package ferry is a bidirectional, struct-first data mapper. One annotated
// struct and one tag grammar drive both directions: Load fills a value from a
// pluggable source, and Dump writes the same value back to a pluggable sink.
//
// Core carries only what no driver can supply for itself - the walk, the schema
// compiler, tag parsing, the codec chain, defaults and zero values - and only
// what core imposes but cannot compile-check, which ships as the thing that
// checks it. Nothing in core knows what a plane is for; planes ship as driver
// modules under driver/ (ADR-0001, ADR-0002).
package ferry
