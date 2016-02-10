package hcsshim

import (
	"io"
	"io/ioutil"
	"os"
	"testing"
)

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
		w.CloseWithError(buildTarFromFiles(dir, w))
	}()
    go func() {
        ch <- untarSimple(r, dir)
    }()
	if err = <-ch; err != nil {
		t.Fatal(err)
	}
}
