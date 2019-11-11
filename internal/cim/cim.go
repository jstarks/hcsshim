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

var (
	ErrFileNotFound  = errors.New("file not found")
	ErrNotADirectory = errors.New("not a directory")
	ErrIsADirectory  = errors.New("is a directory")
)

type region struct {
	f    *os.File
	size int64
}

type fileTable []byte

type Cim struct {
	name       string
	reg        []region
	ftdes      []format.FileTableDirectoryEntry
	ftables    []fileTable
	upcase     []uint16
	root       *inode
	cm         sync.Mutex
	inodeCache map[format.FileID]*inode
	sdCache    map[format.RegionOffset][]byte
}

type File struct {
	c    *Cim
	name string
	r    streamReader
	ino  *inode
}

type inode struct {
	id          format.FileID
	file        format.File
	linkTable   []byte
	streamTable []byte
}

type streamReader struct {
	stream     format.Stream
	off        int64
	pe         format.PeImage
	pemappings []format.PeImageMapping
	peinit     bool
}

type Stream struct {
	c     *Cim
	r     streamReader
	fname string
	name  string
}

type CimError struct {
	Cim    string
	Op     string
	Path   string
	Stream string
	Err    error
}

func (e *CimError) Error() string {
	s := "cim " + e.Op + " " + e.Cim
	if e.Path != "" {
		s += ":" + e.Path
		if e.Stream != "" {
			s += ":" + e.Stream
		}
	}
	s += ": " + e.Err.Error()
	return s
}

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
		fi, err := rf.Stat()
		if err != nil {
			return 0, err
		}
		reg[i].size = fi.Size()
		var rh format.RegionHeader
		err = readBin(rf, &rh)
		if err != nil {
			return 0, fmt.Errorf("reading region header %s: %s", name, err)
		}
		err = validateHeader(&rh.Common)
		if err != nil {
			return 0, fmt.Errorf("validating region header %s: %s", name, err)
		}
	}
	return int(rs.Count), nil
}

func Open(imagePath string, fsName string) (_ *Cim, err error) {
	p := filepath.Join(imagePath, fsName)
	defer func() {
		if err != nil {
			err = &CimError{Cim: p, Op: "open", Err: err}
		}
	}()
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
		name:       p,
		reg:        make([]region, regionCount),
		upcase:     make([]uint16, format.UpcaseTableLength),
		inodeCache: make(map[format.FileID]*inode),
		sdCache:    make(map[format.RegionOffset][]byte),
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
	c.root, err = c.getInode(fs.RootDirectory)
	if err != nil {
		return nil, fmt.Errorf("reading root directory: %s", err)
	}
	return c, nil
}

func (c *Cim) Close() error {
	for i := range c.reg {
		c.reg[i].f.Close()
	}
	return nil
}

func (c *Cim) reader(o format.RegionOffset, off, size int64) (*io.SectionReader, error) {
	oi := int(o.RegionIndex())
	ob := o.ByteOffset()
	if oi >= len(c.reg) || ob == 0 {
		return nil, fmt.Errorf("invalid region offset 0x%x", o)
	}
	reg := c.reg[oi]
	if ob > reg.size || off > reg.size-ob {
		return nil, fmt.Errorf("%s: invalid region offset 0x%x", reg.f.Name(), o)
	}
	maxsize := reg.size - ob - off
	if size < 0 {
		size = maxsize
	} else if size > maxsize {
		return nil, fmt.Errorf("%s: invalid region size %x at offset 0x%x", reg.f.Name(), size, o)
	}
	return io.NewSectionReader(reg.f, ob+off, size), nil
}

func (c *Cim) readCounted(o format.RegionOffset, csize int) ([]byte, error) {
	r, err := c.reader(o, 0, -1)
	if err != nil {
		return nil, err
	}
	var n uint32
	if csize == 2 {
		var n16 uint16
		err = readBin(r, &n16)
		if err != nil {
			return nil, err
		}
		n = uint32(n16)
	} else if csize == 4 {
		var n32 uint32
		err = readBin(r, &n32)
		if err != nil {
			return nil, err
		}
		n = n32
	} else {
		panic("invalid count size")
	}
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

func (c *Cim) OpenAt(dirf *File, p string) (_ *File, err error) {
	fullp := p
	defer func() {
		if err != nil {
			err = &CimError{Cim: c.name, Path: fullp, Op: "openat", Err: err}
		}
	}()
	dirOnly := len(p) > 0 && p[len(p)-1] == '/'
	p = path.Clean(p)
	if dirf != nil && !dirf.IsDir() {
		return nil, ErrNotADirectory
	}
	var ino *inode
	if p[0] == '/' {
		ino = c.root
		p = p[1:]
	} else if dirf == nil {
		ino = c.root
	} else {
		fullp = path.Join(dirf.name, fullp)
		if dirOnly {
			fullp += "/"
		}
		ino = dirf.ino
	}
	if len(p) > 0 && p != "." {
		for _, name := range strings.Split(p, "/") {
			fid, err := c.findChild(ino, name)
			if err != nil {
				return nil, ErrFileNotFound
			}
			ino, err = c.getInode(fid)
			if err != nil {
				return nil, err
			}
		}
	}
	if dirOnly && !ino.IsDir() {
		return nil, ErrNotADirectory
	}
	f := &File{
		c:    c,
		name: fullp,
		ino:  ino,
		r:    streamReader{stream: ino.file.DefaultStream},
	}
	return f, nil
}

func (c *Cim) readFile(id format.FileID, file *format.File) error {
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
	// This second copy is needed because the on-disk file size may be smaller
	// than format.File).
	b := make([]byte, binary.Size(file))
	copy(b, c.ftables[tid][tfid*esize:(tfid+1)*esize])
	readBin(bytes.NewReader(b), file)
	return nil
}

func (c *Cim) getInode(id format.FileID) (*inode, error) {
	c.cm.Lock()
	ino, ok := c.inodeCache[id]
	c.cm.Unlock()
	if ok {
		return ino, nil
	}
	ino = &inode{
		id: id,
	}
	err := c.readFile(id, &ino.file)
	if err != nil {
		return nil, err
	}
	switch typ := ino.file.DefaultStream.Type(); typ {
	case format.StreamTypeData,
		format.StreamTypeLinkTable,
		format.StreamTypePeImage:

	default:
		return nil, fmt.Errorf("unsupported stream type: %d", typ)
	}
	c.cm.Lock()
	c.inodeCache[id] = ino
	c.cm.Unlock()
	return ino, nil
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
	FileID                                                  uint64
	Size                                                    int64
	Attributes                                              uint32
	ReparseTag                                              uint32
	CreationTime, LastWriteTime, ChangeTime, LastAccessTime Filetime
	SecurityDescriptor                                      []byte
	ExtendedAttributes                                      []byte
	ReparseBuffer                                           []byte
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
	sd, ok := c.sdCache[o]
	c.cm.Unlock()
	if ok {
		return sd, nil
	}
	sd, err := c.readCounted(o, 2)
	if err != nil {
		return nil, fmt.Errorf("reading security descriptor at 0x%x: %s", o, err)
	}
	c.cm.Lock()
	c.sdCache[o] = sd
	c.cm.Unlock()
	return sd, nil
}

func (c *Cim) stat(ino *inode) (*FileInfo, error) {
	fi := &FileInfo{
		FileID:         uint64(ino.id),
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
			return nil, fmt.Errorf("reading EA buffer at %#x: %s", ino.file.EaOffset, err)
		}
		fi.ExtendedAttributes = b
	}
	if ino.file.ReparseOffset != format.NullOffset {
		b, err := c.readCounted(ino.file.ReparseOffset, 2)
		if err != nil {
			return nil, fmt.Errorf("reading reparse buffer at %#x: %s", ino.file.EaOffset, err)
		}
		fi.ReparseBuffer = b
		attr |= FILE_ATTRIBUTE_REPARSE_POINT
	}
	fi.Attributes = attr
	return fi, nil
}

func (f *File) Stat() (*FileInfo, error) {
	fi, err := f.c.stat(f.ino)
	if err != nil {
		err = &CimError{Cim: f.c.name, Path: f.name, Op: "stat", Err: err}
	}
	return fi, err
}

func (c *Cim) getPESegment(r *streamReader, off int64) (int64, int64, error) {
	if !r.peinit {
		err := c.readBin(&r.pe, r.stream.DataOffset, 0)
		if err != nil {
			return 0, 0, fmt.Errorf("reading PE image descriptor: %s", err)
		}
		r.pe.DataLength &= 0x7fffffffffffffff // avoid returning negative lengths
		r.pemappings = make([]format.PeImageMapping, r.pe.MappingCount)
		err = c.readBin(r.pemappings, r.stream.DataOffset, int64(binary.Size(&r.pe)))
		if err != nil {
			return 0, 0, fmt.Errorf("reading PE image mappings: %s", err)
		}
		r.peinit = true
	}
	d := int64(0)
	end := r.pe.DataLength
	for _, m := range r.pemappings {
		if int64(m.FileOffset) > off {
			end = int64(m.FileOffset)
			break
		}
		d = int64(m.Delta)
	}
	return d, end - off, nil
}

func (c *Cim) readStream(r *streamReader, b []byte) (_ int, err error) {
	n := len(b)
	rem := r.stream.Size() - r.off
	if int64(n) > rem {
		n = int(rem)
	}
	ro := r.stream.DataOffset
	off := r.off
	if r.stream.Type() == format.StreamTypePeImage {
		delta, segrem, err := c.getPESegment(r, r.off)
		if err != nil {
			return 0, err
		}
		if int64(n) > segrem {
			n = int(segrem)
		}
		ro = r.pe.DataOffset
		off += delta
	}
	n, err = c.readOffsetFull(b[:n], ro, off)
	r.off += int64(n)
	rem -= int64(n)
	if err == nil && rem == 0 {
		err = io.EOF
	}
	return n, err
}

func (f *File) Read(b []byte) (_ int, err error) {
	defer func() {
		if err != nil && err != io.EOF {
			err = &CimError{Cim: f.c.name, Path: f.Name(), Op: "read", Err: err}
		}
	}()
	if f.IsDir() {
		return 0, ErrIsADirectory
	}
	return f.c.readStream(&f.r, b)
}

const (
	ltNameOffSize = 4
	ltNameLenSize = 2
	ltSizeOff     = 0
	ltCountOff    = 4
	ltEntryOff    = 8
	fileIDSize    = 4
	streamSize    = 16
)

func parseName(b []byte, nos []byte, i int) ([]byte, error) {
	size := uint32(len(b))
	no := binary.LittleEndian.Uint32(nos[i*ltNameOffSize:])
	if no > size-ltNameLenSize {
		return nil, fmt.Errorf("invalid name offset %d > %d", no, size-ltNameLenSize)
	}
	nl := binary.LittleEndian.Uint16(b[no:])
	if mnl := (size - ltNameLenSize - no) / 2; uint32(nl) > mnl {
		return nil, fmt.Errorf("invalid name length %d > %d", nl, mnl)
	}
	return b[no+ltNameLenSize : no+ltNameLenSize+uint32(nl)*2], nil
}

func bsearchLinkTable(b []byte, esize int, name string, upcase []uint16) ([]byte, error) {
	if len(b) == 0 {
		return nil, nil
	}
	n := binary.LittleEndian.Uint32(b[ltCountOff:])
	es := b[ltEntryOff:]
	nos := es[n*uint32(esize):]
	lo := 0
	hi := int(n)
	for hi > lo {
		i := lo + (hi-lo)/2
		name16, err := parseName(b, nos, i)
		if err != nil {
			return nil, err
		}
		cmp := cmpcaseUtf8Utf16LE(name, name16, upcase)
		if cmp < 0 {
			hi = i
		} else if cmp == 0 {
			return es[i*esize : (i+1)*esize], nil
		} else {
			lo = i + 1
		}
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
	es := b[ltEntryOff:]
	nos := es[lt.LinkCount*fileIDSize:]
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

func validateLinkTable(b []byte, esize int) error {
	if len(b) < ltEntryOff {
		return fmt.Errorf("invalid link table size %d", len(b))
	}
	size := binary.LittleEndian.Uint32(b[ltSizeOff:])
	n := binary.LittleEndian.Uint32(b[ltCountOff:])
	if size < ltEntryOff {
		return fmt.Errorf("invalid link table size %d", size)
	}
	if int64(size) > int64(len(b)) {
		return fmt.Errorf("link table size mismatch %d < %d", len(b), size)
	}
	b = b[:size]
	if maxn := size - ltEntryOff/(uint32(esize)+ltNameOffSize); maxn < n {
		return fmt.Errorf("link table count mismatch %d < %d", maxn, n)
	}
	return nil
}

func (c *Cim) getDirectoryTable(ino *inode) ([]byte, error) {
	if !ino.IsDir() || ino.file.DefaultStream.Size() == 0 {
		return nil, nil
	}
	c.cm.Lock()
	b := ino.linkTable
	c.cm.Unlock()
	if b == nil {
		b = make([]byte, ino.file.DefaultStream.Size())
		_, err := c.readOffsetFull(b, ino.file.DefaultStream.DataOffset, 0)
		if err != nil {
			return nil, fmt.Errorf("reading directory link table: %s", err)
		}
		err = validateLinkTable(b, fileIDSize)
		if err != nil {
			return nil, err
		}
		c.cm.Lock()
		ino.linkTable = b
		c.cm.Unlock()
	}
	return b, nil
}

func (c *Cim) findChild(ino *inode, name string) (format.FileID, error) {
	table, err := c.getDirectoryTable(ino)
	if err != nil {
		return 0, err
	}
	if table != nil {
		b, err := bsearchLinkTable(table, fileIDSize, name, c.upcase)
		if err != nil {
			return 0, err
		}
		if b != nil {
			return format.FileID(binary.LittleEndian.Uint32(b)), nil
		}
	}
	return 0, ErrFileNotFound
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Readdir() (_ []string, err error) {
	defer func() {
		if err != nil {
			err = &CimError{Cim: f.c.name, Path: f.name, Op: "readdir", Err: err}
		}
	}()
	if !f.ino.IsDir() {
		return nil, ErrNotADirectory
	}
	table, err := f.c.getDirectoryTable(f.ino)
	if err != nil {
		return nil, err
	}
	var names []string
	err = enumLinkTable(table, fileIDSize, func(name string, fid []byte) error {
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func (c *Cim) getStreamTable(ino *inode) ([]byte, error) {
	if ino.file.StreamTableOffset == format.NullOffset {
		return nil, nil
	}
	c.cm.Lock()
	table := ino.streamTable
	c.cm.Unlock()
	if table == nil {
		b, err := c.readCounted(ino.file.StreamTableOffset, 4)
		if err != nil {
			return nil, fmt.Errorf("reading stream link table: %s", err)
		}
		err = validateLinkTable(b, streamSize)
		if err != nil {
			return nil, err
		}
		table = b
		c.cm.Lock()
		ino.streamTable = table
		c.cm.Unlock()
	}
	return table, nil
}

func (f *File) Readstreams() (_ []string, err error) {
	defer func() {
		if err != nil {
			err = &CimError{Cim: f.c.name, Path: f.name, Op: "readstreams", Err: err}
		}
	}()
	table, err := f.c.getStreamTable(f.ino)
	if err != nil {
		return nil, err
	}
	var names []string
	err = enumLinkTable(table, streamSize, func(name string, stream []byte) error {
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func (f *File) OpenStream(name string) (_ *Stream, err error) {
	defer func() {
		if err != nil {
			err = &CimError{Cim: f.c.name, Path: f.name, Stream: name, Op: "openstream", Err: err}
		}
	}()
	table, err := f.c.getStreamTable(f.ino)
	if err != nil {
		return nil, err
	}
	if table != nil {
		sb, err := bsearchLinkTable(table, streamSize, name, f.c.upcase)
		if err != nil {
			return nil, err
		}
		if sb != nil {
			s := &Stream{c: f.c, fname: f.name, name: name}
			readBin(bytes.NewReader(sb), &s.r.stream)
			if typ := s.r.stream.Type(); typ != format.StreamTypeData {
				return nil, fmt.Errorf("unsupported stream type %d", typ)
			}
			return s, nil
		}
	}
	return nil, ErrFileNotFound
}

func (s *Stream) Read(b []byte) (int, error) {
	n, err := s.c.readStream(&s.r, b)
	if err != nil && err != io.EOF {
		err = &CimError{Cim: s.c.name, Path: s.fname, Stream: s.name, Op: "read", Err: err}
	}
	return n, err
}

type StreamInfo struct {
	Size int64
}

func (s *Stream) Stat() (*StreamInfo, error) {
	return &StreamInfo{
		Size: s.r.stream.Size(),
	}, nil
}

func (s *Stream) Name() string {
	return s.name
}
