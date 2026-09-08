package team

import (
	"fmt"
	"os"
	"path/filepath"
)

// readRepoFile retains the package's symlink refusal while keeping the read
// inside the supplied repository even if a checked ancestor is replaced.
func readRepoFile(root, name string) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("repository read path is not local: %q", name)
	}
	if err := refuseSymlink(root, name); err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadFile(name)
}
