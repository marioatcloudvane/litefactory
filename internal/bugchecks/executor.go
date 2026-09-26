// Package bugchecks executes pinned project commands without daemon authority.
package bugchecks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
)

// OutputLimit bounds captured command output before execution is cancelled.
const OutputLimit = 4 << 20

// Isolation is operator configuration, never recipe or agent input. Linux
// bubblewrap supplies a private PID namespace: teardown kills descendants even
// if they create new sessions. There is deliberately no unisolated fallback.
type Isolation struct {
	Binary        string
	ReadOnlyRoots []string
	PATH          string
	Environment   map[string]string
	Revision      string
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
// containment. Runtime sandbox denial is an observed blocked command.
func (p Isolation) Validate() error {
	if runtime.GOOS != "linux" {
		return errors.New("bug checks require Linux bubblewrap PID/mount isolation on this host")
	}
	if p.Revision == "" || !filepath.IsAbs(p.Binary) || len(p.ReadOnlyRoots) == 0 {
		return errors.New("missing operator check isolation profile")
	}
	st, err := os.Stat(p.Binary)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
		return errors.New("check isolation executable unavailable")
	}
	for _, root := range p.ReadOnlyRoots {
		// Only runtime system trees may be exposed; never HOME, daemon state or a
		// broad filesystem root. An operator chooses a subset of these trees.
		switch root {
		case "/usr", "/bin", "/sbin", "/lib", "/lib64":
		default:
			return errors.New("unsupported check runtime mount")
		}
	}
	for _, dir := range filepath.SplitList(p.PATH) {
		if !filepath.IsAbs(dir) || dir == "/" {
			return errors.New("operator check PATH requires absolute runtime directories")
		}
	}
	return nil
}

func (p Isolation) resolve(root string, c config.BugCheckCommand) (string, string, []string, error) {
	if len(c.Argv) == 0 {
		return "", "", nil, errors.New("empty check argv")
	}
	cwd, err := contained(root, filepath.Join(root, c.CWD))
	if err != nil {
		return "", "", nil, err
	}
	var executable string
	if strings.ContainsRune(c.Argv[0], filepath.Separator) {
		if filepath.IsAbs(c.Argv[0]) {
			executable = c.Argv[0]
		} else {
			executable, err = contained(root, filepath.Join(cwd, c.Argv[0]))
			if err != nil {
				return "", "", nil, err
			}
		}
	} else {
		for _, dir := range filepath.SplitList(p.PATH) {
			candidate := filepath.Join(dir, c.Argv[0])
			st, e := os.Stat(candidate)
			if e == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
				executable = candidate
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
	allowed := executable == root || strings.HasPrefix(executable, root+string(filepath.Separator))
	for _, r := range p.ReadOnlyRoots {
		if strings.HasPrefix(executable, r+"/") {
			allowed = true
		}
	}
	if !allowed {
		return "", "", nil, errors.New("check executable outside operator mounts")
	}
	env := []string{"PATH=" + p.PATH, "HOME=/home/check", "TMPDIR=/tmp", "LANG=C.UTF-8"}
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

// Run performs exactly one command, never a retry. The callback runs after the
// namespace process starts and must durably retain its identity; failure kills
// that same process. No recovery path signals a stored bare PID.
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
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	args := []string{"--die-with-parent", "--unshare-all", "--new-session", "--clearenv"}
	for _, r := range p.ReadOnlyRoots {
		args = append(args, "--ro-bind", r, r)
	}
	args = append(args, "--bind", root, root, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/home", "--dir", "/home/check", "--chdir", cwd)
	for _, v := range env {
		k, value, _ := strings.Cut(v, "=")
		args = append(args, "--setenv", k, value)
	}
	args = append(args, "--", executable)
	args = append(args, c.Argv[1:]...)
	infoRead, infoWrite, pipeErr := os.Pipe()
	if pipeErr != nil {
		return fail(pipeErr)
	}
	defer infoRead.Close()
	defer infoWrite.Close()
	syncRead, syncWrite, syncErr := os.Pipe()
	if syncErr != nil {
		if closeErr := infoWrite.Close(); closeErr != nil {
			syncErr = errors.Join(syncErr, closeErr)
		}
		return fail(syncErr)
	}
	defer syncRead.Close()
	defer syncWrite.Close()
	blockRead, blockWrite, blockErr := os.Pipe()
	if blockErr != nil {
		return fail(errors.Join(blockErr, infoWrite.Close(), syncWrite.Close()))
	}
	defer blockRead.Close()
	defer blockWrite.Close()
	args = append([]string{"--json-status-fd", "3", "--sync-fd", "4", "--block-fd", "5"}, args...)
	// #nosec G204 -- operator-selected isolator and validated recipe argv; no shell or inherited credentials.
	cmd := exec.CommandContext(runCtx, p.Binary, args...)
	cmd.ExtraFiles = []*os.File{infoWrite, syncWrite, blockRead}
	cmd.Env = []string{"PATH=" + p.PATH}
	cmd.WaitDelay = 5 * time.Second
	output := &capture{cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = output
	// Re-resolve immediately before launch; namespace mounts enforce containment
	// even if host paths change after this final observation.
	if _, err = contained(root, cwd); err != nil {
		return fail(err)
	}
	out.Started = time.Now().UTC()
	if err = cmd.Start(); err != nil {
		if closeErr := infoWrite.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if closeErr := syncWrite.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return fail(fmt.Errorf("launch isolated check: %w", err))
	}
	if closeErr := infoWrite.Close(); closeErr != nil {
		cancel()
	}
	if closeErr := syncWrite.Close(); closeErr != nil {
		cancel()
	}
	out.CleanupConfirmed = false
	var persistErr error
	if closeErr := blockRead.Close(); closeErr != nil {
		persistErr = closeErr
	}
	if deadlineErr := infoRead.SetReadDeadline(time.Now().Add(5 * time.Second)); deadlineErr != nil {
		persistErr = errors.Join(persistErr, deadlineErr)
	}
	decoder := json.NewDecoder(io.LimitReader(infoRead, 4097))
	var initial struct {
		ChildPID int `json:"child-pid"`
	}
	if decodeErr := decoder.Decode(&initial); decodeErr != nil {
		persistErr = errors.Join(persistErr, decodeErr)
	}
	if initial.ChildPID <= 0 {
		persistErr = errors.Join(persistErr, errors.New("sandbox did not report namespace process"))
	} else {
		out.ProcessID = initial.ChildPID
		out.ProcessIdentity, err = captureProcessIdentity(initial.ChildPID)
		if err != nil {
			persistErr = errors.Join(persistErr, err)
		}
	}
	if persistErr == nil {
		if started == nil {
			persistErr = errors.New("missing durable process observation callback")
		} else {
			persistErr = started(out)
		}
	}
	// The sandbox remains behind its launch barrier until the kernel start and
	// namespace identity have been durably recorded. Failure cancels it in place.
	if persistErr == nil {
		_, persistErr = blockWrite.Write([]byte{1})
	}
	if persistErr != nil {
		cancel()
	}
	err = cmd.Wait()
	out.Ended = time.Now().UTC()
	if deadlineErr := infoRead.SetReadDeadline(time.Now().Add(time.Second)); deadlineErr != nil {
		return fail(deadlineErr)
	}
	childPID := initial.ChildPID
	var childExit *int
	statusValid := persistErr == nil
	for statusValid {
		var status struct {
			Exit *int `json:"exit-code"`
		}
		decodeErr := decoder.Decode(&status)
		if errors.Is(decodeErr, io.EOF) {
			break
		}
		if decodeErr != nil {
			statusValid = false
			break
		}
		if status.Exit != nil {
			if childExit != nil {
				statusValid = false
				break
			}
			childExit = status.Exit
		}
	}

	sandboxReady := statusValid && childPID > 0 && childExit != nil

	// The namespace supervisor exits only after the PID namespace is torn down;
	// a pipe-drain timeout is still indeterminate and retains the work guard.
	if deadlineErr := syncRead.SetReadDeadline(time.Now().Add(5 * time.Second)); deadlineErr != nil {
		return fail(deadlineErr)
	}
	syncBytes, syncReadErr := io.ReadAll(io.LimitReader(syncRead, 1))
	out.CleanupConfirmed = syncReadErr == nil && len(syncBytes) == 0 && !errors.Is(err, exec.ErrWaitDelay)
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
	case !sandboxReady:
		return fail(errors.New("sandbox setup did not confirm child launch"))
	case out.Exit == nil:
		return fail(errors.New("check exit status unavailable"))
	case childExit == nil || *childExit != *out.Exit:
		return fail(errors.New("sandbox child completion differs from supervisor"))
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
