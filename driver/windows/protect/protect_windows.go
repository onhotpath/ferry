//go:build windows

package protect

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The DPAPI-NG entry points, resolved lazily so that loading this package costs
// nothing until something is actually protected.
//
// They are declared here rather than taken from golang.org/x/sys/windows because
// that module does not wrap them: as of v0.29.0 it has the classic CryptProtect
// family and nothing from ncrypt.dll. Declaring four procedures is the whole of
// what that costs, and it is why this driver takes no dependency beyond the one
// ADR-0002 already argued for.
//
// NCryptFreeBuffer is deliberately not among them. See [taken].
var (
	ncrypt = windows.NewLazySystemDLL("ncrypt.dll")

	procCreateDescriptor = ncrypt.NewProc("NCryptCreateProtectionDescriptor")
	procCloseDescriptor  = ncrypt.NewProc("NCryptCloseProtectionDescriptor")
	procProtectSecret    = ncrypt.NewProc("NCryptProtectSecret")
	procUnprotectSecret  = ncrypt.NewProc("NCryptUnprotectSecret")
)

// silentFlag is NCRYPT_SILENT_FLAG: no user interface, ever. A configuration
// load that put a dialog on the screen would hang a service.
const silentFlag = 0x40

// open is the machine's own DPAPI-NG. It does no I/O and holds no handle: every
// call opens what it needs and closes it again, which is what makes this safe to
// enter from many goroutines with nothing shared between them (ADR-0019).
func open() (Protector, error) { return dpapi{}, nil }

// dpapi is DPAPI-NG behind this package's seam.
type dpapi struct{}

var _ Protector = dpapi{}

// Protect encrypts under one protection descriptor rule.
//
// The descriptor handle is opened for this call and closed again by
// [closeDescriptor], and a close that fails discards the ciphertext: a handle the
// API will not close is not a handle this package should go on believing it used.
func (dpapi) Protect(ctx context.Context, descriptor string, plaintext []byte) (blob []byte, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}

	if len(plaintext) == 0 {
		return nil, refusedEmpty("NCryptProtectSecret")
	}

	h, err := descriptorHandle(descriptor)
	if err != nil {
		return nil, err
	}

	defer func() {
		if cerr := closeDescriptor(h); cerr != nil {
			blob, err = nil, errors.Join(err, cerr)
		}
	}()

	var (
		out *byte
		n   uint32
	)

	st, _, _ := procProtectSecret.Call(uintptr(h), silentFlag,
		uintptr(unsafe.Pointer(unsafe.SliceData(plaintext))), uintptr(len(plaintext)),
		0, 0, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&n)))

	runtime.KeepAlive(plaintext)

	if st != 0 {
		return nil, status("NCryptProtectSecret", st)
	}

	return taken(out, n)
}

// Unprotect decrypts what Protect wrote. The descriptor is not repeated, because
// the blob carries it.
//
// The first argument is the optional out parameter that would hand back the
// descriptor the blob was protected under. It is passed as NULL, so no descriptor
// handle is opened here and there is none to close.
func (dpapi) Unprotect(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(ciphertext) == 0 {
		return nil, refusedEmpty("NCryptUnprotectSecret")
	}

	var (
		out *byte
		n   uint32
	)

	st, _, _ := procUnprotectSecret.Call(0, silentFlag,
		uintptr(unsafe.Pointer(unsafe.SliceData(ciphertext))), uintptr(len(ciphertext)),
		0, 0, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&n)))

	runtime.KeepAlive(ciphertext)

	if st != 0 {
		return nil, status("NCryptUnprotectSecret", st)
	}

	return taken(out, n)
}

// descriptorHandle turns a rule string into the handle the protection call takes.
//
// What comes back is a protection descriptor object, which
// NCryptCreateProtectionDescriptor documents as freed by
// NCryptCloseProtectionDescriptor and by nothing else. [closeDescriptor] is the
// one caller of that, and every path out of [dpapi.Protect] reaches it.
func descriptorHandle(rule string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(rule)
	if err != nil {
		return 0, fmt.Errorf("protect: the protection descriptor cannot be spelled in UTF-16: %w", err)
	}

	var h windows.Handle

	st, _, _ := procCreateDescriptor.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h)))

	// p is not read again in Go, so nothing else keeps the string alive across a
	// call that only ever saw its address as an integer.
	runtime.KeepAlive(p)

	if st != 0 {
		return 0, status("NCryptCreateProtectionDescriptor", st)
	}

	return h, nil
}

// closeDescriptor releases what [descriptorHandle] opened, and reports a close
// that failed rather than dropping it.
//
// A discarded status here would hide the two states worth knowing about: a handle
// the API says is invalid, which means this package is tracking one it does not
// own, and a descriptor object that was never freed, which is a leak per protected
// value.
func closeDescriptor(h windows.Handle) error {
	if st, _, _ := procCloseDescriptor.Call(uintptr(h)); st != 0 {
		return status("NCryptCloseProtectionDescriptor", st)
	}

	return nil
}

// taken copies out of the buffer DPAPI-NG allocated and frees it, so that nothing
// this package hands back points into memory the API owns.
//
// It frees with LocalFree and not with NCryptFreeBuffer. Both NCryptProtectSecret
// and NCryptUnprotectSecret document the pMemPara argument - the NULL this
// package passes in both calls - as: "If you set this argument to NULL, the
// LocalAlloc function is used internally to allocate memory and your application
// must call LocalFree to release memory pointed to by the ppbProtectedBlob
// parameter." NCryptFreeBuffer releases what an NCrypt allocator owns; it does not
// own a LocalAlloc block, so handing it one is two allocators disagreeing about a
// heap.
//
// A free that fails is reported and the plaintext is dropped with it. LocalFree
// refusing a pointer means this package passed one the process does not own, and
// carrying on from there is how a corruption stops being local.
func taken(p *byte, n uint32) ([]byte, error) {
	out := make([]byte, n)
	copy(out, unsafe.Slice(p, n))

	if _, err := windows.LocalFree(windows.Handle(unsafe.Pointer(p))); err != nil {
		return nil, fmt.Errorf("protect: LocalFree would not release the buffer DPAPI-NG allocated, which is a "+
			"pointer this process does not own: %w", err)
	}

	return out, nil
}

// refusedEmpty is the zero-length buffer neither call accepts: both document
// NTE_INVALID_PARAMETER for a length below one, and an empty Go slice has no
// backing array whose address is worth handing over.
func refusedEmpty(call string) error {
	return fmt.Errorf("protect: %s was handed nothing at all, and it takes at least one byte", call)
}

// status is what a non-zero SECURITY_STATUS reads as. The code is printed as it
// is documented, in hexadecimal, because that is what an operator searching for
// it will be typing.
func status(call string, st uintptr) error {
	return fmt.Errorf("protect: %s failed with status 0x%08x", call, uint32(st))
}
