package hcsshim

import (
	"runtime"

	"github.com/Microsoft/go-winio"
	"github.com/Sirupsen/logrus"
)

type LayerWriter struct {
	context uintptr
}

func (w *LayerWriter) Next(name string, fileInfo *winio.FileBasicInfo) error {
    if name[0] != '\\' {
        name = `\` + name
    }
	err := importLayerNext(w.context, name, fileInfo)
	if err != nil {
		return makeError(err, "ImportLayerNext", "")
	}
	return nil
}

func (w *LayerWriter) Write(b []byte) (int, error) {
	err := importLayerWrite(w.context, b)
	if err != nil {
		err = makeError(err, "ImportLayerWrite", "")
		return 0, err
	}
	return len(b), err
}

func (w *LayerWriter) Close() (err error) {
	if w.context != 0 {
		err = importLayerEnd(w.context)
		if err != nil {
			err = makeError(err, "ImportLayerEnd", "")
		}
		w.context = 0
	}
	return
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

func NewLayerWriter(info DriverInfo, layerId string, parentLayerPaths []string) (*LayerWriter, error) {
	// Generate layer descriptors
	layers, err := layerPathsToDescriptors(parentLayerPaths)
	if err != nil {
		return nil, err
	}

	// Convert info to API calling convention
	infop, err := convertDriverInfo(info)
	if err != nil {
		return nil, err
	}

	w := &LayerWriter{}
	err = importLayerBegin(&infop, layerId, layers, &w.context)
	if err != nil {
		return nil, makeError(err, "ImportLayerStart", "")
	}
	runtime.SetFinalizer(w, func(w *LayerWriter) { w.Close() })
	return w, nil
}
