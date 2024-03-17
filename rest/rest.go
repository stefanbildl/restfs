package rest

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	isNew    bool
	info     fs.FileInfo
	tempFile *os.File
	api      FileRESTAPI
	name     string
	flag     int
	perm     os.FileMode
}


// ReadDir reads the named directory
// and returns a list of directory entries sorted by filename.
func (restfilesystem *RESTFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	fileInfos, err := restfilesystem.API.GetChildren(context.Background(), name)
	if err != nil {
		return nil, err
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
	fileInfo, err := restfilesystem.API.Stat(ctx, name)

	exists := !errors.Is(err, fs.ErrNotExist)

	if err != nil && (exists || (flag&os.O_CREATE) == 0) {
		return nil, err
	}

	if fileInfo != nil && fileInfo.IsDir() {
		return &File{
			info:  fileInfo,
			isNew: false,
			api:   restfilesystem.API,
			name:  name,
			flag:  flag,
			perm:  perm,
		}, nil
	}

	// is a new file
	// create temp file to store the contents

	err = os.MkdirAll("tmp", 0777)
	if err != nil {
		return nil, err
	}
	tmpFile, err := os.CreateTemp("tmp", strings.ReplaceAll(name, "/", "_")+"*")
	if err != nil {
		return nil, err
	}
	defer tmpFile.Close()

	if exists {
		rc, err := restfilesystem.API.GetContent(ctx, name)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		io.Copy(tmpFile, rc)
		rc.Close()
	}

	tmpFile.Close()

	tmpFile, err = os.OpenFile(tmpFile.Name(), flag, perm)
	if err != nil {
		return nil, err
	}

	if fileInfo == nil {
		fileInfo = &NewFileInfo{
			name:     filepath.Base(name),
			tempFile: tmpFile,
			mode:     perm,
			isDir:    false,
		}
	}

	return &File{
		tempFile: tmpFile,
		isNew:    !exists,
		flag:     flag,
		perm:     perm,
		info:     fileInfo,
		name:     name,
		api:      restfilesystem.API,
	}, nil
}

type NewFileInfo struct {
	tempFile *os.File
	name     string
	mode     fs.FileMode
	isDir    bool
}

func (newfileinfo *NewFileInfo) Name() string {
	return newfileinfo.name
}

func (newfileinfo *NewFileInfo) Size() int64 {
	stat, err := newfileinfo.tempFile.Stat()
	if err != nil {
		return 0
	}

	return stat.Size()
}

func (newfileinfo *NewFileInfo) Mode() fs.FileMode {
	return newfileinfo.mode
}

func (newfileinfo *NewFileInfo) ModTime() time.Time {
	stat, err := newfileinfo.tempFile.Stat()
	if err != nil {
		return time.Time{}
	}
	return stat.ModTime()
}

func (newfileinfo *NewFileInfo) IsDir() bool {
	return newfileinfo.isDir
}

func (newfileinfo *NewFileInfo) Sys() any {
	return nil
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

// ReadDir reads the contents of the directory and returns
// a slice of up to n DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
//
// If n > 0, ReadDir returns at most n DirEntry structures.
// In this case, if ReadDir returns an empty slice, it will return
// a non-nil error explaining why.
// At the end of a directory, the error is io.EOF.
// (ReadDir must return io.EOF itself, not an error wrapping io.EOF.)
//
// If n <= 0, ReadDir returns all the DirEntry values from the directory
// in a single slice. In this case, if ReadDir succeeds (reads all the way
// to the end of the directory), it returns the slice and a nil error.
// If it encounters an error before the end of the directory,
// ReadDir returns the DirEntry list read until that point and a non-nil error.
func (file *File) ReadDir(n int) ([]fs.DirEntry, error) {
	fileInfos, err := file.Readdir(n)
	var dirEntries []fs.DirEntry

	if fileInfos != nil {
		for _, f := range fileInfos {
			dirEntries = append(dirEntries, fs.FileInfoToDirEntry(f))
		}
	}
	return dirEntries, err
}

func (file *File) Seek(offset int64, whence int) (int64, error) {
	return file.tempFile.Seek(offset, whence)
}

func (file *File) Read(p []byte) (n int, err error) {
	return file.tempFile.Read(p)
}

func (file *File) Write(p []byte) (n int, err error) {
	return file.tempFile.Write(p)
}

func (file *File) Close() error {
	if file.info != nil && file.info.IsDir() {
		return nil
	}

	err := file.tempFile.Close()
	if err != nil {
		return err
	}

	if file.flag&(os.O_RDWR|os.O_WRONLY) > 0 {
		readFile, err := os.OpenFile(file.tempFile.Name(), os.O_RDONLY, 0)
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

	err = os.RemoveAll(file.tempFile.Name())
	if err != nil {
		return err
	}

	return nil
}

func (file *File) Readdir(count int) ([]fs.FileInfo, error) {
	return file.api.GetChildren(context.Background(), file.name)
}

func (file *File) Stat() (fs.FileInfo, error) {
	return file.info, nil
}

// guards to ensure that everything works
var _ webdav.FileSystem = &RESTFileSystem{}
var _ webdav.File = &File{}
var _ fs.FS = &RESTFileSystem{}
var _ fs.ReadDirFS = &RESTFileSystem{}
var _ fs.ReadDirFile = &File{}
