package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

// A simple webdav.FileSystem implementation that simplifies working with REST interfaces
type RESTFileSystem struct {
	API FileRESTAPI
}

type FileRESTAPI interface {
	GetContent(ctx context.Context, name string) (io.ReadCloser, error)
	Stat(ctx context.Context, name string) (fs.FileInfo, error)
	GetChildren(ctx context.Context, name string) ([]fs.FileInfo, error)
	MkDir(ctx context.Context, name string, perm os.FileMode) error
	Update(ctx context.Context, name string, rc io.Reader) error
	NewFile(ctx context.Context, name string, rc io.Reader) error
	RemoveAll(ctx context.Context, name string) error
	Rename(ctx context.Context, oldname string, newname string) error
}

type File struct {
	mu    sync.Mutex
	isNew bool
	tf    *os.File
	pos   int64
	api   FileRESTAPI
	name  string
	flag  int
	perm  os.FileMode
}

// ReadDir reads the named directory
// and returns a list of directory entries sorted by filename.
func (restfilesystem *RESTFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	fileInfos, err := restfilesystem.API.GetChildren(context.Background(), name)
	if err != nil {
		return nil, fmt.Errorf("cannot readdir: %w", err)
	}
	var dirEntries []fs.DirEntry
	for _, f := range fileInfos {
		dirEntries = append(dirEntries, fs.FileInfoToDirEntry(f))
	}

	return dirEntries, nil
}

// Open implements fs.FS.
func (restfilesystem *RESTFileSystem) Open(name string) (fs.File, error) {
	return restfilesystem.OpenFile(context.Background(), name, os.O_RDONLY, 0)
}

// Create a new directory
func (restfilesystem *RESTFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return restfilesystem.API.MkDir(ctx, name, perm)
}

func (restfilesystem *RESTFileSystem) OpenFile(
	ctx context.Context,
	name string,
	flag int,
	perm os.FileMode,
) (webdav.File, error) {

	_, err := restfilesystem.API.Stat(ctx, name)
	exists := !errors.Is(err, fs.ErrNotExist)

	// some error occurs or it doesn't exist, yet and Create flag was not set
	if err != nil && (exists || (flag&os.O_CREATE) == 0) {
		return nil, fmt.Errorf("error while opening file: %w", err)
	}

	return &File{
		isNew: !exists,
		flag:  flag,
		perm:  perm,
		name:  name,
		api:   restfilesystem.API,
	}, nil
}

func (restfilesystem *RESTFileSystem) RemoveAll(ctx context.Context, name string) error {
	return restfilesystem.API.RemoveAll(ctx, name)
}

func (restfilesystem *RESTFileSystem) Rename(ctx context.Context, oldName string, newName string) error {
	return restfilesystem.API.Rename(ctx, oldName, newName)
}

func (restfilesystem *RESTFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return restfilesystem.API.Stat(ctx, name)
}

func (f *File) ReadDir(n int) ([]fs.DirEntry, error) {
	fileInfos, err := f.Readdir(n)
	var dirEntries []fs.DirEntry

	if fileInfos != nil {
		for _, f := range fileInfos {
			dirEntries = append(dirEntries, fs.FileInfoToDirEntry(f))
		}
	}
	return dirEntries, err
}

func (f *File) stat() fs.FileInfo {
	s, err := f.api.Stat(context.Background(), f.name)

	if err != nil {
		fmt.Fprintf(os.Stderr, "error in stat: %v\n", err)
		return nil
	}

	return s
}

func (f *File) Size() int64 {
	fi := f.stat()
	if fi == nil {
		return 0
	}

	return fi.Size()
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.tf != nil {
		ret, err := f.tf.Seek(offset, whence)
		f.pos = ret
		return ret, err
	}

	npos := f.pos

	switch whence {
	case io.SeekStart:
		npos = offset
	case io.SeekCurrent:
		npos += offset
	case io.SeekEnd:
		npos = f.Size() + offset
	default:
		npos = -1
	}
	if npos < 0 {
		return 0, os.ErrInvalid
	}
	f.pos = npos
	return int64(f.pos), nil
}

func (f *File) tempFile() (*os.File, error) {
	if f.tf != nil {
		return f.tf, nil
	}

	err := os.MkdirAll("tmp", 0o777)
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp dir: %w", err)
	}

	tf, err := os.CreateTemp("tmp", strings.ReplaceAll(f.name, "/", "_")+"*")
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp file: %w", err)
	}

	var size int64 = 0
	if (f.flag&os.O_TRUNC) == 0 && !f.isNew {
		rc, err := f.api.GetContent(context.Background(), f.name)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		if size, err = io.Copy(tf, rc); err != nil {
			return nil, err
		}
		rc.Close()
	}

	tf.Close()

	f.tf, err = os.OpenFile(tf.Name(), f.flag, f.perm)
	if f.flag&os.O_APPEND > 0 {
		f.pos = size
	}
	return f.tf, err
}

func (f *File) Read(p []byte) (n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	tf, err := f.tempFile()
	if err != nil {
		return -1, err
	}

	n, err = tf.Read(p)
	if err != nil {
		return
	}

	f.pos += int64(n)
	return
}

func (f *File) Write(p []byte) (n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	tf, err := f.tempFile()

	if err != nil {
		return -1, err
	}

	n, err = tf.Write(p)
	if err != nil {
		return
	}

	f.pos += int64(n)
	return
}

func (file *File) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()

	if file.tf != nil {
		defer os.RemoveAll(file.tf.Name())
		defer file.tf.Close()
	}

	if file.isNew {
		var rc io.ReadCloser = io.NopCloser(bytes.NewReader([]byte{}))
		var err error = nil

		if file.tf != nil {
			rc, err = os.Open(file.tf.Name())
			if err != nil {
				return fmt.Errorf("cannot open tempfile: %w", err)
			}
		}

		defer rc.Close()
		err = file.api.NewFile(context.Background(), file.name, rc)
		if err != nil {
			return err
		}

		err = rc.Close()
		if err != nil {
			return err
		}

		if err = file.tf.Close(); err != nil {
			return fmt.Errorf("cannot close: %w", err)
		}

		return nil
	}

	if file.flag&(os.O_RDWR|os.O_WRONLY) > 0 {
		readFile, err := os.OpenFile(file.tf.Name(), os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer readFile.Close()

		if file.isNew {
			file.api.NewFile(context.Background(), file.name, readFile)
		} else {
			file.api.Update(context.Background(), file.name, readFile)
		}

		readFile.Close()
	}

	return nil
}

func (file *File) Readdir(count int) ([]fs.FileInfo, error) {
	return file.api.GetChildren(context.Background(), file.name)
}

func (f *File) Stat() (fs.FileInfo, error) {
	if f.isNew && f.flag&os.O_CREATE > 0 {
		return &newFileInfo{
			t:    time.Now(),
			name: f.name,
			mode: f.perm,
		}, nil
	}

	return f.api.Stat(context.Background(), f.name)
}

type newFileInfo struct {
	name string
	mode fs.FileMode
	t    time.Time
}

func (newfileinfo *newFileInfo) Name() string {
	return newfileinfo.name
}
func (newfileinfo *newFileInfo) Size() int64 {
	return 0
}
func (newfileinfo *newFileInfo) Mode() fs.FileMode {
	return newfileinfo.mode
}

func (newfileinfo *newFileInfo) ModTime() time.Time {
	return newfileinfo.t
}
func (newfileinfo *newFileInfo) IsDir() bool {
	return false
}
func (newfileinfo *newFileInfo) Sys() any {
	return nil
}

// guards to ensure that everything works
var _ webdav.FileSystem = &RESTFileSystem{}
var _ webdav.File = &File{}
var _ fs.FS = &RESTFileSystem{}
var _ fs.ReadDirFS = &RESTFileSystem{}
var _ fs.ReadDirFile = &File{}
