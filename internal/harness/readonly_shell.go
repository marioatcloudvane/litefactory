package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The read-only shell's write boundary.
//
// A run holding the read-only shell (a Validate reviewer, a Spike
// investigator) may run the project's checks: go test, go vet, pytest and the
// like. Those commands are arbitrary code from the branch under review, so the
// command allowlist alone cannot keep the worktree read-only — a test may
// write any file it likes. The run is therefore started with Claude Code's OS
// sandbox enabled for Bash: every command runs with the worktree and $LF_HOME
// unwritable, the credential stores unreadable and the network closed (a
// loopback bind stays allowed for tests that stand a local server up). Writes
// are allowed only to a private per-run temporary directory and a shared
// compiler cache, both outside the worktree, and the child environment points
// TMPDIR and GOCACHE there.
//
// Measured on 2026-10-21 with claude 2.1.306 on macOS: under this stanza with
// --permission-mode dontAsk, `go test` ran and passed with GOCACHE and
// t.TempDir() in the private directory, a test writing into the working
// directory got "operation not permitted", `touch` in the worktree was
// refused, and a command outside --allowedTools was still denied.

// readOnlyShellSandbox is what a read-only shell run needs on top of an
// ordinary run: the settings document to pass as --settings, the environment
// overrides for the child, and a cleanup for the private directory.
type readOnlyShellSandbox struct {
	settingsPath string
	env          []string
	cleanup      func() error
}

// prepareReadOnlyShell writes the run's sandboxed settings document next to
// its log and creates the private temporary directory. The document is the
// operator's settings file with a sandbox stanza added, so its deny rules
// still apply. It lives in the run artefact directory, which the sandbox does
// not let the agent write, so a run cannot loosen its own sandbox.
func (r *Runner) prepareReadOnlyShell(worktree, logPath string) (readOnlyShellSandbox, error) {
	cacheRoot := r.paths.ReviewCacheDir()
	goCache := filepath.Join(cacheRoot, "go-build")
	tmpRoot := filepath.Join(cacheRoot, "tmp")
	for _, dir := range []string{goCache, tmpRoot} {
		if err := os.MkdirAll(dir, runArtefactDirMode); err != nil {
			return readOnlyShellSandbox{}, fmt.Errorf("prepare read-only shell cache: %w", err)
		}
	}
	scratch, err := os.MkdirTemp(tmpRoot, "run-")
	if err != nil {
		return readOnlyShellSandbox{}, fmt.Errorf("prepare read-only shell scratch: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(scratch) }

	// #nosec G304 -- settingsPath is validated operator-owned configuration.
	raw, err := os.ReadFile(r.settingsPath)
	if err != nil {
		return readOnlyShellSandbox{}, errors.Join(fmt.Errorf("read claude settings: %w", err), cleanup())
	}
	document := map[string]any{}
	if err := json.Unmarshal(raw, &document); err != nil {
		return readOnlyShellSandbox{}, errors.Join(fmt.Errorf("decode claude settings %s: %w", r.settingsPath, err), cleanup())
	}
	document["sandbox"] = readOnlyShellSandboxSettings(worktree, r.home, scratch, goCache, r.deniedReadPaths())
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return readOnlyShellSandbox{}, errors.Join(err, cleanup())
	}
	settingsPath := logPath + ".settings.json"
	if err := writeFileAtomic(settingsPath, append(encoded, '\n'), settingsFileMode); err != nil {
		return readOnlyShellSandbox{}, errors.Join(err, cleanup())
	}
	return readOnlyShellSandbox{
		settingsPath: settingsPath,
		env: []string{
			"TMPDIR=" + scratch,
			"GOTMPDIR=" + scratch,
			"GOCACHE=" + goCache,
			"CLAUDE_CODE_TMPDIR=" + scratch,
			// git status and diff may refresh the index; in a worktree the
			// sandbox refuses that write, and the refresh is optional.
			"GIT_OPTIONAL_LOCKS=0",
		},
		cleanup: cleanup,
	}, nil
}

// readOnlyShellSandboxSettings is the sandbox stanza of a read-only shell run.
// Bash commands still need an --allowedTools rule: autoAllowBashIfSandboxed is
// off, so the command allowlist and the write boundary both hold.
func readOnlyShellSandboxSettings(worktree, home, scratch, goCache string, denyRead []string) map[string]any {
	return map[string]any{
		"enabled":                  true,
		"failIfUnavailable":        true,
		"allowUnsandboxedCommands": false,
		"autoAllowBashIfSandboxed": false,
		"excludedCommands":         []string{},
		"filesystem": map[string]any{
			"allowWrite": []string{scratch, goCache},
			"denyWrite":  []string{worktree, home},
			"denyRead":   denyRead,
		},
		"network": map[string]any{
			"allowedDomains":      []string{},
			"deniedDomains":       []string{"*"},
			"allowAllUnixSockets": false,
			"allowUnixSockets":    []string{},
			"allowLocalBinding":   true,
		},
	}
}

// deniedReadPaths are the credential-bearing locations a read-only shell
// command may not read: the same set the doctor's diagnosis shell hides, plus
// litefactory's unredacted logs and database.
func (r *Runner) deniedReadPaths() []string {
	denied := []string{r.paths.LogsDir(), r.paths.DBPath()}
	for _, path := range r.paths.DoctorCredentialPaths(r.osHome) {
		if filepath.IsAbs(path) {
			denied = append(denied, path)
		}
	}
	return denied
}

// overrideEnv returns env with each NAME=value in overrides replacing any
// existing NAME entry.
func overrideEnv(env, overrides []string) []string {
	out := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		replaced := false
		for _, override := range overrides {
			if strings.HasPrefix(override, name+"=") {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, entry)
		}
	}
	return append(out, overrides...)
}

// replaceFlagValue returns args with the value following flag replaced.
func replaceFlagValue(args []string, flag, value string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == flag {
			out[i+1] = value
			return out
		}
	}
	return out
}
