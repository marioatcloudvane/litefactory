// Package bugchecks executes pinned project commands without daemon authority.
package bugchecks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
)

// OutputLimit bounds captured command output before execution is cancelled.
const OutputLimit = 4 << 20

// SandboxProfile names the permission profile the check launcher defines for
// each command. It is supplied on the command line, never from recipe input.
const SandboxProfile = "lf-bug-checks"

// Isolation is operator configuration, never recipe or agent input. Every
// command runs under the Codex sandbox (macOS Seatbelt, Linux Landlock) with
// the same kind of containment the agents run under: the whole filesystem is
// readable except DenyRead, writes are limited to the worktree, a private
// temporary directory and WritableRoots, and the network is off. There is
// deliberately no unisolated fallback.
type Isolation struct {
	// Binary is the Codex CLI that applies the sandbox.
	Binary        string
	WritableRoots []string
	DenyRead      []string
	PATH          string
	// Home is the operator home the command sees, so toolchains find their
	// configuration and the caches WritableRoots opens.
	Home        string
	Environment map[string]string
	Revision    string
}

// CommandResult retains process completion, output capture and cleanup observations.
type CommandResult struct {
	ExecutableDigest string    `json:"executable_digest"`
	Executable       string    `json:"executable"`
	CWD              string    `json:"cwd"`
	Environment      []string  `json:"environment"`
	Started          time.Time `json:"started"`
	Ended            time.Time `json:"ended"`
	Exit             *int      `json:"exit,omitempty"`
	Outcome          string    `json:"outcome"`
	Reason           string    `json:"reason,omitempty"`
	Output           string    `json:"output"`
	CaptureComplete  bool      `json:"capture_complete"`
	CleanupConfirmed bool      `json:"cleanup_confirmed"`
	ProcessID        int       `json:"process_id,omitempty"`
	ProcessIdentity  string    `json:"process_identity,omitempty"`
}

func contained(root, path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("check path escapes worktree")
	}
	return resolved, nil
}

func protected(name string) bool {
	upper := strings.ToUpper(name)
	for _, prefix := range []string{"LF_", "LITEFACTORY_", "GITHUB_", "GH_", "OPENAI_", "ANTHROPIC_", "CLAUDE_", "CODEX_", "AWS_", "SSH_", "MCP_", "DYLD_", "LD_", "BASH_", "ENV"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	for _, part := range []string{"TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "CAPABILITY"} {
		if strings.Contains(upper, part) {
			return true
		}
	}
	switch upper {
	case "PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SHELL", "USER", "LOGNAME", "CDPATH":
		return true
	}
	return false
}

// Validate fails before launch when the host cannot enforce the configured
// containment. Runtime sandbox denial is an observed failed or blocked command.
func (p Isolation) Validate() error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return errors.New("bug checks require the Codex sandbox on macOS or Linux")
	}
	if p.Revision == "" || !filepath.IsAbs(p.Binary) || !filepath.IsAbs(p.Home) {
		return errors.New("missing operator check isolation profile")
	}
	st, err := os.Stat(p.Binary)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
		return errors.New("check sandbox launcher unavailable")
	}
	for _, root := range p.WritableRoots {
		if !sandboxPath(root) || root == "/" || filepath.Clean(root) == filepath.Clean(p.Home) {
			return errors.New("unsupported writable check root")
		}
	}
	for _, path := range p.DenyRead {
		if !sandboxPath(path) || path == "/" {
			return errors.New("unsupported unreadable check path")
		}
	}
	if p.PATH == "" {
		return errors.New("operator check PATH requires absolute runtime directories")
	}
	for _, dir := range filepath.SplitList(p.PATH) {
		if !filepath.IsAbs(dir) || dir == "/" {
			return errors.New("operator check PATH requires absolute runtime directories")
		}
	}
	return nil
}

// sandboxPath accepts only absolute paths the launcher can quote literally.
func sandboxPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, r := range path {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// sandboxArgs defines the per-command permission profile and the command.
// Later entries in the filesystem table never widen a denied path: the
// launcher refuses duplicate keys, so each path appears once.
func (p Isolation) sandboxArgs(root, tmp, cwd, executable string, argv []string) ([]string, error) {
	access := map[string]string{"/": "read", root: "write", tmp: "write"}
	order := []string{"/", root, tmp}
	add := func(path, mode string) error {
		if !sandboxPath(path) {
			return fmt.Errorf("unsupported check sandbox path %q", path)
		}
		if _, dup := access[path]; dup {
			return fmt.Errorf("check sandbox path %q declared twice", path)
		}
		access[path] = mode
		order = append(order, path)
		return nil
	}
	for _, path := range p.WritableRoots {
		if err := add(path, "write"); err != nil {
			return nil, err
		}
	}
	for _, path := range p.DenyRead {
		if err := add(path, "none"); err != nil {
			return nil, err
		}
	}
	for _, path := range order[:3] {
		if !sandboxPath(path) {
			return nil, fmt.Errorf("unsupported check sandbox path %q", path)
		}
	}
	var table strings.Builder
	table.WriteString("{")
	for i, path := range order {
		if i > 0 {
			table.WriteString(",")
		}
		table.WriteString(`"` + path + `"="` + access[path] + `"`)
	}
	table.WriteString("}")
	args := []string{"sandbox", "-P", SandboxProfile,
		"-c", "permissions." + SandboxProfile + ".filesystem=" + table.String(),
		"-c", "permissions." + SandboxProfile + ".network.enabled=false",
		"-C", cwd, "--", executable}
	return append(args, argv[1:]...), nil
}

func (p Isolation) resolve(root string, c config.BugCheckCommand) (string, string, []string, error) {
	if len(c.Argv) == 0 {
		return "", "", nil, errors.New("empty check argv")
	}
	cwd, err := contained(root, filepath.Join(root, c.CWD))
	if err != nil {
		return "", "", nil, err
	}
	pathDirs := filepath.SplitList(p.PATH)
	var executable string
	trusted := false
	if strings.ContainsRune(c.Argv[0], filepath.Separator) {
		if filepath.IsAbs(c.Argv[0]) {
			executable = c.Argv[0]
			trusted = slices.Contains(pathDirs, filepath.Dir(executable))
		} else {
			executable, err = contained(root, filepath.Join(cwd, c.Argv[0]))
			if err != nil {
				return "", "", nil, err
			}
		}
	} else {
		for _, dir := range pathDirs {
			candidate := filepath.Join(dir, c.Argv[0])
			st, e := os.Stat(candidate)
			if e == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
				executable = candidate
				trusted = true
				break
			}
		}
	}
	if executable == "" {
		return "", "", nil, errors.New("check executable unavailable in operator PATH")
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", "", nil, err
	}
	// Operator PATH directories are trusted as named; worktree executables must
	// resolve inside the worktree. Anything else was never granted.
	if !trusted && executable != root && !strings.HasPrefix(executable, root+string(filepath.Separator)) {
		return "", "", nil, errors.New("check executable outside the operator PATH and worktree")
	}
	lang := "C.UTF-8"
	if runtime.GOOS == "darwin" {
		lang = "en_US.UTF-8"
	}
	env := []string{"PATH=" + p.PATH, "HOME=" + p.Home, "LANG=" + lang}
	for _, name := range c.Env {
		value, ok := p.Environment[name]
		if !ok || protected(name) || strings.ContainsRune(value, 0) {
			return "", "", nil, errors.New("unresolved or protected check environment reference")
		}
		env = append(env, name+"="+value)
	}
	return executable, cwd, env, nil
}

type capture struct {
	mu sync.Mutex
	bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (w *capture) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	left := OutputLimit - w.Len()
	if n > left {
		_, _ = w.Buffer.Write(p[:left])
		w.overflow = true
		w.cancel()
		return n, nil
	}
	_, _ = w.Buffer.Write(p)
	return n, nil
}

// Run performs exactly one command, never a retry. The command runs in its own
// process group under the sandbox launcher; the callback durably retains the
// group's identity right after launch, and a failed callback stops the group.
// A crash before that point leaves a claimed launch without identity, which
// reconciliation keeps unresolved. After the command ends, every remaining
// member of its group is stopped and cleanup is confirmed only when the group
// is empty. No recovery path signals a stored bare PID.
func (p Isolation) Run(ctx context.Context, worktree string, c config.BugCheckCommand, started func(CommandResult) error) (out CommandResult) {
	out.Outcome = "blocked"
	out.CleanupConfirmed = true
	fail := func(err error) CommandResult { out.Reason = err.Error(); out.Ended = time.Now().UTC(); return out }
	if err := p.Validate(); err != nil {
		return fail(err)
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 86400 {
		return fail(errors.New("invalid check timeout"))
	}
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return fail(err)
	}
	executable, cwd, env, err := p.resolve(root, c)
	if err != nil {
		return fail(err)
	}
	// #nosec G304 -- resolve validates the configured executable within the trusted PATH or worktree.
	binaryFile, err := os.Open(executable)
	if err != nil {
		return fail(err)
	}
	hash := sha256.New()
	size, hashErr := io.Copy(hash, io.LimitReader(binaryFile, (128<<20)+1))
	closeErr := binaryFile.Close()
	if hashErr != nil || closeErr != nil {
		return fail(errors.Join(hashErr, closeErr))
	}
	if size > 128<<20 {
		return fail(errors.New("check executable exceeds 128 MiB identity bound"))
	}
	out.ExecutableDigest = hex.EncodeToString(hash.Sum(nil))
	out.Executable = executable
	out.CWD = cwd
	out.Environment = append([]string(nil), c.Env...)
	tmp, err := os.MkdirTemp("", "lf-bug-check-")
	if err != nil {
		return fail(err)
	}
	if tmp, err = filepath.EvalSymlinks(tmp); err != nil {
		return fail(err)
	}
	args, err := p.sandboxArgs(root, tmp, cwd, executable, c.Argv)
	if err != nil {
		return fail(errors.Join(err, os.RemoveAll(tmp)))
	}
	env = append(env, "TMPDIR="+tmp)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	// #nosec G204 -- operator-selected sandbox launcher and validated recipe argv; no shell or inherited credentials.
	cmd := exec.CommandContext(runCtx, p.Binary, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return stopGroup(cmd.Process.Pid) }
	cmd.WaitDelay = 5 * time.Second
	output := &capture{cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = output
	// Re-resolve immediately before launch; the sandbox profile enforces
	// containment even if host paths change after this final observation.
	if _, err = contained(root, cwd); err != nil {
		return fail(errors.Join(err, os.RemoveAll(tmp)))
	}
	out.Started = time.Now().UTC()
	if err = cmd.Start(); err != nil {
		return fail(errors.Join(fmt.Errorf("launch sandboxed check: %w", err), os.RemoveAll(tmp)))
	}
	out.CleanupConfirmed = false
	group := cmd.Process.Pid
	out.ProcessID = group
	var persistErr error
	out.ProcessIdentity, persistErr = captureProcessIdentity(group)
	if persistErr == nil {
		if started == nil {
			persistErr = errors.New("missing durable process observation callback")
		} else {
			persistErr = started(out)
		}
	}
	if persistErr != nil {
		cancel()
	}
	err = cmd.Wait()
	out.Ended = time.Now().UTC()
	// The launcher has exited; anything its command left behind in the group is
	// stopped now, and cleanup counts only once the group is observed empty.
	out.CleanupConfirmed = groupEmptyAfterStop(group) && !errors.Is(err, exec.ErrWaitDelay)
	if out.CleanupConfirmed {
		if removeErr := os.RemoveAll(tmp); removeErr != nil {
			out.CleanupConfirmed = false
		}
	}
	output.mu.Lock()
	raw := output.String()
	overflow := output.overflow
	output.mu.Unlock()
	// Every declared value is protected literally, irrespective of its name.
	for _, name := range c.Env {
		if v := p.Environment[name]; v != "" {
			raw = strings.ReplaceAll(raw, v, "[REDACTED]")
		}
	}
	body, redactErr := redact.New(redact.Options{Environ: []string{}}).Apply(context.WithoutCancel(ctx), raw, OutputLimit)
	if redactErr != nil {
		return fail(fmt.Errorf("redact check output: %w", redactErr))
	}
	out.Output = body.String()
	out.CaptureComplete = !overflow && !body.Truncated() && !errors.Is(err, exec.ErrWaitDelay)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		code := cmd.ProcessState.ExitCode()
		out.Exit = &code
	}
	switch {
	case persistErr != nil:
		return fail(fmt.Errorf("retain command identity: %w", persistErr))
	case !out.CleanupConfirmed:
		return fail(errors.New("check process cleanup unknown"))
	case overflow:
		return fail(errors.New("check output exceeded 4 MiB"))
	case ctx.Err() != nil:
		out.Outcome = "cancelled"
		return fail(ctx.Err())
	case runCtx.Err() != nil:
		return fail(errors.New("check timed out"))
	case !out.CaptureComplete:
		return fail(errors.New("check output capture incomplete"))
	case out.Exit == nil:
		return fail(errors.New("check exit status unavailable"))
	case *out.Exit == 0 && err == nil:
		out.Outcome = "passed"
	case *out.Exit == 1:
		out.Outcome = "failed"
		out.Reason = "project check reported test failure"
	default:
		out.Reason = "check infrastructure failure or signal"
	}
	return out
}

// stopGroup kills every member of the command's process group.
func stopGroup(group int) error {
	err := syscall.Kill(-group, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// groupEmptyAfterStop stops any remaining group member and waits briefly for
// the group to disappear. Members that are already exiting can make the kill
// itself fail, so it is repeated until the group is observed empty; only that
// observation confirms cleanup.
func groupEmptyAfterStop(group int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = stopGroup(group)
		if errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
