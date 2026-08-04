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
