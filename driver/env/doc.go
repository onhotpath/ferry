// Package env will hold ferry's environment-variable driver, which ships
// outside core because environment variables have no honest Dump: the target
// people want is a .env file or an environ slice, and .env is a format, which
// is plane knowledge (ADR-0002).
//
// It is a skeleton today. Nothing is implemented, and the module deliberately
// carries no require on core yet, because nothing here imports it.
package env
