// Package env loads configuration from environment variables into a Go struct.
//
//	cfg, err := ferry.Load[Config](ctx, env.New())
//
// # Variable names come from the tags
//
// Each part of a field's address is upper-cased, every byte an environment
// variable name cannot hold becomes an underscore, and nested fields are joined
// with a separator that defaults to "_". So a field tagged name reads NAME,
// a nested db.host reads DB_HOST, and a field tagged feature-flags reads
// FEATURE_FLAGS. Slices and maps read the names that are already there: TAGS_0
// and TAGS_1 fill a []string, and LIMITS_RPS fills a map under the key rps.
//
// Two fields can end up wanting one variable name, because "." and "-" both
// become "_" as well. When that happens the load fails before reading anything
// and names both fields. Rename one, or widen the join with [Separator].
//
// # Set but empty is not the same as unset
//
// FOO= loads as the empty string, and FOO not being set at all is a different
// observation: a field tagged required is satisfied by TOKEN= and fails when
// TOKEN is unset. Nothing else about a value's type survives the trip, because
// an environment variable is text and this plane holds no type information of
// its own.
//
// # There is no way to write back
//
// This package loads only. Nothing in it implements [ferry.Sink], so
// [ferry.Dump] with this package does not compile rather than failing at run
// time. Setting the running process's own environment is rarely what anyone
// wants, and writing a .env file is a different job for a different package.
//
// The design records behind these decisions are in docs/adr/.
package env
