package ferryhttp_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/onhotpath/ferry"
	ferryhttp "github.com/onhotpath/ferry/driver/http"
)

// Filter is the schema the query example loads, and the names its tags carry are
// the query parameters the driver looks for.
type Filter struct {
	Q     string   `ferry:"q,required"`
	Tags  []string `ferry:"tags"`
	Limit int      `ferry:"limit,default=25"`
}

// Example loads a small annotated struct out of one request's query parameters.
//
// The source is built once and is safe to keep in a handler's closure: it holds
// no request, and the values travel in the context instead. A real handler
// passes r.Context() and r.URL.Query(); this one parses a query string so that
// the example is self-contained and runs the same everywhere.
func Example() {
	src := ferryhttp.NewQuerySource() // once, at start-up

	values, err := url.ParseQuery("q=ferry&tags=go&tags=config")
	if err != nil {
		fmt.Println(err)

		return
	}

	f, err := ferry.Load[Filter](ferryhttp.WithQuery(context.Background(), values), src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", f)
	// Output: {Q:ferry Tags:[go config] Limit:25}
}

// Tenant is the schema the header example loads. A hyphen is how a header
// nests, so x-tenant and its two fields are X-Tenant-Id and X-Tenant-Region.
type Tenant struct {
	ID     string `ferry:"id,required"`
	Region string `ferry:"region,default=eu-west-1"`
}

// Example_headers loads out of one request's header fields.
//
// Field names are matched the way net/http spells them, which is
// case-insensitively, so the tag may be written in whatever case reads best.
func Example_headers() {
	src := ferryhttp.NewHeaderSource()

	h := http.Header{}
	h.Set("X-Tenant-Id", "acme")

	type request struct {
		Tenant Tenant `ferry:"x-tenant"`
	}

	req, err := ferry.Load[request](ferryhttp.WithHeaders(context.Background(), h), src)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Printf("%+v\n", req.Tenant)
	// Output: {ID:acme Region:eu-west-1}
}

// Example_repeatedName shows what a parameter occurring more than once means.
//
// It is a sequence and never a value that happened to arrive twice, so reading
// it into a string field is refused rather than silently taking the first.
//
// The two things a handler answering 400 needs are printed here: which parameter
// it is about, and which refusal it is. The message says the rest, and it never
// quotes what the parameter held.
func Example_repeatedName() {
	type page struct {
		Sort string `ferry:"sort"`
	}

	values, err := url.ParseQuery("sort=name&sort=age")
	if err != nil {
		fmt.Println(err)

		return
	}

	_, err = ferry.Load[page](ferryhttp.WithQuery(context.Background(), values), ferryhttp.NewQuerySource())

	var located *ferry.Error
	if errors.As(err, &located) {
		fmt.Println(located.Address())
	}

	fmt.Println(errors.Is(err, ferryhttp.ErrRepeated))
	// Output:
	// /sort
	// true
}

// Example_noRequestInTheContext shows the refusal a handler that forgot
// [ferryhttp.WithQuery] gets.
//
// It is refused before anything is read, because a load answered from nothing
// would report every field missing and a required field would fail for a request
// that supplied it.
func Example_noRequestInTheContext() {
	_, err := ferry.Load[Filter](context.Background(), ferryhttp.NewQuerySource())

	fmt.Println(errors.Is(err, ferryhttp.ErrNoQuery))
	fmt.Println(errors.Is(err, ferry.ErrPlane))
	// Output:
	// true
	// true
}
