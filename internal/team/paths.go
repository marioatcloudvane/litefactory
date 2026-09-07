package team

import (
	"fmt"
	"path/filepath"
	"strings"
)

// litefactoryDir is the repo-relative directory this package owns for its
// own bookkeeping: the provenance lockfile (team.lock.json), the per-repo
// agent source overrides (team.overrides.json) and the flock target
// (.team.lock) that serializes concurrent Add calls.
const litefactoryDir = ".litefactory"

// harnessOwnedDirPrefixes lists the repo-relative, forward-slash directory
// prefixes this package writes standalone per-agent artifacts under, one
// per known harness. Every standalonePath result falls under exactly one of
// these.
var harnessOwnedDirPrefixes = []string{
	".claude/agents/",
	".codex/skills/",
	".opencode/agents/",
}

// Root-file target literals a renderer's second RenderedFile.Path may name.
// internal/render defines the harness-specific constants these mirror
// (claudeRootFile, opencodeRootFile) as unexported to that package, so this
// package carries its own copies rather than importing them — see doc.go's
// Invariants on why internal/team cannot import internal/render at all.
const (
	RootFileClaude   = "CLAUDE.md"
	RootFileOpencode = "AGENTS.md"
)

// knownHarnesses is the set of harness keys this package knows an Owned
// standalone path shape for. codex is deliberately not extended with a
// root-file target — that asymmetry belongs to internal/render's codex
// renderer and is out of this package's scope to relax.
var knownHarnesses = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
}

// validateHarness checks that harness is one this package knows how to
// place a standalone file for.
func validateHarness(harness string) error {
	if !knownHarnesses[harness] {
		return &UnsupportedHarnessError{Harness: harness}
	}
	return nil
}

// isRootFileTarget reports whether p names a recognized root-file
// composition target rather than a standalone Owned Path.
func isRootFileTarget(p string) bool {
	return p == RootFileClaude || p == RootFileOpencode
}

// standalonePath returns the one Owned Path a harness's standalone
// RenderedFile must land at for the given agent key.
func standalonePath(harness, agentKey string) (string, error) {
	switch harness {
	case "claude":
		return fmt.Sprintf(".claude/agents/%s.md", agentKey), nil
	case "codex":
		return fmt.Sprintf(".codex/skills/%s/SKILL.md", agentKey), nil
	case "opencode":
		return fmt.Sprintf(".opencode/agents/%s.md", agentKey), nil
	default:
		return "", &UnsupportedHarnessError{Harness: harness}
	}
}

// validateOwnedPath checks that filePath is exactly the Owned Path this
// package will write agentKey's rendered bytes for harness to, or is a
// recognized root-file composition target. It never trusts a renderer's
// output path at face value: a renderer runs pure, in-process code that
// this package does not otherwise sandbox, so the path it named is
// re-derived from agentKey and checked for exact equality rather than used
// as-is. Since standalonePath's result is built from a KeyPattern-validated
// agentKey (letters, digits, interior hyphens only), a match here can never
// contain "..", a leading "/", or a backslash.
func validateOwnedPath(harness, agentKey, filePath string) error {
	if isRootFileTarget(filePath) {
		return nil
	}
	want, err := standalonePath(harness, agentKey)
	if err != nil {
		return err
	}
	if filePath != want {
		return &PathNotOwnedError{AgentKey: agentKey, Harness: harness, Path: filePath, Want: want}
	}
	return nil
}

// isOwnedWriteTarget reports whether relPath — a repo-relative path exactly
// as passed to writeFileAtomic — falls inside this package's Owned Paths, as
// corrected by US-16 (see AC_AMENDMENTS.md's SEC-7 entry): one of the three
// harness-facing standalone directories, the .litefactory bookkeeping
// directory (lockfile, overrides, per-repo flock), a recognized root-file
// composition target, or the repo-root .gitignore file this package manages
// one line of (T-45).
//
// This is the runtime write guard (T-46): every write this package performs
// funnels through writeFileAtomic, which calls this immediately before a
// write lands rather than relying only on validateOwnedPath's earlier,
// narrower check during AgentRender validation — validateOwnedPath cannot
// see a lockfile, overrides, or .gitignore write at all, since none of those
// originates from a renderer's output. A relPath this returns false for
// means a bug in this package's own write path, not a caller mistake: every
// relPath reaching writeFileAtomic is built by this package itself from its
// own constants and validated inputs.
func isOwnedWriteTarget(relPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if isRootFileTarget(clean) || clean == gitignoreName {
		return true
	}
	if clean == litefactoryDir || strings.HasPrefix(clean, litefactoryDir+"/") {
		return true
	}
	for _, prefix := range harnessOwnedDirPrefixes {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}
