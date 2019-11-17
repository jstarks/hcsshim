package cim

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

type fileInfoInternal struct {
	Attributes uint32
	FileSize   int64

	CreationTime   windows.Filetime
	LastWriteTime  windows.Filetime
	ChangeTime     windows.Filetime
	LastAccessTime windows.Filetime

	SecurityDescriptorBuffer unsafe.Pointer
	SecurityDescriptorSize   uint32

	ReparseDataBuffer unsafe.Pointer
	ReparseDataSize   uint32

	ExtendedAttributes unsafe.Pointer
	EACount            uint32
}

type fsHandle uintptr
type streamHandle uintptr

// Writer represents a single CimFS filesystem. On disk, the image is
// composed of a filesystem file and several object ID and region files.
type Writer struct {
	handle       fsHandle
	activeStream streamHandle
}

func Create(p string) (*Writer, error) {
	return create(filepath.Dir(p), "", filepath.Base(p))
}

func Append(p string, newfsname string) (*Writer, error) {
	return create(filepath.Dir(p), filepath.Base(p), newfsname)
}

func create(imagePath string, oldFSName string, newFSName string) (*Writer, error) {
	var err error
	var oldNameBytes *uint16
	if oldFSName != "" {
		oldNameBytes, err = windows.UTF16PtrFromString(oldFSName)
		if err != nil {
			return nil, err
		}
	}
	var newNameBytes *uint16
	if newFSName != "" {
		newNameBytes, err = windows.UTF16PtrFromString(newFSName)
		if err != nil {
			return nil, err
		}
	}
	var handle fsHandle
	if err := cimCreateImage(imagePath, oldNameBytes, newNameBytes, &handle); err != nil {
		return nil, err
	}
	return &Writer{handle: handle}, nil
}

func (ft Filetime) toWindows() windows.Filetime {
	return windows.Filetime{
		LowDateTime:  uint32(ft),
		HighDateTime: uint32(ft >> 32),
	}
}

// AddFile adds an entry for a file to the image. The file is added at the
// specified path. After calling this function, the file is set as the active
// stream for the image, so data can be written by calling `Write`.
func (w *Writer) AddFile(path string, info *FileInfo) error {
	infoInternal := &fileInfoInternal{
		Attributes:     info.Attributes,
		FileSize:       info.Size,
		CreationTime:   info.CreationTime.toWindows(),
		LastWriteTime:  info.LastWriteTime.toWindows(),
		ChangeTime:     info.ChangeTime.toWindows(),
		LastAccessTime: info.LastAccessTime.toWindows(),
	}

	if len(info.SecurityDescriptor) > 0 {
		infoInternal.SecurityDescriptorBuffer = unsafe.Pointer(&info.SecurityDescriptor[0])
		infoInternal.SecurityDescriptorSize = uint32(len(info.SecurityDescriptor))
	}

	if len(info.ReparseData) > 0 {
		infoInternal.ReparseDataBuffer = unsafe.Pointer(&info.ReparseData[0])
		infoInternal.ReparseDataSize = uint32(len(info.ReparseData))
	}

	if len(info.ExtendedAttributes) > 0 {
		infoInternal.ExtendedAttributes = unsafe.Pointer(&info.ExtendedAttributes[0])
		infoInternal.EACount = uint32(len(info.ExtendedAttributes))
	}

	return cimCreateFile(w.handle, path, infoInternal, &w.activeStream)
}

// Write writes bytes to the active stream.
func (w *Writer) Write(p []byte) (int, error) {
	if w.activeStream == 0 {
		return 0, errors.New("No active stream")
	}

	err := cimWriteStream(w.activeStream, uintptr(unsafe.Pointer(&p[0])), uint32(len(p)))
	if err != nil {
		return 0, err
	}

	return len(p), nil
}

// CloseStream closes the active stream.
func (w *Writer) CloseStream() error {
	if w.activeStream == 0 {
		return errors.New("No active stream")
	}

	return cimCloseStream(w.activeStream)
}

// TODO do this as part of Close?
func (w *Writer) Commit() error {
	return cimCommitImage(w.handle)
}

// Close closes the CimFS filesystem.
func (w *Writer) Close() error {
	return cimCloseImage(w.handle)
}

// RemoveFile deletes the file at `path` from the image.
func (w *Writer) RemoveFile(path string) error {
	return cimDeletePath(w.handle, path)
}

// AddLink adds a hard link from `oldPath` to `newPath` in the image.
func (w *Writer) AddLink(oldPath string, newPath string) error {
	return cimCreateHardLink(w.handle, newPath, oldPath)
}
