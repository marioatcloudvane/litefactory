package turn

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// findPlanningContext inspects only the admitted branch's date directories and
// feature. It never chooses between multiple matching conversations by date.
func findPlanningContext(root *os.Root, in harness.PromptInput) (string, error) {
	branchDir, err := naming.AITeamBranchDir(in.Branch)
	if err != nil {
		return "", err
	}
	if err := checkStoryComponents(root, branchDir); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	dir, err := root.OpenRoot(branchDir)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return "", err
	}
	var found string
	for _, entry := range entries {
		if _, err := time.Parse(naming.SessionDateLayout, entry.Name()); err != nil {
			continue
		}
		candidate := filepath.Join(branchDir, entry.Name(), in.FeatureSlug, planbrief.ContextFileName)
		_, err := readPlanningContext(root, candidate, in)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if found != "" {
			return "", fmt.Errorf("multiple planning contexts match this task; retain one active task session")
		}
		found = filepath.ToSlash(candidate)
	}
	return found, nil
}

func readPlanningContext(root *os.Root, path string, in harness.PromptInput) (string, error) {
	data, err := readPlanArtifact(root, filepath.FromSlash(path), planbrief.MaxContextBytes)
	if err != nil {
		return "", err
	}
	working, err := planbrief.ParseWorkingContext(data)
	if err != nil {
		return "", err
	}
	if working.Parent != in.Ref.String() || working.Branch != in.Branch || working.Feature != in.FeatureSlug {
		return "", fmt.Errorf("planning context identity does not match admitted task, branch and feature")
	}
	return string(data), nil
}
