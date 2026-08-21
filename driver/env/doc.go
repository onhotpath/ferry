// Package env loads configuration from environment variables and .env files into
// a Go struct, and writes a .env file back.
//
//	cfg, err := ferry.Load[Config](ctx, env.New())
//	cfg, err := ferry.Load[Config](ctx, env.New(env.DotEnv()))
//	err = ferry.Dump(ctx, cfg, env.NewDotEnvSink(".env"))
//
// # One plane, in layers
//
// The process environment and a .env file share one namespace and one name, so
// they are one plane here rather than two. The process is the anchor and always
// wins; the files [DotEnv] names are layers underneath it, in the order they were
// named, each winning over the one before. A file that is not there is an empty
// layer, and a file that is there and does not parse is a refusal.
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
// # Writing a file back
//
// [DotEnvSink] saves a struct into a .env file, and a save is a merge: the
// variables your struct maps are replaced where they stand, and the comments, the
// order, the "export " prefixes, the spacing and every variable no field of yours
// maps are all left as they were. The write is atomic and the file is read before
// it is written, so a save that fails leaves your file byte for byte as it was.
//
// # Reloading when a file changes
//
// [Source.Watched] converts a source into one [ferry.BindWatched] takes, and the
// stream it hands back opens with a load and yields a freshly loaded value every
// time a file [DotEnv] named changes. The changes come from the operating
// system's own file notifications, so a hand edit lands without polling latency,
// and one save is one reload.
//
// # Sharp edges
//
// These are the ones that cost time in production rather than at the keyboard,
// and every one of them comes from the same fact: the process environment is
// above the files, and it is not yours.
//
// A save can look as though it did nothing. [ferry.Dump] writes DB_HOST to the
// file, the running process still exports the DB_HOST it started with, and the
// next load answers with that one. Pass [Setenv] to make a save write both halves.
//
// A save replaces the file and not the union. Clearing a slice removes its
// variables from the file, and without [Setenv] the process goes on serving the
// ones it holds.
//
// An ambient variable can invent a map key or make a container look present.
// A map's and a slice's members are whatever the environment holds under their
// name, over the union of every layer, so TAGS_5 exported by somebody else adds
// a sixth element to a slice the file gives two of.
//
// Ambient names collide with short field names. A field tagged path reads PATH
// and one tagged home reads HOME, and what a file says about either is what gets
// silently overridden.
//
// A report names the variable, and editing the file may not fix it. The name a
// report opens with is a function of the address and this driver's settings, so it
// cannot say that the process is shadowing the file.
//
// "export " is kept on a line that has it and is not added to a new one, so a
// shell sourcing the file gets a variable its own children do not.
//
// The sink writes the file a symlink names, so a deployment that swaps the link
// between two saves sends them to two different files.
//
// The way out of the first five is env.Environ(func() []string { return nil }),
// which makes the files the whole plane.
//
// The design records behind these decisions are in docs/adr/.
package env
