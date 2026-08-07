package ferry

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
)

// Compile reports whether T's annotation is legal, from the type alone, with no
// value in hand and no plane reachable.
//
//	func TestSchema(t *testing.T) {
//	    if err := ferry.Compile[Config](); err != nil {
//	        t.Fatal(err)
//	    }
//	}
//
// It runs exactly the compiler [Load] and [Dump] run, and takes the same
// [Option] values, so a type it accepts is a type they accept. It compiles the
// schema and discards it, so it retains no resolution and is safe anywhere,
// including during init.
//
// What it checks is the whole annotation: every exported field names the
// segment it addresses or is marked "-", every named type is in the supported
// set or has a registered codec, and every declaration is admissible at the
// type it sits on.
//
//	Host     string `ferry:"host,required"`
//	Greeting string `ferry:"greeting,default='Hello, world'"`
//	Note     string `ferry:"note,default=it's here"`
//	Odd      string `ferry:"'a,b'"`
//	Skipped  string `ferry:"-"`
//
// It returns nil, or one refusal per address, sorted. Range it with [Elements],
// and match a member with errors.Is against [ErrSchema].
func Compile[T any](opts ...Option) error {
	_, err := schemaOf(reflect.TypeFor[T](), opts, discarded)

	return err
}

// retention is whether the schema a compile produces outlives the call that
// built it, which is what decides whether the compile freezes the registry it
// resolved against.
//
// The distinction is not a convenience. A registry freezes so that no schema is
// ever resolved against one set of codecs and walked against another, and
// [Compile] retains nothing to walk: it compiles a schema and discards it. So a
// Compile in a test, or in an init, does not close the door on a registration a
// later init has not made yet, and every verb that keeps the resolution does
// (ADR-0009, ADR-0010).
type retention bool

const (
	discarded retention = false // Compile: nothing keeps the resolution
	retained  retention = true  // every other verb: something holds the result
)

// schemaOf is the one door into the compiler. Every verb reaches a compiled
// type through this function and no other, so no two entry points can disagree
// about whether a type is legal - which would be the two-engines defect at
// ferry's own front door (ADR-0010).
//
// It resolves the Options first, because an Option list that is wrong is a
// mistake in the program that wrote it rather than in the type being compiled,
// and it fails the call it was handed to rather than describing a schema.
//
// It is also where the schema cache lands, for the same reason: a cache in one
// caller and not the other is two engines again, arrived at by omission.
//
// Retention decides the cache and nothing else now. ADR-0009's obligation is
// that once a type has been resolved against a registry, that registry's answer
// for that type must never change, and it used to be kept by a freeze arranged
// here; ADR-0017 moved it into the registry's own construction, so a registry
// cannot change its answer at any point after it exists and there is nothing
// left for this function to arrange. A compile whose result is discarded keeps
// nothing that could go stale, so [Compile] takes no cache entry.
func schemaOf(t reflect.Type, opts []Option, keep retention) (*schema, error) {
	cfg, err := newConfig(opts)
	if err != nil {
		return nil, err
	}

	return schemaWith(t, cfg, keep)
}

// schemaWith is the same door for a caller that already resolved the Options
// and still needs them afterwards.
//
// The split exists because a load-affecting Option has to reach the load from
// the caller's own resolved config: a cache entry closes over whichever config
// won the race for its slot, and that is sound only for what is in the key
// (ADR-0019, and the rule at [schemaKey]).
func schemaWith(t reflect.Type, cfg config, keep retention) (*schema, error) {
	if !keep {
		return compileSchema(t, cfg)
	}

	return cfg.registry.schemaFor(schemaKey{typ: t, tagKey: cfg.tagKey, decl: cfg.registry.exts.decl}, cfg)
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
	nodeLeaf    nodeKind = iota // a value that crosses the boundary at one address
	nodeStruct                  // a position whose members are compiled below it
	nodePointer                 // a composite that can be nil, so its own address carries an answer
	nodeArray                   // a position whose members are exactly N, from the type
	nodeSlice                   // a position whose members are one Index segment each, from the value
	nodeMap                     // a position whose members are one Name segment each, from the value
)

// node is one compiled position, holding resolved behaviour rather than the
// data it was resolved from: ADR-0008's tag parse is asked once per position
// per schema and never per call (ADR-0010).
type node struct {
	kind nodeKind
	addr Path
	// index is the reflect field index path from the container above this
	// position, which is what makes a promoted field reachable without the walk
	// repeating the field rule.
	//
	// It is relative rather than rooted because a pointer is a container the
	// walk descends through: reflect.Value.FieldByIndex dereferences a pointer
	// step and panics on a nil one, so an index path rooted at the whole value
	// would panic at exactly the field whose nil-ness is the answer.
	index  []int
	fields []*node

	// The leaf's resolved behaviour. A declared default is held as a Value and
	// decoded per load rather than cached as a Go value, because a cached one
	// is aliased across every load of one schema (ADR-0006).
	//
	// codec is the leaf's own boundary behaviour, resolved by identity and then
	// by kind once per position per schema rather than re-decided on every walk
	// (ADR-0005, ADR-0010).
	codec  leafCodec
	def    Value
	hasDef bool

	// required and omitzero sit on the node that owns the address they are
	// about, which for a composite under a pointer is the pointer and not the
	// element (ADR-0003). Both are declared at a position rather than at an
	// address: a node under a dynamic composite is compiled once at the address
	// shape its members share, so one declaration applies to every realised
	// member and the walk needs no second lookup (ADR-0006).
	required bool
	omitzero bool

	// key is a map's key behaviour, resolved here for the same reason codec is:
	// which type keys this map is the type's business and is settled once per
	// position per schema, not per entry of every dump.
	key mapKey
}

// compiler is the state one compile accumulates.
type compiler struct {
	cfg    config
	errs   []error
	leaves []leaf

	// sections and composites are every container address the type determines,
	// kept apart from the leaves and from each other because the three obey
	// different halves of ADR-0003's collision rule and mint different address
	// kinds (ADR-0016). Prefix-freeness is over the leaves alone; a section
	// address is a proper prefix of what is under it by construction; and a
	// composite's members come from the value, so nothing static may sit
	// beneath one.
	sections   []leaf
	composites []leaf

	// clashed is every leaf address prefix-freeness already refused, which is
	// what keeps one mistake to one diagnosis where both collision rules can
	// see it.
	clashed []Path

	// stack is the struct types this compile is currently inside, which is what
	// makes a recursive type detectable from reflect.TypeFor[T]() alone
	// (ADR-0005).
	stack []reflect.Type

	// ext is what the declared foreign tag keys carried, address by address. It
	// is accumulated here and lands on the address set, because that is the
	// handoff a driver already receives (ADR-0021).
	ext ExtTable
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
	// nullable says that a pointer above this position owns its address, so the
	// address is a place a plane can be asked for a Value. It is what separates
	// Auth Cred, which has no address of its own, from Auth *Cred, which has one
	// (ADR-0003).
	nullable bool

	// dynamic says that a slice or a map above this position mints the segment
	// that reaches it, so what is compiled here is an address shape and not an
	// address. Nothing under it joins the static set: a driver is handed only
	// addresses it can fetch, write, name and check, and there is nothing at a
	// shape to fetch (ADR-0003).
	dynamic bool

	// owned says that addr is this position's own address rather than the
	// address it sits inside. A tagged field owns the address its tag names, and
	// so does everything that mints no segment of its own beneath it; the root,
	// an untagged field and a promoted block do not.
	owned bool
}

// locate is where a refusal about the whole of what a site holds is reported,
// and it is ADR-0011's two location spaces stated precisely.
//
// The address wherever this position has one, and the Go field path only where
// it has none. The rule reads "the Go field path at schema compile" and its
// reason is that a field with no tag has no address, so the space is decided by
// whether an address exists and never by which tier the refusal fired in.
func (s site) locate() Path {
	if s.owned {
		return s.addr
	}

	return s.field
}

// compileSchema is the one compiler. Every entry point reaches a compiled type
// through this function and no other.
func compileSchema(t reflect.Type, cfg config) (*schema, error) {
	c := &compiler{cfg: cfg}

	root := c.compileRoot(t)
	c.checkPrefixFree()
	c.checkContainersDistinct()
	c.checkContainersUnique()

	if err := join(c.errs...); err != nil {
		return nil, err
	}

	return &schema{root: root, addrs: c.addressSet()}, nil
}

// compileRoot holds the root to the one rule an entry point's signature cannot
// express: the root must be a struct ferry walks (ADR-0010).
//
// "Walks" is asked of the compiled node and never of the Go kind, which is why
// the registry and the chain are consulted here first, in the order
// [compiler.compileValue] consults them at every position below the root
// (ADR-0007, ADR-0010). A kind-first root disagreed with itself twice: a
// registered struct with tagged fields compiled as a section with its codec
// silently ignored, though the same type one field down was a leaf, and
// netip.Addr was refused for mapping no address rather than for being the leaf
// it resolves to (#306).
//
// A root leaf mints the empty path, which ADR-0003 says an address may not be.
// Measured with the refusal removed, a YAML sink wrote "{}" and returned a nil
// error and a KV sink wrote no key at all, so the value is silently and totally
// lost rather than refused. Whether a root leaf should ever be legal is #309's
// question, and this refusal neither answers it nor anticipates it.
func (c *compiler) compileRoot(t reflect.Type) *node {
	if c.rootIsLeaf(t) {
		c.errAt(Path{}, rootLeafMsg(t))

		return nil
	}

	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		c.errAt(Path{}, fmt.Sprintf(
			"%s is not a struct ferry walks, so it names no address: the root of a schema is a struct "+
				"whose fields name the addresses, and wrapping it in one is the whole remedy", t))

		return nil
	}

	n, _ := c.compileStruct(t, site{}, nil)

	return n
}

// rootIsLeaf reports whether the root type resolves to a leaf, asking exactly
// what the compiler asks at every position below the root.
//
// A pointer goes through pointerLeaf and everything else through leafFor,
// which is [compiler.compileValue]'s own split: *int is a leaf carrying a null
// rather than a container, so a root *int is a root leaf and not a root
// pointer to one (ADR-0005, ADR-0007).
func (c *compiler) rootIsLeaf(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		_, ok := c.cfg.registry.pointerLeaf(t)

		return ok
	}

	_, ok := c.cfg.registry.leafFor(t)

	return ok
}

// rootLeafMsg refuses a type that resolved to a leaf at the root, and it names
// what the type compiled to rather than what its Go kind is: netip.Addr and a
// registered struct are both structs, and being a struct is not what makes them
// refusable here (ADR-0010).
func rootLeafMsg(t reflect.Type) string {
	return fmt.Sprintf("%s compiles to a leaf, so at the root it would sit at the empty path, which is not an "+
		"address: a leaf there is written nowhere and the write reports no error - wrap it in a struct whose "+
		"field names the address it should sit at", t)
}

// compileStruct compiles every field of a struct at the address the struct
// occupies, reporting how many addresses the subtree contributed.
//
// base is the index path this struct's fields are reached through from the
// value the walk holds at it. It is empty for a struct the walk descends into,
// and it is the embedded field's own index path for a promoted block, whose
// fields land in the parent's list and are read out of the parent's value.
func (c *compiler) compileStruct(t reflect.Type, s site, base []int) (n *node, addresses int) {
	n = &node{kind: nodeStruct, addr: s.addr, index: s.index}

	before := len(c.errs)
	count := 0

	c.stack = append(c.stack, t)

	for i := range t.NumField() {
		f := t.Field(i)
		count += c.compileField(&f, n, site{addr: s.addr, field: s.field, index: base, dynamic: s.dynamic})
	}

	c.stack = c.stack[:len(c.stack)-1]

	// ADR-0005: a struct that maps no address looks supported and is a silent
	// total loss - netip.Addr dumped 0 addresses with a nil error and loaded
	// back as an invalid IP. The check is suppressed where this struct already
	// reported, because a field error is why it contributed nothing, and one
	// mistake reporting twice is what ADR-0008's tiers exist to stop.
	if count == 0 && len(c.errs) == before {
		c.errAt(s.locate(), noAddressMsg(t))
	}

	return n, count
}

// noAddressMsg is ADR-0005's sharpest single line, and it names registration as
// the fix because that is what registration is for: time.Location has zero
// exported fields, and a codec collapses such a type to a leaf, which needs no
// address set at all.
//
// ADR-0007's chain shortens the list this rule catches by seven types, which is
// most of what it used to catch: netip.Addr, netip.AddrPort, netip.Prefix and
// big.Int have zero exported fields too, and every one of them declares a text
// pair, so the chain claims them before the backstop is reached.
func noAddressMsg(t reflect.Type) string {
	return fmt.Sprintf("%s maps no address: every struct ferry visits must contribute at least one, or the "+
		"type looks supported and is written nowhere - register a codec for it, or map a field of it", t)
}

// compileField is the field rule, which is the other half of the grammar and is
// where most of the argument is (ADR-0008).
func (c *compiler) compileField(f *reflect.StructField, parent *node, s site) int {
	at := site{
		addr:    s.addr,
		field:   s.field.At(f.Name),
		index:   slices.Concat(s.index, f.Index),
		dynamic: s.dynamic,
	}

	// An unexported field is dispatched before the tag is scanned, because a
	// field reflect can never set has nothing to report about (#261). An
	// anonymous field is not one of these: Go promotes it whether or not its
	// type is exported, so it keeps the scan and the diagnosis.
	if !f.Anonymous && !f.IsExported() {
		return c.compileUnexported(f, at)
	}

	r, err := scanTag(string(f.Tag), c.cfg.tagKey)
	if err != nil {
		c.errFor(at.field, err)

		return 0
	}

	switch {
	case f.Anonymous:
		return c.compileEmbedded(f, parent, at, r)
	case !r.found:
		c.errAt(at.field, c.noTagMsg(f.Name))

		return 0
	}

	return c.compileTagged(f.Type, parent, at, r.value, string(f.Tag))
}

// compileUnexported is the field rule for a field reflect cannot set. It reads
// the tag only to refuse one that can never do anything, and a tag that will
// not scan is not refused at all: the field is skipped either way, so the
// diagnosis would name a mistake with no consequence, on a field the caller may
// not even own (#261).
func (c *compiler) compileUnexported(f *reflect.StructField, s site) int {
	r, err := scanTag(string(f.Tag), c.cfg.tagKey)
	if err != nil {
		return 0
	}

	c.checkUnexported(f, s, r)

	return 0
}

// compileEmbedded is the field rule for an anonymous field, which costs the
// grammar no vocabulary at all: no tag promotes, a tag nests, and "-" skips the
// block. That is what Go's own field namespace already means by embedding, and
// it is why embed, inline and squash are all absent from the vocabulary.
//
// An anonymous field is considered whether or not its own type is exported,
// because Go promotes it either way and reflect can set through it. Skipping it
// would drop a mapped field in silence.
//
// "Reflect can set through it" is the whole of what ADR-0008 measured, and it
// is true of the promotion path only: what reflect clears the read-only flag at
// is the exported field one step below. A tag on an unexported embedded field
// makes the field itself the mapped position, and there is no step below it, so
// that case is refused rather than compiled into a leaf a load panics on
// (#224).
func (c *compiler) compileEmbedded(f *reflect.StructField, parent *node, s site, r read) int {
	if r.found {
		if !f.IsExported() && r.value != skipTag && c.setsTheFieldItself(f.Type) {
			c.errAt(s.field, c.unsettableEmbeddedMsg(f))

			return 0
		}

		return c.compileTagged(f.Type, parent, s, r.value, string(f.Tag))
	}

	if f.Type.Kind() != reflect.Struct {
		c.errAt(s.field, c.promotionMsg(f))

		return 0
	}

	n, count := c.compileStruct(f.Type, s, s.index)
	parent.fields = append(parent.fields, n.fields...)

	return count
}

// setsTheFieldItself reports whether mapping this type writes to the position
// the type sits at, rather than to something below it.
//
// A struct ferry descends into is the one shape that does not: its members are
// reached with FieldByIndex, and reflect drops the read-only flag at the
// exported field that step ends on, which is why a promoted block and a nested
// one both work through an unexported embedded field. Everything else is
// written at the position itself - a leaf parses into it, a pointer is
// materialised into it, a slice and a map are built and set into it, and an
// array's elements inherit its flag - so through an unexported embedded field
// every one of them is a value reflect refuses to set (#224, ADR-0005).
func (c *compiler) setsTheFieldItself(t reflect.Type) bool {
	if _, isLeaf := c.cfg.registry.leafFor(t); isLeaf {
		return true
	}

	return t.Kind() != reflect.Struct
}

// unsettableEmbeddedMsg refuses a tag that maps an unexported embedded field at
// its own position, naming both remedies.
//
// It is worded for a Go author, because the mistake is in a Go type and no
// plane is involved (ADR-0011).
func (c *compiler) unsettableEmbeddedMsg(f *reflect.StructField) string {
	return fmt.Sprintf("embedded field %s is unexported and its %s tag maps the field itself, which reflect "+
		"cannot set: only a struct works there, because what a load sets is its exported fields and never "+
		"the embedded field - give the field an exported name, or mark it %s:%q",
		f.Name, c.cfg.tagKey, c.cfg.tagKey, skipTag)
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
func (c *compiler) compileTagged(t reflect.Type, parent *node, s site, value, raw string) int {
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

	at := site{
		addr:    s.addr.At(tg.name),
		field:   s.field,
		index:   s.index,
		dynamic: s.dynamic,
		owned:   true,
	}

	c.readExtensions(at, raw)

	return c.compileValue(t, parent, at, tg)
}

// readExtensions reads every declared foreign tag key off this field, at the
// address the field's own tag just named.
//
// It runs after the name is known and never before, because the table is
// address-keyed: an address is what a tag declared, and a table keyed by field
// name instead drifts on the first rename (ADR-0021). So a field marked "-",
// and a field ferry reads no tag on at all, carry their extension words
// nowhere.
func (c *compiler) readExtensions(s site, raw string) {
	for _, key := range c.cfg.registry.exts.keys {
		c.readExtension(s, raw, key)
	}
}

// readExtension reads one declared key, and refuses what that key's own
// declaration does not admit.
//
// The scan is ferry's own, run under the foreign key, so a tag that cannot be
// read is diagnosed with the same three sentences and the attribution rule that
// keeps another library's malformed tag out of ferry's report still applies
// (ADR-0008, ADR-0021).
func (c *compiler) readExtension(s site, raw, key string) {
	r, err := scanTag(raw, key)
	if err != nil {
		c.errFor(s.locate(), err)

		return
	}

	if !r.found {
		return
	}

	words, errs := c.cfg.registry.exts.parse(key, r.value)
	for _, e := range errs {
		c.errFor(s.locate(), e)
	}

	// Validated wherever it is written, and recorded only where the position is
	// an address. Under a slice or a map what is compiled is an address shape,
	// which is in no address set, so a word recorded there would be extension
	// data for something the driver was never bound to (ADR-0003, ADR-0021).
	if len(errs) == 0 && !s.dynamic {
		c.ext.put(key, s.addr, words)
	}
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
	if cd, ok := c.cfg.registry.leafFor(t); ok {
		return c.compileLeaf(cd, t, parent, s, tg)
	}

	// Half a text pair is a refusal rather than a silent fall-through to the
	// rules below (ADR-0007). It is detected here, from reflect.TypeFor[T]()
	// alone with no value in hand, so Load and Dump refuse identically and
	// Compile sees it too. ADR-0011 splits the two locations by tier and this
	// is below the first, so it sits at the plane address like every other
	// refusal in this function.
	if msg, incomplete := incompletePair(t); incomplete {
		c.errAt(s.addr, msg)

		return 0
	}

	// A recursive type is asked before its kind is, because the answer for a
	// struct, a pointer and an array would otherwise be to recurse (ADR-0005).
	// A leaf is asked first and never reaches here, which is what makes a
	// registered codec collapse a recursive type to something compilable.
	if cyc, ok := cycleFrom(t, c.stack); ok {
		c.errAt(s.addr, recursionMsg(t, cyc))

		return 0
	}

	// ADR-0011 splits the two locations by tier and this is below the first, so
	// every refusal here sits at the plane address rather than at the Go field
	// path, which is what the neighbouring rules in this function already do.
	switch t.Kind() {
	case reflect.Struct:
		return c.compileNested(t, parent, s, tg)
	case reflect.Pointer:
		return c.compilePointer(t, parent, s, tg)
	case reflect.Array:
		return c.compileArray(t, parent, s, tg)
	case reflect.Slice:
		return c.compileSlice(t, parent, s, tg)
	case reflect.Map:
		return c.compileMap(t, parent, s, tg)
	default:
		c.errAt(s.addr, refusalMsg(t))

		return 0
	}
}

// compilePointer is *T, which mints no segment of its own (ADR-0005).
//
// It is two shapes and the type decides which. A pointer to a leaf is a leaf
// with a null, because a leaf already has an address and a pointer adds a null
// to it rather than a second address. A pointer to a composite is a container
// that can be nil, so it takes an address of its own, which is where the Null a
// nil writes sits and where a plane is asked whether the section is there at all
// (ADR-0003).
func (c *compiler) compilePointer(t reflect.Type, parent *node, s site, tg tag) int {
	if cd, ok := c.cfg.registry.pointerLeaf(t); ok {
		return c.compileLeaf(cd, t, parent, s, tg)
	}

	n := &node{kind: nodePointer, addr: s.addr, index: s.index}
	n.required, n.omitzero = heldAt(s, tg)

	// The element is compiled at the same address, because the pointer mints no
	// segment, and with the same tag, because the options were written for what
	// the pointer points at. nullable is what tells the element that its address
	// is a place a plane can be asked, which is the whole difference between
	// Auth Cred and Auth *Cred.
	count := c.compileValue(t.Elem(), n, site{
		addr:     s.addr,
		field:    s.field,
		nullable: true,
		dynamic:  s.dynamic,
		owned:    s.owned,
	}, tg)
	if count == 0 {
		return 0
	}

	parent.fields = append(parent.fields, n)
	c.recordUnder(n, s)

	return count
}

// compileSlice is []T, whose length is a property of the value rather than of
// the type, so it mints one Index segment per element and the compiler records
// a shape rather than addresses (ADR-0005).
//
// That is the capability difference an array does not have and it is not
// cosmetic: a slice's element addresses do not exist until there is a value, so
// Dump reaches every one of them always and Load reaches them only from a
// source that can list. The element is compiled once, at the address shape its
// members share, and the walk carries the realised address it stands at.
func (c *compiler) compileSlice(t reflect.Type, parent *node, s site, tg tag) int {
	c.checkDynamicOptions(t, s.addr, tg)

	n := &node{kind: nodeSlice, addr: s.addr, index: s.index}
	n.required, n.omitzero = heldAt(s, tg)

	// An element inherits no option, for the reason [compileArray] gives: the
	// tag names the composite, and what each option would mean at an element is
	// not something inheritance decides silently.
	count := c.compileValue(t.Elem(), n, shapeSite(s), tag{})
	if count == 0 {
		return 0
	}

	parent.fields = append(parent.fields, n)
	c.recordComposite(s)

	return count
}

// compileMap is map[K]V, which mints one Name segment per key.
//
// The key type is resolved before the element is compiled, because a map whose
// key ferry cannot address is refused whatever its values are, and reporting the
// element's own faults underneath that would be two diagnoses for one mistake.
func (c *compiler) compileMap(t reflect.Type, parent *node, s site, tg tag) int {
	c.checkDynamicOptions(t, s.addr, tg)

	key, ok := c.cfg.registry.mapKeyFor(t.Key())
	if !ok {
		c.errAt(s.addr, c.cfg.registry.mapKeyMsg(t.Key()))

		return 0
	}

	n := &node{kind: nodeMap, addr: s.addr, index: s.index, key: key}
	n.required, n.omitzero = heldAt(s, tg)

	count := c.compileValue(t.Elem(), n, shapeSite(s), tag{})
	if count == 0 {
		return 0
	}

	parent.fields = append(parent.fields, n)
	c.recordComposite(s)

	return count
}

// shapeSite is where a dynamic composite's element is compiled: one address
// shape every member shares, and out of the static address set.
//
// It carries no reflect index path, because a member of a slice or a map is
// reached by position or by key rather than by field. The count it returns still
// reaches the enclosing struct, which is what makes the maps-no-address backstop
// count minted address shapes rather than static leaf addresses - without it,
// struct{ Limits map[string]int } contributes nothing and does not compile.
func shapeSite(s site) site {
	return site{addr: s.addr.shape(), field: s.field.shape(), dynamic: true, owned: true}
}

// checkDynamicOptions is the second tier at a slice or a map.
//
// required names an address, so it is admissible exactly where that address's
// children come from the type, and here they come from the value (ADR-0006).
// The refusal carries the remedy because the user reaching for it has a
// legitimate intent that is simply not writable: five YAML documents give three
// distinct observations at a container address, and a missing key and an empty
// list are one of them.
func (c *compiler) checkDynamicOptions(t reflect.Type, addr Path, tg tag) {
	if tg.required {
		c.errAt(addr, fmt.Sprintf(
			"required is not available on %s: a plane cannot report present and empty at a container address, "+
				"so required could only mean at least one element, which is a constraint on the value rather "+
				"than an assertion about the plane - model the distinction as a struct with a set flag, or "+
				"check len() after Load", t))
	}

	c.checkNoDefault(t, addr, tg)
}

// recordLeaf adds a leaf address to the static set, and adds nothing under a
// dynamic composite: what is compiled there is a shape, a driver is never handed
// one, and there is nothing at it to fetch or write (ADR-0003).
func (c *compiler) recordLeaf(s site) {
	if s.dynamic {
		return
	}

	c.leaves = append(c.leaves, leaf{addr: s.addr, field: s.field})
}

// recordSection adds a place whose children are known from the type: a struct,
// an array, or either behind a pointer. Its address is where the plane is asked
// whether the section is there at all.
//
// It adds nothing where a pointer above already owns the address, because a
// pointer adds no second bit: a nil *Cred and a pointer to an empty Cred are
// one address carrying one answer (ADR-0005).
func (c *compiler) recordSection(s site) {
	if s.dynamic || s.nullable {
		return
	}

	c.sections = append(c.sections, leaf{addr: s.addr, field: s.field})
}

// recordComposite adds a place whose children come from the value, which is
// where the Null an empty one writes sits and the address a driver is asked to
// enumerate (ADR-0016).
func (c *compiler) recordComposite(s site) {
	if s.dynamic || s.nullable {
		return
	}

	c.composites = append(c.composites, leaf{addr: s.addr, field: s.field})
}

// recordUnder adds the container address a pointer occupies, at the kind of
// whatever it points at.
//
// A pointer mints no segment of its own, so the address is the pointer's and
// the kind is the pointee's: *Cred is a section and *[]string is a composite,
// and *int never reaches here because a pointer to a leaf is a leaf.
func (c *compiler) recordUnder(n *node, s site) {
	switch k, ok := containerKind(n); {
	case !ok:
		return
	case k == kindSection:
		c.recordSection(s)
	default:
		c.recordComposite(s)
	}
}

// containerKind is the address kind a compiled position occupies, and whether
// it occupies a container address at all.
//
// A pointer defers to what it points at, because it mints no segment and takes
// the address of its pointee (ADR-0003). A leaf occupies no container address,
// which is what makes *int one address and not two.
func containerKind(n *node) (addrKind, bool) {
	switch n.kind {
	case nodeStruct, nodeArray:
		return kindSection, true
	case nodeSlice, nodeMap:
		return kindComposite, true
	case nodePointer:
		if len(n.fields) == 0 {
			return 0, false
		}

		return containerKind(n.fields[elemShape])
	default:
		return 0, false
	}
}

// compileArray is [N]T, whose length is part of the type, so it mints exactly N
// Index segments and every one of them is a static address (ADR-0005).
//
// That is the capability difference between an array and a slice, and it is not
// cosmetic: an array's element addresses are known from reflect.TypeFor[T]()
// with no value in hand, so an array is loadable from a source that cannot
// enumerate and a slice is not. An array is therefore a section and never a
// composite: its children are compiled from the type and no driver is ever
// asked to list them, which is why a Name child can no longer appear under one
// (ADR-0016, #264).
func (c *compiler) compileArray(t reflect.Type, parent *node, s site, tg tag) int {
	// An array's N element addresses come from the type, which is the tier
	// required is admissible in (ADR-0006), and it is not a leaf, so a default
	// has no single address to sit at.
	c.checkNoDefault(t, s.addr, tg)

	if t.Len() == 0 {
		c.errAt(s.addr, emptyArrayMsg(t))

		return 0
	}

	n := &node{kind: nodeArray, addr: s.addr, index: s.index}
	n.required, n.omitzero = heldAt(s, tg)

	count := 0

	// The position is counted in uint rather than converted from one, because
	// Path.Elem takes the position unsigned: a negative one has no meaning and
	// a conversion is a place the constraint could be lost.
	var at uint

	for range t.Len() {
		before := len(c.errs)
		// An element inherits no option. The tag names the array, its options
		// were checked once above, and what each option would mean at an
		// element is #77's rather than something inheritance decides silently.
		count += c.compileValue(t.Elem(), n, elemSite(s, at), tag{})
		at++

		// Every element has one type, so a fault in it is the same fault N times
		// at N addresses. Reporting it once is what keeps [100]chan int one
		// refusal rather than a hundred identical ones.
		if len(c.errs) > before {
			break
		}
	}

	if count == 0 {
		return 0
	}

	parent.fields = append(parent.fields, n)
	c.recordSection(s)

	return count
}

// emptyArrayMsg refuses [0]T, which maps no address for the reason struct{}
// does and was silently dropped where struct{} was refused (#260). The element
// type is never compiled either, so a [0]chan int used to reach a shipped
// schema.
func emptyArrayMsg(t reflect.Type) string {
	return fmt.Sprintf("%s maps no address: a zero-length array has no elements, so nothing under it is "+
		"addressable and its element type is never checked - drop the field, or mark it %q", t, skipTag)
}

// elemSite is where one array element sits. Its Go field path carries the index
// too, because "the third element of Arr" is what a reader needs and "Arr" is
// what every element would otherwise be called.
func elemSite(s site, at uint) site {
	return site{addr: s.addr.Elem(at), field: s.field.Elem(at), dynamic: s.dynamic, owned: true}
}

// compileLeaf records a value that crosses the boundary at one address, with
// the behaviour that carries it resolved here rather than in the walk.
//
// A declared default is held as the text it was written as, wrapped in the
// String Value a flat plane would have reported, and it is decoded fresh on
// every load. The alternative was tried and it aliases: two independently
// loaded structs shared one backing array for a []byte default, and mutating
// either corrupted the other (ADR-0006).
func (c *compiler) compileLeaf(cd leafCodec, t reflect.Type, parent *node, s site, tg tag) int {
	c.checkLeafOptions(cd, t, s.addr, tg)

	n := &node{
		kind: nodeLeaf, addr: s.addr, index: s.index, codec: cd,
		def: String(tg.def), hasDef: tg.hasDef,
	}
	n.required, n.omitzero = heldAt(s, tg)

	parent.fields = append(parent.fields, n)
	c.recordLeaf(s)

	return 1
}

// heldAt is the two options the node that owns an address carries.
//
// A pointer above a composite owns that address (ADR-0003), so the element
// compiled beneath it carries neither. Both ask a question about one address,
// and asking it at two nodes answers it twice: a nil *Cred with omitzero would
// have skipped its own Null and then been asked again, and a required *Cred
// would have refused twice at one address.
func heldAt(s site, tg tag) (required, omitzero bool) {
	if s.nullable {
		return false, false
	}

	return tg.required, tg.omitzero
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
//
// It is also where required on a plain struct is admitted, which is a repair
// rather than a permission. required names an address and is admissible exactly
// where that address's children come from the type (ADR-0006), and a struct's
// do: it means the plane supplied at least one of them, which is the same thing
// it means on *struct. An earlier draft refused it here on the ground that a
// struct has no address of its own, and ADR-0006 quotes that sentence in order
// to overturn it.
func (c *compiler) compileNested(t reflect.Type, parent *node, s site, tg tag) int {
	c.checkNoDefault(t, s.addr, tg)

	n, count := c.compileStruct(t, s, nil)
	if count == 0 {
		return 0
	}

	n.required, n.omitzero = heldAt(s, tg)
	parent.fields = append(parent.fields, n)
	c.recordSection(s)

	return count
}

// checkLeafOptions is the second and third tiers at a leaf, in that order, and
// the ordering is what stops one field's single mistake reporting as three
// errors (ADR-0006).
//
// The second tier is that a declared default has to be text this leaf's own
// parser accepts. It is checked from reflect.TypeFor[T]() alone, with no value
// in hand and no plane reachable, through exactly the decode a load will run
// over the same text - a default validated by one parser and applied by another
// would be the two conversion authorities ferry exists in order not to have.
//
// The third tier runs only over options that cleared the second, and both of
// its contradictions involve a default, so it is reached only where there is
// one and it parsed. A default that does not parse has no value to compare
// against zero, so naming the contradiction for it would name the wrong
// mistake.
func (c *compiler) checkLeafOptions(cd leafCodec, t reflect.Type, addr Path, tg tag) {
	if !tg.hasDef {
		return
	}

	v := reflect.New(t).Elem()
	if err := cd.decode(v, String(tg.def)); err != nil {
		c.errBecause(addr, badDefaultMsg(t, tg.def, err), err)

		return
	}

	if tg.required {
		c.errAt(addr, "required and default contradict: a default answers the absence required forbids")
	}

	// A default equal to the zero value is not a contradiction, because
	// omitting it and reapplying it land on the same value. Which text that is
	// became the leaf's own question when the type set widened past string:
	// "0" is int's zero, "false" is bool's and "0s" is time.Duration's, and not
	// one of them is the empty text a string-only compiler could compare with.
	if tg.omitzero && !v.IsZero() {
		c.errAt(addr, fmt.Sprintf("omitzero and default=%s contradict: an explicit zero would be omitted "+
			"and would load back as the default", tg.def))
	}
}

// badDefaultMsg refuses a declared default whose text this leaf's own parser
// does not accept.
//
// It names the text, and that is not the leak ADR-0011 forbids: the rule is
// about a value the plane supplied, and this one was written in the tag by the
// person reading the message. The cause is named where there is one, because
// "invalid syntax" and "value out of range" are different mistakes with
// different fixes, and it stays reachable either way so errors.Is against
// strconv's own sentinels still answers.
func badDefaultMsg(t reflect.Type, def string, err error) string {
	head := fmt.Sprintf("default %q is not a valid %s", def, t)

	cause := errors.Unwrap(err)
	if cause == nil {
		return head + ": a declared default is text, parsed by exactly the parser a value from the plane is"
	}

	return head + ": " + cause.Error()
}

// checkNoDefault refuses a declared default on a composite. A default is text
// parsed into one value at one address, and a composite's value is spread over
// the addresses beneath it, so there is nowhere for the text to land.
func (c *compiler) checkNoDefault(t reflect.Type, addr Path, tg tag) {
	if tg.hasDef {
		c.errAt(addr, fmt.Sprintf(
			"%s is not a leaf, so it has no single address a default could sit at: seed the value instead", t))
	}
}

// recursionMsg refuses a type whose static address set is unbounded.
//
// It names registration, and that is not a formality: a codec collapses a type
// to a leaf and a leaf needs no address set, so the thing that makes the set
// unenumerable stops being asked (ADR-0005).
func recursionMsg(t, cycle reflect.Type) string {
	head := fmt.Sprintf("%s is recursive", t)
	if t != cycle {
		head += fmt.Sprintf(", through %s", cycle)
	}

	return head + ": its addresses cannot be enumerated, and an address set that cannot be handed to a " +
		"driver before any I/O is not one ferry can compile - register a codec for it, which collapses it " +
		"to a leaf, or break the cycle"
}

// cycleFrom reports the first type reachable from t that the compile is already
// inside, and whether there is one.
//
// It searches through pointers, arrays, slices and maps, which are the steps
// that carry a type without naming a new struct, and it never steps through a
// struct's fields: those are the compile's own descent, and inside is the stack
// it keeps as it goes. Searching through a slice and a map is what makes
// struct{ Kids []Tree } a recursive type rather than a slice whose element
// happens not to compile, which is the wrong diagnosis for it.
func cycleFrom(t reflect.Type, inside []reflect.Type) (reflect.Type, bool) {
	if slices.Contains(inside, t) {
		return t, true
	}

	inside = append(slices.Clone(inside), t)

	for _, e := range carriedBy(t) {
		if cycle, ok := cycleFrom(e, inside); ok {
			return cycle, true
		}
	}

	return nil, false
}

// carriedBy is the types a composite reaches without a struct in between. A
// map's key is included because a map keyed by a type that reaches the map is
// as unbounded as one valued by it.
func carriedBy(t reflect.Type) []reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return []reflect.Type{t.Elem()}
	case reflect.Map:
		return []reflect.Type{t.Key(), t.Elem()}
	default:
		return nil
	}
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
			c.clashed = append(c.clashed, l.addr)
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

// checkContainersDistinct is the other half of ADR-0003's collision rule.
//
// A container address is exempt from prefix-freeness, because it is a proper
// prefix of what is under it by construction and that is what makes it a
// container. It must still be distinct from every leaf address: a container is
// asked whether it is there and never for a value, so a leaf sharing its
// address is a value with nowhere to be.
func (c *compiler) checkContainersDistinct() {
	at := make(map[Path]Path, len(c.leaves))
	for _, l := range c.leaves {
		at[l.addr] = l.field
	}

	for _, k := range c.containers() {
		field, ok := at[k.addr]

		// A leaf whose address is a proper prefix of another leaf's has already
		// been reported, and it is the same mistake: the subtree beneath the
		// container is what made it a prefix. Saying it twice is the duplicate
		// diagnosis ADR-0008's tiers exist to stop, and the case that needs
		// this rule at all is a leaf on a container with nothing static under
		// it, which prefix-freeness cannot see (ADR-0016).
		if !ok || slices.Contains(c.clashed, k.addr) {
			continue
		}

		c.errAt(k.addr, fmt.Sprintf(
			"a container address and a leaf address at once, %s and %s: a container is asked whether it is "+
				"there and never for a value, so a value at it has nowhere to be", k.field, field))
	}
}

// checkContainersUnique refuses two container addresses that are one address.
//
// Nothing checked a container against another container, so two fields tagged
// at one address compiled clean and the dump then realised the container and
// something beneath it at once, which ADR-0003 rules out: measured, a nil
// second pointer wrote a null over the first one's child and the round trip
// lost it in silence (#225).
func (c *compiler) checkContainersUnique() {
	at := make(map[Path]Path, len(c.sections)+len(c.composites))

	for _, k := range c.containers() {
		field, taken := at[k.addr]
		if taken {
			c.errAt(k.addr, fmt.Sprintf(
				"addressed by two containers, %s and %s: a container address is never realised at the same "+
					"time as anything beneath it, and where one of them is a composite the addresses under "+
					"it come from the value, so one of the two would write over the other's children",
				field, k.field))

			continue
		}

		at[k.addr] = k.field
	}
}

// containers is every container address the type determines, sections first, in
// the order they were compiled.
func (c *compiler) containers() []leaf { return slices.Concat(c.sections, c.composites) }

// addressSet is what a driver's Bind is handed: every address the type
// determines, each typed by what can be asked at it, and never a wildcard
// shape (ADR-0003, ADR-0016).
//
// The kind is the whole of what used to be missing. Three types that compiled
// to one address set now compile to three, so a driver classifies once at Bind
// rather than inferring per call which question an address admits (#239).
func (c *compiler) addressSet() *AddressSet {
	members := make([]Member, 0, len(c.leaves)+len(c.sections)+len(c.composites))

	for _, l := range c.leaves {
		members = append(members, leafOf(l.addr))
	}

	for _, k := range c.sections {
		members = append(members, sectionOf(k.addr))
	}

	for _, k := range c.composites {
		members = append(members, compositeOf(k.addr))
	}

	a := newAddressSet(members...)
	a.ext = c.ext

	return a
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

// errBecause records a refusal ferry worded itself over a cause worth keeping,
// which is a declared default the leaf's own parser refused: the message is
// ferry's and the cause stays reachable, so errors.Is against strconv.ErrRange
// answers through it.
func (c *compiler) errBecause(loc Path, msg string, cause error) {
	c.errs = append(c.errs, newError(momentCompile, ErrSchema, loc, msg).withCause(cause))
}
