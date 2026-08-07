package planetransfer_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/examples/planetransfer"
)

// Service is the annotated struct both planes are described by. One type,
// two directions, and neither plane appears in it.
type Service struct {
	Host   string            `ferry:"host"`
	Port   int               `ferry:"port"`
	Tags   []string          `ferry:"tags"`
	Labels map[string]string `ferry:"labels"`
}

// Example is the whole of plane-to-plane transfer: load the struct out of one
// plane, dump it into another.
//
// The two verbs are ferry's own and there is nothing between them. Swap either
// plane for a module under driver/ and these three lines do not move.
func Example() {
	ctx := context.Background()

	from := planetransfer.New(map[ferry.Path]ferry.Value{
		ferry.At("host"):            ferry.String("db1.internal"),
		ferry.At("port"):            ferry.Number("5432"),
		ferry.At("tags").Elem(0):    ferry.String("primary"),
		ferry.At("tags").Elem(1):    ferry.String("eu-west"),
		ferry.At("labels", "owner"): ferry.String("platform"),
	})

	to := planetransfer.New(nil)

	cfg, err := ferry.Load[Service](ctx, from.Source())
	if err != nil {
		fmt.Println("load:", err)

		return
	}

	if err := ferry.Dump(ctx, cfg, to.Sink()); err != nil {
		fmt.Println("dump:", err)

		return
	}

	fmt.Print(to.Contents())
	// Output:
	// /host = string("db1.internal")
	// /labels/owner = string("platform")
	// /port = number("5432")
	// /tags#0 = string("primary")
	// /tags#1 = string("eu-west")
}

// Example_throughTheStruct is what the trip through the struct costs, which is
// the part of a transfer worth knowing before running one.
//
// The source plane holds one address the type does not name and one empty
// sequence. The first does not cross, because the struct is the whole of what
// moves; the second arrives as a null, because a nil slice and an empty one are
// one value.
func Example_throughTheStruct() {
	ctx := context.Background()

	from := planetransfer.New(map[ferry.Path]ferry.Value{
		ferry.At("host"):    ferry.String("db1.internal"),
		ferry.At("port"):    ferry.Number("5432"),
		ferry.At("comment"): ferry.String("written by hand, and not in the struct"),
	})

	to := planetransfer.New(nil)

	cfg, err := ferry.Load[Service](ctx, from.Source())
	if err != nil {
		fmt.Println("load:", err)

		return
	}

	if err := ferry.Dump(ctx, cfg, to.Sink()); err != nil {
		fmt.Println("dump:", err)

		return
	}

	fmt.Print(to.Contents())

	// And the null loads back as the nil it came from, so the transfer is
	// stable: run it again and the destination does not change.
	back, err := ferry.Load[Service](ctx, to.Source())
	if err != nil {
		fmt.Println("load back:", err)

		return
	}

	fmt.Println("tags are nil again:", back.Tags == nil)
	// Output:
	// /host = string("db1.internal")
	// /labels = null
	// /port = number("5432")
	// /tags = null
	// tags are nil again: true
}

// Example_refused is the third consequence: a value the type refuses fails the
// transfer rather than crossing.
//
// The plane holds its own null at an address the struct maps to an int, which
// is presence carrying a value no int can hold. Nothing is written to the
// destination, because the load never produces a value to dump.
func Example_refused() {
	ctx := context.Background()

	from := planetransfer.New(map[ferry.Path]ferry.Value{
		ferry.At("host"): ferry.String("db1.internal"),
		ferry.At("port"): ferry.Null,
	})

	_, err := ferry.Load[Service](ctx, from.Source())

	fmt.Println(refusal(err))
	// Output:
	// /port refused: true
}

// refusal reports one failure's address and its class, which is what a caller
// matches on: the message text is not API.
func refusal(err error) string {
	for _, e := range ferry.Elements(err) {
		fe, ok := errors.AsType[*ferry.Error](e)
		if !ok {
			continue
		}

		return fmt.Sprintf("%s refused: %t", fe.Address(), errors.Is(e, ferry.ErrValue))
	}

	return "nothing refused"
}
