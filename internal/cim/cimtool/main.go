package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/cim"
)

func main() {
	err := func() error {
		args := os.Args[1:]
		p := args[0]
		cmd := args[1]
		args = args[2:]
		switch cmd {
		case "stat", "ls", "cat":
			c, err := cim.Open(p)
			if err != nil {
				return err
			}
			defer c.Close()
			cp := args[0]
			f, err := c.OpenAt(nil, cp)
			if err != nil {
				return err
			}
			switch cmd {
			case "stat":
				fi, err := f.Stat()
				if err != nil {
					return err
				}
				fmt.Printf("%s: %+v\n", f.Name(), fi)
			case "ls":
				if f.IsDir() {
					fs, err := f.Readdir()
					if err != nil {
						return err
					}
					for _, fn := range fs {
						fmt.Println(fn)
					}
				} else {
					fmt.Println(path.Base(f.Name()))
				}
			case "cat":
				_, err := io.Copy(os.Stdout, f)
				if err != nil {
					return err
				}
			}
		case "mount":
			var (
				g   guid.GUID
				err error
			)
			if len(args) == 0 {
				g, err = guid.NewV4()
			} else {
				g, err = guid.FromString(args[0])
			}
			if err != nil {
				return err
			}
			err = cim.Mount(p, g)
			if err != nil {
				return err
			}
			fmt.Println(g)
		case "unmount":
			g, err := guid.FromString(p)
			if err != nil {
				return err
			}
			return cim.Unmount(g)
		case "create":
			hp := os.Args[3]
			w, err := cim.Create(p)
			if err != nil {
				return err
			}
			defer w.Close()
			err = filepath.Walk(hp, func(p string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				cfi := &cim.FileInfo{
					Size:          fi.Size(),
					LastWriteTime: cim.FiletimeFromTime(fi.ModTime()),
				}
				if fi.IsDir() {
					cfi.Attributes |= cim.FILE_ATTRIBUTE_DIRECTORY
				}
				rp, err := filepath.Rel(hp, p)
				if err != nil {
					return err
				}
				err = w.AddFile(filepath.FromSlash(rp), cfi)
				if err != nil {
					return err
				}
				defer w.CloseStream()
				if !fi.IsDir() {
					f, err := os.Open(p)
					if err != nil {
						return err
					}
					defer f.Close()
					_, err = io.Copy(w, f)
					if err != nil {
						return err
					}
				}
				err = w.CloseStream()
				if err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				return err
			}
			err = w.Commit()
			if err != nil {
				return err
			}
			err = w.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown command %s", cmd)
		}
		return nil
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
