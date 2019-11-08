package cim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf16"

	"github.com/Microsoft/hcsshim/internal/cim/format"
)

type region struct {
	f    *os.File
	size int64
}

type Cim struct {
	reg     []region
	ftables []format.FileTableDirectoryEntry
	upcase  []uint16
	root    FileID
}

type File struct {
	c         *Cim
	id        FileID
	file      format.File
	size, off int64
}

type Stream struct{}

type FileID uint32

func readBin(r io.Reader, v interface{}) error {
	err := binary.Read(r, binary.LittleEndian, v)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return err
}

func validateHeader(h *format.CommonHeader) error {
	if !bytes.Equal(h.Magic[:], format.MagicValue[:]) {
		return errors.New("not a cim file")
	}
	if h.Version.Major != format.CurrentVersion.Major {
		return fmt.Errorf("unsupported cim version %v", h.Version)
	}
	return nil
}

func loadRegionSet(rs *format.RegionSet, imagePath string, reg []region) (int, error) {
	for i := 0; i < int(rs.Count); i++ {
		name := fmt.Sprintf("region_%v_%d", rs.Id, i)
		rf, err := os.Open(filepath.Join(imagePath, name))
		if err != nil {
			return 0, err
		}
		reg[i].f = rf
		size, err := rf.Seek(0, 2)
		if err != nil {
			return 0, err
		}
		_, err = rf.Seek(0, 0)
		if err != nil {
			return 0, err
		}
		reg[i].size = size
		var rh format.RegionHeader
		err = readBin(rf, &rh)
		if err != nil {
			return 0, fmt.Errorf("reading region header: %s", err)
		}
		err = validateHeader(&rh.Common)
		if err != nil {
			return 0, fmt.Errorf("validating region header: %s", err)
		}
	}
	return int(rs.Count), nil
}

func Open(imagePath string, fsName string) (_ *Cim, err error) {
	p := filepath.Join(imagePath, fsName)
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var fsh format.FilesystemHeader
	err = readBin(f, &fsh)
	if err != nil {
		return nil, fmt.Errorf("reading filesystem header: %s", err)
	}
	err = validateHeader(&fsh.Common)
	if err != nil {
		return nil, fmt.Errorf("validating filesystem header: %s", err)
	}
	parents := make([]format.RegionSet, fsh.ParentCount)
	err = readBin(f, parents)
	if err != nil {
		return nil, fmt.Errorf("reading parent region sets: %s", err)
	}
	regionCount := int(fsh.Regions.Count)
	for i := range parents {
		regionCount += int(parents[i].Count)
	}
	if regionCount == 0 || regionCount > 0x10000 {
		return nil, fmt.Errorf("invalid region count %d", regionCount)
	}
	c := &Cim{
		reg:    make([]region, regionCount),
		upcase: make([]uint16, format.UpcaseTableLength),
	}
	defer func() {
		if err != nil {
			c.Close()
		}
	}()

	reg := c.reg
	for i := range parents {
		n, err := loadRegionSet(&parents[i], imagePath, reg)
		if err != nil {
			return nil, err
		}
		reg = reg[n:]
	}
	_, err = loadRegionSet(&fsh.Regions, imagePath, reg)
	if err != nil {
		return nil, err
	}

	var fs format.Filesystem
	err = c.readBin(&fs, fsh.FilesystemOffset, 0)
	if err != nil {
		return nil, fmt.Errorf("reading filesystem info: %s", err)
	}
	c.root = FileID(fs.RootDirectory)
	c.ftables = make([]format.FileTableDirectoryEntry, fs.FileTableDirectoryLength)
	err = c.readBin(c.ftables, fs.FileTableDirectoryOffset, 0)
	if err != nil {
		return nil, fmt.Errorf("reading file table directory: %s", err)
	}
	err = c.readBin(c.upcase, fs.UpcaseTableOffset, 0)
	if err != nil {
		return nil, fmt.Errorf("reading upcase table: %s", err)
	}
	return c, nil
}

func (c *Cim) Root() FileID {
	return c.root
}

func (c *Cim) reader(o format.RegionOffset, off, size int64) (*io.SectionReader, error) {
	oi := int(o.RegionIndex())
	ob := o.ByteOffset()
	if oi >= len(c.reg) || ob == 0 {
		return nil, fmt.Errorf("invalid region offset %x", o)
	}
	reg := c.reg[oi]
	if ob > reg.size || off > reg.size-ob {
		return nil, fmt.Errorf("%s: invalid region offset %x", reg.f.Name(), o)
	}
	maxsize := reg.size - ob - off
	if size < 0 {
		size = maxsize
	} else if size > maxsize {
		return nil, fmt.Errorf("%s: invalid region size %x at offset %x", reg.f.Name(), size, o)
	}
	return io.NewSectionReader(reg.f, ob+off, size), nil
}

func (c *Cim) readCounted16(o format.RegionOffset) ([]byte, error) {
	r, err := c.reader(o, 0, -1)
	if err != nil {
		return nil, err
	}
	var n16 [2]byte
	_, err = io.ReadFull(r, n16[:])
	if err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint16(n16[:])
	b := make([]byte, n)
	_, err = io.ReadFull(r, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (c *Cim) readOffsetFull(b []byte, o format.RegionOffset, off int64) (int, error) {
	r, err := c.reader(o, off, int64(len(b)))
	if err != nil {
		return 0, err
	}
	return io.ReadFull(r, b)
}

func (c *Cim) readBin(v interface{}, o format.RegionOffset, off int64) error {
	r, err := c.reader(o, off, int64(binary.Size(v)))
	if err != nil {
		return err
	}
	return readBin(r, v)
}

func (c *Cim) OpenPath(p string) (*File, error) {
	return nil, errors.New("OpenPath: unsupported")
}

func (c *Cim) readFile(id FileID, file *format.File) error {
	if id == 0 {
		return fmt.Errorf("invalid file ID %#x", id)
	}
	tid := uint64((id - 1) / format.FilesPerTable)
	tfid := uint16((id - 1) % format.FilesPerTable)
	if tid >= uint64(len(c.ftables)) || tfid >= c.ftables[tid].Count {
		return fmt.Errorf("invalid file ID %#x", id)
	}
	esize := int(c.ftables[tid].EntrySize)
	size := binary.Size(file)
	if size > esize {
		size = esize
	}
	b := make([]byte, binary.Size(file))
	_, err := c.readOffsetFull(b[:size], c.ftables[tid].Offset, int64(tfid)*int64(esize))
	if err != nil {
		return err
	}
	readBin(bytes.NewReader(b), file)
	return nil
}

func (c *Cim) OpenID(id FileID) (*File, error) {
	f := &File{
		c:  c,
		id: id,
	}
	err := c.readFile(id, &f.file)
	if err != nil {
		return nil, err
	}
	switch typ := f.file.DefaultStream.Type(); typ {
	case format.StreamTypeData, format.StreamTypeLinkTable, format.StreamTypePeImage:

	default:
		return nil, fmt.Errorf("unsupported stream type: %d", typ)
	}
	return f, nil
}

type Filetime uint64

func (ft Filetime) Time() time.Time {
	panic("unsupported")
}

type Statbuf struct {
	FileID                                                  FileID
	Size                                                    int64
	Attributes                                              uint32
	ReparseTag                                              uint32
	CreationTime, LastWriteTime, ChangeTime, LastAccessTime Filetime
	SecurityDescriptor                                      []byte
	ExtendedAttributes                                      []byte
	ReparseBuffer                                           []byte
	// Streams?
}

type StreamStatbuf struct {
	Size int64
}

const (
	FILE_ATTRIBUTE_READONLY      = 0x00000001
	FILE_ATTRIBUTE_HIDDEN        = 0x00000002
	FILE_ATTRIBUTE_SYSTEM        = 0x00000004
	FILE_ATTRIBUTE_DIRECTORY     = 0x00000010
	FILE_ATTRIBUTE_ARCHIVE       = 0x00000020
	FILE_ATTRIBUTE_REPARSE_POINT = 0x00000400
)

func (f *File) IsDir() bool {
	return f.file.DefaultStream.Type() == format.StreamTypeLinkTable
}

func (f *File) Stat() (*Statbuf, error) {
	buf := &Statbuf{
		FileID:         f.id,
		Size:           f.file.DefaultStream.Size(),
		ReparseTag:     f.file.ReparseTag,
		CreationTime:   Filetime(f.file.CreationTime),
		LastWriteTime:  Filetime(f.file.LastWriteTime),
		ChangeTime:     Filetime(f.file.ChangeTime),
		LastAccessTime: Filetime(f.file.LastAccessTime),
	}
	attr := uint32(0)
	if f.file.Flags&format.FileFlagReadOnly != 0 {
		attr |= FILE_ATTRIBUTE_READONLY
	}
	if f.file.Flags&format.FileFlagHidden != 0 {
		attr |= FILE_ATTRIBUTE_HIDDEN
	}
	if f.file.Flags&format.FileFlagSystem != 0 {
		attr |= FILE_ATTRIBUTE_SYSTEM
	}
	if f.file.Flags&format.FileFlagArchive != 0 {
		attr |= FILE_ATTRIBUTE_ARCHIVE
	}
	if f.IsDir() {
		attr |= FILE_ATTRIBUTE_DIRECTORY
	}
	if f.file.SdOffset != format.NullOffset {
		b, err := f.c.readCounted16(f.file.SdOffset)
		if err != nil {
			return nil, err
		}
		buf.SecurityDescriptor = b
	}
	if f.file.EaOffset != format.NullOffset {
		b := make([]byte, f.file.EaLength)
		_, err := f.c.readOffsetFull(b, f.file.EaOffset, 0)
		if err != nil {
			return nil, err
		}
		buf.ExtendedAttributes = b
	}
	if f.file.ReparseOffset != format.NullOffset {
		b, err := f.c.readCounted16(f.file.ReparseOffset)
		if err != nil {
			return nil, err
		}
		buf.ReparseBuffer = b
		attr |= FILE_ATTRIBUTE_REPARSE_POINT
	}
	buf.Attributes = attr
	return buf, nil
}

func (f *File) Read(b []byte) (int, error) {
	if f.IsDir() {
		return 0, errors.New("is a directory")
	}
	if typ := f.file.DefaultStream.Type(); typ != format.StreamTypeData {
		return 0, fmt.Errorf("read of unsupported stream type %d", typ)
	}
	n := len(b)
	if int64(n) > f.file.DefaultStream.Size()-f.off {
		n = int(f.file.DefaultStream.Size() - f.off)
		b = b[n:]
	}
	n, err := f.c.readOffsetFull(b, f.file.DefaultStream.DataOffset, f.off)
	f.off += int64(n)
	return n, err
}

func (f *File) OpenStream(name string) (*Stream, error) {
	return nil, errors.New("unsupported")
}

type DirEntry struct {
	Name   string
	FileID FileID
}

func (f *File) Readdir() ([]DirEntry, error) {
	if !f.IsDir() {
		return nil, errors.New("not a directory")
	}
	size := f.file.DefaultStream.Size()
	if size == 0 {
		return nil, nil
	}
	b := make([]byte, size)
	_, err := f.c.readOffsetFull(b, f.file.DefaultStream.DataOffset, 0)
	if err != nil {
		return nil, fmt.Errorf("reading link table: %s", err)
	}

	var lt format.LinkTable
	r := bytes.NewReader(b)
	readBin(r, &lt)
	fids := b[8:]
	nos := fids[lt.LinkCount*4:]
	des := make([]DirEntry, lt.LinkCount)
	for i := range des {
		r.Seek(int64(binary.LittleEndian.Uint32(nos[i*4:])), 0)
		var nl uint16
		readBin(r, &nl)
		name16 := make([]uint16, nl)
		readBin(r, name16)
		des[i].FileID = FileID(binary.LittleEndian.Uint32(fids[i*4:]))
		des[i].Name = string(utf16.Decode(name16))
	}
	return des, nil
}

func (c *Cim) Close() error {
	for i := range c.reg {
		c.reg[i].f.Close()
	}
	return nil
}
