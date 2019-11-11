package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/Microsoft/hcsshim/internal/cim"
)

func main() {
	err := func() error {
		p := os.Args[1]
		c, err := cim.Open(filepath.Dir(p), filepath.Base(p))
		if err != nil {
			return err
		}
		defer c.Close()
		cp := os.Args[3]
		f, err := c.OpenAt(nil, cp)
		if err != nil {
			return err
		}
		switch cmd := os.Args[2]; cmd {
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
