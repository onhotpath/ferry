// Package kv will hold ferry's key-value driver, the plane that exercises the
// I/O axis of the driver contract: network access, cancellation, and batch
// versus per-address reads.
//
// It is a skeleton today. Nothing is implemented, and the module deliberately
// carries no require on core yet, because nothing here imports it.
package kv
