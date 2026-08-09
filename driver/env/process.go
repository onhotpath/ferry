package env

import "os"

// Process is where the optional second half of a dump writes: the environment of
// a running process.
//
// Two methods rather than one, and the second is load-bearing. A save that
// shortens a slice writes TAGS_0 to the file and leaves TAGS_1 and TAGS_2 with
// nowhere to go; the file's copies are swept, and without Unsetenv the process
// serves them back and the next load reads a three-element slice. A map that
// lost a key is the same mechanism.
//
// The implementation a caller usually wants is the running process itself, which
// is what [Setenv] uses when it is given nothing.
//
// A test that supplies its own is four lines, and it is deliberately not part of
// this package:
//
//	type tEnv struct{ t *testing.T }
//
//	func (e tEnv) Setenv(k, v string) error { e.t.Setenv(k, v); return nil }
//	func (e tEnv) Unsetenv(k string) error  { return os.Unsetenv(k) }
//
// Unsetenv must really unset. testing.T.Setenv restores the value it saved when
// the test cleans up, so calling os.Unsetenv during it is safe, and returning
// nil without unsetting reintroduces exactly the case above. Note also that
// testing.T.Setenv forbids testing.T.Parallel, which is why [Environ] exists and
// why this package's own tests use a fake rather than the running process.
type Process interface {
	Setenv(name, value string) error
	Unsetenv(name string) error
}

// Setenv makes a dump apply itself to a running process as well as to the file.
//
//	err := ferry.Dump(ctx, cfg, env.NewDotEnvSink(".env", env.Setenv(nil)))
//
// A nil argument is the running process, through [os.Setenv] and [os.Unsetenv].
// Anything else is where the writes go instead, which is what a test supplies.
//
// It is off by default, because changing a process's own environment is visible
// to every goroutine in it and to every child it starts afterwards, and that is
// something a caller should have named.
//
// It exists because the plane is a composite and a save that writes only half of
// it leaves the two halves disagreeing. Without it, a dump writes DB_HOST to the
// file while the process still exports the old DB_HOST, and the next load
// answers with the old one: the save looks as though it did nothing.
//
// The process half runs after the file has been replaced, so a save that could
// not write the file has not already changed the environment.
func Setenv(p Process) SinkOption {
	return sinkOnly(func(c *sinkConfig) {
		if p == nil {
			p = osProcess{}
		}

		c.proc = p
	})
}

// osProcess is the running process's own environment, and it is what [Setenv] is
// given nothing for.
type osProcess struct{}

func (osProcess) Setenv(name, value string) error { return os.Setenv(name, value) }
func (osProcess) Unsetenv(name string) error      { return os.Unsetenv(name) }
