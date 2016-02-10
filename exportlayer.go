package hcsshim

import (
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
)

var errorIterationCanceled = errors.New("")

type fileEntry struct {
	f    *os.File
	path string
	fi   os.FileInfo
	err  error
}

type fileLayerReader struct {
	root        string
	result      chan *fileEntry
	proceed     chan bool
	closeFunc   func() error
	currentFile *os.File
}

func newFileLayerReader(root string, closeFunc func() error) *fileLayerReader {
	r := &fileLayerReader{
		root:      root,
		result:    make(chan *fileEntry),
		proceed:   make(chan bool),
		closeFunc: closeFunc,
	}
	go r.walk()
	return r
}

func (r *fileLayerReader) walk() {
	defer close(r.result)
	if !<-r.proceed {
		return
	}
	err := filepath.Walk(r.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == r.root {
			return nil
		}
		var f *os.File
		if !info.IsDir() {
			f, err = os.Open(path)
			if err != nil {
				return err
			}
		}
		r.result <- &fileEntry{f, path, info, nil}
		if !<-r.proceed {
			return errorIterationCanceled
		}
		return nil
	})
	if err == errorIterationCanceled {
		return
	}
	if err == nil {
		err = io.EOF
	}
	for {
		r.result <- &fileEntry{err: err}
		if !<-r.proceed {
			break
		}
	}
}

func (r *fileLayerReader) Next() (*Win32FileInfo, error) {
	if r.currentFile != nil {
		r.currentFile.Close()
		r.currentFile = nil
	}
	r.proceed <- true
	fe := <-r.result
	if fe == nil {
		return nil, errors.New("fileLayerReader closed")
	}
	if fe.err != nil {
		return nil, fe.err
	}
	relPath, err := filepath.Rel(r.root, fe.path)
	if err != nil {
		fe.f.Close()
		return nil, err
	}
	var typ byte
	if fe.fi.IsDir() {
		typ = TypeDirectory
	} else {
		typ = TypeFile
	}
	fi := &Win32FileInfo{
		Name:               relPath,
		Size:               fe.fi.Size(),
		ModTime:            fe.fi.ModTime(),
		CreateTime:         fe.fi.ModTime(),
		Type:               typ,
		LinkTarget:         "",
		SecurityDescriptor: "",
	}
	if attr, ok := fe.fi.Sys().(*syscall.Win32FileAttributeData); ok {
		fi.CreateTime = time.Unix(0, attr.CreationTime.Nanoseconds())
		fi.AccessTime = time.Unix(0, attr.LastAccessTime.Nanoseconds())
	}
	r.currentFile = fe.f
	return fi, nil
}

func (r *fileLayerReader) Read(b []byte) (int, error) {
	if r.currentFile == nil {
		return 0, io.EOF
	}
	return r.currentFile.Read(b)
}

func (r *fileLayerReader) Close() error {
	r.proceed <- false
	<-r.result
	if r.currentFile != nil {
		r.currentFile.Close()
		r.currentFile = nil
	}
	if r.closeFunc != nil {
		return r.closeFunc()
	}
	return nil
}

// ExportLayer will create a folder at exportFolderPath and fill that folder with
// the transport format version of the layer identified by layerId. This transport
// format includes any metadata required for later importing the layer (using
// ImportLayer), and requires the full list of parent layer paths in order to
// perform the export.
func ExportLayer(info DriverInfo, layerId string, exportFolderPath string, parentLayerPaths []string) error {
	title := "hcsshim::ExportLayer "
	logrus.Debugf(title+"flavour %d layerId %s folder %s", info.Flavour, layerId, exportFolderPath)

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

	err = exportLayer(&infop, layerId, exportFolderPath, layers)
	if err != nil {
		err = makeErrorf(err, title, "layerId=%s flavour=%d folder=%s", layerId, info.Flavour, exportFolderPath)
		logrus.Error(err)
		return err
	}

	logrus.Debugf(title+"succeeded flavour=%d layerId=%s folder=%s", info.Flavour, layerId, exportFolderPath)
	return nil
}

func GetLayerReader(info DriverInfo, layerId string, parentLayerPaths []string) (LayerReader, error) {
	dir, err := ioutil.TempDir("", "hcs")
	if err != nil {
		return nil, err
	}
	err = ExportLayer(info, layerId, dir, parentLayerPaths)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	r := newFileLayerReader(dir, func() error {
		os.RemoveAll(dir)
		return nil
	})
	return r, nil
}
