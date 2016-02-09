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
		ch <- buildTarFromFiles(dir, w)
	}()
	err = untarSimple(r, dir)
	if err != nil {
		t.Fatal(err)
	}
	err = <-ch
	if err != nil {
		t.Fatal(err)
	}
}
