package main

import (
	"errors"
	"io"
	"os"
	"syscall"
	"unsafe"
)

//go:generate go run ../../mksyscall_windows.go -output zsyscall_windows.go vhd.go
//sys openVirtualDisk(typ *virtualStorageType, path string, accessMask uint32, flags uint32, parameters *openVirtualDiskParameters, handle *syscall.Handle) (e error) = virtdisk.OpenVirtualDisk
//sys attachVirtualDisk(handle syscall.Handle, sd uintptr, flags uint32, pflags uint32, parameters uintptr, overlapped uintptr) (e error) = virtdisk.AttachVirtualDisk
//sys getVirtualDiskInformation(handle syscall.Handle, size *uint32, info unsafe.Pointer, used *uint32) (e error) = virtdisk.GetVirtualDiskInformation
//sys getOverlappedResult(handle syscall.Handle, o *syscall.Overlapped, n *uint32, wait uint32) (err error) = kernelbase.GetOverlappedResult

type virtualStorageType struct {
	DeviceID uint32
	VendorID [16]byte
}

type openVirtualDiskParameters struct {
	Version                    uint32
	GetInfoOnly, ReadOnly      uint32
	ResiliencyGUID, SnapshotID [16]byte
}

type getVirtualDiskInfoSize struct {
	Version                   uint32
	_                         uint32
	VirtualSize, PhysicalSize int64
	BlockSize, SectorSize     uint32
}

const _ATTACH_VIRTUAL_DISK_FLAG_NO_LOCAL_HOST = 0x8

type vhd struct {
	h    syscall.Handle
	name string
}

func waitOp(h syscall.Handle, o *syscall.Overlapped, err error, n uint32) (int, error) {
	if err == syscall.ERROR_IO_PENDING {
		err = getOverlappedResult(h, o, &n, 1)
	}
	if err != nil && err != syscall.ERROR_BUFFER_OVERFLOW {
		n = 0
	}
	return int(n), err
}

func (v *vhd) ReadAt(b []byte, off int64) (int, error) {
	var nu uint32
	o := syscall.Overlapped{Offset: uint32(off), OffsetHigh: uint32(off >> 32)}
	n, err := waitOp(v.h, &o, syscall.ReadFile(v.h, b, &nu, &o), nu)
	if err != nil {
		return n, &os.PathError{Op: "read", Path: v.name, Err: err}
	}
	if int(n) < len(b) {
		err = io.EOF
	}
	return int(n), err
}

func (v *vhd) WriteAt(b []byte, off int64) (int, error) {
	var nu uint32
	o := syscall.Overlapped{Offset: uint32(off), OffsetHigh: uint32(off >> 32)}
	n, err := waitOp(v.h, &o, syscall.WriteFile(v.h, b, &nu, &o), nu)
	if err == syscall.ERROR_IO_PENDING {
		syscall.WaitForSingleObject(v.h, syscall.INFINITE)
		if o.Internal != 0 {
			err = syscall.Errno(o.Internal)
		}
	}
	if err != nil {
		return n, &os.PathError{Op: "write", Path: v.name, Err: err}
	}
	if int(n) < len(b) {
		return n, &os.PathError{Op: "write", Path: v.name, Err: errors.New("truncated write")}
	}
	return n, err
}

func (v *vhd) Length() (int64, error) {
	info := getVirtualDiskInfoSize{Version: 1}
	size := uint32(unsafe.Sizeof(info))
	err := getVirtualDiskInformation(v.h, &size, unsafe.Pointer(&info), nil)
	if err != nil {
		return 0, &os.PathError{Op: "GetVirtualDiskInformation", Path: v.name, Err: err}
	}
	return info.VirtualSize, nil
}

func openVhd(p string) (*vhd, error) {
	var h syscall.Handle
	params := openVirtualDiskParameters{
		Version: 2,
	}
	err := openVirtualDisk(&virtualStorageType{}, p, 0, 0, &params, &h)
	if err != nil {
		return nil, &os.PathError{Op: "OpenVirtualDisk", Path: p, Err: err}
	}
	return &vhd{h, p}, nil
}

func (v *vhd) AttachRaw() error {
	err := attachVirtualDisk(v.h, 0, _ATTACH_VIRTUAL_DISK_FLAG_NO_LOCAL_HOST, 0, 0, 0)
	if err != nil {
		return &os.PathError{Op: "AttachVirtualDisk", Path: v.name, Err: err}
	}
	return nil
}
