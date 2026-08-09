//go:build windows

package winreg

import (
	"context"
	"errors"
	"strconv"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// open is the machine's own registry, under one hive, one subkey path and one
// view.
//
// It does no I/O. Every call the returned [Registry] answers opens the one key it
// needs and closes it again, so nothing here holds a handle and nothing shares
// one between goroutines - which is what makes [reader.MaxConcurrent]'s promise
// cost one mutex over the key function and nothing else (ADR-0019).
func open(h Hive, base string, v View) (Registry, error) {
	root, err := rootOf(h)
	if err != nil {
		return nil, err
	}

	return &machine{root: root, base: base, wow: viewFlag(v)}, nil
}

// machine is the Windows registry as this driver's seam sees it.
type machine struct {
	root registry.Key
	base string
	wow  uint32
}

var (
	_ Registry = (*machine)(nil)
	_ Notifier = (*machine)(nil)
)

// rootOf is the predefined key one [Hive] names. An unknown hive is refused by
// [config.settle] before this is reached, so the default arm is the guard rather
// than a case.
func rootOf(h Hive) (registry.Key, error) {
	switch h {
	case LocalMachine:
		return registry.LOCAL_MACHINE, nil
	case CurrentUser:
		return registry.CURRENT_USER, nil
	case ClassesRoot:
		return registry.CLASSES_ROOT, nil
	case Users:
		return registry.USERS, nil
	case CurrentConfig:
		return registry.CURRENT_CONFIG, nil
	default:
		return 0, optionError("the hive must be one of winreg.LocalMachine, winreg.CurrentUser, " +
			"winreg.ClassesRoot, winreg.Users or winreg.CurrentConfig")
	}
}

// viewFlag is the WOW64 access bit one [View] asks for, and zero for the view the
// process would get on its own.
func viewFlag(v View) uint32 {
	switch v {
	case View64:
		return registry.WOW64_64KEY
	case View32:
		return registry.WOW64_32KEY
	default:
		return 0
	}
}

// path is the full subkey path under the hive: what the driver was built over,
// and the address's own subkey below it.
func (m *machine) path(subkey string) string { return joinKey(m.base, subkey) }

// openKey opens one subkey, reporting a key that is not there as absence rather
// than as a failure, which is [Registry]'s own rule.
func (m *machine) openKey(subkey string, access uint32) (registry.Key, bool, error) {
	k, err := registry.OpenKey(m.root, m.path(subkey), access|m.wow)
	if errors.Is(err, registry.ErrNotExist) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, err
	}

	return k, true, nil
}

// Get reads one value and reports what the registry records it as.
func (m *machine) Get(ctx context.Context, subkey, name string) (Datum, bool, error) {
	if err := ctx.Err(); err != nil {
		return Datum{}, false, err
	}

	k, found, err := m.openKey(subkey, registry.QUERY_VALUE)
	if err != nil || !found {
		return Datum{}, false, err
	}

	defer func() { _ = k.Close() }()

	_, kind, err := k.GetValue(name, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return Datum{}, false, nil
	}

	if err != nil {
		return Datum{}, false, err
	}

	d, err := datum(k, name, kind)

	return d, err == nil, err
}

// datum reads the payload of one value, by the type the registry recorded.
//
// REG_EXPAND_SZ goes through the same call as REG_SZ, which is the one that reads
// what is actually stored: expanding it is a separate call and this driver never
// makes it.
func datum(k registry.Key, name string, kind uint32) (Datum, error) {
	switch kind {
	case registry.SZ, registry.EXPAND_SZ:
		text, _, err := k.GetStringValue(name)

		return Datum{Type: stringType(kind), Text: text}, err
	case registry.DWORD, registry.QWORD:
		n, _, err := k.GetIntegerValue(name)

		return Datum{Type: numberType(kind), Text: strconv.FormatUint(n, base10)}, err
	case registry.BINARY:
		b, _, err := k.GetBinaryValue(name)

		return Datum{Type: TypeBinary, Binary: b}, err
	case registry.MULTI_SZ:
		return Datum{Type: TypeMultiString}, nil
	default:
		return Datum{Type: TypeOther}, nil
	}
}

// stringType and numberType are the two arms of [datum] that carry more than one
// registry type each.
func stringType(kind uint32) Type {
	if kind == registry.EXPAND_SZ {
		return TypeExpandString
	}

	return TypeString
}

func numberType(kind uint32) Type {
	if kind == registry.QWORD {
		return TypeQWord
	}

	return TypeDWord
}

// List answers with the value names and the immediate subkey names under one key.
func (m *machine) List(ctx context.Context, subkey string) (Listing, bool, error) {
	if err := ctx.Err(); err != nil {
		return Listing{}, false, err
	}

	k, found, err := m.openKey(subkey, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil || !found {
		return Listing{}, false, err
	}

	defer func() { _ = k.Close() }()

	values, err := k.ReadValueNames(0)
	if err != nil {
		return Listing{}, false, err
	}

	keys, err := k.ReadSubKeyNames(0)
	if err != nil {
		return Listing{}, false, err
	}

	return Listing{Values: values, Keys: keys}, true, nil
}

// Set writes one value, creating every key on the way down to it.
func (m *machine) Set(ctx context.Context, subkey, name string, d Datum) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	k, _, err := registry.CreateKey(m.root, m.path(subkey), registry.WRITE|m.wow)
	if err != nil {
		return err
	}

	defer func() { _ = k.Close() }()

	switch d.Type {
	case TypeBinary:
		return k.SetBinaryValue(name, d.Binary)
	case TypeExpandString:
		return k.SetExpandStringValue(name, d.Text)
	default:
		return k.SetStringValue(name, d.Text)
	}
}

// Create makes one key and everything above it, and opens an existing one, which
// is also the permission question a save asks when it starts.
func (m *machine) Create(ctx context.Context, subkey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	k, _, err := registry.CreateKey(m.root, m.path(subkey), registry.WRITE|m.wow)
	if err != nil {
		return err
	}

	return k.Close()
}

// DeleteValue removes one value, and reports nothing for a value or a key that is
// not there.
func (m *machine) DeleteValue(ctx context.Context, subkey, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	k, found, err := m.openKey(subkey, registry.SET_VALUE)
	if err != nil || !found {
		return err
	}

	defer func() { _ = k.Close() }()

	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}

	return nil
}

// DeleteKey removes one key and everything under it.
//
// The recursion is this driver's rather than the registry's: RegDeleteKey refuses
// a key that still has subkeys, and a caller of [Registry] should not have to know
// that.
func (m *machine) DeleteKey(ctx context.Context, subkey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	children, found, err := m.childrenOf(subkey)
	if err != nil || !found {
		return err
	}

	for _, name := range children {
		if err := m.DeleteKey(ctx, joinKey(subkey, name)); err != nil {
			return err
		}
	}

	if err := registry.DeleteKey(m.root, m.path(subkey)); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}

	return nil
}

// childrenOf is the immediate subkey names of one key, read and the handle closed
// before anything is deleted: a delete under an open enumeration is what leaves a
// key half removed.
func (m *machine) childrenOf(subkey string) ([]string, bool, error) {
	k, found, err := m.openKey(subkey, registry.ENUMERATE_SUB_KEYS)
	if err != nil || !found {
		return nil, false, err
	}

	names, err := k.ReadSubKeyNames(0)

	if closeErr := k.Close(); err == nil {
		err = closeErr
	}

	return names, true, err
}

// notifyFilter is what a change means here: a value written, and a subkey added
// or removed. A change to a key's own security descriptor is not a change to what
// it holds.
const notifyFilter = windows.REG_NOTIFY_CHANGE_NAME | windows.REG_NOTIFY_CHANGE_LAST_SET

// Notify blocks until something under this driver's key changes.
//
// The whole subtree is watched, which is what makes one watch answer for a schema
// whose addresses are spread over several subkeys.
//
// There is no interval anywhere in it. RegNotifyChangeKeyValue signals an event
// and the wait is on that event and on a second one the context closes, so a
// cancelled watch returns at once and a quiet registry costs nothing (ADR-0020).
func (m *machine) Notify(ctx context.Context) (bool, error) {
	k, found, err := m.openKey("", registry.NOTIFY)
	if err != nil {
		return false, err
	}

	if !found {
		return false, noRegistry()
	}

	defer func() { _ = k.Close() }()

	changed, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return false, err
	}

	defer func() { _ = windows.CloseHandle(changed) }()

	if err := windows.RegNotifyChangeKeyValue(windows.Handle(k), true, notifyFilter, changed, true); err != nil {
		return false, err
	}

	return waitFor(ctx, changed)
}

// waitFor blocks on the change event and on a second event the context closes,
// and reports which of the two woke it.
func waitFor(ctx context.Context, changed windows.Handle) (bool, error) {
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return false, err
	}

	defer func() { _ = windows.CloseHandle(stop) }()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			_ = windows.SetEvent(stop)
		case <-done:
		}
	}()

	woke, err := windows.WaitForMultipleObjects([]windows.Handle{changed, stop}, false, windows.INFINITE)
	if err != nil {
		return false, err
	}

	return woke == windows.WAIT_OBJECT_0, nil
}
