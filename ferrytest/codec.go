package ferrytest

import (
	"bytes"
	"context"
	"encoding"
	"fmt"
	"reflect"

	"github.com/onhotpath/ferry"
)

// Codec is the codec conformance suite: seven cases over the registration
// machinery, and every one of them that the zero value can reach run again over
// each codec the registry actually holds.
//
//	func TestCodec(t *testing.T) {
//	    reg := ferry.MustRegistry(ferry.StringText[netip.Addr]().AsMapKey())
//
//	    ferrytest.RoundTrip(t, ferrytest.MemPlane(), proofs, ferry.WithRegistry(reg))
//	    ferrytest.Codec(t, reg)
//	}
//
// It takes no [context.Context], for [Driver]'s reason.
//
// # What it promises, exactly
//
// Two things, and the second is bounded in a way worth reading before relying
// on it.
//
// That ferry's registration machinery works in this build. A defect in the piece
// of reflection every registration goes through is a defect in every codec
// anybody ever registers, and it is one no proof a registrant could write would
// catch, because their codec is correct.
//
// And that every codec in reg survives its own zero value. For each type the
// registry holds, the suite builds an annotated struct around that type and
// walks it: the zero value encodes, what ferry wrote loads back, encoding what
// came back writes the same thing again, and the same text read as a plain
// string loads identically. The zero value is the bound because it is the only
// value this suite has without being handed one.
//
// # What it does not promise
//
// It does not check your codec away from its zero value, and most of what makes
// a codec wrong lives there. A lossy codec and a constant codec pass every case
// here, because both are correct at the zero value. So do two map keys that fold
// to one address, which need two values to see at all, and so does a null policy
// that disagrees with itself away from the zero value: where the disagreement is
// at the zero value the per-registrant round trip catches it, and where it is
// anywhere else nothing here has a value to find it with.
//
// What closes that gap is [RoundTrip], which drives your own values through the
// real engine, [Injective] over the values you will use as map keys, and
// [Complete], which reports a registered type you wrote no [Proof] for. Run all
// four. A green Codec on its own says the machinery is sound and your codec is
// sound at one value, and that is the whole of it.
func Codec(t T, reg *ferry.Registry, opts ...ferry.Option) {
	t.Helper()

	c := &codecRun{rep: t, reg: reg, opts: opts}

	// The seam, checked once. Without it the per-registrant half of the suite
	// cannot run at all and case 1 falls back to guessing which arm a
	// registration used, which is #143 - so this is a refusal rather than a
	// degraded run.
	if !coreWalkOK {
		t.Errorf("this build of ferry publishes no reflect.Value-rooted walk on the seam ferrytest reads, so " +
			"no case here can reach a type it was handed as a reflect.Type")

		return
	}

	// The Option list, resolved once against a struct with nothing registered
	// behind it, so a list that cannot be honoured is one report rather than one
	// per case. A [ferry.TagKey] lands here, because a key that is not the one
	// these probes were written under leaves them unable to compile, and so does
	// a second [ferry.WithRegistry]: core resolves against exactly one registry
	// and each case below needs its own.
	if err := ferry.Compile[onlyLeaf](c.with(ferry.MustRegistry())...); err != nil {
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

// run is the seven cases, in the order ADR-0014 lists them.
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
	c.guard(codecNullNo, c.caseNullPolicy)

	c.registered()
}

// registered is the per-registrant half: cases 2, 3 and 5 again, over every
// type the caller's registry actually holds, at the zero value.
//
// The seven cases above are about the machinery, and they stay: a defect in the
// wrapper is a defect in every codec anybody registers, and a caller whose
// registry is empty still wants that answered. This pass is the other half of
// the same question, and it costs one schema compile per registered type against
// the caller's own registry.
//
// It stops at the zero value, and that bound is structural rather than an
// omission. Cases 4 and 6 need a value away from the zero and two distinct keys
// respectively, and nothing core holds supplies either: they stay probe-only
// under any seam, and the per-registrant version of them is a [Proof].
func (c *codecRun) registered() {
	c.rep.Helper()

	for _, t := range c.reg.Types() {
		var (
			wrote ferry.Value
			ok    bool
		)

		c.guard(codecNilEncodeNo, func() { wrote, ok = c.zeroEncodes(t) })

		if !ok {
			continue
		}

		c.guard(codecNilDecodeNo, func() { c.zeroDecodes(t, wrote) })
		c.guard(codecAcceptNo, func() { c.acceptsString(t, wrote) })
	}
}

// zeroEncodes is case 2 for one registered type: the wrapper encodes the zero
// value without panicking and without an error.
//
// [ferry.NewRegistry] already runs the codec against this value, so the gain
// here is not a new value but a named report: the two defects cases 2 and
// 3 exist for were panics, and a panic at registration is a stack trace out of
// a package the registrant has never opened.
func (c *codecRun) zeroEncodes(t reflect.Type) (ferry.Value, bool) {
	c.rep.Helper()

	got, err := c.dumpZero(t)
	if err != nil {
		c.fail(codecNilEncodeNo, fmt.Sprintf("%s: dumping the zero value through the registered codec failed "+
			"with %v: the zero value is the one value core holds without being handed one, and a codec that "+
			"cannot write it cannot write a field nobody set", t, err))

		return ferry.Value{}, false
	}

	return got, true
}

// zeroDecodes is case 3 for one registered type: what ferry wrote for the zero
// value loads back, and encoding what came back writes the same thing again.
//
// The second half is what registration does not do. Its totality check encodes
// the zero and decodes the result, and asserts only that neither errors - so a
// codec whose decode half answers something else entirely passes it, and the
// plane text is not a fixed point of the pair.
func (c *codecRun) zeroDecodes(t reflect.Type, wrote ferry.Value) {
	c.rep.Helper()

	back, err := c.loadInto(t, wrote)
	if err != nil {
		c.fail(codecNilDecodeNo, fmt.Sprintf("%s: loading back the %#v ferry wrote for the zero value failed "+
			"with %v: a codec that cannot read its own output cannot round trip anything", t, wrote, err))

		return
	}

	again, err := c.dumpRoot(back)
	if err != nil {
		c.fail(codecNilDecodeNo, fmt.Sprintf("%s: re-encoding what %#v loaded as failed with %v", t, wrote, err))

		return
	}

	if again != wrote {
		c.fail(codecNilDecodeNo, fmt.Sprintf("%s: the zero value encodes to %#v, and loading that back and "+
			"encoding it again writes %#v: the codec's two halves disagree at the one value they are both "+
			"guaranteed to see", t, wrote, again))
	}
}

// acceptsString is case 5 for one registered type: the codec loads its own
// output back from a plane that spells it String.
//
// Core donates String to the declared kind before any codec is called, because
// a codec seeing the raw value fails on env, query parameters and Consul. This
// asserts the donation end to end for the registrant's own codec, and it is
// silent for a codec that declares String already, where there is nothing to
// donate.
func (c *codecRun) acceptsString(t reflect.Type, wrote ferry.Value) {
	c.rep.Helper()

	text, ok := textOf(wrote)
	if !ok || wrote.Kind() == ferry.KindString {
		return
	}

	back, err := c.loadInto(t, ferry.String(text))
	if err != nil {
		c.fail(codecAcceptNo, fmt.Sprintf("%s: the codec declares %s and refused the same text spelled String, "+
			"with %v: String is donated to the declared kind before any codec is called, so this is what a "+
			"plane with no types of its own hands it", t, wrote.Kind(), err))

		return
	}

	if again, err := c.dumpRoot(back); err != nil || again != wrote {
		c.fail(codecAcceptNo, fmt.Sprintf("%s: the same text spelled String loaded as something that encodes "+
			"to %#v, want %#v (err %v)", t, again, wrote, err))
	}
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
//
// # Why it observes rather than assumes
//
// The two spellings disagreeing only matters where ferry uses them, and
// registration is step one of ADR-0007's chain and beats the text pair - so a
// type registered through [ferry.StringValue] or [ferry.NumberValue] carries a
// disagreement ferry will never consult. Reporting that is a false positive
// with a false explanation attached, because the plane holds neither string
// (#143).
//
// So this case does not guess which arm a registration used. It dumps the zero
// value through the caller's own registry and reads what ferry actually wrote,
// and only then asks whether the pair is in play. That is a question about
// behaviour rather than about a registration's internals, which is why it needs
// no accessor on [ferry.Codec] and leaves ADR-0009's "no accessor" finding
// standing.
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

	if appendErr != nil && marshalErr != nil {
		// Both halves refusing the zero value is the type's own business: what
		// this case is about is the two of them disagreeing.
		return
	}

	if !c.ferryWritesTheAppender(t, appended, appendErr) {
		return
	}

	switch {
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

// ferryWritesTheAppender is #143's fix: whether ferry's own output for this
// type is the appender's bytes.
//
// It answers by dumping the zero value through the caller's registry and
// reading what landed, so it cannot mistake a [ferry.StringValue] registration
// for a [ferry.StringText] one, and it needs to know nothing about how the
// registration was built.
//
// Two shapes, and the second is the narrow one. Where the appender succeeds, it
// is in play exactly when ferry wrote its text - kind included or not, because
// a text registration may name either kind and the question is about the bytes.
// Where
// the appender fails, it is in play exactly when the dump failed too, since a
// registration that bypassed it would have written something.
//
// A codec that writes precisely what the appender would is not a false
// positive, and the answer here is the true one: the plane really does hold the
// appender's bytes, so a reader expecting the marshaler's is really reading
// somebody else's.
func (c *codecRun) ferryWritesTheAppender(t reflect.Type, appended []byte, appendErr error) bool {
	c.rep.Helper()

	wrote, err := c.dumpZero(t)
	if appendErr != nil {
		return err != nil
	}

	if err != nil {
		return false
	}

	text, ok := textOf(wrote)

	return ok && text == string(appended)
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

	if v := got[probeAddrPath]; v != ferry.Null {
		c.fail(codecNilEncodeNo, fmt.Sprintf("a nil interface encoded to %#v, and its codec answers a nil with "+
			"%#v: what reached the codec was not the value the field held", v, ferry.Null))
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

	src := Static(map[ferry.Path]ferry.Value{probeAddrPath: ferry.Null})

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

// caseDeclaredKind is case 4: the kind a registration writes is the one its
// constructor names (ADR-0007, ADR-0017).
//
// The kind is what a plane is promised on the way out and what a string is
// donated to on the way back, so a registration landing at the wrong one works
// on one plane class and fails on the next. It used to be a check core ran on
// every encode, against a kind the registrant declared as an argument, and there
// is no such argument any more: an encode half returns a bool, a string or a
// []byte and ferry wraps it, so the kind is a property of which constructor was
// called.
//
// That makes this a question about the machinery rather than about a
// registrant's codec. It registers one codec per constructor and asserts that
// each lands at its own kind, which is the promise every registration in the
// program now rests on.
func (c *codecRun) caseDeclaredKind() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecKindNo, kindCodecs()...)
	if !ok {
		return
	}

	got, err := Record(context.Background(), kindHolder{N: probeNumber, S: "s", Y: "y"}, c.with(reg)...)
	if err != nil {
		c.fail(codecKindNo, fmt.Sprintf("dumping one value per kind-named constructor failed with %v", err))

		return
	}

	for addr, want := range kindWanted {
		if v := got[ferry.At(addr)]; v.Kind() != want {
			c.fail(codecKindNo, fmt.Sprintf("the codec built by the %s constructor wrote %#v: a registration's "+
				"kind is the one its constructor names, and it is what every plane is promised", want, v))
		}
	}
}

// caseNullPolicy is case 7: a null policy round-trips through the null, and
// through a value that is not the null (ADR-0017).
//
// A policy says what a plane's null becomes and which values write one back, and
// its law is that the two agree: isNull(load()) must hold, or the round trip
// lies silently and only on the null path. This drives both arms through the
// real engine, which is the only place the two halves meet.
func (c *codecRun) caseNullPolicy() {
	c.rep.Helper()

	reg, ok := c.probeRegistry(codecNullNo, nullCodec())
	if !ok {
		return
	}

	c.nullRoundTrips(reg, nullHolder{}, ferry.Null)
	c.nullRoundTrips(reg, nullHolder{Value: probeNullable}, ferry.String(string(probeNullable)))
}

// nullRoundTrips dumps one value under a null policy, asserts what landed, and
// loads it back into a value that encodes the same thing again.
func (c *codecRun) nullRoundTrips(reg *ferry.Registry, in nullHolder, want ferry.Value) {
	c.rep.Helper()

	got, err := Record(context.Background(), in, c.with(reg)...)
	if err != nil {
		c.fail(codecNullNo, fmt.Sprintf("dumping %#v under a null policy failed with %v", in.Value, err))

		return
	}

	if v := got[probeNullPath]; v != want {
		c.fail(codecNullNo, fmt.Sprintf("%#v dumped as %#v, want %#v: a null policy writes a null for the values "+
			"it calls null and the inner codec's own value for every other", in.Value, v, want))

		return
	}

	src := Static(map[ferry.Path]ferry.Value{probeNullPath: want})

	back, err := ferry.Load[nullHolder](context.Background(), src, c.with(reg)...)
	if err != nil {
		c.fail(codecNullNo, fmt.Sprintf("loading %#v back through a null policy failed with %v", want, err))

		return
	}

	if back.Value != in.Value {
		c.fail(codecNullNo, fmt.Sprintf("%#v dumped as %#v and loaded back as %#v: a policy that loads a value it "+
			"does not recognise on the way back makes the round trip lie on the null path alone",
			in.Value, want, back.Value))
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

// probeRegistry builds a registry holding the probe registrations one case
// needs, reporting a refusal rather than running that case against a registry
// that does not hold what it is about.
//
// A registry is complete at birth, so the refusal arrives from
// [ferry.NewRegistry] rather than from the case that would have used it, and
// this reports it as the case it belongs to.
func (c *codecRun) probeRegistry(n int, codecs ...ferry.Registration) (*ferry.Registry, bool) {
	c.rep.Helper()

	reg, err := ferry.NewRegistry(codecs...)
	if err != nil {
		c.fail(n, fmt.Sprintf("the suite's own probe codec was refused at registration: %v", err))

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
// CI output knows which of the seven went red.
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
