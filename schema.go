package ferry

import (
	"fmt"
	"reflect"
	"slices"
)

// Compile reports whether T's annotation compiles, from the type alone.
//
// It is callable from a test with no value in hand and no plane reachable:
//
//	func TestSchema(t *testing.T) {
//	    if err := ferry.Compile[Config](); err != nil {
//	        t.Fatal(err)
//	    }
//	}
//
// It is not a second compiler with the same rules. It is the same function the
// load and dump verbs run, because two entry points that could disagree about
// whether a type is legal would be the two-engines defect at ferry's own front
// door (ADR-0010). It takes the same Options for the same reason: a compile
// that could not see [TagKey] would answer about a schema no load will ever
// build.
//
// It is Compile and not Validate because ADR-0001 rules validation out by
// architecture - the type is the validation - and a package that decides that
// cannot export Validate honestly. It compiles the schema and discards it, so
// it retains no resolution and it is safe anywhere, including during init.
//
// What a struct has to carry, in full (ADR-0008):
//
//	Host     string `ferry:"host,required"`
//	Greeting string `ferry:"greeting,default='Hello, world'"`
//	Note     string `ferry:"note,default=it's here"`
//	Odd      string `ferry:"'a,b'"`
//	Skipped  string `ferry:"-"`
//
// Every exported field names the segment it addresses or is marked "-", and
// ferry never invents a name: measured over 10,012 third-party Go files and the
// standard library, a Go field name is byte-exactly the name the author wanted
// about one time in twenty. Under an explicit name, exporting a field cannot
// silently change what a program writes to a plane.
//
// It returns nil, or one refusal per address, joined and sorted. Range it with
// [Elements] and match a member with errors.Is against [ErrSchema].
func Compile[T any](opts ...Option) error {
	_, err := schemaOf(reflect.TypeFor[T](), opts)

	return err
}

// schemaOf is the one door into the compiler. Compile, Load, LoadOver and Dump
// all reach a compiled type through this function and no other, so the two
// entry points cannot disagree about whether a type is legal - which would be
// the two-engines defect at ferry's own front door (ADR-0010).
//
// It resolves the Options first, because an Option list that is wrong is a
// mistake in the program that wrote it rather than in the type being compiled,
// and it fails the call it was handed to rather than describing a schema.
//
// It is also where the schema cache lands, for the same reason: a cache in one
// caller and not the other is two engines again, arrived at by omission.
func schemaOf(t reflect.Type, opts []Option) (*schema, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}

	return compileSchema(t, cfg)
}

// schema is a compiled type: the node tree a walk iterates, and the address set
// a driver is bound to.
//
// The address set is a field of the thing the walk reads rather than a second
// computation of the same rule. That is what stops the compiler and the walk
// disagreeing about which fields exist, which ADR-0008 found as a live defect
// in a real prototype - the compiler promoted an embedded field and the walk
// did not, so the schema promised /name and the walk visited /Common/name, with
// no error from either.
type schema struct {
	root  *node
	addrs *AddressSet
}

// nodeKind is what a compiled position is.
type nodeKind uint8

const (
	nodeLeaf   nodeKind = iota // a value that crosses the boundary at one address
	nodeStruct                 // a position whose members are compiled below it
)

// node is one compiled position, holding resolved behaviour rather than the
// data it was resolved from: ADR-0008's tag parse is asked once per position
// per schema and never per call (ADR-0010).
type node struct {
	kind nodeKind
	addr Path
	// index is the reflect field index path from the root, which is what makes
	// a promoted field reachable without the walk repeating the field rule.
	index  []int
	fields []*node

	// The leaf's resolved behaviour. A declared default is held as a Value and
	// decoded per load rather than cached as a Go value, because a cached one
	// is aliased across every load of one schema (ADR-0006).
	//
	// codec is the leaf's own boundary behaviour, resolved by identity and then
	// by kind once per position per schema rather than re-decided on every walk
	// (ADR-0005, ADR-0010).
	codec    leafCodec
	def      Value
	hasDef   bool
	required bool
	omitzero bool
}

// compiler is the state one compile accumulates.
type compiler struct {
	cfg    config
	errs   []error
	leaves []leaf
}

// leaf pairs a leaf address with the Go field path that named it, so a
// collision can name both fields rather than only the place they collide.
type leaf struct {
	addr  Path
	field Path
}

// site is where a field sits: the address its container occupies, the Go field
// path to it, and the reflect index path from the root.
//
// The two paths are two spaces, which is a rule rather than an accident. A
// first-tier refusal is located at the Go field path, because a field whose tag
// does not parse never named an address and the whole error is that it did not;
// everything below the first tier is located at the address (ADR-0011).
type site struct {
	addr  Path
	field Path
	index []int
}

// compileSchema is the one compiler. Every entry point reaches a compiled type
// through this function and no other.
func compileSchema(t reflect.Type, cfg config) (*schema, error) {
	c := &compiler{cfg: cfg}

	root := c.compileRoot(t)
	c.checkPrefixFree()

	if err := join(c.errs...); err != nil {
		return nil, err
	}

	return &schema{root: root, addrs: c.addressSet()}, nil
}

// compileRoot holds the root to the one rule an entry point's signature cannot
// express: the root must be a struct ferry walks (ADR-0010).
//
// A root leaf mints the empty path, which ADR-0003 says an address may not be.
// Measured with the check removed, a YAML sink wrote "{}" and returned a nil
// error, so the value is silently and totally lost rather than refused.
func (c *compiler) compileRoot(t reflect.Type) *node {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		c.errAt(Path{}, fmt.Sprintf(
			"%s is not a struct ferry walks, so it names no address: the root of a schema is a struct "+
				"whose fields name the addresses, and wrapping it in one is the whole remedy", t))

		return nil
	}

	n, _ := c.compileStruct(t, site{})

	return n
}

// compileStruct compiles every field of a struct at the address the struct
// occupies, reporting how many addresses the subtree contributed.
func (c *compiler) compileStruct(t reflect.Type, s site) (n *node, addresses int) {
	n = &node{kind: nodeStruct, addr: s.addr, index: s.index}

	before := len(c.errs)
	count := 0

	for i := range t.NumField() {
		f := t.Field(i)
		count += c.compileField(&f, n, s)
	}

	// ADR-0005: a struct that maps no address looks supported and is a silent
	// total loss - netip.Addr dumped 0 addresses with a nil error and loaded
	// back as an invalid IP. The check is suppressed where this struct already
	// reported, because a field error is why it contributed nothing, and one
	// mistake reporting twice is what ADR-0008's tiers exist to stop.
	if count == 0 && len(c.errs) == before {
		c.errAt(s.field, fmt.Sprintf(
			"%s maps no address: every struct ferry visits must contribute at least one, or the type "+
				"looks supported and is written nowhere", t))
	}

	return n, count
}

// compileField is the field rule, which is the other half of the grammar and is
// where most of the argument is (ADR-0008).
func (c *compiler) compileField(f *reflect.StructField, parent *node, s site) int {
	at := site{addr: s.addr, field: s.field.At(f.Name), index: slices.Concat(s.index, f.Index)}

	r, err := scanTag(string(f.Tag), c.cfg.tagKey)
	if err != nil {
		c.errFor(at.field, err)

		return 0
	}

	switch {
	case f.Anonymous:
		return c.compileEmbedded(f, parent, at, r)
	case !f.IsExported():
		c.checkUnexported(f, at, r)

		return 0
	case !r.found:
		c.errAt(at.field, c.noTagMsg(f.Name))

		return 0
	}

	return c.compileTagged(f.Type, parent, at, r.value)
}

// compileEmbedded is the field rule for an anonymous field, which costs the
// grammar no vocabulary at all: no tag promotes, a tag nests, and "-" skips the
// block. That is what Go's own field namespace already means by embedding, and
// it is why embed, inline and squash are all absent from the vocabulary.
//
// An anonymous field is considered whether or not its own type is exported,
// because Go promotes it either way and reflect can set through it. Skipping it
// would drop a mapped field in silence.
func (c *compiler) compileEmbedded(f *reflect.StructField, parent *node, s site, r read) int {
	if r.found {
		return c.compileTagged(f.Type, parent, s, r.value)
	}

	if f.Type.Kind() != reflect.Struct {
		c.errAt(s.field, c.promotionMsg(f))

		return 0
	}

	n, count := c.compileStruct(f.Type, s)
	parent.fields = append(parent.fields, n.fields...)

	return count
}

// promotionMsg refuses an embedded field that cannot be promoted, naming both
// remedies.
//
// The pointer is the case found by auditing rather than by designing.
// Promotion walks the pointed-to struct at the parent address, so the pointer
// has no address subtree of its own and nothing to be materialised from.
// Measured before the refusal existed: the schema compiled clean, a load left
// the pointer nil with err=nil, and the value went nowhere.
func (c *compiler) promotionMsg(f *reflect.StructField) string {
	if f.Type.Kind() == reflect.Pointer {
		return fmt.Sprintf("embedded field %s is a pointer, and promotion walks the pointed-to struct at the "+
			"parent address, so the pointer has no subtree to be materialised from: give it a name, as "+
			"%s:\"<name>\", to nest it, or mark it %s:%q", f.Name, c.cfg.tagKey, c.cfg.tagKey, skipTag)
	}

	return fmt.Sprintf("embedded field %s is a %s, and only a struct can be promoted to the parent address: "+
		"give it a name, as %s:\"<name>\", to nest it, or mark it %s:%q",
		f.Name, f.Type, c.cfg.tagKey, c.cfg.tagKey, skipTag)
}

// checkUnexported skips an unexported field, and refuses a tag on one.
//
// "-" on an unexported field is redundant and accepted, which is
// encoding/json/v2's reading of the same case. Any other tag is an error,
// because reflect cannot set the field and the tag can never do anything.
func (c *compiler) checkUnexported(f *reflect.StructField, s site, r read) {
	if !r.found || r.value == skipTag {
		return
	}

	c.errAt(s.field, fmt.Sprintf(
		"field %s is unexported, so reflect cannot set it and its %s tag can never do anything: "+
			"export the field, or delete the tag", f.Name, c.cfg.tagKey))
}

// noTagMsg names the field and both remedies, which is the mitigation for the
// cost this rule has: a thirty-field config struct carries thirty tags.
func (c *compiler) noTagMsg(name string) string {
	return fmt.Sprintf("field %s carries no %s tag: every exported field must name the segment it addresses, "+
		"or be marked %s:%q", name, c.cfg.tagKey, c.cfg.tagKey, skipTag)
}

// compileTagged parses a tag and compiles the field at the address it named.
// The first tier ends here: a field whose tag does not parse is asked nothing
// below, and its address never joins the set.
func (c *compiler) compileTagged(t reflect.Type, parent *node, s site, value string) int {
	tg, errs := parseTag(value, c.cfg.tagKey)
	if len(errs) > 0 {
		for _, err := range errs {
			c.errFor(s.field, err)
		}

		return 0
	}

	if tg.skip {
		return 0
	}

	return c.compileValue(t, parent, site{addr: s.addr.At(tg.name), field: s.field, index: s.index}, tg)
}

// compileValue is the second and third tiers: is each option legal at this
// field's type, and do two that both survived that conflict (ADR-0006).
//
// The leaf lookup runs before the struct arm, and that ordering is ADR-0005's
// identity-before-kind rule arriving at the compiler. time.Time's kind is
// struct, so a kind-first compiler walks its three unexported fields, finds no
// address, and refuses with the maps-no-address rule the very type ferry owns a
// representation for.
func (c *compiler) compileValue(t reflect.Type, parent *node, s site, tg tag) int {
	if cd, ok := leafFor(t); ok {
		return c.compileLeaf(cd, t, parent, s, tg)
	}

	if t.Kind() == reflect.Struct {
		return c.compileNested(t, parent, s, tg)
	}

	// ADR-0011 splits the two locations by tier and this is below the first, so
	// the refusal sits at the plane address rather than at the Go field path,
	// which is what the neighbouring rules in this function already do.
	c.errAt(s.addr, refusalMsg(t))

	return 0
}

// compileLeaf records a value that crosses the boundary at one address, with
// the behaviour that carries it resolved here rather than in the walk.
func (c *compiler) compileLeaf(cd leafCodec, t reflect.Type, parent *node, s site, tg tag) int {
	c.checkContradictions(cd, t, s.addr, tg)
	parent.fields = append(parent.fields, &node{
		kind: nodeLeaf, addr: s.addr, index: s.index, codec: cd,
		def: String(tg.def), hasDef: tg.hasDef, required: tg.required, omitzero: tg.omitzero,
	})
	c.leaves = append(c.leaves, leaf{addr: s.addr, field: s.field})

	return 1
}

// refusalMsg is the diagnosis for a type outside the set, and it is three
// messages rather than one because ADR-0005 sorts the refusals by what actually
// limits each and only one group is permanent.
//
// Offering registration as the remedy for a chan would be naming a remedy that
// does not exist, which is the same mistake ADR-0005 corrected for time.Time as
// a map key.
func refusalMsg(t reflect.Type) string {
	head := fmt.Sprintf("%s is not a type ferry maps to an address", t)

	switch {
	case permanentlyRefused(t.Kind()):
		return head + fmt.Sprintf(": a %s exists only inside the process that made it, so no text could carry "+
			"it and no codec can be written for it", t.Kind())
	case t.Kind() == reflect.Complex64 || t.Kind() == reflect.Complex128:
		return head + ": no plane in ferry's range has a complex type, so this is a refusal by policy rather " +
			"than by constraint: register a codec for it if yours does"
	default:
		return head + ": register a codec for it, or model it as a type ferry carries"
	}
}

// permanentlyRefused is ADR-0005's category (a), the only refusals nothing
// lifts: a codec has to produce a kind and text and rebuild the value from that
// text alone, and for these there is nothing text could carry.
//
// func is the sharpest of the four and fails on the encode side before the
// decode side is reached: measured, reflect.TypeFor[func()]().Comparable() is
// false, so a codec cannot even ask which registered function this is. A chan
// is comparable, and its identity is a pointer into this process's heap.
func permanentlyRefused(k reflect.Kind) bool {
	switch k {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Uintptr:
		return true
	default:
		return false
	}
}

// compileNested is a struct under a name of its own, which is the whole of what
// prefixing is under a structured address: the nested struct's tag is the
// prefix, so there is no concatenation to get wrong and no prefix= to spell.
func (c *compiler) compileNested(t reflect.Type, parent *node, s site, tg tag) int {
	c.checkStructOptions(t, s.addr, tg)

	n, count := c.compileStruct(t, s)
	parent.fields = append(parent.fields, n)

	return count
}

// checkStructOptions is the second tier at a struct. A non-pointer struct has
// no address of its own (ADR-0003), so neither required nor a default has
// anything to sit at; omitzero is admissible at every type, because it asks a
// question about the Go value rather than about an address.
func (c *compiler) checkStructOptions(t reflect.Type, addr Path, tg tag) {
	if tg.required {
		c.errAt(addr, fmt.Sprintf(
			"required is not available on %s: a struct has no address of its own for required to assert "+
				"about, so put it on the field that has to be there", t))
	}

	if tg.hasDef {
		c.errAt(addr, fmt.Sprintf(
			"%s is not a leaf, so it has no single address a default could sit at: seed the value instead", t))
	}
}

// checkContradictions is the third tier, and it runs only over options that
// cleared the second: a contradiction between two options is only meaningful if
// both are individually legal here.
func (c *compiler) checkContradictions(cd leafCodec, t reflect.Type, addr Path, tg tag) {
	if tg.required && tg.hasDef {
		c.errAt(addr, "required and default contradict: a default answers the absence required forbids")
	}

	// A default equal to the zero value is not a contradiction, because
	// omitting it and reapplying it land on the same value. Which text that is
	// became the leaf's own question when the type set widened past string:
	// "0" is int's zero, "false" is bool's and "0s" is time.Duration's, and not
	// one of them is the empty text a string-only compiler could compare with.
	if tg.omitzero && tg.hasDef && !declaredZero(cd, t, tg.def) {
		c.errAt(addr, fmt.Sprintf("omitzero and default=%s contradict: an explicit zero would be omitted "+
			"and would load back as the default", tg.def))
	}
}

// declaredZero reports whether a declared default decodes to the type's own
// zero value, through the same parser a load would use.
//
// A default that does not decode at all is a different mistake from a
// contradiction between two options, and reporting this one for it would name
// the wrong one, so it is not a contradiction here.
func declaredZero(cd leafCodec, t reflect.Type, def string) bool {
	v := reflect.New(t).Elem()
	if err := cd.decode(v, String(def)); err != nil {
		return true
	}

	return v.IsZero()
}

// checkPrefixFree is ADR-0003's collision rule, over the leaf addresses: no
// leaf address is a prefix of another, and a path is a prefix of itself so this
// subsumes exact duplicates.
//
// Prefix-freeness rather than duplicate detection is the rule, and the reason
// is the plane rather than the schema. A leaf at /db and a subtree under /db
// are two distinct addresses that a flat plane holds happily, as DB and
// DB_HOST; measured on a tree emitter, writing the pair leaves the scalar gone,
// and reversing the write order loses the other one instead. Core adopts the
// constraint the stricter plane imposes, because that is what makes a compiled
// schema representable on every plane rather than on the one it was written
// against.
func (c *compiler) checkPrefixFree() {
	slices.SortStableFunc(c.leaves, func(a, b leaf) int { return a.addr.Compare(b.addr) })

	for i, l := range c.leaves {
		// Sorted, anything p is a prefix of follows p immediately, and the run
		// ends at the first address it is not a prefix of.
		for _, m := range c.leaves[i+1:] {
			if !l.addr.isPrefixOf(m.addr) {
				break
			}

			c.errAt(l.addr, collisionMsg(l, m))
		}
	}
}

func collisionMsg(l, m leaf) string {
	if l.addr == m.addr {
		return fmt.Sprintf("addressed by two fields, %s and %s", l.field, m.field)
	}

	return fmt.Sprintf("a leaf address and a prefix of %s, which no tree plane can hold both of: %s and %s",
		m.addr, l.field, m.field)
}

// addressSet is what a driver's Bind is handed. It holds the leaf addresses and
// no container address, because a non-pointer struct cannot be nil and so has
// nothing a plane could be asked for at its own address (ADR-0003).
func (c *compiler) addressSet() *AddressSet {
	addrs := make([]Path, len(c.leaves))
	for i, l := range c.leaves {
		addrs[i] = l.addr
	}

	return NewAddressSet(addrs...)
}

// errAt records one refusal. Every refusal a compile produces is one of these:
// the compile moment, the schema class, and a location that is either the Go
// field path or the address.
func (c *compiler) errAt(loc Path, msg string) {
	c.errs = append(c.errs, newError(momentCompile, ErrSchema, loc, msg))
}

// errFor records a refusal the scanner or the grammar produced, keeping the
// cause reachable so errors.Is against strconv's own sentinels still answers.
func (c *compiler) errFor(loc Path, err error) {
	c.errs = append(c.errs, newError(momentCompile, ErrSchema, loc, err.Error()).withCause(err))
}
