//go:build windows

package protect

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The DPAPI-NG entry points, resolved lazily so that loading this package costs
// nothing until something is actually protected.
//
// They are declared here rather than taken from golang.org/x/sys/windows because
// that module does not wrap them: as of v0.29.0 it has the classic CryptProtect
// family and nothing from ncrypt.dll. Declaring five procedures is the whole of
// what that costs, and it is why this driver takes no dependency beyond the one
// ADR-0002 already argued for.
var (
	ncrypt = windows.NewLazySystemDLL("ncrypt.dll")

	procCreateDescriptor  = ncrypt.NewProc("NCryptCreateProtectionDescriptor")
	procCloseDescriptor   = ncrypt.NewProc("NCryptCloseProtectionDescriptor")
	procProtectSecret     = ncrypt.NewProc("NCryptProtectSecret")
	procUnprotectSecret   = ncrypt.NewProc("NCryptUnprotectSecret")
	procFreeProtectBuffer = ncrypt.NewProc("NCryptFreeBuffer")
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
func (dpapi) Protect(ctx context.Context, descriptor string, plaintext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	h, err := descriptorHandle(descriptor)
	if err != nil {
		return nil, err
	}

	defer func() { _, _, _ = procCloseDescriptor.Call(uintptr(h)) }()

	var (
		out *byte
		n   uint32
	)

	st, _, _ := procProtectSecret.Call(uintptr(h), silentFlag,
		uintptr(unsafe.Pointer(unsafe.SliceData(plaintext))), uintptr(len(plaintext)),
		0, 0, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&n)))
	if st != 0 {
		return nil, status("NCryptProtectSecret", st)
	}

	return taken(out, n), nil
}

// Unprotect decrypts what Protect wrote. The descriptor is not repeated, because
// the blob carries it.
func (dpapi) Unprotect(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var (
		out *byte
		n   uint32
	)

	st, _, _ := procUnprotectSecret.Call(0, silentFlag,
		uintptr(unsafe.Pointer(unsafe.SliceData(ciphertext))), uintptr(len(ciphertext)),
		0, 0, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&n)))
	if st != 0 {
		return nil, status("NCryptUnprotectSecret", st)
	}

	return taken(out, n), nil
}

// descriptorHandle turns a rule string into the handle the protection call takes.
func descriptorHandle(rule string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(rule)
	if err != nil {
		return 0, fmt.Errorf("protect: the protection descriptor cannot be spelled in UTF-16: %w", err)
	}

	var h windows.Handle

	st, _, _ := procCreateDescriptor.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h)))
	if st != 0 {
		return 0, status("NCryptCreateProtectionDescriptor", st)
	}

	return h, nil
}

// taken copies out of the buffer DPAPI-NG allocated and frees it, so that
// nothing this package hands back points into memory the API owns.
func taken(p *byte, n uint32) []byte {
	defer func() { _, _, _ = procFreeProtectBuffer.Call(uintptr(unsafe.Pointer(p))) }()

	out := make([]byte, n)
	copy(out, unsafe.Slice(p, n))

	return out
}

// status is what a non-zero SECURITY_STATUS reads as. The code is printed as it
// is documented, in hexadecimal, because that is what an operator searching for
// it will be typing.
func status(call string, st uintptr) error {
	return fmt.Errorf("protect: %s failed with status 0x%08x", call, uint32(st))
}
