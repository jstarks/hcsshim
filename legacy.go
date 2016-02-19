package hcsshim

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Microsoft/go-winio"
)

var errorIterationCanceled = errors.New("")

func openFileOrDir(path string, mode uint32, createDisposition uint32) (file *os.File, err error) {
	winPath, err := syscall.UTF16FromString(path)
	if err != nil {
		return
	}
	h, err := syscall.CreateFile(&winPath[0], mode, syscall.FILE_SHARE_READ, nil, createDisposition, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return
	}
	file = os.NewFile(uintptr(h), path)
	return
}

type fileEntry struct {
	path string
	fi   os.FileInfo
	err  error
}

type LegacyLayerReader struct {
	root         string
	result       chan *fileEntry
	proceed      chan bool
	currentFile  *os.File
	backupReader *winio.BackupFileReader
}

func NewLegacyLayerReader(root string) *LegacyLayerReader {
	r := &LegacyLayerReader{
		root:    root,
		result:  make(chan *fileEntry),
		proceed: make(chan bool),
	}
	go r.walk()
	return r
}

func readTombstones(path string) (map[string]([]string), error) {
	tf, err := os.Open(filepath.Join(path, "tombstones.txt"))
	if err != nil {
		return nil, err
	}
	defer tf.Close()
	s := bufio.NewScanner(tf)
	if !s.Scan() || s.Text() != "Version 1.0" {
		return nil, errors.New("invalid tombstones file")
	}

	ts := make(map[string]([]string))
	for s.Scan() {
		t := s.Text()[1:] // skip leading `\`
		dir := filepath.Dir(t)
		ts[dir] = append(ts[dir], t)
	}
	if err = s.Err(); err != nil {
		return nil, err
	}

	return ts, nil
}

func (r *LegacyLayerReader) walk() {
	defer close(r.result)
	if !<-r.proceed {
		return
	}

	ts, err := readTombstones(r.root)
	if err != nil {
		goto ErrorLoop
	}

	err = filepath.Walk(r.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == r.root || path == filepath.Join(r.root, "tombstones.txt") {
			return nil
		}
		r.result <- &fileEntry{path, info, nil}
		if !<-r.proceed {
			return errorIterationCanceled
		}

		// List all the tombstones.
		if info.IsDir() {
			relPath, err := filepath.Rel(r.root, path)
			if err != nil {
				return err
			}
			if dts, ok := ts[relPath]; ok {
				for _, t := range dts {
					r.result <- &fileEntry{t, nil, nil}
					if !<-r.proceed {
						return errorIterationCanceled
					}
				}
			}
		}
		return nil
	})
	if err == errorIterationCanceled {
		return
	}
	if err == nil {
		err = io.EOF
	}

ErrorLoop:
	for {
		r.result <- &fileEntry{err: err}
		if !<-r.proceed {
			break
		}
	}
}

func (r *LegacyLayerReader) reset() {
	if r.backupReader != nil {
		r.backupReader.Close()
		r.backupReader = nil
		r.currentFile.Close()
		r.currentFile = nil
	}
}

func (r *LegacyLayerReader) Next() (path string, size int64, fileInfo *winio.FileBasicInfo, err error) {
	r.reset()
	r.proceed <- true
	fe := <-r.result
	if fe == nil {
		err = errors.New("LegacyLayerReader closed")
		return
	}
	if fe.err != nil {
		err = fe.err
		return
	}

	path, err = filepath.Rel(r.root, fe.path)
	if err != nil {
		return
	}

	if fe.fi == nil {
		// This is a tombstone. Return a nil fileInfo.
		return
	}

	size = fe.fi.Size()
	f, err := openFileOrDir(fe.path, syscall.GENERIC_READ, 0)
	if err != nil {
		return
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	fileInfo, err = winio.GetFileBasicInfo(f)
	if err != nil {
		return
	}

	r.currentFile = f
	r.backupReader = winio.NewBackupFileReader(f, false)
	f = nil
	return
}

func (r *LegacyLayerReader) Read(b []byte) (int, error) {
	if r.backupReader == nil {
		return 0, io.EOF
	}
	return r.backupReader.Read(b)
}

func (r *LegacyLayerReader) Close() error {
	r.proceed <- false
	<-r.result
	r.reset()
	return nil
}

type LegacyLayerWriter struct {
	root         string
	currentFile  *os.File
	backupWriter *winio.BackupFileWriter
	tombstones   []string
}

func NewLegacyLayerWriter(root string) *LegacyLayerWriter {
	return &LegacyLayerWriter{
		root: root,
	}
}

func (w *LegacyLayerWriter) reset() {
	if w.backupWriter != nil {
		w.backupWriter.Close()
		w.backupWriter = nil
		w.currentFile.Close()
		w.currentFile = nil
	}
}

func (w *LegacyLayerWriter) Add(name string, fileInfo *winio.FileBasicInfo) error {
	w.reset()
	path := filepath.Join(w.root, name)

	createDisposition := uint32(syscall.CREATE_NEW)
	if (fileInfo.FileAttributes & syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		err := os.Mkdir(path, 0)
		if err != nil {
			return err
		}
		createDisposition = syscall.OPEN_EXISTING
	}

	f, err := openFileOrDir(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, createDisposition)
	if err != nil {
		return err
	}
	defer func() {
		if f != nil {
			f.Close()
			os.Remove(path)
		}
	}()

	err = winio.SetFileBasicInfo(f, fileInfo)
	if err != nil {
		return err
	}

	w.currentFile = f
	w.backupWriter = winio.NewBackupFileWriter(f, false)
	f = nil
	return nil
}

func (w *LegacyLayerWriter) Remove(name string) error {
	w.tombstones = append(w.tombstones, name)
	return nil
}

func (w *LegacyLayerWriter) Write(b []byte) (int, error) {
	if w.backupWriter == nil {
		return 0, errors.New("closed")
	}
	return w.backupWriter.Write(b)
}

func (w *LegacyLayerWriter) Close() error {
	w.reset()
	tf, err := os.Create(filepath.Join(w.root, "tombstones.txt"))
	if err != nil {
		return err
	}
	defer tf.Close()
	_, err = tf.Write([]byte("Version 1.0\n"))
	if err != nil {
		return err
	}
	for _, t := range w.tombstones {
		_, err = tf.Write([]byte(filepath.Join(`\`, t) + "\n"))
		if err != nil {
			return err
		}
	}
	return nil
}
