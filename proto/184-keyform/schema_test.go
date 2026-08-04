package keyform

import (
	"context"
	"errors"
	"testing"

	"github.com/onhotpath/ferry"
)

// The schemas issue #184 asks for.

type Flat1 struct {
	Q    string `ferry:"q"`
	Page int    `ferry:"page"`
}

type DB2 struct {
	Host string `ferry:"host"`
	Port int    `ferry:"port"`
}

type Nested2 struct {
	DB DB2 `ferry:"db"`
}

type Cred struct {
	User string `ferry:"user"`
}

type DB3 struct {
	Host string `ferry:"host"`
	Auth Cred   `ferry:"auth"`
}

type Nested3 struct {
	DB DB3 `ferry:"db"`
}

type Slicey struct {
	Tags []string `ferry:"tags"`
}

type Mappy struct {
	Limits map[string]int `ferry:"limits"`
}

type Header1 struct {
	RequestID string `ferry:"x-request-id"`
	Auth      string `ferry:"authorization"`
}

// Forwarded is the case that decides whether a header plane nests: the
// registry's own multi-word field names are already a hyphen join, so a nested
// struct spells X-Forwarded-For and X-Forwarded-Proto exactly.
type Forwarded struct {
	For   string `ferry:"for"`
	Proto string `ferry:"proto"`
}

type HeaderNested struct {
	XForwarded Forwarded `ferry:"x-forwarded"`
}

// Collision candidates.

type BracketCollider struct {
	A  DB2    `ferry:"a"`
	AB string `ferry:"a[host]"`
}

type FlatCollider struct {
	A  DB2    `ferry:"a"`
	AB string `ferry:"a.host"`
}

type HeaderHyphenCollider struct {
	X    Req    `ferry:"x"`
	Flat string `ferry:"x-request-id"`
}

type Req struct {
	Request ReqID `ferry:"request"`
}

type ReqID struct {
	ID string `ferry:"id"`
}

// captureSource is how the prototype gets at the AddressSet a schema
// determines: it is a real ferry.Source whose Bind keeps what core hands it.
type captureSource struct{ addrs *ferry.AddressSet }

func (c *captureSource) Bind(a *ferry.AddressSet) (ferry.OpenFunc, error) {
	c.addrs = a

	return nil, errStop
}

var errStop = errors.New("captured")

func addrsOf[T any](t *testing.T) *ferry.AddressSet {
	t.Helper()

	c := &captureSource{}
	if _, err := ferry.Load[T](context.Background(), c); !errors.Is(err, errStop) {
		t.Fatalf("capture: %v", err)
	}

	return c.addrs
}
