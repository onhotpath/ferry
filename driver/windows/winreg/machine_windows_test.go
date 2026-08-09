//go:build windows

package winreg_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/winreg"
)

// The tests in this file are the only ones in the package that reach the
// machine's own registry, and they are the reason the Windows job exists.
//
// Every other test hands the driver a [winreg.Store], which replaces the whole of
// the tagged seam: on a Windows runner those tests execute exactly the same
// statements they execute on Linux. What is asserted here is what only Windows can
// answer - the syscalls, the value types as the registry actually records them,
// the redirector, and the change notification.
//
// They run under HKEY_CURRENT_USER, which needs no elevation, in a key named for
// this process and removed again when the test ends. A machine whose registry
// cannot be written there skips rather than fails, because a sandbox with no
// writable hive is not this driver being broken.

// scratchKeys numbers the scratch keys this process hands out, and it is what
// makes each of them one key nobody else is using.
//
// A timestamp does not. time.Now on Windows reads _SYSTEM_TIME straight out of
// KUSER_SHARED_DATA, which the kernel refreshes once per clock tick - 15.6ms
// unless some process on the machine has raised the timer resolution - so every
// UnixNano taken inside one tick is the same number. Every test in this file
// calls t.Parallel and then scratchKey, so all of them resume together and take
// that number microseconds apart: a timestamped path hands the whole file one
// shared key, and each test's own cleanup then removes the key the others are
// still working in. That is what the first Windows run of this file measured -
// a load that came back empty because another test had swept the key, and an
// ERROR_KEY_DELETED from a key TestMachineWatchFires still held a notification
// handle on, which is what keeps a removed key alive and refusing.
var scratchKeys atomic.Uint64

// scratchKey makes one key nobody else is using and removes it, and everything
// under it, when the test ends.
//
// The process id keeps two `go test` runs on one machine apart, and the counter
// keeps this run's own parallel tests apart.
func scratchKey(t *testing.T) string {
	t.Helper()

	path := fmt.Sprintf(`Software\ferry-winreg-test\%d-%d`, os.Getpid(), scratchKeys.Add(1))

	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Skipf("this machine has no writable registry under HKEY_CURRENT_USER: %v", err)
	}

	if err := k.Close(); err != nil {
		t.Fatalf("closing the scratch key: %v", err)
	}

	t.Cleanup(func() { removeTree(registry.CURRENT_USER, path) })

	return path
}

// removeTree removes one key and everything under it, which is what a cleanup
// needs and what RegDeleteKey alone will not do.
func removeTree(root registry.Key, path string) {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}

	names, _ := k.ReadSubKeyNames(0)
	_ = k.Close()

	for _, name := range names {
		removeTree(root, path+`\`+name)
	}

	_ = registry.DeleteKey(root, path)
}

// openScratch opens one scratch key for reading and writing.
func openScratch(t *testing.T, path string) registry.Key {
	t.Helper()

	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}

	t.Cleanup(func() { _ = k.Close() })

	return k
}

// TestWindowsHoldsAValueAndASubkeyOfOneNameAtOnce is the measurement this whole
// module rests on, and it is asserted against Windows rather than against the
// in-repo store that was written to model it.
//
// A field tagged a and a nested struct tagged a are two addresses here, and they
// are two addresses only because the registry keeps values and subkeys in
// separate namespaces. If it did not, the address mapping would be wrong, the
// two-namespace argument for admitting this module would be wrong with it, and
// the answer is to stop rather than to work around it.
func TestWindowsHoldsAValueAndASubkeyOfOneNameAtOnce(t *testing.T) {
	t.Parallel()

	path := scratchKey(t)
	k := openScratch(t, path)

	if err := k.SetStringValue("a", "the value"); err != nil {
		t.Fatalf("writing the value a: %v", err)
	}

	sub := openScratch(t, path+`\a`)
	if err := sub.SetStringValue("host", "the subkey"); err != nil {
		t.Fatalf("writing under the subkey a: %v", err)
	}

	values, err := k.ReadValueNames(0)
	if err != nil {
		t.Fatalf("listing the values: %v", err)
	}

	keys, err := k.ReadSubKeyNames(0)
	if err != nil {
		t.Fatalf("listing the subkeys: %v", err)
	}

	if !slices.Contains(values, "a") || !slices.Contains(keys, "a") {
		t.Fatalf("STOP: this Windows holds values %v and subkeys %v under one key, so a value and a subkey "+
			"of one name do NOT coexist. Every address this driver computes assumes they do, and so does the "+
			"argument that admitted this module. Nothing here should be patched around it", values, keys)
	}

	got, err := ferry.Load[bothNamespaces](t.Context(), winreg.NewSource(winreg.CurrentUser, path))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Leaf != "the value" || got.Section.Host != "the subkey" {
		t.Errorf("loaded %+v, want the value and the subkey read separately", got)
	}
}

// TestMachineRoundTrip is the driver's own save and load over the real registry,
// with no store in the middle.
func TestMachineRoundTrip(t *testing.T) {
	t.Parallel()

	path := scratchKey(t)
	want := typed{Text: "h", Expand: "e", Count: 42, Big: 18446744073709551615, Raw: []byte{0x00, 0xff, 'A'}}

	if err := ferry.Dump(t.Context(), want, winreg.NewSink(winreg.CurrentUser, path)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	got, err := ferry.Load[typed](t.Context(), winreg.NewSource(winreg.CurrentUser, path))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Text != want.Text || got.Expand != want.Expand || got.Count != want.Count || got.Big != want.Big ||
		string(got.Raw) != string(want.Raw) {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
}

// TestMachineRefusesACollidingSchema is the fold asserted where the fold is real:
// this registry is the case-insensitive store the check exists for.
func TestMachineRefusesACollidingSchema(t *testing.T) {
	t.Parallel()

	path := scratchKey(t)

	_, err := ferry.Load[hostTwice](t.Context(), winreg.NewSource(winreg.CurrentUser, path))
	if !errors.Is(err, ferry.ErrPlane) {
		t.Fatalf("Load answered %v, want an error reaching ferry.ErrPlane", err)
	}

	for _, at := range []string{"/Host", "/host"} {
		if !strings.Contains(err.Error(), at) {
			t.Errorf("the refusal does not name %s: %v", at, err)
		}
	}
}

// TestMachineKeepsAnExpandableString is the type-preserving write against the
// registry's own type tags, which is the one thing a self-consistent store cannot
// prove.
//
// The text is read exactly as stored - Windows is not asked to expand it - and the
// value is still REG_EXPAND_SZ after a save has written new text over it.
func TestMachineKeepsAnExpandableString(t *testing.T) {
	t.Parallel()

	path := scratchKey(t)
	k := openScratch(t, path)

	if err := k.SetExpandStringValue("text", `%SystemRoot%\before`); err != nil {
		t.Fatalf("writing the expandable string: %v", err)
	}

	got, err := ferry.Load[oneText](t.Context(), winreg.NewSource(winreg.CurrentUser, path))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Text != `%SystemRoot%\before` {
		t.Errorf("loaded %q, want the stored text with nothing expanded", got.Text)
	}

	after := oneText{Text: `%SystemRoot%\after`}
	if err := ferry.Dump(t.Context(), after, winreg.NewSink(winreg.CurrentUser, path)); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	_, kind, err := k.GetValue("text", nil)
	if err != nil {
		t.Fatalf("reading back the type: %v", err)
	}

	if kind != registry.EXPAND_SZ {
		t.Errorf("the save stored type %d, want REG_EXPAND_SZ (%d)", kind, registry.EXPAND_SZ)
	}

	text, _, err := k.GetStringValue("text")
	if err != nil || text != after.Text {
		t.Errorf("the value holds %q (%v), want %q", text, err, after.Text)
	}
}

// TestMachineUnderANamedView is [winreg.WithView] over the real redirector.
//
// What it asserts is that naming the view routes every call in this driver
// through the same side of it, the sweep's own removals included: a save, a
// replacement and a load all agree. It does not assert that the two views hold
// different keys, because HKEY_CURRENT_USER outside Software\Classes is not on
// the redirection list at all, and the keys that are need HKEY_LOCAL_MACHINE and
// the rights that go with it.
func TestMachineUnderANamedView(t *testing.T) {
	t.Parallel()

	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("the redirector has two sides only on 64-bit Windows, and this is %s", runtime.GOARCH)
	}

	path := scratchKey(t)
	view := winreg.WithView(winreg.View64)

	both := tagsMap{Tags: map[string]string{"a": "1", "b": "2"}}
	if err := ferry.Dump(t.Context(), both, winreg.NewSink(winreg.CurrentUser, path, view)); err != nil {
		t.Fatalf("the first Dump: %v", err)
	}

	one := tagsMap{Tags: map[string]string{"a": "1"}}
	if err := ferry.Dump(t.Context(), one, winreg.NewSink(winreg.CurrentUser, path, view)); err != nil {
		t.Fatalf("the second Dump: %v", err)
	}

	got, err := ferry.Load[tagsMap](t.Context(), winreg.NewSource(winreg.CurrentUser, path, view))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Tags) != 1 || got.Tags["a"] != "1" {
		t.Errorf("loaded %v, want the one member the second save wrote", got.Tags)
	}
}

// TestMachineWatchFires is RegNotifyChangeKeyValue end to end, and it covers the
// two things only the real notification can show: that the registration is live
// when the constructor returns, and that a change landing while the callback runs
// is still reported.
func TestMachineWatchFires(t *testing.T) {
	t.Parallel()

	path := scratchKey(t)
	k := openScratch(t, path)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		inside  = make(chan struct{})
		release = make(chan struct{})
		again   = make(chan struct{}, 1)
		first   = true
	)

	_ = winreg.NewSource(winreg.CurrentUser, path, winreg.Watch(ctx, func(context.Context) {
		if first {
			first = false

			close(inside)
			<-release

			return
		}

		select {
		case again <- struct{}{}:
		default:
		}
	}))

	if err := k.SetStringValue("text", "first"); err != nil {
		t.Fatalf("writing the first change: %v", err)
	}

	select {
	case <-inside:
	case <-time.After(30 * time.Second):
		t.Fatal("the watch never called back")
	}

	if err := k.SetStringValue("text", "during"); err != nil {
		t.Fatalf("writing the change inside the callback: %v", err)
	}

	close(release)

	select {
	case <-again:
	case <-time.After(30 * time.Second):
		t.Fatal("a change that landed while the callback was running was lost")
	}
}
