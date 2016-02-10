package hcsshim

import (
	"io"
	"time"
)

const (
	// Types
	TypeFile = iota
	TypeDirectory
	TypeSymbolicLink
	TypeDirectorySymbolicLink
	TypeDirectoryJunction
)

type Win32FileInfo struct {
	Name                                        string    // The file name relative to the layer root
	Size                                        int64     // The size of the data stream for the file
	ModTime, AccessTime, CreateTime, ChangeTime time.Time // The various file times
	Type                                        byte      // The type of the file
	LinkTarget                                  string    // The target of symbolic links
	SecurityDescriptor                          string    // The security descriptor for the file in SDDL format
}

type LayerReader interface {
	Next() (*Win32FileInfo, error)
	io.Reader
	io.Closer
}

type LayerWriter interface {
	Next(*Win32FileInfo) error
	io.Writer
	io.Closer
}
