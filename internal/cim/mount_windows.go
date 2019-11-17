package cim

import (
	"path/filepath"

	"github.com/Microsoft/go-winio/pkg/guid"
)

type MountError struct {
	Cim        string
	Op         string
	VolumeGUID guid.GUID
	Err        error
}

func (e *MountError) Error() string {
	s := "cim " + e.Op
	if e.Cim != "" {
		s += " " + e.Cim
	}
	s += " " + e.VolumeGUID.String() + ": " + e.Err.Error()
	return s
}

// MountImage mounts the CimFS image at `path` to the volume `volumeGUID`.
func Mount(p string, volumeGUID guid.GUID) error {
	err := cimMountImage(filepath.Dir(p), filepath.Base(p), 0, &volumeGUID)
	if err != nil {
		err = &MountError{Cim: p, Op: "Mount", VolumeGUID: volumeGUID, Err: err}
	}
	return err
}

// UnmountImage unmounts the CimFS volume `volumeGUID`.
func Unmount(volumeGUID guid.GUID) error {
	err := cimDismountImage(&volumeGUID)
	if err != nil {
		err = &MountError{Op: "Unmount", VolumeGUID: volumeGUID, Err: err}
	}
	return err
}
