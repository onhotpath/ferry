package httpdecisions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/onhotpath/ferry"
)

// This file is not one of the four questions. It isolates a core defect
// question 4 walked into, in the smallest source that shows it and with no
// query string or header anywhere near it.
//
// A driver that returns more than one located failure from Close loses all but
// the first, silently. core's fromDriver reads the address off the first
// ferry.ErrorAt carrier it finds and then replaces the cause with that
// carrier's own error, so the errors.Join that held the rest is discarded.

// twoAtClose is a minimal Source whose reader answers everything Absent and
// reports two located failures at Close.
type twoAtClose struct{ join bool }

func (s twoAtClose) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) {
		return closeTwice{join: s.join}, nil
	}, nil
}

type closeTwice struct{ join bool }

func (closeTwice) Get(context.Context, ferry.Path) (ferry.Value, error) {
	return ferry.Value{}, nil
}

func (c closeTwice) Close() error {
	first := ferry.ErrorAt(ferry.Path{}.At("q"), fmt.Errorf("%w: the first failure", ferry.ErrPlane))
	second := ferry.ErrorAt(ferry.Path{}.At("r"), fmt.Errorf("%w: the second failure", ferry.ErrPlane))

	if !c.join {
		return first
	}

	return errors.Join(first, second)
}

// TestCoreDropsAllButTheFirstLocatedCloseFailure is the defect, asserted.
func TestCoreDropsAllButTheFirstLocatedCloseFailure(t *testing.T) {
	type schema struct {
		Q string `ferry:"q"`
		R string `ferry:"r"`
	}

	for _, join := range []bool{false, true} {
		_, err := ferry.Load[schema](context.Background(), twoAtClose{join: join})

		t.Logf("")
		t.Logf("=== Close returns %s ===", map[bool]string{false: "one ErrorAt", true: "errors.Join of two ErrorAt"}[join])
		t.Logf("%%+v: %+v", err)
		t.Logf("Elements()  = %d", len(ferry.Elements(err)))
		t.Logf("Address()   = %q", addressOf(err))
		t.Logf("mentions the first failure  = %v", strings.Contains(fmt.Sprint(err), "the first failure"))
		t.Logf("mentions the second failure = %v", strings.Contains(fmt.Sprint(err), "the second failure"))
	}
}

// TestTheSameJoinWithoutErrorAtSurvives is the control: the loss is caused by
// ferry.ErrorAt and not by errors.Join, so a driver that wants both findings
// reported today has to give up the address on both.
func TestTheSameJoinWithoutErrorAtSurvives(t *testing.T) {
	_, err := ferry.Load[struct {
		Q string `ferry:"q"`
	}](context.Background(), plainJoin{})

	t.Logf("%%+v: %+v", err)
	t.Logf("mentions the first failure  = %v", strings.Contains(fmt.Sprint(err), "the first failure"))
	t.Logf("mentions the second failure = %v", strings.Contains(fmt.Sprint(err), "the second failure"))
}

type plainJoin struct{}

func (plainJoin) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return func(context.Context) (ferry.Reader, error) { return closeJoinNoAddr{}, nil }, nil
}

type closeJoinNoAddr struct{}

func (closeJoinNoAddr) Get(context.Context, ferry.Path) (ferry.Value, error) {
	return ferry.Value{}, nil
}

func (closeJoinNoAddr) Close() error {
	return errors.Join(
		fmt.Errorf("%w: the first failure at /q", ferry.ErrPlane),
		fmt.Errorf("%w: the second failure at /r", ferry.ErrPlane),
	)
}

// TestTheSameLossAtBind is where the defect bites the use case ferry.ErrorAt's
// own doc comment names: a driver refusing over a whole address set knows which
// members it disliked, and core keeps one.
func TestTheSameLossAtBind(t *testing.T) {
	_, err := ferry.Load[struct {
		Q string `ferry:"q"`
		R string `ferry:"r"`
	}](context.Background(), refusingBind{})

	t.Logf("%%+v: %+v", err)
	t.Logf("Elements()  = %d", len(ferry.Elements(err)))
	t.Logf("mentions /q = %v", strings.Contains(fmt.Sprint(err), "the first failure"))
	t.Logf("mentions /r = %v", strings.Contains(fmt.Sprint(err), "the second failure"))
}

type refusingBind struct{}

func (refusingBind) Bind(*ferry.AddressSet) (ferry.OpenFunc, error) {
	return nil, errors.Join(
		ferry.ErrorAt(ferry.Path{}.At("q"), fmt.Errorf("%w: the first failure", ferry.ErrPlane)),
		ferry.ErrorAt(ferry.Path{}.At("r"), fmt.Errorf("%w: the second failure", ferry.ErrPlane)),
	)
}
