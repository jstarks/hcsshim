package hcsshim

import (
	"archive/tar"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

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
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		relName, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hdr.Name = relName
		err = t.WriteHeader(hdr)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(t, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	t.Close()
	return err
}

func ExportLayerToTar(info DriverInfo, layerId string, parentLayerPaths []string) (io.ReadCloser, error) {
	dir, err := ioutil.TempDir("", "hcs")
	if err != nil {
		return nil, err
	}
	err = ExportLayer(info, layerId, dir, parentLayerPaths)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	r, w := io.Pipe()
	go func() {
		err := buildTarFromFiles(dir, w)
		os.RemoveAll(dir)
		w.CloseWithError(err)
	}()
	return r, nil
}
