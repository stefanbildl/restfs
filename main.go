package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"

	"github.com/stefanbildl/restfs/rest"
	"golang.org/x/net/webdav"
)

func main() {

	filesystem := &rest.RESTFileSystem{
			API: &MockAPI{
				dir: "mock",
			},
		};

	fmt.Println("Contents:")
	fmt.Println("=========")

	fs.WalkDir(filesystem, "/", func(path string, d fs.DirEntry, err error) error {
		fmt.Println(path);
		return nil
	})


	h := webdav.Handler{
		LockSystem: webdav.NewMemLS(),
		FileSystem: filesystem,
		Prefix: "/dav/",
		Logger: func(r *http.Request, err error) {
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					fmt.Println(err.Error())
				}
			}
		},
	}

	httpfs := http.FileServerFS(filesystem)

	http.Handle("/dav/", &h)
	http.Handle("/", httpfs)

	http.ListenAndServe(":5555", nil)
}

type File interface {
	webdav.File
}

type MockAPI struct {
	dir webdav.Dir
}

// NewFile implements rest.FileRESTAPI.
func (mockapi *MockAPI) NewFile(ctx context.Context, name string, rc io.Reader) error {

	buf, err := io.ReadAll(rc)

	fmt.Printf("Writing new FILE (%s):\n", name)
	fmt.Println(string(buf))

	f, err := mockapi.dir.OpenFile(ctx, name, os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, bytes.NewReader(buf))

	fmt.Printf("\nDONE\n")
	return err
}

func (mockapi *MockAPI) GetContent(ctx context.Context, name string) (io.ReadCloser, error) {
	return mockapi.dir.OpenFile(ctx, name, os.O_RDONLY, 0)
}

func (mockapi *MockAPI) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	return mockapi.dir.Stat(ctx, name)
}
func (mockapi *MockAPI) GetChildren(ctx context.Context, name string) ([]fs.FileInfo, error) {
	fmt.Printf("Get children (%s)\n", name)
	dir, err := mockapi.dir.OpenFile(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return dir.Readdir(0)
}
func (mockapi *MockAPI) MkDir(ctx context.Context, name string, perm os.FileMode) error {
	fmt.Printf("mkdir (%s)\n", name)
	return mockapi.dir.Mkdir(ctx, name, perm)
}
func (mockapi *MockAPI) Update(ctx context.Context, name string, rc io.Reader) error {
	fmt.Printf("update (%s)\n", name)
	buf, err := io.ReadAll(rc)
	fmt.Println(string(buf))
	f, err := mockapi.dir.OpenFile(ctx, name, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	fmt.Printf("\nDONE\n")
	return nil
}
func (mockapi *MockAPI) RemoveAll(ctx context.Context, name string) error {
	fmt.Printf("remove (%s)\n", name)
	return mockapi.dir.RemoveAll(ctx, name)
}

func (mockapi *MockAPI) Rename(ctx context.Context, oldname string, newname string) error {
	fmt.Printf("rename (%s) -> (%s)\n", oldname, newname)
	return mockapi.dir.Rename(ctx, oldname, newname)
}

var _ rest.FileRESTAPI = &MockAPI{}
