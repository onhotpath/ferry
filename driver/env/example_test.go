package env_test

import (
	"context"
	"fmt"
	"os"

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
