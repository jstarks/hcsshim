// +build ignore

package hcsshim

import (
	"C" // this must be here to allow callbacks on non-Go threads
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"syscall"
)

var DataError = errors.New("invalid data from backup stream")

//sys backupRead(handle syscall.Handle, buffer *byte, size uint32, read *uint32, abort bool, processSecurity bool, context *uintptr) (err error) = BackupRead
// sys convertSecurityDescriptorToStringSecurityDescriptor(sd *byte, revision uint32, info uint32, sd *uintptr, size *uint32) (err error) = advapi32.ConvertSecurityDescriptorToStringSecurityDescriptorW

type backupReader struct {
	handle  syscall.Handle
	context uintptr
}

func makeBackupReader(handle syscall.Handle) *backupReader {
	return &backupReader{handle: handle}
}

func (r *backupReader) Read(p []byte) (int, error) {
	var read uint32
	err := backupRead(r.handle, &p[0], uint32(len(p)), &read, false, true, &r.context)
	if err != nil {
		return 0, err
	}
	if read == 0 && len(p) != 0 {
		return 0, io.EOF
	}
	return int(read), nil
}

func (r *backupReader) Close() error {
	backupRead(r.handle, nil, 0, nil, true, true, &r.context)
	return nil
}

const (
	offStreamId = 0
	offAttr     = 4
	offSize     = 8
	offNameSize = 16
	offStream   = 20

	streamIdData     = 1
	streamIdEaData   = 2
	streamIdSecurity = 3
	streamIdAltData  = 4
	streamIdReparse  = 8
	streamIdSparse   = 9
)

func ReadHeader(r io.Reader) error {
	br := bufio.NewReader(r)
	var s = make([]byte, offStream)
	for {
		n, err := br.Read(s)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if n != offStream {
			return DataError
		}
		streamId := binary.LittleEndian.Uint32(s[offStreamId:offAttr])
		// attr := binary.LittleEndian.Uint32(s[offAttr:offSize])
		size := binary.LittleEndian.Uint64(s[offSize:offNameSize])
		nameSize := binary.LittleEndian.Uint32(s[offNameSize:offStream])
		switch streamId {
		case streamIdEaData:
			var used int
			for {
				var nextOffset uint32
				var flags byte
				var nameLength byte
				var valueLength uint16
				err = binary.Read(br, binary.LittleEndian, &nextOffset)
				if err == nil {
					err = binary.Read(br, binary.LittleEndian, &flags)
				}
				if err == nil {
					err = binary.Read(br, binary.LittleEndian, &nameLength)
				}
				if err == nil {
					err = binary.Read(br, binary.LittleEndian, &valueLength)
				}
				var eaName string
				if err == nil {
					eaNameS := make([]byte, nameLength)
					err = binary.Read(br, binary.LittleEndian, eaNameS)
					eaName = string(eaNameS)
				}
				if err != nil {
					return err
				}
				fmt.Printf("EA %s\n", eaName)
				if nextOffset == 0 {
					used += int(nameLength) + 8
					break
				}
				_, err = br.Discard(int(nextOffset - 8 - uint32(nameLength)))
				if err != nil {
					return err
				}
				used += int(nextOffset)
			}
			_, err = br.Discard(int(size) - used)
			if err != nil {
				return err
			}

		case streamIdData, streamIdAltData, streamIdReparse, streamIdSecurity, streamIdSparse:
			name := make([]uint16, nameSize/2)
			err = binary.Read(br, binary.LittleEndian, name)
			if err != nil {
				return err
			}
			fmt.Printf("stream %d: %s\n", streamId, syscall.UTF16ToString(name))
			_, err = br.Discard(int(nameSize) + int(size))
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown stream id %d", streamId)
		}
	}
	return nil
}
