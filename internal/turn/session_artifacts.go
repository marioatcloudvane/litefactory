package turn

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// sessionArtifactMarkers are the files tried, in this exact order, to locate
// the running task's aiteam session directory: the first one whose file is
// present wins, regardless of which brief the admitted mode goes on to want.
// naming.ManifestName is tried first because every mode writes it before
// either brief exists — a spike, or a run whose mode has not reached Research
// or Plan yet, still has one, and trying a brief first would report such a
// session as not found.
var sessionArtifactMarkers = []string{naming.ManifestName, planbrief.FileName, naming.ResearchBriefName}

// existingSessionArtifacts locates this task's aiteam session directory and
// names the brief relevant to the admitted mode, for the "existing session
// artifact" pointer [harness.BuildPrompt] writes into a cold run's prompt
// (AC-009).
//
// dir is "" when none of sessionArtifactMarkers resolves a directory (see
// [findSessionDir]) — most commonly a task's first-ever run, which has no
// .aiteam/ entry at all. brief is "" whenever mode has no brief of its own
// (agent.ModeSpike), has not resolved to one yet ([harness.InferMode]), or the
// file that mode wants is not actually present in dir — a directory that
// exists is not license to name a file it does not hold (SEC-21).
func existingSessionArtifacts(worktreeDir, branch string, issue int, day time.Time, mode string, log *slog.Logger) (dir, brief string) {
	for _, marker := range sessionArtifactMarkers {
		if found, ok := findSessionDir(worktreeDir, branch, issue, day, marker, log); ok {
			dir = found
			break
		}
	}
	if dir == "" {
		return "", ""
	}

	for _, candidate := range modeBriefCandidates(mode) {
		if sessionFileExists(worktreeDir, dir, candidate) {
			return dir, candidate
		}
	}
	return dir, ""
}

// modeBriefCandidates returns the brief names worth checking for mode, in the
// order they should be tried — the first one actually present in the session
// directory wins. It is empty for a mode with no brief of its own
// (agent.ModeSpike) or one this package has no basis to guess yet
// ([harness.InferMode], which resolves after this prompt is already built).
func modeBriefCandidates(mode string) []string {
	switch mode {
	case string(agent.ModeResearch):
		return []string{naming.ResearchBriefName}
	case string(agent.ModePlan):
		// Plan continues from Research's brief when one exists, and falls
		// back to its own once Research is done and Plan has started
		// writing plan_brief.md.
		return []string{naming.ResearchBriefName, planbrief.FileName}
	case string(agent.ModeImplement), string(agent.ModeValidate):
		return []string{planbrief.FileName}
	default:
		// agent.ModeSpike and harness.InferMode both get a directory only.
		return nil
	}
}

// sessionFileExists reports whether name is present as a regular file, with
// no symlink component anywhere in its path, at dir/name inside worktreeDir.
//
// It is the same rooted, symlink-refusing check [sessionDirOn] already
// applies to its own marker (SEC-21), reused here because the brief
// [existingSessionArtifacts] wants to name is not always the file that
// located dir: the directory can be found via naming.ManifestName while the
// mode-relevant brief is a different file that needs its own presence check.
func sessionFileExists(worktreeDir, dir, name string) bool {
	root, err := os.OpenRoot(worktreeDir)
	if err != nil {
		return false
	}
	defer root.Close()
	path := filepath.Join(filepath.FromSlash(dir), name)
	if err := checkStoryComponents(root, path); err != nil {
		return false
	}
	info, err := root.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// existingSessionTechnical supplements the existing brief pointer only for Plan.
func existingSessionTechnical(worktreeDir, dir, mode string) string {
	if mode == string(agent.ModePlan) && dir != "" && sessionFileExists(worktreeDir, dir, naming.ResearchTechnicalName) {
		return naming.ResearchTechnicalName
	}
	return ""
}
