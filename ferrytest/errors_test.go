package ferrytest_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// errCfg fails three ways at once: two required addresses the plane is silent
// at, and one it holds garbage at. Three is the smallest number that tells an
// exact-set assertion apart from a contains one, because two of them can be
// named while the third is not.
type errCfg struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,required"`
	Rate int    `ferry:"rate"`
}

// threeFailures is one fresh load per subtest, through the seam, so that every
// element under assertion is one ferry itself minted.
func threeFailures(t *testing.T) error {
	t.Helper()

	_, err := ferry.Load[errCfg](context.Background(), ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("rate"): ferry.String("abc"),
	}))
	if err == nil {
		t.Fatal("the load reported nothing, and every case in this file is about what it reports")
	}

	return err
}

func TestDiffErrors(t *testing.T) {
	cases := []struct {
		name string
		want []ferrytest.Want
		diff []string
	}{
		{
			name: "the exact set is no difference at all",
			want: []ferrytest.Want{
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
				{Address: ferry.At("port"), Class: ferry.ErrMissing},
				{Address: ferry.At("rate"), Class: ferry.ErrValue},
			},
		},
		{
			name: "order does not matter, because it is a set",
			want: []ferrytest.Want{
				{Address: ferry.At("rate"), Class: ferry.ErrValue},
				{Address: ferry.At("port"), Class: ferry.ErrMissing},
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
			},
		},
		{
			name: "two wanted where three arrived names the third",
			want: []ferrytest.Want{
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
				{Address: ferry.At("port"), Class: ferry.ErrMissing},
			},
			diff: []string{
				"got /rate: invalid value, and nothing wanted it: " +
					"ferry: /rate: the plane's value is not a valid int",
			},
		},
		{
			name: "an address the call did not fail at",
			want: []ferrytest.Want{
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
				{Address: ferry.At("port"), Class: ferry.ErrMissing},
				{Address: ferry.At("rate"), Class: ferry.ErrValue},
				{Address: ferry.At("db", "timeout"), Class: ferry.ErrValue},
			},
			diff: []string{"want /db/timeout: invalid value, and nothing reported it"},
		},
		{
			name: "the right address under the wrong class is both halves of the diff",
			want: []ferrytest.Want{
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
				{Address: ferry.At("port"), Class: ferry.ErrMissing},
				{Address: ferry.At("rate"), Class: ferry.ErrMissing},
			},
			diff: []string{
				"got /rate: invalid value, and nothing wanted it: " +
					"ferry: /rate: the plane's value is not a valid int",
				"want /rate: missing, and nothing reported it",
			},
		},
		{
			name: "one Want cannot cover two failures",
			want: []ferrytest.Want{
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
				{Address: ferry.At("rate"), Class: ferry.ErrValue},
			},
			diff: []string{
				"got /port: missing, and nothing wanted it: " +
					"ferry: /port: required, and the plane holds nothing at this address",
			},
		},
		{
			name: "no Want at all is every failure unexpected",
			diff: []string{
				"got /host: missing, and nothing wanted it: " +
					"ferry: /host: required, and the plane holds nothing at this address",
				"got /port: missing, and nothing wanted it: " +
					"ferry: /port: required, and the plane holds nothing at this address",
				"got /rate: invalid value, and nothing wanted it: " +
					"ferry: /rate: the plane's value is not a valid int",
			},
		},
		{
			name: "a Want with no class is named as the mistake it is",
			want: []ferrytest.Want{
				{Address: ferry.At("host"), Class: ferry.ErrMissing},
				{Address: ferry.At("port"), Class: ferry.ErrMissing},
				{Address: ferry.At("rate"), Class: ferry.ErrValue},
				{Address: ferry.At("rate")},
			},
			diff: []string{"want /rate: no class, so nothing can match it"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ferrytest.DiffErrors(threeFailures(t), c.want...)
			if !slices.Equal(got, c.diff) {
				t.Errorf("diff\n\t%q\nwant\n\t%q", got, c.diff)
			}
		})
	}
}

// TestDiffErrorsNoFailure covers the two ends of the range: a call that failed
// at nothing and was expected to, and one that was not.
func TestDiffErrorsNoFailure(t *testing.T) {
	if got := ferrytest.DiffErrors(nil); got != nil {
		t.Errorf("diff %q, want none", got)
	}

	got := ferrytest.DiffErrors(nil, ferrytest.Want{Address: ferry.At("host"), Class: ferry.ErrMissing})
	want := []string{"want /host: missing, and nothing reported it"}

	if !slices.Equal(got, want) {
		t.Errorf("diff\n\t%q\nwant\n\t%q", got, want)
	}
}

// TestDiffErrorsForeignError is the element ferry did not build: it has no
// address to read and answers to no sentinel, and the report says so rather than
// pretending it landed at the root.
func TestDiffErrorsForeignError(t *testing.T) {
	got := ferrytest.DiffErrors(errors.New("something else entirely"))
	want := []string{"got (no address): no ferry class, and nothing wanted it: something else entirely"}

	if !slices.Equal(got, want) {
		t.Errorf("diff\n\t%q\nwant\n\t%q", got, want)
	}
}

// noWriteSink fails twice with no address at all: a commit that declares
// [ferry.ErrReadOnly] and a close that declares nothing.
type noWriteSink struct{}

func (noWriteSink) Bind(*ferry.AddressSet) (ferry.OpenWriterFunc, error) {
	return func(context.Context) (ferry.Writer, error) { return noWriteWriter{}, nil }, nil
}

type noWriteWriter struct{}

func (noWriteWriter) Set(context.Context, ferry.LeafAddr, ferry.Value) error { return nil }

func (noWriteWriter) Commit(context.Context) error {
	return fmt.Errorf("%w: the token has no write ACL", ferry.ErrReadOnly)
}

func (noWriteWriter) Close() error { return errors.New("flush failed") }

// TestDiffErrorsSubordinateSentinel is why the pairing is a matching rather than
// a greedy pass.
//
// Both failures have no address, so both are candidates for a Want that names
// none, and one of them answers to two sentinels where the other answers to one.
// A pass that let ErrPlane take the read-only failure first would leave ErrReadOnly
// with nothing and report a difference that is not there.
func TestDiffErrorsSubordinateSentinel(t *testing.T) {
	err := ferry.Dump(context.Background(), errCfg{Host: "h", Port: 1}, noWriteSink{})
	if err == nil {
		t.Fatal("the dump reported nothing, and this case is about what it reports")
	}

	for _, order := range [][]ferrytest.Want{
		{{Class: ferry.ErrPlane}, {Class: ferry.ErrReadOnly}},
		{{Class: ferry.ErrReadOnly}, {Class: ferry.ErrPlane}},
	} {
		if got := ferrytest.DiffErrors(err, order...); got != nil {
			t.Errorf("diff %q, want none", got)
		}
	}

	// The narrower sentinel twice is a genuine difference: only one of the two
	// failures declares it.
	got := ferrytest.DiffErrors(err, ferrytest.Want{Class: ferry.ErrReadOnly}, ferrytest.Want{Class: ferry.ErrReadOnly})
	want := []string{
		"got (no address): plane error, driver, and nothing wanted it: ferry: closing the plane: flush failed",
		"want (no address): plane is read only, and nothing reported it",
	}

	if !slices.Equal(got, want) {
		t.Errorf("diff\n\t%q\nwant\n\t%q", got, want)
	}
}

// indexed is where the report's ordering is visible: segment-wise, so index 7
// precedes index 10, where the rendered addresses sort the other way.
type indexed struct {
	Workers [11]int `ferry:"workers"`
}

func TestDiffErrorsOrdersSegmentWise(t *testing.T) {
	_, err := ferry.Load[indexed](context.Background(), ferrytest.Static(map[ferry.Path]ferry.Value{
		ferry.At("workers").Elem(7):  ferry.String("x"),
		ferry.At("workers").Elem(10): ferry.String("y"),
	}))
	if err == nil {
		t.Fatal("the load reported nothing, and this case is about the order it reports in")
	}

	got := ferrytest.DiffErrors(err)
	want := []string{
		"got /workers#7: invalid value, and nothing wanted it: " +
			"ferry: /workers#7: the plane's value is not a valid int",
		"got /workers#10: invalid value, and nothing wanted it: " +
			"ferry: /workers#10: the plane's value is not a valid int",
	}

	if !slices.Equal(got, want) {
		t.Errorf("diff\n\t%q\nwant\n\t%q", got, want)
	}
}

func TestCheckErrors(t *testing.T) {
	t.Run("an exact set reports nothing", func(t *testing.T) {
		var c capture

		ferrytest.CheckErrors(&c, threeFailures(t),
			ferrytest.Want{Address: ferry.At("host"), Class: ferry.ErrMissing},
			ferrytest.Want{Address: ferry.At("port"), Class: ferry.ErrMissing},
			ferrytest.Want{Address: ferry.At("rate"), Class: ferry.ErrValue},
		)

		if len(c.lines) != 0 {
			t.Errorf("reported %q, want nothing", c.lines)
		}

		if c.helpers != 1 {
			t.Errorf("called Helper %d times, want 1: without it every failure is attributed to a line "+
				"inside ferrytest", c.helpers)
		}
	})

	t.Run("one line per difference", func(t *testing.T) {
		var c capture

		ferrytest.CheckErrors(&c, threeFailures(t),
			ferrytest.Want{Address: ferry.At("host"), Class: ferry.ErrMissing},
		)

		want := []string{
			"got /port: missing, and nothing wanted it: " +
				"ferry: /port: required, and the plane holds nothing at this address",
			"got /rate: invalid value, and nothing wanted it: " +
				"ferry: /rate: the plane's value is not a valid int",
		}

		if !slices.Equal(c.lines, want) {
			t.Errorf("reported\n\t%q\nwant\n\t%q", c.lines, want)
		}
	})
}
