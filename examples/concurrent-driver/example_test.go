package concurrentdriver_test

import (
	"context"
	"fmt"

	"github.com/onhotpath/ferry"
	concurrentdriver "github.com/onhotpath/ferry/examples/concurrent-driver"
)

// Config is the struct both examples load: four addresses over one plane, which
// is a shape a serial walk pays for four round trips.
type Config struct {
	Host  string `ferry:"host,required"`
	Port  int    `ferry:"port,default=5432"`
	Users string `ferry:"users"`
	Cache string `ferry:"cache"`
}

func contents() map[ferry.Path]ferry.Value {
	return map[ferry.Path]ferry.Value{
		ferry.At("host"):  ferry.String("db1"),
		ferry.At("port"):  ferry.Number("5432"),
		ferry.At("users"): ferry.String("users-svc"),
		ferry.At("cache"): ferry.String("cache-svc"),
	}
}

// The caller's half: one Option. Overlap happens because the plane's instance
// declared it tolerates overlap and the caller granted a budget; either one
// missing is a serial walk. The same budget reached the driver behind its own
// open, where a driver that batches would spend it.
func Example() {
	plane := concurrentdriver.New(contents(), 4)

	cfg, err := ferry.Load[Config](context.Background(), plane, ferry.MaxConcurrency(4))
	if err != nil {
		fmt.Println("load:", err)

		return
	}

	fmt.Printf("%s:%d over %s and %s\n", cfg.Host, cfg.Port, cfg.Users, cfg.Cache)
	fmt.Println("the driver's open read a budget of", plane.Budget())
	fmt.Println("reads open at once:", plane.Peak())

	// Output:
	// db1:5432 over users-svc and cache-svc
	// the driver's open read a budget of 4
	// reads open at once: 4
}

// The same Option over a plane whose instance declares nothing. Absence of the
// capability is a serial walk, so no driver changes behaviour because a caller
// set the Option, and the value that comes back is the same one.
func Example_withoutTheCapability() {
	plane := concurrentdriver.NewSerial(contents())

	cfg, err := ferry.Load[Config](context.Background(), plane, ferry.MaxConcurrency(4))
	if err != nil {
		fmt.Println("load:", err)

		return
	}

	fmt.Printf("%s:%d over %s and %s\n", cfg.Host, cfg.Port, cfg.Users, cfg.Cache)
	fmt.Println("reads open at once:", plane.Peak())

	// Output:
	// db1:5432 over users-svc and cache-svc
	// reads open at once: 1
}
