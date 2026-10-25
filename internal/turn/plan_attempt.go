package turn

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// preparePlanAttempt invalidates old transport at every subprocess boundary.
// Context stays committed and reusable; transport never proves a future attempt.
func preparePlanAttempt(worktree, branch string, run store.Run, in *harness.PromptInput, log *slog.Logger) error {
	if in.PlanProposalPath == "" {
		return nil
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range []string{planbrief.OutcomeFileName, planbrief.ProposalFileName} {
		if _, err := readPlanArtifact(root, path, planbrief.MaxProposalBytes); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("prepare plan transport: %w", err)
		}
		if err := root.Remove(path); err != nil {
			return err
		}
	}
	// Optional context is evidence, never an admission or authority requirement.
	// Keep its validation strict, but do not launch with bytes that failed it.
	in.PlanWorkingContext = ""
	if in.PlanContextUnavailable == "" {
		if in.PlanContextPath == "" {
			in.PlanContextPath, err = findPlanningContext(root, *in)
		}
		if err == nil && in.PlanContextPath != "" {
			in.PlanWorkingContext, err = readPlanningContext(root, in.PlanContextPath, *in)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn("optional planning context unavailable", slog.Any("error", err))
			in.PlanContextUnavailable = "Optional planning context is unavailable; its source was preserved and excluded from this request."
			in.PlanWorkingContext = ""
		}
	}
	if in.PlanContextPath == "" || in.PlanContextUnavailable != "" {
		var dir string
		for _, marker := range []string{planbrief.FileName, "RESEARCH_BRIEF.md"} {
			if found, ok := findSessionDir(worktree, branch, in.Ref.Issue, run.StartedAt, marker, log); ok {
				dir = found
				break
			}
		}
		if dir == "" {
			dir, err = naming.AITeamSessionDir(branch, run.StartedAt, in.FeatureSlug)
			if err != nil {
				return err
			}
		}
		if in.PlanContextUnavailable != "" {
			// Never replace unusable source, even on a resumed subprocess attempt.
			dir = filepath.Join(dir, "plan-attempt-"+rand.Text())
		}
		in.PlanContextPath = filepath.ToSlash(filepath.Join(dir, planbrief.ContextFileName))
	}
	in.PlanOutcomePath = planbrief.OutcomeFileName
	in.PlanRunID = run.ID
	in.PlanAttempt = rand.Text()
	return nil
}

// readPlanArtifact pins the immediate directory and verifies both file bytes and
// path identity after reading. No symlink component, escape or replaced parent
// can redirect a handoff to a different artifact unnoticed.
func readPlanArtifact(root *os.Root, path string, limit int64) ([]byte, error) {
	artifact, err := openPlanArtifact(root, path, limit)
	if err != nil {
		return nil, err
	}
	defer artifact.dir.Close()
	if err := artifact.unchanged(root, limit); err != nil {
		return nil, err
	}
	return artifact.data, nil
}

// planArtifact follows planIndex's pinned-descriptor pattern. Opening and
// verification are separate so each boundary checks the originally observed
// directory, file identity and bytes, rather than merely accepting a fresh read.
type planArtifact struct {
	dir     *os.Root
	path    string
	dirInfo os.FileInfo
	info    os.FileInfo
	data    []byte
}

func openPlanArtifact(root *os.Root, path string, limit int64) (_ *planArtifact, err error) {
	if !filepath.IsLocal(path) {
		return nil, fmt.Errorf("plan artifact path escapes worktree")
	}
	if err := checkStoryComponents(root, path); err != nil {
		return nil, err
	}
	dir, err := root.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, dir.Close())
		}
	}()
	dirInfo, err := dir.Stat(".")
	if err != nil {
		return nil, err
	}
	info, err := dir.Lstat(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	data, err := readStoryFile(dir, filepath.Base(path), limit)
	if err != nil {
		return nil, err
	}
	return &planArtifact{dir: dir, path: path, dirInfo: dirInfo, info: info, data: data}, nil
}

func (p *planArtifact) unchanged(root *os.Root, limit int64) error {
	again, err := readStoryFile(p.dir, filepath.Base(p.path), limit)
	if err != nil {
		return err
	}
	after, err := p.dir.Lstat(filepath.Base(p.path))
	if err != nil {
		return err
	}
	if err := checkStoryComponents(root, p.path); err != nil {
		return err
	}
	currentDir, err := root.Lstat(filepath.Dir(p.path))
	if err != nil {
		return err
	}
	if !os.SameFile(p.dirInfo, currentDir) || !os.SameFile(p.info, after) || p.info.Size() != after.Size() || !p.info.ModTime().Equal(after.ModTime()) || !bytes.Equal(p.data, again) {
		return fmt.Errorf("unstable plan artifact")
	}
	return nil
}
