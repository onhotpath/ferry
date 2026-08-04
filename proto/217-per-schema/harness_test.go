package perschema_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/onhotpath/ferry"
	ps "github.com/onhotpath/ferry/proto/perschema"
)

// The two schemas #210's finding is stated against: one name, one plane, two Go
// types.
type (
	// Encodings reads the name as the sequence it is on the wire.
	Encodings struct {
		Encodings []string `ferry:"accept-encoding"`
	}

	// Encoding reads the same name as one value.
	Encoding struct {
		Encoding string `ferry:"accept-encoding"`
	}
)

// The schemas the alias, required and fallback declarations are stated against.
type (
	// Trace names one field, and is the schema every declaration below is
	// correct for.
	Trace struct {
		ID string `ferry:"trace-id"`
	}

	// TraceAndLegacy names the alias's target as well, so the renamed key space
	// is not injective.
	TraceAndLegacy struct {
		ID     string `ferry:"trace-id"`
		Legacy string `ferry:"x-trace-id"`
	}

	// Other names none of them.
	Other struct {
		Agent string `ferry:"user-agent"`
	}
)

// The schemas the AddressSet probe is stated against.
type (
	// Static holds a container whose members come from the type.
	Static struct {
		DB struct {
			Host string `ferry:"host"`
			Port int    `ferry:"port"`
		} `ferry:"db"`
	}

	// Dynamic holds a container whose members come from the value.
	Dynamic struct {
		Tags   []string          `ferry:"tags"`
		Limits map[string]string `ferry:"limits"`
	}

	// Leaf holds neither.
	Leaf struct {
		Tags   string `ferry:"tags"`
		Limits string `ferry:"limits"`
	}
)

func ctxWith(pairs ...[2]string) context.Context {
	return ps.WithHeaders(context.Background(), ps.Header(pairs...))
}

func gzip() [2]string { return [2]string{"Accept-Encoding", "gzip"} }

func br() [2]string { return [2]string{"Accept-Encoding", "br"} }

// outcome is one line saying what a load produced: the value, or the error's
// class and its one-line text.
func outcome[T any](v T, err error) string {
	if err == nil {
		return fmt.Sprintf("%+v", v)
	}

	return class(err) + ": " + err.Error()
}

// class is which of ADR-0011's sentinels an error carries.
func class(err error) string {
	var out []string

	for _, c := range []struct {
		name string
		err  error
	}{
		{"ErrSchema", ferry.ErrSchema},
		{"ErrMissing", ferry.ErrMissing},
		{"ErrValue", ferry.ErrValue},
		{"ErrPlane", ferry.ErrPlane},
		{"ErrDriver", ferry.ErrDriver},
	} {
		if errors.Is(err, c.err) {
			out = append(out, c.name)
		}
	}

	if len(out) == 0 {
		return "(no ferry class)"
	}

	return strings.Join(out, "+")
}

// full is everything a caller sees when it goes wrong: the class, the number of
// elements, and the whole of %+v.
func full(err error) string {
	if err == nil {
		return "        (no error)"
	}

	els := ferry.Elements(err)

	var b strings.Builder

	fmt.Fprintf(&b, "        class    = %s\n", class(err))
	fmt.Fprintf(&b, "        Elements = %d\n", len(els))

	for i, e := range els {
		at := "(none)"
		if a := addressOf(e); a != "" {
			at = a
		}

		fmt.Fprintf(&b, "          [%d] at %s  %s\n", i, at, e)
	}

	fmt.Fprintf(&b, "        %%+v:\n")

	for _, line := range strings.Split(strings.TrimRight(fmt.Sprintf("%+v", err), "\n"), "\n") {
		fmt.Fprintf(&b, "        | %s\n", line)
	}

	return strings.TrimRight(b.String(), "\n")
}

func addressOf(err error) string {
	var fe *ferry.Error
	if errors.As(err, &fe) {
		return fe.Address().String()
	}

	return ""
}

// loadBoth runs one schema through both entry points over one Source, which is
// the comparison #210 made and this prototype reproduces on a new base.
func loadBoth[T any](src ferry.Source, h http.Header) (string, string) {
	ctx := ps.WithHeaders(context.Background(), h)

	oneShot := outcome(ferry.Load[T](ctx, src))

	b, err := ferry.Bind[T](src)
	if err != nil {
		return oneShot, class(err) + ": " + err.Error()
	}

	return oneShot, outcome(b.Load(ctx))
}
