package cim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/hcsshim/internal/cim/format"
)

type region struct {
	f    *os.File
	size int64
}

type fileTable []byte

type Cim struct {
	reg     []region
	ftdes   []format.FileTableDirectoryEntry
	ftables []fileTable
	upcase  []uint16
	root    inode
	cm      sync.Mutex
	sdCache map[format.RegionOffset][]byte
}

type File struct {
	c          *Cim
	name       string
	off        int64
	ino        inode
	pe         format.PeImage
	pemappings []format.PeImageMapping
	peinit     bool
}

type inode struct {
	id        FileID
	file      format.File
	linkTable []byte
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
		name := fmt.Sprintf("region_%v_%d", rs.ID, i)
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
		reg:     make([]region, regionCount),
		upcase:  make([]uint16, format.UpcaseTableLength),
		sdCache: make(map[format.RegionOffset][]byte),
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
	c.ftables = make([]fileTable, fs.FileTableDirectoryLength)
	c.ftdes = make([]format.FileTableDirectoryEntry, fs.FileTableDirectoryLength)
	err = c.readBin(c.ftdes, fs.FileTableDirectoryOffset, 0)
	if err != nil {
		return nil, fmt.Errorf("reading file table directory: %s", err)
	}
	err = c.readBin(c.upcase, fs.UpcaseTableOffset, 0)
	if err != nil {
		return nil, fmt.Errorf("reading upcase table: %s", err)
	}
	err = c.getInode(FileID(fs.RootDirectory), &c.root)
	if err != nil {
		return nil, fmt.Errorf("reading root directory: %s", err)
	}
	return c, nil
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

func (c *Cim) OpenAt(dirf *File, p string) (*File, error) {
	fullp := p
	dirOnly := len(p) > 0 && p[len(p)-1] == '/'
	p = path.Clean(p)
	f := &File{c: c}
	if dirf != nil && !dirf.IsDir() {
		return nil, fmt.Errorf("not a directory %s", dirf.name)
	}
	var ino *inode
	if p[0] == '/' {
		ino = &c.root
		p = p[1:]
	} else if dirf == nil {
		ino = &c.root
	} else {
		fullp = path.Join(dirf.name, fullp)
		if dirOnly {
			fullp += "/"
		}
		ino = &dirf.ino
	}
	if len(p) > 0 && p != "." {
		for _, name := range strings.Split(p, "/") {
			fid, err := c.findChild(ino, name)
			if err != nil {
				return nil, fmt.Errorf("file not found '%s'", fullp)
			}
			err = c.getInode(fid, &f.ino)
			if err != nil {
				return nil, err
			}
			ino = &f.ino
		}
	} else {
		f.ino = *ino
	}
	if dirOnly && !f.IsDir() {
		return nil, fmt.Errorf("not a directory '%s'", fullp)
	}
	f.name = fullp
	return f, nil
}

func (c *Cim) readFile(id FileID, file *format.File) error {
	if id == 0 {
		return fmt.Errorf("invalid file ID %#x", id)
	}
	tid := uint64((id - 1) / format.FilesPerTable)
	tfid := int((id - 1) % format.FilesPerTable)
	if tid >= uint64(len(c.ftdes)) || tfid >= int(c.ftdes[tid].Count) {
		return fmt.Errorf("invalid file ID %#x", id)
	}
	esize := int(c.ftdes[tid].EntrySize)
	if c.ftables[tid] == nil {
		b := make([]byte, esize*int(c.ftdes[tid].Count))
		_, err := c.readOffsetFull(b, c.ftdes[tid].Offset, 0)
		if err != nil {
			return fmt.Errorf("reading file table %d: %s", tid, err)
		}
		c.ftables[tid] = b
	}
	// TODO avoid this second copy (needed because the on-disk file size may be
	// smaller than format.File).
	b := make([]byte, binary.Size(file))
	copy(b, c.ftables[tid][tfid*esize:(tfid+1)*esize])
	readBin(bytes.NewReader(b), file)
	return nil
}

func (c *Cim) getInode(id FileID, ino *inode) error {
	*ino = inode{
		id: id,
	}
	err := c.readFile(id, &ino.file)
	if err != nil {
		return err
	}
	switch typ := ino.file.DefaultStream.Type(); typ {
	case format.StreamTypeData,
		format.StreamTypeLinkTable,
		format.StreamTypePeImage:

	default:
		return fmt.Errorf("unsupported stream type: %d", typ)
	}
	return nil
}

type Filetime int64

func (ft Filetime) Time() time.Time {
	if ft == 0 {
		return time.Time{}
	}
	// 100-nanosecond intervals since January 1, 1601
	nsec := int64(ft)
	// change starting time to the Epoch (00:00:00 UTC, January 1, 1970)
	nsec -= 116444736000000000
	// convert into nanoseconds
	nsec *= 100
	return time.Unix(0, nsec)
}

func (ft Filetime) String() string {
	return ft.Time().String()
}

type FileInfo struct {
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

func (ino *inode) IsDir() bool {
	return ino.file.DefaultStream.Type() == format.StreamTypeLinkTable
}

func (f *File) IsDir() bool {
	return f.ino.IsDir()
}

func (c *Cim) getSd(o format.RegionOffset) ([]byte, error) {
	c.cm.Lock()
	if sd, ok := c.sdCache[o]; ok {
		c.cm.Unlock()
		return sd, nil
	}
	c.cm.Unlock()
	sd, err := c.readCounted16(o)
	if err != nil {
		return nil, err
	}
	c.cm.Lock()
	c.sdCache[o] = sd
	c.cm.Unlock()
	return sd, nil
}

func (c *Cim) stat(ino *inode) (*FileInfo, error) {
	fi := &FileInfo{
		FileID:         ino.id,
		Size:           ino.file.DefaultStream.Size(),
		ReparseTag:     ino.file.ReparseTag,
		CreationTime:   Filetime(ino.file.CreationTime),
		LastWriteTime:  Filetime(ino.file.LastWriteTime),
		ChangeTime:     Filetime(ino.file.ChangeTime),
		LastAccessTime: Filetime(ino.file.LastAccessTime),
	}
	attr := uint32(0)
	if ino.file.Flags&format.FileFlagReadOnly != 0 {
		attr |= FILE_ATTRIBUTE_READONLY
	}
	if ino.file.Flags&format.FileFlagHidden != 0 {
		attr |= FILE_ATTRIBUTE_HIDDEN
	}
	if ino.file.Flags&format.FileFlagSystem != 0 {
		attr |= FILE_ATTRIBUTE_SYSTEM
	}
	if ino.file.Flags&format.FileFlagArchive != 0 {
		attr |= FILE_ATTRIBUTE_ARCHIVE
	}
	if ino.IsDir() {
		attr |= FILE_ATTRIBUTE_DIRECTORY
	}
	if ino.file.SdOffset != format.NullOffset {
		sd, err := c.getSd(ino.file.SdOffset)
		if err != nil {
			return nil, err
		}
		fi.SecurityDescriptor = sd
	}
	if ino.file.EaOffset != format.NullOffset {
		b := make([]byte, ino.file.EaLength)
		_, err := c.readOffsetFull(b, ino.file.EaOffset, 0)
		if err != nil {
			return nil, err
		}
		fi.ExtendedAttributes = b
	}
	if ino.file.ReparseOffset != format.NullOffset {
		b, err := c.readCounted16(ino.file.ReparseOffset)
		if err != nil {
			return nil, err
		}
		fi.ReparseBuffer = b
		attr |= FILE_ATTRIBUTE_REPARSE_POINT
	}
	fi.Attributes = attr
	return fi, nil
}

func (f *File) Stat() (*FileInfo, error) {
	return f.c.stat(&f.ino)
}

func (f *File) getPESegment(off int64) (int64, int64, error) {
	if !f.peinit {
		err := f.c.readBin(&f.pe, f.ino.file.DefaultStream.DataOffset, 0)
		if err != nil {
			return 0, 0, fmt.Errorf("reading PE image descriptor: %s", err)
		}
		f.pe.DataLength &= 0x7fffffffffffffff // avoid returning negative lengths
		f.pemappings = make([]format.PeImageMapping, f.pe.MappingCount)
		err = f.c.readBin(f.pemappings, f.ino.file.DefaultStream.DataOffset, int64(binary.Size(&f.pe)))
		if err != nil {
			return 0, 0, fmt.Errorf("reading PE image mappings: %s", err)
		}
		f.peinit = true
	}
	d := int64(0)
	end := f.pe.DataLength
	for _, m := range f.pemappings {
		if int64(m.FileOffset) > off {
			end = int64(m.FileOffset)
			break
		}
		d = int64(m.Delta)
	}
	return d, end - d, nil
}

func (f *File) Read(b []byte) (int, error) {
	if f.IsDir() {
		return 0, errors.New("is a directory")
	}
	if typ := f.ino.file.DefaultStream.Type(); typ != format.StreamTypeData {
		return 0, fmt.Errorf("read of unsupported stream type %d", typ)
	}
	n := len(b)
	rem := f.ino.file.DefaultStream.Size() - f.off
	if int64(n) > rem {
		n = int(rem)
	}
	off := f.off
	if f.ino.file.DefaultStream.Type() == format.StreamTypePeImage {
		delta, segrem, err := f.getPESegment(f.off)
		if err != nil {
			return 0, err
		}
		if int64(n) > segrem {
			n = int(segrem)
		}
		off += delta
	}
	n, err := f.c.readOffsetFull(b[:n], f.ino.file.DefaultStream.DataOffset, off)
	f.off += int64(n)
	rem -= int64(n)
	if err == nil && rem == 0 {
		err = io.EOF
	}
	return n, err
}

func (f *File) OpenStream(name string) (*Stream, error) {
	return nil, errors.New("unsupported")
}

func parseName(b []byte, nos []byte, i int) ([]byte, error) {
	size := uint32(len(b))
	no := binary.LittleEndian.Uint32(nos[i*4:])
	if no > size-2 {
		return nil, fmt.Errorf("invalid name offset %d > %d", no, size-2)
	}
	nl := binary.LittleEndian.Uint16(b[no:])
	if mnl := (size - 2 - no) / 2; uint32(nl) > mnl {
		return nil, fmt.Errorf("invalid name length %d > %d", nl, mnl)
	}
	return b[no+2 : no+2+uint32(nl)*2], nil
}

func (c *Cim) bsearchLinkTable(b []byte, esize int, name string) ([]byte, error) {
	if len(b) == 0 {
		return nil, nil
	}
	n := binary.LittleEndian.Uint32(b[4:])
	es := b[8:]
	nos := es[n*4:]
	lo := 0
	hi := int(n)
	i := hi / 2
	for hi > lo {
		name16, err := parseName(b, nos, i)
		if err != nil {
			return nil, err
		}
		cmp := cmpcaseUtf8Utf16LE(name, name16, c.upcase)
		if cmp < 0 {
			hi = i
		} else if cmp == 0 {
			return es[i*esize : (i+1)*esize], nil
		} else {
			lo = i
		}
		i = lo + (hi-lo)/2
	}
	return nil, nil
}

func enumLinkTable(b []byte, esize int, f func(string, []byte) error) error {
	if len(b) == 0 {
		return nil
	}
	var lt format.LinkTable
	r := bytes.NewReader(b)
	readBin(r, &lt)
	es := b[8:]
	nos := es[lt.LinkCount*4:]
	for i := 0; i < int(lt.LinkCount); i++ {
		name, err := parseName(b, nos, i)
		if err != nil {
			return err
		}
		if err := f(parseUtf16LE(name), es[i*esize:(i+1)*esize]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cim) getDirectoryTable(ino *inode) ([]byte, error) {
	if ino.linkTable == nil && ino.IsDir() {
		ssize := ino.file.DefaultStream.Size()
		if ssize == 0 {
			return nil, nil
		}
		b := make([]byte, ssize)
		_, err := c.readOffsetFull(b, ino.file.DefaultStream.DataOffset, 0)
		if err != nil {
			return nil, fmt.Errorf("reading link table: %s", err)
		}
		if len(b) < 8 {
			return nil, fmt.Errorf("invalid link table size %d", len(b))
		}
		size := binary.LittleEndian.Uint32(b[0:])
		n := binary.LittleEndian.Uint32(b[4:])
		if size < 8 {
			return nil, fmt.Errorf("invalid link table size %d", size)
		}
		if int64(size) > int64(len(b)) {
			return nil, fmt.Errorf("link table size mismatch %d < %d", len(b), size)
		}
		b = b[:size]
		esize := 4
		if maxn := size - 8/(uint32(esize)+4); maxn < n {
			return nil, fmt.Errorf("link table count mismatch %d < %d", maxn, n)
		}
		ino.linkTable = b
	}
	return ino.linkTable, nil
}

func (c *Cim) findChild(ino *inode, name string) (FileID, error) {
	table, err := c.getDirectoryTable(ino)
	if err != nil {
		return 0, err
	}
	if table != nil {
		b, err := c.bsearchLinkTable(table, 4, name)
		if err != nil {
			return 0, err
		}
		if b != nil {
			return FileID(binary.LittleEndian.Uint32(b)), nil
		}
	}
	return 0, errors.New("file not found")
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Readdir() ([]string, error) {
	if !f.ino.IsDir() {
		return nil, errors.New("not a directory")
	}
	table, err := f.c.getDirectoryTable(&f.ino)
	if err != nil {
		return nil, err
	}
	var names []string
	err = enumLinkTable(table, 4, func(name string, fid []byte) error {
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func (c *Cim) Close() error {
	for i := range c.reg {
		c.reg[i].f.Close()
	}
	return nil
}
