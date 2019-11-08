package cim_test

import (
	"path"
	"testing"

	"github.com/Microsoft/hcsshim/internal/cim"
)

func walk(t *testing.T, c *cim.Cim, f *cim.File, name string, depthLeft int) {
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %+v", name, fi)
	if !f.IsDir() || depthLeft == 0 {
		return
	}
	des, err := f.Readdir()
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		cname := path.Join(name, de.Name)
		cf, err := c.OpenID(de.FileID)
		if err != nil {
			t.Fatal(err)
		}
		walk(t, c, cf, cname, depthLeft-1)
	}
}

func TestCim(t *testing.T) {
	c, err := cim.Open(`\\scratch2\scratch\kevpar\snapshot.cim`, "layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenID(c.Root())
	if err != nil {
		t.Fatal(err)
	}
	walk(t, c, f, "/", 2)
}
