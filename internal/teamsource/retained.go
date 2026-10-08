package teamsource

import (
	"bytes"
	"io/fs"
	"path"
	"time"
)

// WorkflowOrigin is captured selection metadata, separate from the graph identity.
type WorkflowOrigin struct {
	Kind, Source, Slug, Version, ReleaseDigest string
	GraphPath, GraphFile, Team                 string
	TeamConfig                                 []byte
	ConfigDigest                               string
}

// RetainedFiles is an immutable owned in-memory file tree used for reconstruction.
// Callers must copy the bytes before crossing an independently mutable boundary.
type RetainedFiles map[string][]byte

// Open returns a read-only file backed by retained bytes.
func (f RetainedFiles) Open(name string) (fs.File, error) {
	if !safeResearchPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	raw, ok := f[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &retainedFile{Reader: bytes.NewReader(raw), name: name, size: int64(len(raw))}, nil
}

type retainedFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *retainedFile) Stat() (fs.FileInfo, error) {
	return retainedInfo{name: f.name, size: f.size}, nil
}
func (*retainedFile) Close() error { return nil }

type retainedInfo struct {
	name string
	size int64
}

func (i retainedInfo) Name() string     { return path.Base(i.name) }
func (i retainedInfo) Size() int64      { return i.size }
func (retainedInfo) Mode() fs.FileMode  { return 0o400 }
func (retainedInfo) ModTime() time.Time { return time.Time{} }
func (retainedInfo) IsDir() bool        { return false }
func (retainedInfo) Sys() any           { return nil }
