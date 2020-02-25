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
	name         string
	handle       fsHandle
	activeName   string
	activeStream streamHandle
	activeLeft   int64
}

func Create(p string) (*Writer, error) {
	w, err := create(filepath.Dir(p), "", filepath.Base(p))
	if err != nil {
		err = &OpError{Cim: p, Op: "Create", Err: err}
	}
	return w, err
}

func Append(p string, newFSName string) (*Writer, error) {
	w, err := create(filepath.Dir(p), filepath.Base(p), newFSName)
	if err != nil {
		err = &PathError{Cim: p, Op: "Append", Path: newFSName, Err: err}
	}
	return w, err
}

func create(imagePath string, oldFSName string, newFSName string) (_ *Writer, err error) {
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
	return &Writer{handle: handle, name: filepath.Join(imagePath, newFSName)}, nil
}

func (ft Filetime) toWindows() windows.Filetime {
	return windows.Filetime{
		LowDateTime:  uint32(ft),
		HighDateTime: uint32(ft >> 32),
	}
}

func toNtPath(p string) string {
	p = filepath.FromSlash(p)
	for len(p) > 0 && p[0] == filepath.Separator {
		p = p[1:]
	}
	return p
}

// Equivalent to SDDL of "D:NO_ACCESS_CONTROL"
var nullSd = []byte{1, 0, 4, 128, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

// WriteFile adds an entry for a file to the image. The file is added at the
// specified path. After calling this function, the file is set as the active
// stream for the image, so data can be written by calling `Write`.
func (w *Writer) WriteFile(path string, info *FileInfo) error {
	err := w.closeStream()
	if err != nil {
		return err
	}
	infoInternal := &fileInfoInternal{
		Attributes:     info.Attributes,
		FileSize:       info.Size,
		CreationTime:   info.CreationTime.toWindows(),
		LastWriteTime:  info.LastWriteTime.toWindows(),
		ChangeTime:     info.ChangeTime.toWindows(),
		LastAccessTime: info.LastAccessTime.toWindows(),
	}
	sd := info.SecurityDescriptor
	if len(sd) == 0 {
		// Passing an empty security descriptor creates a CIM in a weird state.
		// Pass the NULL DACL.
		sd = nullSd
	}
	infoInternal.SecurityDescriptorBuffer = unsafe.Pointer(&sd[0])
	infoInternal.SecurityDescriptorSize = uint32(len(sd))
	if len(info.ReparseData) > 0 {
		infoInternal.ReparseDataBuffer = unsafe.Pointer(&info.ReparseData[0])
		infoInternal.ReparseDataSize = uint32(len(info.ReparseData))
	}
	if len(info.ExtendedAttributes) > 0 {
		infoInternal.ExtendedAttributes = unsafe.Pointer(&info.ExtendedAttributes[0])
		infoInternal.EACount = uint32(len(info.ExtendedAttributes))
	}
	err = cimCreateFile(w.handle, toNtPath(path), infoInternal, &w.activeStream)
	if err != nil {
		return &PathError{Cim: w.name, Op: "WriteFile", Path: path, Err: err}
	}
	w.activeName = path
	if info.Attributes&(FILE_ATTRIBUTE_DIRECTORY|FILE_ATTRIBUTE_SPARSE_FILE) == 0 {
		w.activeLeft = info.Size
	}
	return nil
}

// Write writes bytes to the active stream.
func (w *Writer) Write(p []byte) (int, error) {
	if w.activeStream == 0 {
		return 0, errors.New("no active stream")
	}
	if int64(len(p)) > w.activeLeft {
		return 0, &PathError{Cim: w.name, Op: "Write", Path: w.activeName, Err: errors.New("wrote too much")}
	}
	err := cimWriteStream(w.activeStream, uintptr(unsafe.Pointer(&p[0])), uint32(len(p)))
	if err != nil {
		err = &PathError{Cim: w.name, Op: "Write", Path: w.activeName, Err: err}
		return 0, err
	}
	w.activeLeft -= int64(len(p))
	return len(p), nil
}

func (w *Writer) closeStream() error {
	if w.activeStream == 0 {
		return nil
	}
	err := cimCloseStream(w.activeStream)
	if err == nil && w.activeLeft > 0 {
		// Validate here because CimCloseStream does not and this improves error
		// reporting. Otherwise the error will occur in the context of
		// cimWriteStream.
		err = errors.New("write truncated")
	}
	if err != nil {
		err = &PathError{Cim: w.name, Op: "closeStream", Path: w.activeName, Err: err}
	}
	w.activeLeft = 0
	w.activeStream = 0
	w.activeName = ""
	return err
}

// TODO do this as part of Close?
func (w *Writer) Commit() error {
	err := w.closeStream()
	if err != nil {
		return err
	}
	err = cimCommitImage(w.handle)
	if err != nil {
		err = &OpError{Cim: w.name, Op: "Commit", Err: err}
	}
	return err
}

// Close closes the CimFS filesystem.
func (w *Writer) Close() error {
	if w.handle == 0 {
		return errors.New("invalid writer")
	}
	w.closeStream()
	err := cimCloseImage(w.handle)
	if err != nil {
		err = &OpError{Cim: w.name, Op: "Close", Err: err}
	}
	w.handle = 0
	return err
}

// Unlink deletes the file at `path` from the image.
func (w *Writer) Unlink(path string) error {
	err := cimDeletePath(w.handle, path)
	if err != nil {
		err = &PathError{Cim: w.name, Op: "Unlink", Path: path, Err: err}
	}
	return err
}

type LinkError struct {
	Cim string
	Op  string
	Old string
	New string
	Err error
}

func (e *LinkError) Error() string {
	return "cim " + e.Op + " " + e.Old + " " + e.New + ": " + e.Err.Error()
}

// Link adds a hard link from `oldPath` to `newPath` in the image.
func (w *Writer) Link(oldPath string, newPath string) error {
	err := w.closeStream()
	if err != nil {
		return err
	}
	err = cimCreateHardLink(w.handle, toNtPath(newPath), toNtPath(oldPath))
	if err != nil {
		err = &LinkError{Cim: w.name, Op: "Link", Old: oldPath, New: newPath, Err: err}
	}
	return err
}
