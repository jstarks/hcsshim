package cim_test

import (
	"strings"
	"testing"

	"github.com/Microsoft/hcsshim/internal/cim"
)

func walk(cr *cim.Reader, f *cim.File, maxDepth int, fn func(*cim.File, *cim.Stream) error) error {
	baseDepth := strings.Count(f.Name(), "/")
	return cim.Walk(f, func(f *cim.File, s *cim.Stream) (bool, error) {
		err := fn(f, s)
		if err != nil {
			return false, err
		}
		if strings.Count(f.Name(), "/") >= baseDepth+maxDepth {
			return true, cim.SkipDir
		}
		return true, nil
	})
}

func TestCim(t *testing.T) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	err = walk(cr, f, 3, func(f *cim.File, s *cim.Stream) error {
		if s == nil {
			fi, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %+v", f.Name(), fi)
		} else {
			t.Logf("%s:%s: size %d", f.Name(), s.Name(), s.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpen(t *testing.T) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.Open("Files/Windows")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %+v", f.Name(), fi)
}

func TestOpenMissing(t *testing.T) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()
	_, err = cr.Open("Files/WindowsX")
	if cerr, ok := err.(*cim.CimError); !ok || cerr.Err != cim.ErrFileNotFound {
		t.Fatalf("expected cim error got %s", err)
	}
}

func BenchmarkStat(b *testing.B) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.Open("Files/Windows/System32")
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
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.Open("Files/Windows/System32")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		_, err := f.OpenAt("xmllite.dll")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenAbsolute(b *testing.B) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer cr.Close()
	for i := 0; i < b.N; i++ {
		_, err := cr.Open("Files/Windows/System32/xmllite.dll")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWalk(b *testing.B) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.Open("/")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		err = walk(cr, f, -1, func(f *cim.File, s *cim.Stream) error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
