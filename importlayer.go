package hcsshim

import (
	"archive/tar"
    "errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Sirupsen/logrus"
)

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

func setTimesFromTarHeader(h syscall.Handle, hdr *tar.Header) error {
	mtime := syscall.NsecToFiletime(hdr.ModTime.UnixNano())
	atime := syscall.NsecToFiletime(hdr.AccessTime.UnixNano())
	return syscall.SetFileTime(h, &mtime, &atime, &mtime)
}

type fileLayerWriter struct {
    currentFile *os.File
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
    return nil
}

func (f *fileLayerWriter) Next(fi *Win32FileInfo) error {
    if f.currentFile != nil {
        f.currentFile.Close()
        f.currentFile = nil
    }
    if fi.Type == TypeFile {
        pathp, err := syscall.UTF16PtrFromString(fi.Name)
        if err != nil {
            return err
        }
        h, err := syscall.CreateFile(pathp,
            syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil,
            syscall.CREATE_NEW, 0, 0)
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
        f.currentFile = os.NewFile(uintptr(h), fi.Name)
    } else {
        err := os.Mkdir(fi.Name, 0)
        if err != nil {
            return err
        }
    }
    return nil
}

func untarSimple(r io.Reader, root string) error {
	t := tar.NewReader(r)
    w := &fileLayerWriter{}
    defer w.Close()
	type dirInfo struct {
		path string
		hdr  *tar.Header
	}
	for {
		hdr, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		fi := hdr.FileInfo()
		if hdr.Name == "." || hdr.Name == ".." {
            panic(hdr.Name)
			//panic(fmt.Sprintf("%v", hdr))
			continue
		}
        var typ byte = TypeFile
        if fi.IsDir() {
            typ = TypeDirectory
        }
		path := filepath.Join(root, hdr.Name)
        wfi := &Win32FileInfo {
            Name: path,
            Size: 0,
            Type: typ,
        }
        err = w.Next(wfi)
        if err != nil {
            return err
        }
        if !fi.IsDir() {
			_, err = io.Copy(w, t)
            if err != nil {
                return err
            }
		}
	}
	return nil
}

func ImportLayerFromTar(info DriverInfo, layerId string, tar io.Reader, parentLayerPaths []string) error {
	dir, err := ioutil.TempDir("", "hcs")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	err = untarSimple(tar, dir)
	if err != nil {
		return err
	}
	return ImportLayer(info, layerId, dir, parentLayerPaths)
}
