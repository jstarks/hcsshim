package cim_test

import (
	"testing"

	"github.com/Microsoft/hcsshim/internal/cim"
)

func walk(cr *cim.Reader, f *cim.File, depthLeft int, fn func(*cim.File, *cim.Stream) error) error {
	err := fn(f, nil)
	if err != nil {
		return err
	}
	ss, err := f.Readstreams()
	if err != nil {
		return err
	}
	for _, sn := range ss {
		s, err := f.OpenStream(sn)
		if err != nil {
			return err
		}
		err = fn(f, s)
		if err != nil {
			return err
		}
	}
	if !f.IsDir() || depthLeft == 0 {
		return nil
	}
	names, err := f.Readdir()
	if err != nil {
		return err
	}
	for _, name := range names {
		cf, err := cr.OpenAt(f, name)
		if err != nil {
			return err
		}
		walk(cr, cf, depthLeft-1, fn)
	}
	return nil
}

func TestCim(t *testing.T) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.OpenAt(nil, "/")
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
			si, err := s.Stat()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s:%s: %+v", f.Name(), s.Name(), si)
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
	f, err := cr.OpenAt(nil, "Files/Windows")
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
	_, err = cr.OpenAt(nil, "Files/WindowsX")
	if cerr, ok := err.(*cim.ReaderError); !ok || cerr.Err != cim.ErrFileNotFound {
		t.Fatalf("expected cim error got %s", err)
	}
}

func BenchmarkStat(b *testing.B) {
	cr, err := cim.Open("testdata/layer.fs")
	if err != nil {
		b.Fatal(err)
	}
	defer cr.Close()
	f, err := cr.OpenAt(nil, "Files/Windows/System32")
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
	f, err := cr.OpenAt(nil, "Files/Windows/System32")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		_, err := cr.OpenAt(f, "xmllite.dll")
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
		_, err := cr.OpenAt(nil, "Files/Windows/System32/xmllite.dll")
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
	f, err := cr.OpenAt(nil, "/")
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
