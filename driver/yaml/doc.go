// Package yaml will hold ferry's YAML driver, the plane that exercises the
// serialization axis of the driver contract: a format with quoting and escaping
// rules, nested structure flattened to addresses, and parse errors that must
// surface rather than be discarded.
//
// It is a skeleton today. Nothing is implemented, and the module deliberately
// carries no require on core yet, because nothing here imports it.
package yaml
