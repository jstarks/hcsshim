package hcsshim

import (
	"syscall"
	"testing"
)

func TestReaderThing(t *testing.T) {
	h, err := syscall.Open(`c:\windows\system32\kernelbase.dll`, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(h)

	err = ReadHeader(makeBackupReader(h))
	if err != nil {
		t.Fatal(err)
	}
}
