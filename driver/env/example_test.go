package env_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/env"
)

// Config is the schema the example below loads, and the addresses its tags name
// are what the driver folds into environment variable names.
type Config struct {
	Name string `ferry:"name,required"`
	DB   DB     `ferry:"db"`
}

// DB is the nested struct, and the reason DB_HOST is one name rather than two.
type DB struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port,default=5432"`
}

// Example loads a small annotated struct out of the process environment.
//
// It sets the variables itself so that the example is self-contained and runs
// the same everywhere. Ordinary use sets nothing: the environment is already
// there, and env.New() reads it. Every other test in this package injects its
// environment through [env.Environ] instead, which is what a hermetic test
// wants; this one is deliberately the plain call a user writes.
func Example() {
	os.Setenv("NAME", "checkout")
	os.Setenv("DB_HOST", "db.internal")
	os.Unsetenv("DB_PORT") // not set at all, so the default applies

	cfg, err := ferry.Load[Config](context.Background(), env.New())
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Name:checkout DB:{Host:db.internal Port:5432}}
}

// Service is the same idea as [Config] under a different tag key: the fields
// carry `env` rather than `ferry`, which is what ferry.TagKey names.
type Service struct {
	Name    string `env:"service,required"`
	Timeout int    `env:"timeout,default=30"`
}

// Example_tagKey reads the struct tag key `env` instead of the default `ferry`.
//
// ferry.TagKey is core's Option and is not this driver's, so it renames the key
// for every type in the call and for every plane, not only for this one. It
// names where to look and never what the content means: the name, required and
// default= inside the tag are unchanged.
func Example_tagKey() {
	os.Setenv("SERVICE", "checkout")
	os.Unsetenv("TIMEOUT") // not set at all, so the default applies

	svc, err := ferry.Load[Service](context.Background(), env.New(), ferry.TagKey("env"))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", svc)
	// Output: {Name:checkout Timeout:30}
}

// ExampleRootVar loads a schema that is one value, from the variable named for
// it.
//
// The root of such a schema carries no segment, so there is nothing for this
// driver to fold a name out of and no field or map key could ever produce one.
// Without the option the load is refused at Bind, before anything is read.
func ExampleRootVar() {
	environ := func() []string { return []string{"APP_PORT=8080"} }

	port, err := ferry.Load[int](context.Background(), env.New(env.Environ(environ), env.RootVar("APP_PORT")))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(port)
	// Output: 8080
}

// Feature is a schema with a boolean an operator writes in words.
type Feature struct {
	Enabled bool `ferry:"enabled"`
}

// ExampleBoolWords loads a bool from an environment that spells one on and off.
//
// Without the option the field takes true and false and nothing else, because
// that is what a bool's own parser reads. With it, this environment's own words
// are what a boolean is spelled with here, and the four below are accepted while
// on is the one a true is written as.
func ExampleBoolWords() {
	environ := func() []string { return []string{"ENABLED=on"} }

	cfg, err := ferry.Load[Feature](context.Background(),
		env.New(env.Environ(environ), env.BoolWords("on", "off", "true", "false")))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Enabled:true}
}

// ExampleDotEnv layers a .env file underneath the process environment.
//
// The file fills in what the process does not say, and the process wins wherever
// both say something. Naming several files layers them in order, lowest first.
func ExampleDotEnv() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("# the box the database is on\nDB_HOST=db.internal\nDB_PORT=6543\n"), 0o600)

	environ := func() []string { return []string{"NAME=checkout", "DB_PORT=5432"} }

	cfg, err := ferry.Load[Config](context.Background(), env.New(env.Environ(environ), env.DotEnv(path)))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", cfg)
	// Output: {Name:checkout DB:{Host:db.internal Port:5432}}
}

// ExampleNewDotEnvSink saves a struct back into a .env file somebody else wrote.
//
// The variables the struct maps are replaced where they stand. The comment, the
// order, the export prefix and the variable no field maps are all left as they
// were, which is what makes a hand-maintained file survive being written back.
func ExampleNewDotEnvSink() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("# the box the database is on\nexport DB_HOST=old\nUNRELATED=keep me\n"), 0o600)

	cfg := Config{Name: "checkout", DB: DB{Host: "db.internal", Port: 5432}}

	if err := ferry.Dump(context.Background(), cfg, env.NewDotEnvSink(path)); err != nil {
		fmt.Println(err)

		return
	}

	saved, _ := os.ReadFile(path)
	fmt.Print(string(saved))
	// Output:
	// # the box the database is on
	// export DB_HOST=db.internal
	// DB_PORT=5432
	// UNRELATED=keep me
	// NAME=checkout
}

// ExampleSetenv saves the file and the running process together, so that the two
// halves of the plane agree afterwards.
//
// Without it the file would hold the new value and the next load would answer
// with the old one, because the process is the layer above every file. The
// process here is a stand-in rather than the running one, which is what an
// example that changes nothing outside itself wants; env.Setenv(nil) names the
// real thing.
func ExampleSetenv() {
	dir, _ := os.MkdirTemp("", "ferry-env")
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, ".env")
	vars := map[string]string{"NAME": "old", "DB_HOST": "old"}

	cfg := Config{Name: "checkout", DB: DB{Host: "db.internal", Port: 5432}}

	if err := ferry.Dump(context.Background(), cfg, env.NewDotEnvSink(path, env.Setenv(fake{vars}))); err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(vars["NAME"], vars["DB_HOST"])
	// Output: checkout db.internal
}

// fake is the stand-in process the example above writes to.
type fake struct{ vars map[string]string }

func (f fake) Setenv(name, value string) error { f.vars[name] = value; return nil }
func (f fake) Unsetenv(name string) error      { delete(f.vars, name); return nil }
