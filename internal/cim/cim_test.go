package cim_test

import (
	"testing"

	"github.com/Microsoft/hcsshim/internal/cim"
)

func walk(t *testing.T, c *cim.Cim, f *cim.File, depthLeft int) {
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %+v", f.Name(), fi)
	if !f.IsDir() || depthLeft == 0 {
		return
	}
	names, err := f.Readdir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		cf, err := c.OpenAt(f, name)
		if err != nil {
			t.Fatal(err)
		}
		walk(t, c, cf, depthLeft-1)
	}
}

func TestCim(t *testing.T) {
	c, err := cim.Open(`\\scratch2\scratch\kevpar\snapshot.cim`, "layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "/")
	if err != nil {
		t.Fatal(err)
	}
	walk(t, c, f, 2)
}

func TestOpen(t *testing.T) {
	c, err := cim.Open(`\\scratch2\scratch\kevpar\snapshot.cim`, "layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "Files/Windows")
	if err != nil {
		t.Fatal(err)
	}
	walk(t, c, f, 0)
}
