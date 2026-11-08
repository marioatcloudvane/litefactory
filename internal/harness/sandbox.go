package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The run sandbox.
//
// Every run that holds Bash runs its shell commands inside Claude Code's OS
// sandbox, whatever its mode. The tools are not narrowed; the sandbox is the
// boundary:
//
//   - Reading is refused for $LF_HOME's state and every credential store
//     ([Runner.deniedReadPaths]), and so is writing to them.
//   - A read-only run (Validate reviewer, Bug analyst, Spike investigator)
//     cannot write its worktree or $LF_HOME, and its network is closed (a
//     loopback bind stays allowed for tests that stand a local server up).
//   - A writable run may write its worktree, its git directory, its
//     submission directory and the shared build and package caches, and may
//     reach the package and forge hosts in [sandboxAllowedDomains]. The
//     sandbox accepts no "allow every host" entry, so the list is explicit.
//
// Both write caches and temporary files to a private per-run directory
// outside the worktree, and the child environment points TMPDIR and GOCACHE
// there. Only Bash is sandboxed: Read, Edit and the other file tools are bound
// by the settings document's deny rules, and WebFetch/WebSearch by Claude.
// The daemon's own git commit and push, and the checks it runs, are not
// agent Bash commands and are not affected.
//
// Measured with claude on macOS: under the read-only stanza `go test` passed
// with GOCACHE and t.TempDir() in the private directory and a write into the
// worktree got "operation not permitted" (2.1.306, 2026-10-21). Under the
// writable stanza (2.1.319, 2026-11-07) ~/.ssh, ~/.config/gh and
// $LF_HOME/config.yaml were unreadable, a denied directory was unwritable even
// below an allowed one, curl reached api.github.com, registry.npmjs.org and
// pypi.org and was refused example.org, git ls-remote reached github.com, and
// `go list -m -versions` reached proxy.golang.org only with
// enableWeakerNetworkIsolation (Go verifies TLS through trustd). gh refuses to
// run without a readable credential, so it is unavailable inside the sandbox.

// sandboxAllowedDomains are the hosts a writable run's shell may reach: the
// forge and the package registries of the shipped stacks (Go, Node,
// Python, .NET) and Playwright's browser downloads.
func sandboxAllowedDomains() []string {
	return []string{
		"github.com", "*.github.com", "*.githubusercontent.com",
		"proxy.golang.org", "sum.golang.org", "storage.googleapis.com", "golang.org", "*.golang.org", "go.dev", "*.go.dev",
		"registry.npmjs.org", "*.npmjs.org", "registry.yarnpkg.com",
		"pypi.org", "files.pythonhosted.org",
		"api.nuget.org", "*.nuget.org",
		"cdn.playwright.dev", "playwright.download.prss.microsoft.com",
	}
}

// runSandbox is what a sandboxed run needs on top of an ordinary run: the
// settings document to pass as --settings, the environment overrides for the
// child, and a cleanup for the private directory.
type runSandbox struct {
	settingsPath string
	env          []string
	cleanup      func() error
}

// prepareSandbox writes the run's sandboxed settings document next to its log
// and creates the private temporary directory. The document is the operator's
// settings file with the sandbox stanza added, so its deny rules still apply.
// It lives in the run artefact directory, which the sandbox does not let the
// agent write, so a run cannot loosen its own sandbox.
func (r *Runner) prepareSandbox(req agent.Request, logPath string) (runSandbox, error) {
	readOnly := toolSurface(req).ReadOnly
	worktree := req.WorktreePath
	cacheRoot := r.paths.ReviewCacheDir()
	goCache := ""
	switch {
	case r.goBuildCaches.Main != "":
		if err := r.goBuildCaches.Revalidate(worktree); err != nil {
			return runSandbox{}, err
		}
		goCache = r.goBuildCaches.Main
		if readOnly {
			goCache = r.goBuildCaches.Reviewer
		}
	case readOnly:
		goCache = filepath.Join(cacheRoot, "go-build")
	}
	tmpRoot := filepath.Join(cacheRoot, "tmp")
	for _, dir := range []string{goCache, tmpRoot} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, runArtefactDirMode); err != nil {
			return runSandbox{}, fmt.Errorf("prepare sandbox cache: %w", err)
		}
	}
	scratch, err := os.MkdirTemp(tmpRoot, "run-")
	if err != nil {
		return runSandbox{}, fmt.Errorf("prepare sandbox scratch: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(scratch) }

	// #nosec G304 -- settingsPath is validated operator-owned configuration.
	raw, err := os.ReadFile(r.settingsPath)
	if err != nil {
		return runSandbox{}, errors.Join(fmt.Errorf("read claude settings: %w", err), cleanup())
	}
	document := map[string]any{}
	if err := json.Unmarshal(raw, &document); err != nil {
		return runSandbox{}, errors.Join(fmt.Errorf("decode claude settings %s: %w", r.settingsPath, err), cleanup())
	}
	if readOnly {
		document["sandbox"] = readOnlyShellSandboxSettings(worktree, r.home, scratch, goCache, r.deniedReadPaths())
	} else {
		document["sandbox"] = r.writableSandboxSettings(worktree, scratch, goCache, req.SubmissionRoot)
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return runSandbox{}, errors.Join(err, cleanup())
	}
	settingsPath := logPath + ".settings.json"
	if err := writeFileAtomic(settingsPath, append(encoded, '\n'), settingsFileMode); err != nil {
		return runSandbox{}, errors.Join(err, cleanup())
	}
	env := []string{
		"TMPDIR=" + scratch,
		"GOTMPDIR=" + scratch,
		"CLAUDE_CODE_TMPDIR=" + scratch,
		// git status and diff may refresh the index; the refresh is optional,
		// and a read-only sandbox refuses that write.
		"GIT_OPTIONAL_LOCKS=0",
	}
	if goCache != "" {
		env = append(env, "GOCACHE="+goCache)
	}
	return runSandbox{settingsPath: settingsPath, env: env, cleanup: cleanup}, nil
}

// readOnlyShellSandboxSettings is the sandbox stanza of a read-only run. No
// command escapes it: allowUnsandboxedCommands is off and nothing is excluded.
func readOnlyShellSandboxSettings(worktree, home, scratch, goCache string, denyRead []string) map[string]any {
	return map[string]any{
		"enabled":                  true,
		"failIfUnavailable":        true,
		"allowUnsandboxedCommands": false,
		"autoAllowBashIfSandboxed": true,
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

// writableSandboxSettings is the sandbox stanza of a writable run. A denied
// path stays denied below an allowed one, so the submission directory, which
// lives under $LF_HOME's submission root, is excluded from the write denies
// and re-allowed for reading by name.
func (r *Runner) writableSandboxSettings(worktree, scratch, goCache, submissionRoot string) map[string]any {
	denyRead := r.deniedReadPaths()
	denyWrite := slices.Clone(denyRead)
	allowWrite := []string{worktree, scratch}
	allowRead := []string{}
	unixSockets := []string{}
	if gitDir := worktreeGitDir(worktree); gitDir != "" {
		allowWrite = append(allowWrite, gitDir)
	}
	if goCache != "" {
		allowWrite = append(allowWrite, goCache)
	}
	allowWrite = append(allowWrite, r.packageCacheDirs()...)
	if submissionRoot != "" {
		submissions := r.paths.SubmissionDir()
		denyWrite = slices.DeleteFunc(denyWrite, func(path string) bool { return path == submissions })
		allowWrite = append(allowWrite, submissionRoot)
		allowRead = append(allowRead, submissionRoot)
		unixSockets = append(unixSockets, filepath.Join(submissionRoot, "s"))
	}
	return map[string]any{
		"enabled":                  true,
		"failIfUnavailable":        true,
		"allowUnsandboxedCommands": false,
		"autoAllowBashIfSandboxed": true,
		"excludedCommands":         []string{},
		// Go programs (go, gh, golangci-lint) verify TLS through trustd; the
		// module proxy is unreachable without it.
		"enableWeakerNetworkIsolation": true,
		"filesystem": map[string]any{
			"allowWrite": allowWrite,
			"denyWrite":  denyWrite,
			"denyRead":   denyRead,
			"allowRead":  allowRead,
		},
		"network": map[string]any{
			"allowedDomains":      sandboxAllowedDomains(),
			"allowAllUnixSockets": false,
			"allowUnixSockets":    unixSockets,
			"allowLocalBinding":   true,
		},
	}
}

// packageCacheDirs are the operator-home caches a build or install writes:
// the Go module cache and checksum database, the per-user cache roots and
// npm's cache. None holds a credential.
func (r *Runner) packageCacheDirs() []string {
	if r.osHome == "" || !filepath.IsAbs(r.osHome) {
		return nil
	}
	env := envMap(r.environ)
	goPath := filepath.Join(r.osHome, "go")
	if path := env["GOPATH"]; path != "" && filepath.IsAbs(strings.Split(path, string(os.PathListSeparator))[0]) {
		goPath = strings.Split(path, string(os.PathListSeparator))[0]
	}
	dirs := []string{filepath.Join(goPath, "pkg")}
	if modCache := env["GOMODCACHE"]; modCache != "" && filepath.IsAbs(modCache) {
		dirs = append(dirs, modCache)
	}
	return append(dirs, filepath.Join(r.osHome, "Library", "Caches"), filepath.Join(r.osHome, ".cache"), filepath.Join(r.osHome, ".npm"))
}

// worktreeGitDir is the per-worktree git directory a linked worktree's .git
// file points at, where git keeps its index and HEAD. A main checkout's .git
// directory is inside the worktree already, and an unreadable or unexpected
// .git yields "".
func worktreeGitDir(worktree string) string {
	// #nosec G304 -- worktree is the validated absolute run directory.
	raw, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		return ""
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
	if !ok || !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Clean(dir)
}

// deniedReadPaths are the credential-bearing locations a sandboxed command may
// not read: the same set the doctor's diagnosis shell hides, plus
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
