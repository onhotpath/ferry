package protect

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// Protector is protection as this package needs it: bytes to bytes, and back.
//
// It is an interface rather than a dependency, so a test double, a hardware
// module or somebody else's key management is a few lines and this package never
// learns which of them it is talking to. [Using] is where one is handed over, and
// a source or a sink built without one reaches DPAPI-NG - which exists on Windows
// and nowhere else, so everywhere else the decorator refuses at Bind.
//
// Three things an implementer owns.
//
// Protect is allowed to be randomised, and DPAPI-NG is: the same plaintext
// protects to different bytes on every call. Nothing here compares two
// ciphertexts, and nothing may be built on their being equal.
//
// Unprotect either returns the exact plaintext Protect was given or fails. There
// is no third answer, and in particular there is no answer that means "this was
// not protected": whether the stored bytes were ever protected is settled before
// Unprotect is called, by a marker this package writes.
//
// Cancellation is yours. The decorator hands its caller's context to every call
// and adds no deadline of its own.
//
// Safety for use from many goroutines at once is yours. A source or a sink is
// constructed once and a binding is held for the life of a process, so one of
// these is reached from wherever a load or a save happens, and the plane
// underneath may declare that it tolerates overlapping calls.
type Protector interface {
	// Protect encrypts plaintext so that only a principal the descriptor names
	// can decrypt it.
	Protect(ctx context.Context, descriptor string, plaintext []byte) ([]byte, error)

	// Unprotect decrypts what Protect produced, or fails. The descriptor is not
	// repeated, because it travels inside the ciphertext.
	Unprotect(ctx context.Context, ciphertext []byte) ([]byte, error)
}

// Descriptor is who may decrypt: a protection descriptor rule string, in the
// form DPAPI-NG spells one.
//
// It is data rather than a function, so a source and a sink are configured with
// a value that can be written down, compared and put in a test.
//
// There are two families of rule string, and which one you are holding decides
// whether a machine can use it at all.
//
// A rule that names a security principal - one beginning SID= or SDDL= - is
// resolved by Active Directory's key distribution service, so it works on a
// machine joined to a domain and fails on one that is not, with
// NTE_ENCRYPTION_FAILURE at the first save. [LocalSystem] is one of these.
//
// A rule beginning LOCAL= is resolved by the machine itself and needs no domain.
// [CurrentUser] and [LocalMachine] are these two, and they are what a standalone
// machine has.
//
// Every other rule string DPAPI-NG accepts - a certificate, a set of web
// credentials - is written out here in full and is not a constant this package
// ships.
type Descriptor string

// CurrentUser protects to the account the process is running as, and it is the
// descriptor to reach for unless the machine is joined to a domain.
//
// Only that account, on this machine, can decrypt the value. A copy of the store
// taken anywhere else is unreadable, and so is the store read here by any other
// account. A service running as the local system account gets from this exactly
// what [LocalSystem] promises, on a machine that needs no domain to give it.
//
// The principal is whoever runs the process, which is the sharp edge: run the
// same program by hand as an ordinary user and the value is protected to that
// user, and the service that was going to read it back cannot.
const CurrentUser Descriptor = "LOCAL=user"

// LocalMachine protects to the machine, so every account on it can decrypt the
// value. It needs no domain.
//
// It is the descriptor for a value more than one account has to read: a service
// that writes it and an operator's tool that reads it.
//
// It grants what classic DPAPI at machine scope grants, which is every principal
// on the machine, so the store's own access control list is the only thing
// narrowing that down. Reach for [CurrentUser] wherever one account is enough.
const LocalMachine Descriptor = "LOCAL=machine"

// LocalSystem protects to the local system account, by naming its well-known
// security identifier.
//
// It says what classic DPAPI at machine scope cannot: this value is for the
// local system account and for nothing else, where machine scope grants
// decryption to every principal on the machine and leaves the store's access
// control list as the only thing keeping the value in.
//
// It works on a machine joined to an Active Directory domain and nowhere else. A
// SID rule names a principal the domain's key distribution service resolves, so
// on a standalone machine the first save fails with NTE_ENCRYPTION_FAILURE and
// nothing is written. [CurrentUser] is how a service running as the local system
// account gets the same narrowing with no domain behind it.
const LocalSystem Descriptor = "SID=S-1-5-18"

// ErrNoProtection reports a machine with no DPAPI-NG on it.
//
// It is what a source or a sink built without [Using] refuses with, at Bind and
// before any load, on every operating system but Windows. Supplying a
// [Protector] is what makes this package usable elsewhere.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] and [ferry.Dump] returned.
var ErrNoProtection = errors.New("protect: there is no DPAPI-NG here")

// ErrNotDeclared reports a schema compiled against a registry that was never
// given [Extension], where [FromTags] was asked to read the tag.
//
// It is the one refusal this package exists to make. Without it, a forgotten
// declaration reads exactly like a struct with no secrets in it, and every value
// a field marked would be written in the clear with nothing saying so.
//
// It wraps [ferry.ErrPlane] and lands at Bind, before any read or write.
var ErrNotDeclared = errors.New("protect: the registry was not given protect.Extension()")

// ErrOption reports a decorator that cannot be built: no plane to wrap, no
// selector, or an empty [Descriptor].
//
// [Over] and [OverSink] take options and return no error, so this lands at Bind,
// which is the first moment the decorator is asked for anything. It wraps
// [ferry.ErrPlane].
var ErrOption = errors.New("protect: unusable decorator option")

// ErrCiphertext reports a value that could not be encrypted on its way into the
// plane, or a stored value that carries this package's marker and could not be
// turned back into the value it was written from.
//
// It is always loud, in both directions. A value that was never protected is
// read back as it stands, which is how an existing deployment migrates, but a
// value that says it was protected and cannot be unprotected is a failure and
// never a plaintext quietly passed through - and a value that cannot be
// encrypted fails the save rather than being written in the clear.
//
// It wraps [ferry.ErrPlane], the report names the address, and whatever the
// [Protector] itself reported stays reachable underneath.
var ErrCiphertext = errors.New("protect: this value is marked as a secret and the protection failed on it")

// Option is a setting handed to [Over] or to [OverSink].
//
// There is one, [Using], and both halves take it. They are not two types here as
// they are in a plane's own driver, because nothing this decorator does differs
// between the directions: it protects on the way in and unprotects on the way
// out, through the same [Protector] and under the same [Descriptor].
type Option interface {
	apply(*config)
}

// optionFunc is the implementation behind every setting, which is what makes
// each of those a one-line constructor.
type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// Using names the protection this decorator encrypts and decrypts through.
//
//	src := protect.Over(store, protect.CurrentUser, protect.FromTags(), protect.Using(fake))
//
// A nil argument is DPAPI-NG, which is the default and which exists on Windows
// and nowhere else: elsewhere, a source or a sink built without this refuses at
// Bind with [ErrNoProtection].
//
// It is what makes a test hermetic, and it is the seam protection this package
// does not know about arrives through.
//
// Give the same one to both halves. A sink protecting through one and a source
// unprotecting through another never meet, and nothing checks that the two
// agree.
func Using(p Protector) Option {
	return optionFunc(func(c *config) { c.keeper = p })
}

// config is a decorator's settled configuration, copied into every binding so
// that one reconfigured after Bind cannot change a binding already handed out.
type config struct {
	desc Descriptor
	sel  Selector

	// keeper is the protection this decorator runs through. It is nil until
	// [config.settle] resolves it, which is [Using]'s argument where one was
	// given and DPAPI-NG otherwise.
	keeper Protector

	// err is what building the configuration refused with, held until Bind for
	// the reason [ErrOption] gives: an Option is applied inside a constructor
	// that returns no error, so the refusal waits for the first moment the
	// decorator is asked for anything.
	err error
}

// newConfig resolves one option list into the configuration a decorator runs
// under.
//
// apply is the loop over the options, which is the only thing that differs
// between the two constructors - and here it does not even differ, since both
// halves take one Option type (ADR-0018: a driver's settings are data, and a
// decorator's are the same data in both directions).
func newConfig(d Descriptor, sel Selector, opts []Option) config {
	c := config{desc: d, sel: sel}

	for _, o := range opts {
		o.apply(&c)
	}

	c.settle()

	return c
}

// settle checks what the constructor was given and resolves the protection
// behind it.
//
// The protection is resolved here rather than at the open because opening it is
// not I/O: an implementation records nothing, and every call it answers reaches
// the API it needs. That is also what makes this decorator safe to enter from
// many goroutines with no handle shared between them (ADR-0019).
func (c *config) settle() {
	if c.sel == nil {
		c.refuse(optionError("a selector is required, and protect.FromTags() is the one this package ships: " +
			"nothing to select is not the same question as nothing selected"))

		return
	}

	if c.desc == "" {
		c.refuse(optionError("the protection descriptor is empty: protect.CurrentUser is the account this " +
			"process runs as, and any DPAPI-NG rule string is a descriptor"))

		return
	}

	if c.keeper != nil {
		return
	}

	keeper, err := open()

	c.keeper = keeper
	c.refuse(err)
}

// refuse records a refusal, keeping the first so that a configuration with two
// mistakes in it reports the one nearest the beginning rather than the one
// nearest the end.
func (c *config) refuse(err error) { c.err = cmp.Or(c.err, err) }

// bind is what both halves ask before they hand a binding back: the
// configuration is usable, there is a plane under it, and the selector's view of
// this address set is legal.
//
// Every refusal a declaration alone can carry fires here, before any I/O, which
// is the same bargain driver/yaml's node tags already make (ADR-0021).
func (c *config) bind(plane any, addrs *ferry.AddressSet) (map[ferry.Path]bool, error) {
	if c.err != nil {
		return nil, c.err
	}

	if plane == nil {
		return nil, optionError("there is no source or sink to decorate: protect wraps a plane and is not one")
	}

	return c.sel.selected(addrs)
}

// optionError states the class this package has an opinion about and keeps
// [ErrOption] reachable underneath it.
func optionError(msg string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrOption, msg)
}

// noProtection is the refusal every non-Windows build of [open] gives (ADR-0004:
// what Bind may refuse for is what it can see without touching the plane, and
// the protection API not existing on this operating system is exactly that).
func noProtection() error {
	return fmt.Errorf("%w: %w: this program is not running on Windows, so there is no DPAPI-NG to reach: "+
		"hand this decorator a protector of your own with protect.Using", ferry.ErrPlane, ErrNoProtection)
}
