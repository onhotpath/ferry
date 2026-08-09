// Package proto holds the throwaway probes for #116. It never merges.
//
// The question: ADR-0005 sorts refusals into categories, puts uintptr in two of
// them at once, and the two differ in exactly one observable way - one offers
// registration as the remedy and the other says nothing lifts it. Which is it?
//
// Run: go test -v ./proto/
package proto

import (
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"testing"
	"unsafe"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/ferrytest"
)

// P1: does a uintptr survive a round trip through a plane, bit for bit?
//
// ADR-0005's category (c) asserts it does ("uintptr round-trips as a uint") and
// its category (a) asserts the opposite ("the value does not exist outside the
// process"). Only one of them can be right.
func TestP1UintptrRoundTrips(t *testing.T) {
	reg := mustRegistry(t, ferry.NumberValue(uintptrText, parseUintptr))

	obj := new([4]int)
	in := ptrConf{P: uintptr(unsafe.Pointer(obj)), Q: ^uintptr(0)}

	inst := ferrytest.MemPlane().Open()
	if err := ferry.Dump(t.Context(), in, inst.Sink, ferry.WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	back, err := ferry.Load[ptrConf](t.Context(), inst.Source, ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	t.Logf("a real heap address: in=%d out=%d identical=%v", in.P, back.P, in.P == back.P)
	t.Logf("the widest uintptr:  in=%d out=%d identical=%v", in.Q, back.Q, in.Q == back.Q)

	runtime.KeepAlive(obj)
}

// P2: the number survives, but does what it points at?
//
// This is the half category (a) is right about, and it is worth seeing rather
// than reasoning about, because the failure does not look like a failure.
func TestP2TheReferentDoesNot(t *testing.T) {
	reg := mustRegistry(t, ferry.NumberValue(uintptrText, parseUintptr))

	obj := new([4]int)
	obj[0] = 42

	collected := make(chan struct{})
	runtime.AddCleanup(obj, func(struct{}) { close(collected) }, struct{}{})

	inst := ferrytest.MemPlane().Open()
	if err := ferry.Dump(t.Context(), ptrConf{P: uintptr(unsafe.Pointer(obj))}, inst.Sink,
		ferry.WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	back, err := ferry.Load[ptrConf](t.Context(), inst.Source, ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	// The collector does not track a uintptr, so nothing kept obj alive on the
	// strength of the copy sitting in the plane.
	obj = nil

	runtime.GC()
	runtime.GC()

	select {
	case <-collected:
		t.Log("the object the address named was COLLECTED while the address sat in the plane")
	default:
		t.Log("not collected on this run")
	}

	revived := (*[4]int)(unsafe.Pointer(back.P)) //nolint:govet // the point of the probe
	t.Logf("dereferencing the round-tripped address still reads %d, which is the hazard: "+
		"a use-after-free that looks like a successful round trip", revived[0])
}

// P3: is a codec accepted for each of the four kinds ADR-0005 calls permanent?
//
// This is the probe the category boundary actually rests on, because ADR-0005
// says the sort is "tested against a registered codec rather than reasoned
// about".
func TestP3EveryPermanentKindTakesACodec(t *testing.T) {
	for _, c := range []struct {
		kind string
		reg  ferry.Registration
	}{
		{"uintptr", ferry.NumberValue(uintptrText, parseUintptr)},
		{"unsafe.Pointer", ferry.NumberValue(rawPtrText, parseRawPtr)},
		{"chan int", ferry.StringValue(chanText, parseChan)},
		{"func()", ferry.StringValue(funcText, parseFunc)},
	} {
		_, err := ferry.NewRegistry(c.reg)
		t.Logf("%-15s registers: err=%v", c.kind, err)
	}
}

// P4: what does the registry actually gate on, if not the kind?
//
// The first attempt at the chan codec failed, and the message is the finding:
// the one obligation is totality over the zero value. Map the zero to "" and
// every one of the four kinds passes.
func TestP4TheGateIsTotalityOverTheZeroValue(t *testing.T) {
	partial := func(c chan int) (string, error) {
		for name, known := range chans {
			if known == c {
				return name, nil
			}
		}

		return "", errNoSuch // no answer for the nil chan
	}

	_, err := ferry.NewRegistry(ferry.StringValue(partial, parseChan))
	t.Logf("a codec that does not answer for the zero value: %v", err)

	_, err = ferry.NewRegistry(ferry.StringValue(chanText, parseChan))
	t.Logf("the same codec, answering for nil:               %v", err)
}

// P5: does a chan actually come back, through a plane?
//
// A name table is the honest codec for a process-local identity, and it needs
// the encode half to ask "which registered thing is this", so it needs
// comparability. A chan is comparable. A func is not, which is the one real
// asymmetry inside category (a).
func TestP5AChanComesBackAndAFuncCannot(t *testing.T) {
	t.Logf("chan int comparable = %v", reflect.TypeFor[chan int]().Comparable())
	t.Logf("func()   comparable = %v  <- so encode cannot ask which func this is",
		reflect.TypeFor[func()]().Comparable())

	reg := mustRegistry(t, ferry.StringValue(chanText, parseChan))

	type conf struct {
		C chan int `ferry:"c"`
	}

	inst := ferrytest.MemPlane().Open()
	in := conf{C: chans["work"]}

	if err := ferry.Dump(t.Context(), in, inst.Sink, ferry.WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	back, err := ferry.Load[conf](t.Context(), inst.Source, ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	t.Logf("the same channel came back: %v", back.C == in.C)
}

// P6: a named uintptr, which is how the kind reaches a real config struct.
//
// reflect.StructField.Offset, syscall.Handle and a memory-mapped base address
// are all a uintptr under a name of their own, and none of them is a pointer
// into this process's heap. Refusing by kind catches every one of them.
func TestP6ANamedUintptrIsTheRealCase(t *testing.T) {
	type conf struct {
		Off offset `ferry:"off"`
	}

	if err := ferry.Compile[conf](); err == nil {
		t.Fatal("a uintptr compiled clean with no codec")
	} else {
		t.Logf("with no codec: %v", err)
	}

	reg := mustRegistry(t, ferry.NumberValue(offsetText, parseOffset))

	inst := ferrytest.MemPlane().Open()
	if err := ferry.Dump(t.Context(), conf{Off: 4096}, inst.Sink, ferry.WithRegistry(reg)); err != nil {
		t.Fatalf("dump: %+v", err)
	}

	back, err := ferry.Load[conf](t.Context(), inst.Source, ferry.WithRegistry(reg))
	if err != nil {
		t.Fatalf("load: %+v", err)
	}

	t.Logf("with a codec: 4096 -> %d", back.Off)
}

type ptrConf struct {
	P uintptr `ferry:"p"`
	Q uintptr `ferry:"q"`
}

// offset is a uintptr that was never an address.
type offset uintptr

var (
	chans     = map[string]chan int{"work": make(chan int), "done": make(chan int)}
	errNoSuch = errors.New("no such value")
)

func uintptrText(u uintptr) (string, error) { return strconv.FormatUint(uint64(u), 10), nil }

func parseUintptr(s string) (uintptr, error) {
	n, err := strconv.ParseUint(s, 10, 64)

	return uintptr(n), err
}

func offsetText(o offset) (string, error) { return strconv.FormatUint(uint64(o), 10), nil }

func parseOffset(s string) (offset, error) {
	n, err := strconv.ParseUint(s, 10, 64)

	return offset(n), err
}

func rawPtrText(p unsafe.Pointer) (string, error) {
	return strconv.FormatUint(uint64(uintptr(p)), 10), nil
}

// parseRawPtr is the half go vet objects to, and the objection is the finding:
// "possible misuse of unsafe.Pointer". Nothing says the same of the uintptr
// codec above.
func parseRawPtr(s string) (unsafe.Pointer, error) {
	n, err := strconv.ParseUint(s, 10, 64)

	return unsafe.Pointer(uintptr(n)), err //nolint:govet // the point of the probe
}

func chanText(c chan int) (string, error) {
	if c == nil {
		return "", nil
	}

	for name, known := range chans {
		if known == c { // comparable, so this question can be asked at all
			return name, nil
		}
	}

	return "", errNoSuch
}

func parseChan(s string) (chan int, error) {
	if s == "" {
		return nil, nil
	}

	c, ok := chans[s]
	if !ok {
		return nil, errNoSuch
	}

	return c, nil
}

// funcText is total over the zero value and cannot be written for anything
// else, because func is not comparable.
func funcText(f func()) (string, error) {
	if f == nil {
		return "", nil
	}

	return "", errNoSuch
}

func parseFunc(s string) (func(), error) {
	if s == "" {
		return nil, nil
	}

	return nil, errNoSuch
}

func mustRegistry(t *testing.T, items ...ferry.Registration) *ferry.Registry {
	t.Helper()

	reg, err := ferry.NewRegistry(items...)
	if err != nil {
		t.Fatalf("register: %+v", err)
	}

	return reg
}
