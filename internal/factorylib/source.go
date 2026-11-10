package factorylib

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync"

	"github.com/cloudvane/litefactory/internal/config"
)

// Source supplies the factory library a new piece of work captures from.
type Source interface {
	Library(context.Context) (*Library, error)
}

// Library makes a fixed library a Source.
func (l *Library) Library(context.Context) (*Library, error) { return l, nil }

// Installed reads the library installed in one factory home. Each call checks
// which digest is current, so a reinstall reaches new work without a restart;
// a verified digest is read from disk only once.
type Installed struct {
	paths  config.Paths
	mu     sync.Mutex
	cached *Library
}

// NewInstalled returns the installed-library source of a factory home.
func NewInstalled(paths config.Paths) *Installed { return &Installed{paths: paths} }

// Open returns the installed-library source after verifying the current
// installation, so a daemon refuses to start without a valid library.
func Open(ctx context.Context, paths config.Paths) (*Installed, error) {
	installed := NewInstalled(paths)
	if _, err := installed.Library(ctx); err != nil {
		return nil, err
	}
	return installed, nil
}

// Library returns the verified current installation.
func (i *Installed) Library(ctx context.Context) (*Library, error) {
	digest, err := currentDigest(i.paths)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cached != nil && i.cached.digest == digest {
		return i.cached, nil
	}
	lib, err := LoadDir(ctx, filepath.Join(i.paths.FactoryLibraryDir(), digest))
	if err != nil {
		return nil, err
	}
	i.cached = lib
	return lib, nil
}

// Or returns source, or the installed library of paths when source is nil.
func Or(source Source, paths config.Paths) Source {
	if source != nil {
		return source
	}
	return NewInstalled(paths)
}

// DirFS is a filesystem over one registered directory of the source's current
// library. Every open resolves the library again; an uninstalled library
// fails each open with ErrNotInstalled.
func DirFS(source Source, dir string) fs.FS { return dirFS{source: source, dir: dir} }

type dirFS struct {
	source Source
	dir    string
}

func (d dirFS) Open(name string) (fs.File, error) {
	lib, err := d.source.Library(context.TODO())
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	files, err := lib.FS(d.dir)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return files.Open(name)
}
