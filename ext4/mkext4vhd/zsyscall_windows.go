// Code generated mksyscall_windows.exe DO NOT EDIT

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var _ unsafe.Pointer

// Do the interface allocations only once for common
// Errno values.
const (
	errnoERROR_IO_PENDING = 997
)

var (
	errERROR_IO_PENDING error = syscall.Errno(errnoERROR_IO_PENDING)
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case errnoERROR_IO_PENDING:
		return errERROR_IO_PENDING
	}
	// TODO: add more here, after collecting data on the common
	// error values see on Windows. (perhaps when running
	// all.bat?)
	return e
}

var (
	modvirtdisk   = windows.NewLazySystemDLL("virtdisk.dll")
	modkernelbase = windows.NewLazySystemDLL("kernelbase.dll")

	procOpenVirtualDisk           = modvirtdisk.NewProc("OpenVirtualDisk")
	procAttachVirtualDisk         = modvirtdisk.NewProc("AttachVirtualDisk")
	procGetVirtualDiskInformation = modvirtdisk.NewProc("GetVirtualDiskInformation")
	procGetOverlappedResult       = modkernelbase.NewProc("GetOverlappedResult")
)

func openVirtualDisk(typ *virtualStorageType, path string, accessMask uint32, flags uint32, parameters *openVirtualDiskParameters, handle *syscall.Handle) (e error) {
	var _p0 *uint16
	_p0, e = syscall.UTF16PtrFromString(path)
	if e != nil {
		return
	}
	return _openVirtualDisk(typ, _p0, accessMask, flags, parameters, handle)
}

func _openVirtualDisk(typ *virtualStorageType, path *uint16, accessMask uint32, flags uint32, parameters *openVirtualDiskParameters, handle *syscall.Handle) (e error) {
	r0, _, _ := syscall.Syscall6(procOpenVirtualDisk.Addr(), 6, uintptr(unsafe.Pointer(typ)), uintptr(unsafe.Pointer(path)), uintptr(accessMask), uintptr(flags), uintptr(unsafe.Pointer(parameters)), uintptr(unsafe.Pointer(handle)))
	if r0 != 0 {
		e = syscall.Errno(r0)
	}
	return
}

func attachVirtualDisk(handle syscall.Handle, sd uintptr, flags uint32, pflags uint32, parameters uintptr, overlapped uintptr) (e error) {
	r0, _, _ := syscall.Syscall6(procAttachVirtualDisk.Addr(), 6, uintptr(handle), uintptr(sd), uintptr(flags), uintptr(pflags), uintptr(parameters), uintptr(overlapped))
	if r0 != 0 {
		e = syscall.Errno(r0)
	}
	return
}

func getVirtualDiskInformation(handle syscall.Handle, size *uint32, info unsafe.Pointer, used *uint32) (e error) {
	r0, _, _ := syscall.Syscall6(procGetVirtualDiskInformation.Addr(), 4, uintptr(handle), uintptr(unsafe.Pointer(size)), uintptr(info), uintptr(unsafe.Pointer(used)), 0, 0)
	if r0 != 0 {
		e = syscall.Errno(r0)
	}
	return
}

func getOverlappedResult(handle syscall.Handle, o *syscall.Overlapped, n *uint32, wait uint32) (err error) {
	r1, _, e1 := syscall.Syscall6(procGetOverlappedResult.Addr(), 4, uintptr(handle), uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(n)), uintptr(wait), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}
