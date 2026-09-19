package turn

import (
	"fmt"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// ValidateResearchArtifacts checks references under a trusted worktree and the
// exact admitted session. Root-relative lstat rejects symlinks at every component;
// os.Root confines resolution even if an ancestor changes during inspection.
func ValidateResearchArtifacts(worktree, session string, artifacts []agentresult.Artifact) error {
	if !agentresult.ValidPath(session) {
		return fmt.Errorf("research artifacts: invalid session path")
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return fmt.Errorf("research artifacts root: %w", err)
	}
	defer root.Close()
	for _, a := range artifacts {
		if !agentresult.ValidPath(a.Path) || !strings.HasPrefix(a.Path, session+"/") {
			return fmt.Errorf("research artifact must belong to the admitted session")
		}
		parts := strings.Split(a.Path, "/")
		for i := range parts {
			info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
			if err != nil {
				return fmt.Errorf("inspect research artifact: %w", err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("research artifact has symlink ancestry")
			}
			if i == len(parts)-1 {
				if !info.Mode().IsRegular() {
					return fmt.Errorf("research artifact is not a regular file")
				}
			} else if !info.IsDir() {
				return fmt.Errorf("research artifact ancestor is not a directory")
			}
		}
	}
	return nil
}
