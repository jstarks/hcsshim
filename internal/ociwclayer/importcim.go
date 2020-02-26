package ociwclayer

import (
	"archive/tar"
	"encoding/base64"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	winio "github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/cim"
	"github.com/Microsoft/hcsshim/internal/cim/layer"
)

var reparseCimTombstone = []byte{} // BUGBUG

const (
	hdrEaPrefix       = "MSWINDOWS.xattr."
	hdrSd             = "MSWINDOWS.rawsd"
	hdrMountPoint     = "MSWINDOWS.mountpoint"
	hdrFileAttributes = "MSWINDOWS.fileattr"
)

func addTarFile(w *cim.Writer, t *tar.Reader, hdr *tar.Header) error {
	if base := path.Base(hdr.Name); strings.HasPrefix(base, whiteoutPrefix) {
		fi := &cim.FileInfo{
			ReparseData: reparseCimTombstone,
		}
		err := w.WriteFile(hdr.Name, fi)
		if err != nil {
			return err
		}
	} else if hdr.Typeflag == tar.TypeLink {
		err := w.Link(hdr.Linkname, hdr.Name)
		if err != nil {
			return err
		}
	} else {
		fi := &cim.FileInfo{}
		if attrStr, ok := hdr.PAXRecords[hdrFileAttributes]; ok {
			attr, err := strconv.ParseUint(attrStr, 10, 32)
			if err != nil {
				return err
			}
			fi.Attributes = uint32(attr) & (cim.FILE_ATTRIBUTE_READONLY | cim.FILE_ATTRIBUTE_HIDDEN | cim.FILE_ATTRIBUTE_SYSTEM | cim.FILE_ATTRIBUTE_ARCHIVE)
		}
		if hdr.Typeflag == tar.TypeDir {
			fi.Attributes |= cim.FILE_ATTRIBUTE_DIRECTORY
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			fi.Size = hdr.Size
		}
		if sdraw, ok := hdr.PAXRecords[hdrSd]; ok {
			sd, err := base64.StdEncoding.DecodeString(sdraw)
			if err != nil {
				return err
			}
			fi.SecurityDescriptor = sd
		}
		var eas []winio.ExtendedAttribute
		for k, v := range hdr.PAXRecords {
			if !strings.HasPrefix(k, hdrEaPrefix) {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return err
			}
			eas = append(eas, winio.ExtendedAttribute{
				Name:  k[len(hdrEaPrefix):],
				Value: data,
			})
		}
		if len(eas) != 0 {
			eadata, err := winio.EncodeExtendedAttributes(eas)
			if err != nil {
				return err
			}
			fi.ExtendedAttributes = eadata
		}
		if hdr.Typeflag == tar.TypeSymlink {
			_, isMountPoint := hdr.PAXRecords[hdrMountPoint]
			rp := winio.ReparsePoint{
				Target:       filepath.FromSlash(hdr.Linkname),
				IsMountPoint: isMountPoint,
			}
			fi.ReparseData = winio.EncodeReparsePoint(&rp)
			fi.Attributes |= cim.FILE_ATTRIBUTE_REPARSE_POINT
		}
		err := w.WriteFile(hdr.Name, fi)
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			_, err = io.Copy(w, t)
			if err != nil {
				return fmt.Errorf("copying data to CIM: %s", err)
			}
		}
	}
	return nil
}

func ImportCimLayer(r io.Reader, p string, parentLayerPaths []string) (int64, error) {
	fp := filepath.Join(p, "base.fs")
	w, err := cim.Create(fp)
	if err != nil {
		return 0, err
	}
	defer w.Close()

	isBase := len(parentLayerPaths) == 0
	t := tar.NewReader(r)
	for {
		hdr, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		if !strings.HasPrefix(hdr.Name, "UtilityVM/") { // skip for now because utility VM has cross-layer hard links
			err = addTarFile(w, t, hdr)
			if err != nil {
				return 0, err
			}
		}
	}
	err = w.Commit()
	if err != nil {
		return 0, err
	}
	w.Close()
	w, err = cim.Append(fp, "layer.fs")
	if err != nil {
		return 0, err
	}
	defer w.Close()
	if isBase {
		w.WriteFile("Hives", &cim.FileInfo{Attributes: cim.FILE_ATTRIBUTE_DIRECTORY})
		for _, hive := range []struct{ Hive, File string }{{"SYSTEM", "System"}, {"SOFTWARE", "Software"}, {"SAM", "Sam"}, {"SECURITY", "Security"}, {"DEFAULT", "DefaultUser"}} {
			err = w.Link("Files/Windows/System32/Config/"+hive.Hive, "Hives/"+hive.File+"_Base")
			if err != nil {
				return 0, err
			}
		}
		// TODO: UtilityVM processing
	} else {
		var layers []layer.Layer
		for i, lp := range parentLayerPaths {
			id := guid.GUID{Data1: uint32(len(parentLayerPaths) - i)}
			layers = append(layers, layer.Layer{ID: id, Path: filepath.Join(lp, "layer.fs")})
		}
		err = layer.Expand(w, fp, "Files", layers[len(layers)-1].ID, layers)
		if err != nil {
			return 0, err
		}
		pcr, err := cim.Open(layers[len(layers)-1].Path)
		if err != nil {
			return 0, err
		}
		defer pcr.Close()
		for _, hive := range []string{"System", "Software", "Sam", "Security", "DefaultUser"} {
			f, err := pcr.Open("Hives/" + hive + "_Base")
			if err != nil {
				return 0, err
			}
			// TODO: merge hives
			err = w.WriteFile("Hives/"+hive+"_Base", &cim.FileInfo{Size: f.Size()})
			if err != nil {
				return 0, err
			}
			_, err = io.Copy(w, f)
			if err != nil {
				return 0, err
			}
		}
	}
	err = w.Commit()
	if err != nil {
		return 0, err
	}
	return 0, nil
}
