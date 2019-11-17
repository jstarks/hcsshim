package cim

import (
	"path/filepath"

	"github.com/Microsoft/go-winio/pkg/guid"
)

// MountImage mounts the CimFS image at `path` to the volume `volumeGUID`.
func MountImage(p string, volumeGUID guid.GUID) error {
	return cimMountImage(filepath.Dir(p), filepath.Base(p), 0, &volumeGUID)
}

// UnmountImage unmounts the CimFS volume `volumeGUID`.
func UnmountImage(volumeGUID guid.GUID) error {
	return cimDismountImage(&volumeGUID)
}
