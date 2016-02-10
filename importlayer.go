package hcsshim

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Sirupsen/logrus"
)

type fileLayerWriter struct {
	root        string
	closeFunc   func() error
	currentFile *os.File
}

func newFileLayerWriter(root string, closeFunc func() error) *fileLayerWriter {
	return &fileLayerWriter{
		root:      root,
		closeFunc: closeFunc,
	}
}

func (f *fileLayerWriter) Write(b []byte) (int, error) {
	if f.currentFile == nil {
		return 0, errors.New("closed")
	}
	return f.currentFile.Write(b)
}

func (f *fileLayerWriter) Close() error {
	if f.currentFile != nil {
		f.currentFile.Close()
		f.currentFile = nil
	}
	if f.closeFunc != nil {
		return f.closeFunc()
	}
	return nil
}

func (f *fileLayerWriter) Next(fi *Win32FileInfo) error {
	if f.currentFile != nil {
		f.currentFile.Close()
		f.currentFile = nil
	}
	path := filepath.Join(f.root, fi.Name)
    var attr uint32
    var createMode uint32
    closeFile := false
    switch fi.Type {
    case TypeFile:
        createMode = syscall.CREATE_NEW
    case TypeDirectory:
        err := os.Mkdir(path, 0)
        if err != nil {
            return err
        }
        createMode = syscall.OPEN_EXISTING
        attr |= syscall.FILE_FLAG_BACKUP_SEMANTICS
        closeFile = true
    default:
        // We don't need to support these for the TP4 format.
        return errors.New("entry not supported")
    }
    
    pathp, err := syscall.UTF16PtrFromString(path)
    if err != nil {
        return err
    }
    h, err := syscall.CreateFile(pathp,
        syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil,
        createMode, attr, 0)
    if err != nil {
        return err
    }
    mtime := syscall.NsecToFiletime(fi.ModTime.UnixNano())
    atime := syscall.NsecToFiletime(fi.AccessTime.UnixNano())
    ctime := syscall.NsecToFiletime(fi.CreateTime.UnixNano())
    err = syscall.SetFileTime(h, &mtime, &atime, &ctime)
    if err != nil {
        syscall.Close(h)
        return err
    }
    if closeFile {
        syscall.Close(h)
    } else {
        f.currentFile = os.NewFile(uintptr(h), path)
    }
	return nil
}

// ImportLayer will take the contents of the folder at importFolderPath and import
// that into a layer with the id layerId.  Note that in order to correctly populate
// the layer and interperet the transport format, all parent layers must already
// be present on the system at the paths provided in parentLayerPaths.
func ImportLayer(info DriverInfo, layerId string, importFolderPath string, parentLayerPaths []string) error {
	title := "hcsshim::ImportLayer "
	logrus.Debugf(title+"flavour %d layerId %s folder %s", info.Flavour, layerId, importFolderPath)

	// Generate layer descriptors
	layers, err := layerPathsToDescriptors(parentLayerPaths)
	if err != nil {
		return err
	}

	// Convert info to API calling convention
	infop, err := convertDriverInfo(info)
	if err != nil {
		logrus.Error(err)
		return err
	}

	err = importLayer(&infop, layerId, importFolderPath, layers)
	if err != nil {
		err = makeErrorf(err, title, "layerId=%s flavour=%d folder=%s", layerId, info.Flavour, importFolderPath)
		logrus.Error(err)
		return err
	}

	logrus.Debugf(title+"succeeded flavour=%d layerId=%s folder=%s", info.Flavour, layerId, importFolderPath)
	return nil
}

func GetLayerWriter(info DriverInfo, layerId string, parentLayerPaths []string) (LayerWriter, error) {
	dir, err := ioutil.TempDir("", "hcs")
	if err != nil {
		return nil, err
	}
	w := newFileLayerWriter(dir, func() error {
		err := ImportLayer(info, layerId, dir, parentLayerPaths)
		os.RemoveAll(dir)
		return err
	})
	return w, nil
}
