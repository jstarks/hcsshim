package hcsshim

import (
	"errors"
	"path/filepath"

	winio "github.com/Microsoft/go-winio"
	"github.com/Microsoft/hcsshim/internal/wclayer"
)

func layerPath(info *DriverInfo, id string) string {
	return filepath.Join(info.HomeDir, id)
}

func ActivateLayer(info DriverInfo, id string) error {
	return wclayer.ActivateLayer(layerPath(&info, id))
}
func CreateLayer(info DriverInfo, id, parent string) error {
	return wclayer.CreateLayer(layerPath(&info, id), parent)
}
func CreateSandboxLayer(info DriverInfo, layerId, parentId string, parentLayerPaths []string) error {
	return wclayer.CreateSandboxLayer(layerPath(&info, layerId), parentLayerPaths)
}
func DeactivateLayer(info DriverInfo, id string) error {
	return wclayer.DeactivateLayer(layerPath(&info, id))
}
func DestroyLayer(info DriverInfo, id string) error {
	return wclayer.DestroyLayer(layerPath(&info, id))
}
func ExpandSandboxSize(info DriverInfo, layerId string, size uint64) error {
	return wclayer.ExpandSandboxSize(layerPath(&info, layerId), size)
}
func ExportLayer(info DriverInfo, layerId string, exportFolderPath string, parentLayerPaths []string) error {
	return wclayer.ExportLayer(layerPath(&info, layerId), exportFolderPath, parentLayerPaths)
}
func GetLayerMountPath(info DriverInfo, id string) (string, error) {
	return wclayer.GetLayerMountPath(layerPath(&info, id))
}
func GetSharedBaseImages() (imageData string, err error) {
	return wclayer.GetSharedBaseImages()
}
func ImportLayer(info DriverInfo, layerID string, importFolderPath string, parentLayerPaths []string) error {
	return wclayer.ImportLayer(layerPath(&info, layerID), importFolderPath, parentLayerPaths)
}
func LayerExists(info DriverInfo, id string) (bool, error) {
	return wclayer.LayerExists(layerPath(&info, id))
}
func PrepareLayer(info DriverInfo, layerId string, parentLayerPaths []string) error {
	return wclayer.PrepareLayer(layerPath(&info, layerId), parentLayerPaths)
}
func ProcessBaseLayer(path string) error {
	return wclayer.ProcessBaseLayer(path)
}
func ProcessUtilityVMImage(path string) error {
	return wclayer.ProcessUtilityVMImage(path)
}
func UnprepareLayer(info DriverInfo, layerId string) error {
	return wclayer.UnprepareLayer(layerPath(&info, layerId))
}

type DriverInfo struct {
	Flavour int
	HomeDir string
}

// FilterLayerReader provides an interface for extracting the contents of an on-disk layer.
type FilterLayerReader struct{}

// Next reads the next available file from a layer, ensuring that parent directories are always read
// before child files and directories.
//
// Next returns the file's relative path, size, and basic file metadata. Read() should be used to
// extract a Win32 backup stream with the remainder of the metadata and the data.
func (r *FilterLayerReader) Next() (string, int64, *winio.FileBasicInfo, error) {
	return "", 0, nil, errors.New("not implemented")
}

// Read reads from the current file's Win32 backup stream.
func (r *FilterLayerReader) Read(b []byte) (int, error) {
	return 0, errors.New("not implemented")
}

// Close frees resources associated with the layer reader. It will return an
// error if there was an error while reading the layer or of the layer was not
// completely read.
func (r *FilterLayerReader) Close() (err error) {
	return errors.New("not implemented")
}

// FilterLayerWriter provides an interface to write the contents of a layer to the file system.
type FilterLayerWriter struct{}

// Add adds a file or directory to the layer. The file's parent directory must have already been added.
//
// name contains the file's relative path. fileInfo contains file times and file attributes; the rest
// of the file metadata and the file data must be written as a Win32 backup stream to the Write() method.
// winio.BackupStreamWriter can be used to facilitate this.
func (w *FilterLayerWriter) Add(name string, fileInfo *winio.FileBasicInfo) error {
	return errors.New("not supported")
}

// AddLink adds a hard link to the layer. The target of the link must have already been added.
func (w *FilterLayerWriter) AddLink(name string, target string) error {
	return errors.New("not supported")
}

// Remove removes a file from the layer. The file must have been present in the parent layer.
//
// name contains the file's relative path.
func (w *FilterLayerWriter) Remove(name string) error {
	return errors.New("not supported")
}

// Write writes more backup stream data to the current file.
func (w *FilterLayerWriter) Write(b []byte) (int, error) {
	return 0, errors.New("not supported")
}

// Close completes the layer write operation. The error must be checked to ensure that the
// operation was successful.
func (w *FilterLayerWriter) Close() (err error) {
	return errors.New("not supported")
}

type GUID = wclayer.GUID

func NameToGuid(name string) (id GUID, err error) {
	return wclayer.NameToGuid(name)
}
func NewGUID(source string) *GUID {
	return wclayer.NewGUID(source)
}

type LayerReader = wclayer.LayerReader

func NewLayerReader(info DriverInfo, layerID string, parentLayerPaths []string) (LayerReader, error) {
	return wclayer.NewLayerReader(layerPath(&info, layerID), parentLayerPaths)
}

type LayerWriter = wclayer.LayerWriter

func NewLayerWriter(info DriverInfo, layerID string, parentLayerPaths []string) (LayerWriter, error) {
	return wclayer.NewLayerWriter(layerPath(&info, layerID), parentLayerPaths)
}

type WC_LAYER_DESCRIPTOR = wclayer.WC_LAYER_DESCRIPTOR
