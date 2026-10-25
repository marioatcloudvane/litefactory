package turn

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
			return "", &planContextRefusal{path: filepath.ToSlash(branchDir), err: fmt.Errorf("multiple planning contexts match this task; retain one active task session")}
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
		return "", &planContextRefusal{path: filepath.ToSlash(path), err: err}
	}
	if err := checkPlanningIdentity(working, in.Ref.String(), in.Branch, in.FeatureSlug); err != nil {
		return "", &planContextRefusal{path: filepath.ToSlash(path), err: err}
	}
	return string(data), nil
}

// checkPlanningIdentity names every mismatched identity field, so the owner
// learns what to correct rather than only that something differs.
func checkPlanningIdentity(working planbrief.WorkingContext, parent, branch, feature string) error {
	var mismatches []string
	for _, field := range []struct{ name, got, want string }{
		{"parent", working.Parent, parent},
		{"branch", working.Branch, branch},
		{"feature", working.Feature, feature},
	} {
		if field.got != field.want {
			mismatches = append(mismatches, fmt.Sprintf("%s is %q, expected %q", field.name, clipIdentity(field.got), field.want))
		}
	}
	if len(mismatches) == 0 {
		return nil
	}
	return fmt.Errorf("planning context identity does not match admitted task, branch and feature: %s", strings.Join(mismatches, "; "))
}

// clipIdentity bounds agent-authored identity values before they reach logs
// and issue comments; the context itself may be up to MaxContextBytes.
func clipIdentity(value string) string {
	const limit = 80
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "") + "…"
}

// planContextRefusal identifies retained context that cannot be admitted:
// malformed, foreign to this task, or ambiguous. Preparation preserves it and
// continues without its bytes; current-attempt output remains strictly checked.
type planContextRefusal struct {
	path string
	err  error
}

func (e *planContextRefusal) Error() string { return fmt.Sprintf("%s: %v", e.path, e.err) }

func (e *planContextRefusal) Unwrap() error { return e.err }

// reason is the report-ready explanation for a refused turn.
func (e *planContextRefusal) reason() string {
	return fmt.Sprintf("Plan refused: the retained planning context %s cannot be used: %v. No agent launched. Correct or remove that file in this task's worktree, then comment again.", e.path, e.err)
}
