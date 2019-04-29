package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	err := func() error {
		f, err := openVhd("test.vhdx")
		if err != nil {
			return err
		}
		size, err := f.Length()
		if err != nil {
			return err
		}
		fmt.Println(strconv.FormatInt(size, 16))
		err = f.AttachRaw()
		if err != nil {
			return err
		}
		var b [4096]byte
		for i := range b {
			b[i] = byte(i)
		}
		_, err = f.WriteAt(b[:], 0)
		if err != nil {
			return err
		}
		n, err := f.ReadAt(b[:], 0)
		if err != nil && n != len(b) {
			return err
		}
		return nil
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
