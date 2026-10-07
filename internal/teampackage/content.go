package teampackage

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/cloudvane/litefactory/internal/teamsource"
)

// confinedFS pins the content directory across path renames and prevents
// symlink escapes even if an uncoordinated local writer changes a component.
type confinedFS struct{ root *os.Root }

func (f confinedFS) Open(name string) (fs.File, error) {
	if !safePath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	components := strings.Split(name, "/")
	for i := range components {
		component := strings.Join(components[:i+1], "/")
		info, err := f.root.Lstat(component)
		if err != nil {
			return nil, err
		}
		if i < len(components)-1 && !info.IsDir() {
			return nil, fmt.Errorf("unsafe content directory %s", component)
		}
		if i == len(components)-1 && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe content file %s", name)
		}
	}
	before, err := f.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	file, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	limit := teamsource.MaxSourceBytes
	if path.Base(name) == "team.config" {
		limit = MaxCatalogueBytes
	}
	if !info.Mode().IsRegular() || info.Size() > limit || !os.SameFile(before, info) {
		_ = file.Close()
		return nil, fmt.Errorf("unsafe or changed content file %s", name)
	}
	return file, nil
}
