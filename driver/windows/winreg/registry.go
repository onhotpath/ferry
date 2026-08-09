package winreg

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
)

// Registry is the Windows registry as this driver needs it: read one value,
// list what lies directly under one subkey, write one value, create one subkey,
// remove one value, remove one subkey and everything under it.
//
// It is an interface rather than a dependency, so a test double, an in-memory
// store or a remote registry is a few lines and this package never learns which
// of them it is talking to. [Store] is where one is handed over, and a source or
// a sink built without one reaches the machine's own registry - which exists on
// Windows and nowhere else, so everywhere else the driver refuses at Bind.
//
// Every subkey is relative to the hive and subkey path the source or the sink
// was constructed with, and the empty subkey is that key itself. The empty value
// name is the key's own unnamed value, which the registry editor shows as
// (Default).
//
// Five things an implementer owns.
//
// Absence is a result and not an error. Get reports a value the registry does
// not hold with found false and a nil error, and List does the same for a subkey
// that is not there, so a backend's own not-found stays distinguishable from a
// real failure. A zero-length value is a value the registry holds.
//
// Removal is idempotent. DeleteValue and DeleteKey report nothing for an object
// that is not there, and DeleteKey removes everything beneath the subkey as well
// as the subkey itself, because the registry's own delete refuses a key that
// still has children and this driver has no use for that refusal.
//
// Creation is implicit on the write path. Set creates every subkey on the way
// down to the value it writes, so a driver that stages a write at a\b\c does not
// have to create a and a\b first.
//
// Cancellation is yours. The driver hands its caller's context to every call and
// adds no deadline of its own, so an implementation that ignores the context is
// the only thing standing between a cancelled load and a blocked one.
//
// Safety for use from many goroutines at once is yours, and it is ordinary
// rather than exotic. A source or a sink is constructed once and a binding is
// held for the life of a process, so one of these is reached from wherever a
// load or a save happens - and this package's reader declares that it tolerates
// overlapping calls, so one load can reach it from several goroutines too.
type Registry interface {
	// Get answers with the value stored at name under subkey, and with found
	// false where the registry holds no such value.
	Get(ctx context.Context, subkey, name string) (Datum, bool, error)

	// List answers with the value names and the immediate subkey names under
	// subkey, and with found false where the subkey is not there. A subkey that
	// exists and holds nothing is found with an empty [Listing], which is what
	// tells a container that is there and empty from one that was never written.
	List(ctx context.Context, subkey string) (Listing, bool, error)

	// Set writes one value, creating subkey and everything above it as needed,
	// and replacing whatever was at the name before including its type.
	Set(ctx context.Context, subkey, name string, d Datum) error

	// Create makes subkey and everything above it, and does nothing where it is
	// already there. The empty subkey is the driver's own key, so Create with it
	// is what a sink asks at the open to find out whether it may write at all.
	Create(ctx context.Context, subkey string) error

	// DeleteValue removes one value, and reports nothing where the value or the
	// subkey holding it is not there.
	DeleteValue(ctx context.Context, subkey, name string) error

	// DeleteKey removes one subkey and everything under it, and reports nothing
	// where it is not there.
	DeleteKey(ctx context.Context, subkey string) error
}

// Notifier is implemented by a [Registry] that can report a change under the key
// the driver was built over, and it is what [Watch] needs.
//
// It is optional. A Registry that is no Notifier is refused at Bind when a watch
// was asked for, because a watch that opens successfully and never fires is the
// failure the option exists to avoid.
//
// Registering and waiting are two calls rather than one, and that is the whole
// point of the shape. [Watch] arms the next registration before it runs the
// callback, so a registration is live for the entire time the callback and the
// load inside it take. An implementation that registered inside the wait would
// have no registration during that window and would lose every change that
// landed in it.
type Notifier interface {
	// Arm registers for the next change under the driver's own key and answers
	// with the [Change] that waits for it.
	//
	// The registration is live when Arm returns, so a change between Arm and
	// [Change.Wait] is reported by that Wait rather than missed. ctx bounds the
	// registration itself and not the wait that follows it.
	Arm(ctx context.Context) (Change, error)
}

// Change is one armed registration: one wait, and the release that follows it.
//
// It is what [Notifier.Arm] answers with, and a watcher waits on it once and
// closes it once.
type Change interface {
	// Wait blocks until the change this registration was armed for happens,
	// until ctx is done, or until the watch cannot be kept.
	//
	// It reports true where a change happened, including one that landed before
	// Wait was called. False with a nil error is the watch ending quietly, which
	// is what a cancelled context produces, and any error is the watch being
	// lost.
	Wait(ctx context.Context) (bool, error)

	// Close releases the registration. It is called once, after the wait, and
	// whatever it reports is discarded: there is nothing a watcher could do with
	// it.
	Close() error
}

// Listing is what one subkey holds directly: the names of its values, and the
// names of its immediate subkeys.
//
// The two are separate because the registry keeps them in separate namespaces,
// so a value host and a subkey host under one key are two objects and both
// appear here.
type Listing struct {
	// Values is the names of the values in this key, in whatever order the
	// registry gave them. An empty name is the key's own unnamed value.
	Values []string

	// Keys is the names of the immediate subkeys of this key, one segment each
	// and never a path.
	Keys []string
}

// Hive is one of the registry's predefined root keys, and it is the first
// argument to [NewSource] and [NewSink].
//
// The zero Hive names none of them, so a hive nobody chose is refused at Bind
// rather than silently reading HKEY_CLASSES_ROOT.
type Hive uint8

// The five hives a configuration is ever kept in.
const (
	// LocalMachine is HKEY_LOCAL_MACHINE, which is machine-wide and needs
	// administrator rights to write.
	LocalMachine Hive = iota + 1

	// CurrentUser is HKEY_CURRENT_USER, which is per user and writable by that
	// user.
	CurrentUser

	// ClassesRoot is HKEY_CLASSES_ROOT.
	ClassesRoot

	// Users is HKEY_USERS.
	Users

	// CurrentConfig is HKEY_CURRENT_CONFIG.
	CurrentConfig
)

// String is the hive's own Win32 name, which is what a report opens with.
func (h Hive) String() string {
	switch h {
	case LocalMachine:
		return "HKEY_LOCAL_MACHINE"
	case CurrentUser:
		return "HKEY_CURRENT_USER"
	case ClassesRoot:
		return "HKEY_CLASSES_ROOT"
	case Users:
		return "HKEY_USERS"
	case CurrentConfig:
		return "HKEY_CURRENT_CONFIG"
	default:
		return "an unknown hive"
	}
}

// View chooses which side of the registry redirector a 32-bit and a 64-bit
// process see, and it is [WithView]'s subject.
type View uint8

const (
	// ViewNative is the view the running process gets by default: a 32-bit
	// process on 64-bit Windows is redirected into WOW6432Node and a 64-bit one
	// is not. It is the default.
	ViewNative View = iota

	// View64 is the 64-bit view, whatever the process's own bitness.
	View64

	// View32 is the 32-bit view, under WOW6432Node.
	View32
)

// ErrNoRegistry reports a machine with no Windows registry on it.
//
// It is what a source or a sink built without [Store] refuses with, at Bind and
// before any load, on every operating system but Windows. Supplying a [Registry]
// is what makes this package usable elsewhere, and it is how its own tests run.
//
// It wraps [ferry.ErrPlane], and it stays reachable under ferry's wrapper, so
// errors.Is answers for it on what [ferry.Load] and [ferry.Dump] returned.
var ErrNoRegistry = errors.New("winreg: there is no Windows registry here")

// noRegistry states the class this driver has an opinion about and keeps
// [ErrNoRegistry] reachable underneath it. It is the refusal every non-Windows
// build of [open] gives (ADR-0004: what Bind may refuse for is what it can see
// without touching the plane, and the plane not existing on this operating
// system is exactly that).
func noRegistry() error {
	return fmt.Errorf("%w: %w: this program is not running on Windows, so there is no registry to reach: "+
		"hand this driver a registry of your own with winreg.Store", ferry.ErrPlane, ErrNoRegistry)
}
