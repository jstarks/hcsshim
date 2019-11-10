package cim_test

import (
	"testing"

	"github.com/Microsoft/hcsshim/internal/cim"
)

func walk(c *cim.Cim, f *cim.File, depthLeft int, fn func(*cim.File) error) error {
	fn(f)
	if !f.IsDir() || depthLeft == 0 {
		return nil
	}
	names, err := f.Readdir()
	if err != nil {
		return err
	}
	for _, name := range names {
		cf, err := c.OpenAt(f, name)
		if err != nil {
			return err
		}
		walk(c, cf, depthLeft-1, fn)
	}
	return nil
}

func TestCim(t *testing.T) {
	c, err := cim.Open(`testdata`, "layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "/")
	if err != nil {
		t.Fatal(err)
	}
	err = walk(c, f, 2, func(f *cim.File) error {
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %+v", f.Name(), fi)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpen(t *testing.T) {
	c, err := cim.Open(`testdata`, "layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "Files/Windows")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %+v", f.Name(), fi)
}

func BenchmarkStat(b *testing.B) {
	c, err := cim.Open(`testdata`, "layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "Files/Windows/System32")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		_, err := f.Stat()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpen(b *testing.B) {
	c, err := cim.Open(`testdata`, "layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "Files/Windows/System32")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		_, err := c.OpenAt(f, "xmllite.dll")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenAbsolute(b *testing.B) {
	c, err := cim.Open(`testdata`, "layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < b.N; i++ {
		_, err := c.OpenAt(nil, "Files/Windows/System32/xmllite.dll")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWalk(b *testing.B) {
	c, err := cim.Open(`testdata`, "layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	f, err := c.OpenAt(nil, "/")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		err = walk(c, f, -1, func(f *cim.File) error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
