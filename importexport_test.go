package hcsshim

import (
	"archive/tar"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
)

// buildTarFromFiles builds a tar from a set of files.
// This is intended to be used for TP4; after TP4, Windows should have a proper streaming
// version of ExportLayer to call.
func buildTarFromFiles(root string, w io.Writer) error {
	t := tar.NewWriter(w)
	r := newFileLayerReader(root, nil)
	defer r.Close()
	for {
		fi, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
			return err
		}
		hdr := &tar.Header{
			Name:       fi.Name,
			Size:       fi.Size,
			ModTime:    fi.ModTime,
			AccessTime: fi.AccessTime,
			ChangeTime: fi.ChangeTime,
			Linkname:   fi.LinkTarget,
		}
		err = t.WriteHeader(hdr)
		if err != nil {
			panic(err)
			return err
		}
		if fi.Type == TypeFile {
			_, err = io.Copy(t, r)
			if err != nil {
				panic(err)
				return err
			}
		} else if fi.Size > 0 {
			panic(fmt.Errorf("%s had non-zero size %d", fi.Name, fi.Size))
		}
	}
	return t.Close()
}

func untarSimple(r io.Reader, root string) error {
	t := tar.NewReader(r)
	w := newFileLayerWriter(root, nil)
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
			panic(err)
			return err
		}
		fi := hdr.FileInfo()
		if hdr.Name == "." {
			continue
		}
		var typ byte = TypeFile
		if fi.IsDir() {
			typ = TypeDirectory
		}
		wfi := &Win32FileInfo{
			Name: hdr.Name,
			Size: 0,
			Type: typ,
		}
		err = w.Next(wfi)
		if err != nil {
			panic(err)
			return err
		}
		if !fi.IsDir() {
			_, err = io.Copy(w, t)
			if err != nil {
				panic(err)
				return err
			}
		}
	}
	return nil
}

func TestExportImport(t *testing.T) {
	dir, err := ioutil.TempDir("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dir2, err := ioutil.TempDir("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir2)

	r, w := io.Pipe()
	ch := make(chan error)
	go func() {
		w.CloseWithError(buildTarFromFiles(`c:\go\bin`, w))
	}()
	go func() {
		ch <- untarSimple(r, dir2)
	}()
	if err = <-ch; err != nil {
		t.Fatal(err)
	}
}
