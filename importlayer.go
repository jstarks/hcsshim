package hcsshim

import (
	"archive/tar"
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

func untarSimple(r io.Reader, root string) error {
	t := tar.NewReader(r)
	type dirInfo struct {
		path string
		hdr  *tar.Header
	}
	var dirs []dirInfo
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
			//panic(fmt.Sprintf("%v", hdr))
			continue
		}
		path := filepath.Join(root, hdr.Name)
		if fi.IsDir() {
			err = os.Mkdir(path, 0)
			if err != nil {
				return err
			}
			dirs = append(dirs, dirInfo{path, hdr})
		} else {
			pathp, err := syscall.UTF16PtrFromString(path)
			if err != nil {
				return err
			}
			h, err := syscall.CreateFile(pathp,
				syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil,
				syscall.CREATE_NEW, 0, 0)
			if err != nil {
				return err
			}
			f := os.NewFile(uintptr(h), path)
			_, err = io.Copy(f, t)
			f.Close()
			if err != nil {
				return err
			}
			err = setTimesFromTarHeader(h, hdr)
			if err != nil {
				return err
			}
		}
	}
	for _, d := range dirs {
		pathp, err := syscall.UTF16PtrFromString(d.path)
		if err != nil {
			return err
		}
		h, err := syscall.CreateFile(pathp,
			syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil,
			syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return err
		}
		err = setTimesFromTarHeader(h, d.hdr)
		syscall.Close(h)
		if err != nil {
			return err
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
