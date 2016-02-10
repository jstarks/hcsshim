package hcsshim

import (
	"archive/tar"
    "errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
    "time"

	"github.com/Sirupsen/logrus"
)

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

// buildTarFromFiles builds a tar from a set of files.
// This is intended to be used for TP4; after TP4, Windows should have a proper streaming
// version of ExportLayer to call.
func buildTarFromFiles(root string, w io.Writer) error {
	t := tar.NewWriter(w)
    r := newFileLayerReader(root, false)
    defer r.Close()
    for {
        fi, err := r.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        hdr := &tar.Header {
            Name: fi.Name,
            Size: fi.Size,
            ModTime: fi.ModTime,
            AccessTime: fi.AccessTime,
            ChangeTime: fi.ChangeTime,
            Linkname: fi.LinkTarget,
        }
		err = t.WriteHeader(hdr)
		if err != nil {
			return err
		}
		if fi.Type == TypeFile {
			_, err = io.Copy(t, r)
			if err != nil {
				return err
			}
		}
		return nil
	}
	return t.Close()
}

const (
    // Types
    TypeFile = iota
    TypeDirectory
    TypeSymbolicLink
    TypeDirectorySymbolicLink
)
    
type Win32FileInfo struct {
    Name string // The file name relative to the layer root
    Size int64 // The size of the data stream for the file
    ModTime, AccessTime, CreateTime, ChangeTime time.Time // The various file times
    Type byte // The type of the file
    LinkTarget string // The target of symbolic links
    SecurityDescriptor string // The security descriptor for the file in SDDL format
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

type fileEntry struct {
    f *os.File
    path string
    fi os.FileInfo
    err error
}

type fileLayerReader struct {
    root string
    result chan *fileEntry
    proceed chan bool
    deleteOnClose bool
    currentFile *os.File
}

func newFileLayerReader(root string, deleteOnClose bool) *fileLayerReader {
    r := &fileLayerReader{
        root: root,
        result: make(chan *fileEntry),
        proceed: make(chan bool),
        deleteOnClose: deleteOnClose,
    }
    go r.walk()
    return r
}

var errorDone = errors.New("done")

func (r *fileLayerReader) walk() {
    defer close(r.result)
    if !<-r.proceed {
        return
    }
	err := filepath.Walk(r.root, func(path string, info os.FileInfo, err error) error {
        var f *os.File
        if err == nil && !info.IsDir() {
            f, err = os.Open(path)
        }
        r.result <- &fileEntry{f, path, info, err}
        if !<-r.proceed {
            return errorDone // any error will stop
        }
        return err
	})
    if err == errorDone {
        return
    }
    for {
        r.result <- &fileEntry{err: io.EOF}
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
        typ = TypeFile
    } else {
        typ = TypeDirectory
    }
    fi := &Win32FileInfo{
        Name: relPath,
        Size: fe.fi.Size(),
        ModTime: fe.fi.ModTime(),
//        AccessTime: fe.fi.AccessTime(),
        CreateTime: fe.fi.ModTime(), // should look up the change time
//        ChangeTime: fe.fi.ChangeTime(),
        Type: typ,
        LinkTarget: "",
        SecurityDescriptor: "",
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
    if r.deleteOnClose {
        os.RemoveAll(r.root)
    }
    return nil
}

func ExportLayerAsStream(info DriverInfo, layerId string, parentLayerPaths []string) (LayerReader, error) {
	dir, err := ioutil.TempDir("", "hcs")
	if err != nil {
		return nil, err
	}
	err = ExportLayer(info, layerId, dir, parentLayerPaths)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
    return newFileLayerReader(dir, true), nil
}
