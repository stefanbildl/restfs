
# Go REST WebDAV Adapter

This package provides an adapter for seamlessly integrating a working webdav.Handler with the following interface:

```go
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
```

# Usage

Simply include your implementation of FileRESTAPI into s RESTFileSystem

```go
fs := &RESTFileSystem {
    API: <your implementation>
}
```

Then, you can pass this file system to the webdav.Handler:

```go
h := webdav.Handler{
    LockSystem: webdav.NewMemLS(),
    FileSystem: filesystem,
}
```

You have the flexibility to use any lock system implementations you desire.

Example
```go
// Your implementation of FileRESTAPI
type MyFileRESTAPI struct { }
// Implement methods of FileRESTAPI
...

// Usage example
api := MyFileRESTAPI{}

// Create RESTFileSystem with your implementation
fs := RESTFileSystem{API: api}

// Pass to webdav.Handler
handler := webdav.Handler{
    LockSystem: webdav.NewMemLS(),
    FileSystem: fs,
}
// Now you can use handler as your webdav handler
```
Feel free to explore and integrate different lock system implementations as per your requirements.

# Contributions
Contributions are welcome! If you have any suggestions, improvements, or fixes, feel free to open an issue or submit a pull request.
