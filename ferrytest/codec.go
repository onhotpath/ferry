package ferrytest

import (
	"bytes"
	"context"
	"encoding"
	"fmt"
	"reflect"

	"github.com/onhotpath/ferry"
)

// Codec is the codec conformance suite: six cases over one registry, and about
// four lines for a registrant to adopt.
//
//	func TestCodec(t *testing.T) {
//	    reg := ferry.NewRegistry()
//	    if err := reg.Register(ferry.TextCodec[netip.Addr](ferry.KindString).AsMapKey()); err != nil {
//	        t.Fatal(err)
//	    }
//
//	    ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs, ferry.WithRegistry(reg))
//	    ferrytest.Codec(t, reg)
//	}
//
// It takes no [context.Context], for [Driver]'s reason, and every case cites the
// ADR sentence it executes.
//
// # What it asserts that a proof cannot
//
// A registrant's proof already exercises their codec through the ordinary walk,
// under ADR-0005's triple, and that is where a lossy codec, a constant codec and
// a codec declaring the wrong kind are caught. This suite is for the layer under
// that one: the generic wrapper is the single piece of reflection the
// registration API owns, it exists precisely so that a registrant never writes a
// reflect.Value, and a defect in it is a defect in every codec anybody ever
// registers. Two such defects were found three prototypes in, both one token
// wide, and neither was catchable by any proof a registrant could write, because
// the codec itself was correct (ADR-0009). Cases 2 and 3 are those two.
//
// # Why the cases run against this package's own probe types
//
// Cases 2 to 6 register a codec here and walk it, rather than walking the
// caller's. That is forced rather than chosen, and it is the same wall #109
// records for [Golden]: [ferry.Dump] compiles its schema from its type
// parameter, a type parameter is fixed at compile time, and what
// [ferry.Registry.Types] hands back is a reflect.Type. There is no route from a
// runtime type to a walk over it, so a suite handed only a registry cannot dump
// a value of a type it did not name in its own source.
//
// What that costs is exactly what it does not cost. The properties these cases
// assert belong to the registration machinery rather than to any one
// registration - the wrapper's behaviour at a nil interface, core's donation of
// String to a declared kind, the refusal of a codec that drifts off the kind it
// declared, and where a map key's text comes from - so a probe discharges them
// for the caller's build of core, which is what a registrant's CI is testing.
// The per-registration half is the proof, and it runs through [RoundTrip].
//
// Case 1 is the exception and reads the caller's own types, because it is a
// property of a type rather than of a codec.
func Codec(t T, reg *ferry.Registry, opts ...ferry.Option) {
	t.Helper()

	c := &codecRun{rep: t, reg: reg, opts: opts}

	// The Option list, resolved once against a struct with nothing registered
	// behind it, so a list that cannot be honoured is one report rather than one
	// per case. A [ferry.TagKey] lands here, because a key that is not the one
	// these probes were written under leaves them unable to compile, and so does
	// a second [ferry.WithRegistry]: core resolves against exactly one registry
	// and each case below needs its own.
	if err := ferry.Compile[onlyLeaf](c.with(ferry.NewRegistry())...); err != nil {
		t.Errorf("these Options leave the codec suite unable to compile the struct it dumps a probe in: %v", err)

		return
	}

	c.run()
}

// codecRun is one Codec call, carried down to the cases.
type codecRun struct {
	rep  reporter
	reg  *ferry.Registry
	opts []ferry.Option
}

// run is the six cases, in the order ADR-0014 lists them.
//
// Each is guarded, because the defect cases 2 and 3 exist for was a panic rather
// than an error: a registrant whose codec trips it should read which case went
// red, not a stack trace out of a package they have never opened.
func (c *codecRun) run() {
	c.rep.Helper()

	c.guard(codecTextNo, c.caseTextPair)
	c.guard(codecNilEncodeNo, c.caseNilInterfaceEncode)
	c.guard(codecNilDecodeNo, c.caseNilInterfaceDecode)
	c.guard(codecKindNo, c.caseDeclaredKind)
	c.guard(codecAcceptNo, c.caseAcceptsWhatItEmits)
	c.guard(codecKeyNo, c.caseKeyText)
}

// caseTextPair is case 1: AppendText and MarshalText agree (ADR-0007).
//
// ferry prefers the appender, which costs one allocation rather than two, and
// preferring one spelling inherits an obligation the compiler cannot check: the
// two must produce the same bytes. Every standard-library type carrying both
// implements one in terms of the other, so they agree there by construction, and
// nothing enforces it for a user type - which is why this is a case and not a
// promise.
//
// It reads the registrant's own types, and it asks the one value core holds
// without being given one: the zero value. A wider question needs values, and
// values are what a [Proof] carries.
func (c *codecRun) caseTextPair() {
	c.rep.Helper()

	for _, t := range c.reg.Types() {
		c.textPairAgrees(t)
	}
}

// textPairAgrees compares the two spellings for one type, and is silent for a
// type that declares fewer than both.
func (c *codecRun) textPairAgrees(t reflect.Type) {
	c.rep.Helper()

	zero := reflect.New(t).Interface()

	appender, isAppender := zero.(encoding.TextAppender)
	marshaler, isMarshaler := zero.(encoding.TextMarshaler)

	if !isAppender || !isMarshaler {
		return
	}

	appended, appendErr := appender.AppendText(nil)
	marshalled, marshalErr := marshaler.MarshalText()

	switch {
	case appendErr != nil && marshalErr != nil:
		// Both halves refusing the zero value is the type's own business: what
		// this case is about is the two of them disagreeing.
		return
	case appendErr != nil || marshalErr != nil:
		c.fail(codecTextNo, fmt.Sprintf("%s: AppendText failed with %v and MarshalText with %v: ferry calls "+
			"whichever is present and prefers the appender, so one half failing is one half of the type "+
			"working", t, appendErr, marshalErr))
	case !bytes.Equal(appended, marshalled):
		c.fail(codecTextNo, fmt.Sprintf("%s: AppendText wrote %q and MarshalText wrote %q: ferry prefers the "+
			"appender, so the plane holds the first of these and a reader expecting the second is reading "+
			"somebody else's bytes", t, appended, marshalled))
	default:
		return
	}
}

// caseNilInterfaceEncode is case 2: a registered interface codec at its nil zero
// value, encoding (ADR-0009).
//
// ADR-0005 makes the interface case the headline demonstration that a codec
// collapses a type to a leaf, and the zero value of an interface is a nil
// interface, so this is the intersection of two rules the design leans on. The
// defect it exists for was a type assertion in the wrapper's encode half:
// v.Interface().(T) on a nil interface field panics, one token away from the
// comma-ok form that does not, and the fix costs nothing measurable.
func (c *codecRun) caseNilInterfaceEncode() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecNilEncodeNo, ifaceCodec())
	if !ok {
		return
	}

	got, err := Record(context.Background(), ifaceHolder{}, c.with(reg)...)
	if err != nil {
		c.fail(codecNilEncodeNo, fmt.Sprintf("dumping a nil interface through a registered codec failed with "+
			"%v, and its codec answers a nil with a Null", err))

		return
	}

	if v := got[probeAddrPath]; v != ferry.Null() {
		c.fail(codecNilEncodeNo, fmt.Sprintf("a nil interface encoded to %#v, and its codec answers a nil with "+
			"%#v: what reached the codec was not the value the field held", v, ferry.Null()))
	}
}

// caseNilInterfaceDecode is case 3: the same, decoding (ADR-0009).
//
// The wrapper's decode half is the second of the two defects, and it is the
// mirror of the first: dst.Set(reflect.ValueOf(out)) for a nil out is a Set on a
// zero reflect.Value and panics, where dst.Set(reflect.ValueOf(&out).Elem())
// yields a value of the codec's own static type whatever the dynamic value is.
func (c *codecRun) caseNilInterfaceDecode() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecNilDecodeNo, ifaceCodec())
	if !ok {
		return
	}

	src := Static(map[ferry.Path]ferry.Value{probeAddrPath: ferry.Null()})

	back, err := ferry.Load[ifaceHolder](context.Background(), src, c.with(reg)...)
	if err != nil {
		c.fail(codecNilDecodeNo, fmt.Sprintf("loading a Null into a registered interface failed with %v, and "+
			"its codec answers a Null with a nil", err))

		return
	}

	if back.Value != nil {
		c.fail(codecNilDecodeNo, fmt.Sprintf("a Null loaded as %#v, and its codec answers a Null with a nil: "+
			"what the wrapper wrote back was not what the codec returned", back.Value))
	}
}

// caseDeclaredKind is case 4: a codec's declared kind matches what it emits
// (ADR-0007).
//
// The declared kind is what a plane is promised on the way out and what String
// is donated to on the way back, so a codec that declares one kind and produces
// another works on one plane class and fails on the next. The refusal has to
// reach beyond the zero value, which registration already checks: a codec whose
// zero encodes correctly and whose other values drift is exactly the shape a
// registration-time check cannot see.
func (c *codecRun) caseDeclaredKind() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecKindNo, driftingCodec())
	if !ok {
		return
	}

	_, err := Record(context.Background(), driftHolder{Value: "x"}, c.with(reg)...)
	if err == nil {
		c.fail(codecKindNo, "a codec declaring string and producing a number at a value other than its zero was "+
			"written to the plane: the declared kind is what every plane in ferry's range is promised")
	}
}

// caseAcceptsWhatItEmits is case 5: a codec accepts every kind it emits
// (ADR-0007).
//
// Core donates String to the declared kind before calling any codec, and that
// donation is core's rather than each registrant's for a measured reason: a
// codec seeing the raw value fails on env, query parameters and Consul, which is
// three of ADR-0004's four first-party planes. So a codec declaring Number must
// load from a plane that says Number and from one that says String, and this
// asserts both.
func (c *codecRun) caseAcceptsWhatItEmits() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecAcceptNo, numberCodec())
	if !ok {
		return
	}

	for _, v := range []ferry.Value{ferry.Number(probeNumber), ferry.String(probeNumber)} {
		src := Static(map[ferry.Path]ferry.Value{probeNumberPath: v})

		back, err := ferry.Load[numberHolder](context.Background(), src, c.with(reg)...)
		if err != nil {
			c.fail(codecAcceptNo, fmt.Sprintf("a codec declaring number refused %#v: it emits number, and "+
				"String is donated to the declared kind before any codec is called", v))

			continue
		}

		if string(back.Value) != probeNumber {
			c.fail(codecAcceptNo, fmt.Sprintf("%#v loaded as %q, want %q", v, back.Value, probeNumber))
		}
	}
}

// caseKeyText is case 6: a key codec is injective under == over ferry's own key
// text (ADR-0005, amended under #31).
//
// Two halves, and the first is why [Injective] exists at all: the text that
// addresses a plane is the registered codec's, never the type's own String(), so
// a registrant checking their keys through the type's own spelling is checking
// something ferry does not use. The second half is core's refusal - two keys
// rendering alike are one address, and which of the two survives is which the
// walk writes last, so it is refused as the address is minted rather than
// resolved.
func (c *codecRun) caseKeyText() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecKeyNo, foldingCodec().AsMapKey())
	if !ok {
		return
	}

	got, err := Record(context.Background(), keyed[folding]{Map: map[folding]string{probeMixed: ""}}, c.with(reg)...)
	if err != nil {
		c.fail(codecKeyNo, fmt.Sprintf("dumping a map under a registered key codec failed with %v", err))

		return
	}

	for addr := range got {
		if text := lastSegment(addr); text != probeFolded {
			c.fail(codecKeyNo, fmt.Sprintf("the key %q addressed %q, and its codec writes %q: the text that "+
				"addresses a plane is the codec's and never the key type's own spelling",
				probeMixed, text, probeFolded))
		}
	}

	c.keyCollisionRefused(reg)
}

// keyCollisionRefused is case 6's second half: two keys folding to one text are
// refused rather than silently merged.
func (c *codecRun) keyCollisionRefused(reg *ferry.Registry) {
	c.rep.Helper()

	both := map[folding]string{probeMixed: "", probeShouted: ""}

	if _, err := Record(context.Background(), keyed[folding]{Map: both}, c.with(reg)...); err == nil {
		c.fail(codecKeyNo, "two map keys that render to one address were written without a refusal, so one "+
			"entry is lost and which one survives is the order the walk happened to take")
	}
}

// probeRegistry builds a registry holding one probe registration, reporting a
// refusal rather than running a case against a registry that does not hold what
// the case is about.
func (c *codecRun) probeRegistry(n int, g ferry.Reg) (*ferry.Registry, bool) {
	c.rep.Helper()

	reg := ferry.NewRegistry()
	if err := reg.Register(g); err != nil {
		c.fail(n, "the suite's own probe codec was refused at registration: "+err.Error())

		return nil, false
	}

	return reg, true
}

// with is the caller's Option list plus the registry one case needs, which is
// where a caller who supplied a registry of their own is refused: core resolves
// against exactly one.
func (c *codecRun) with(reg *ferry.Registry) []ferry.Option {
	out := make([]ferry.Option, 0, len(c.opts)+1)
	out = append(out, c.opts...)

	return append(out, ferry.WithRegistry(reg))
}

// fail names the case a report came from, so that a registrant reading their own
// CI output knows which of the six went red.
func (c *codecRun) fail(n int, msg string) {
	c.rep.Helper()

	c.rep.Errorf("codec case %d: %s", n, msg)
}

// guard reports a panic as the case it happened in.
//
// Both defects cases 2 and 3 exist for were panics inside core's own reflection,
// raised on a value a registrant's codec handled correctly. A suite that let one
// through would abort the run at case 2 and report nothing about the four cases
// after it.
func (c *codecRun) guard(n int, body func()) {
	c.rep.Helper()

	defer func() {
		if r := recover(); r != nil {
			c.fail(n, fmt.Sprintf("panicked: %v", r))
		}
	}()

	body()
}
