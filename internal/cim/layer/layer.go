package layer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf16"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/cim"
)

const (
	reparseTagWci          = 0x80000018
	reparseTagWciTombstone = 0xA000001F
)

type Layer struct {
	ID   guid.GUID
	Path string
}

type wciReparseInfo struct {
	Tag        uint32
	Size       uint16
	Reserved   uint16
	Version    uint32
	Flags      uint32
	LayerID    guid.GUID
	NameLength uint16
}

func isNotFound(err error) bool {
	cerr, ok := err.(*cim.CimError)
	return ok && cerr.Err == cim.ErrFileNotFound
}

func decodeWcifs(f *cim.File) (guid.GUID, string, error) {
	fi, err := f.Stat()
	if err != nil {
		return guid.GUID{}, "", err
	}
	b := bytes.NewReader(fi.ReparseData)
	var info wciReparseInfo
	err = binary.Read(b, binary.LittleEndian, &info)
	if err != nil {
		return guid.GUID{}, "", fmt.Errorf("reading WCI reparse info: %s", err)
	}
	if info.Tag != reparseTagWci {
		return guid.GUID{}, "", fmt.Errorf("wrong reparse tag 0x%x", info.Tag)
	}
	if int(info.Size) > len(fi.ReparseData) {
		return guid.GUID{}, "", fmt.Errorf("invalid reparse length %d > %d", info.Size, len(fi.ReparseData))
	}
	if info.Version != 1 {
		return guid.GUID{}, "", fmt.Errorf("unsupported wcifs version %d", info.Version)
	}
	name16 := make([]uint16, info.NameLength)
	err = binary.Read(b, binary.LittleEndian, name16)
	if err != nil {
		return guid.GUID{}, "", fmt.Errorf("reading WCI reparse name: %s", err)
	}
	return info.LayerID, string(utf16.Decode(name16)), nil
}

func encodeWcifs(id guid.GUID, p string) (uint32, []byte) {
	var buf bytes.Buffer
	p16 := utf16.Encode([]rune(p))
	info := wciReparseInfo{
		Tag:        reparseTagWci,
		Version:    1,
		LayerID:    id,
		NameLength: uint16(len(p16)),
	}
	info.Size = uint16(binary.Size(info) - 8 + len(p16)*2)
	binary.Write(&buf, binary.LittleEndian, &info)
	binary.Write(&buf, binary.LittleEndian, p16)
	return reparseTagWci, buf.Bytes()
}

func findParent(p string, parentID guid.GUID, ls map[guid.GUID]*cim.Reader) (guid.GUID, *cim.File, error) {
	id := parentID
	for i := 0; i < len(ls); i++ {
		l, ok := ls[id]
		if !ok {
			return id, nil, errors.New("invalid layer ID")
		}
		f, rem, err := l.Root().WalkPath(p)
		if err != nil {
			return id, nil, err
		}
		if !f.IsDir() {
			return id, nil, nil
		}
		if f.ReparseTag() != reparseTagWci {
			if rem == "" {
				return id, f, nil
			} else {
				return id, nil, nil
			}
		}
		nextID, tp, err := decodeWcifs(f)
		if err != nil {
			return id, nil, err
		}
		p = strings.ReplaceAll(tp, "\\", "/") + "/" + rem
		id = nextID
	}
	return id, nil, errors.New("layer loop")
}

func Expand(p string, expandedFS string, parentID guid.GUID, layers []Layer) error {
	r, err := cim.Open(p)
	if err != nil {
		return err
	}
	defer r.Close()
	ls := make(map[guid.GUID]*cim.Reader)
	for _, l := range layers {
		lr, err := cim.Open(l.Path)
		if err != nil {
			return err
		}
		defer lr.Close()
		ls[l.ID] = lr
	}
	if _, ok := ls[parentID]; !ok {
		return errors.New("parent layer does not exist")
	}
	w, err := cim.Append(p, expandedFS)
	if err != nil {
		return err
	}
	defer w.Close()
	err = cim.Walk(r.Root(), func(f *cim.File, _ *cim.Stream) (bool, error) {
		if f.ReparseTag() == reparseTagWciTombstone {
			err := w.RemoveFile(f.Name())
			if err != nil {
				return false, err
			}
		}
		if !f.IsDir() {
			return false, nil
		}
		pid, pf, err := findParent(f.Name(), parentID, ls)
		if err != nil {
			return false, err
		}
		if pf != nil {
			cs, err := pf.Readdir()
			if err != nil {
				return false, err
			}
			for _, c := range cs {
				_, err := f.OpenAt(c)
				if err == nil {
					// Do not replace files that already exist.
					// N.B. this will also handle tombstones.
					continue
				}
				if !isNotFound(err) {
					return false, err
				}
				pfc, err := pf.OpenAt(c)
				if err != nil {
					return false, err
				}
				fi, err := pfc.Stat()
				if err != nil {
					return false, err
				}
				if len(fi.ReparseData) == 0 {
					pfcp := pfc.Name()
					for len(pfcp) > 0 && pfcp[0] == '/' {
						pfcp = pfcp[1:]
					}
					pfcp = strings.ReplaceAll(pfcp, "/", `\\`)
					fi.ReparseTag, fi.ReparseData = encodeWcifs(pid, pfcp)
					fi.Attributes |= cim.FILE_ATTRIBUTE_REPARSE_POINT
					// WCI reparse points are sparse so that they can report the
					// file's size without having any actual backing data.
					fi.Attributes |= cim.FILE_ATTRIBUTE_SPARSE_FILE
				}
				err = w.AddFile(path.Join(f.Name(), c), fi)
				if err != nil {
					return false, err
				}
				err = w.CloseStream()
				if err != nil {
					return false, err
				}
				// TODO: handle streams
			}
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return w.Commit()
}
