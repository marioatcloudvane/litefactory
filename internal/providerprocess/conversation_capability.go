package providerprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// ConversationProbe performs only local capability checks, never a model call.
// Output and runtime are bounded and ambient secrets/settings are removed.
func ConversationProbe(ctx context.Context, binary string, args, env []string, directory string) (string, error) {
	return conversationProbe(ctx, binary, args, env, directory, func(int) time.Duration { return 10 * time.Second }, waitForRetry)
}

// ErrProbeTransient preserves a local availability failure separately from an
// affirmative isolation refusal. No model process has been authorized yet.
var ErrProbeTransient = errors.New("conversation capability probe temporarily unavailable")

func conversationProbe(ctx context.Context, binary string, args, env []string, directory string, timeout func(attempt int) time.Duration, wait func(context.Context, time.Duration) error) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		probeCtx, cancel := context.WithTimeout(ctx, timeout(attempt))
		//nolint:gosec // Adapter-owned executable and local capability arguments.
		cmd := exec.CommandContext(probeCtx, binary, args...)
		cmd.Env, cmd.Dir, cmd.Stderr = env, directory, io.Discard
		cmd.WaitDelay = time.Second
		var output bytes.Buffer
		cmd.Stdout = &limitedProbeWriter{target: &output}
		err := cmd.Run()
		deadline := errors.Is(probeCtx.Err(), context.DeadlineExceeded)
		cancel()
		if err == nil {
			return output.String(), nil
		}
		if ctx.Err() != nil {
			return "", errors.Join(err, ctx.Err())
		}
		transient := deadline || killedProbe(err) || missingExecutable(err, binary, directory)
		if deadline {
			err = errors.Join(err, context.DeadlineExceeded)
		}
		if !transient {
			return "", fmt.Errorf("conversation capability probe failed: %w", err)
		}
		err = fmt.Errorf("%w: %w", ErrProbeTransient, err)
		if attempt == 2 {
			return "", err
		}
		if waitErr := wait(ctx, time.Duration(attempt+1)*time.Second); waitErr != nil {
			return "", errors.Join(err, waitErr)
		}
	}
	panic("unreachable probe attempt")
}

func killedProbe(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}

type limitedProbeWriter struct{ target *bytes.Buffer }

func (w *limitedProbeWriter) Write(p []byte) (int, error) {
	if w.target.Len()+len(p) > 128<<10 {
		return 0, errors.New("conversation capability output too large")
	}
	return w.target.Write(p)
}

// PrepareConversationDirectory rejects symlinks before provider-owned state is used.
func PrepareConversationDirectory(state string) (home, scratch string, err error) {
	unlock, err := LockConversation(state)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	for _, name := range []string{"home", "scratch"} {
		path := filepath.Join(state, name)
		if e := os.MkdirAll(path, 0700); e != nil {
			return "", "", e
		}
		info, e := os.Lstat(path)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("invalid private provider directory")
		}
		//nolint:gosec // Private directories require owner traversal; no group or other access.
		if e = os.Chmod(path, 0700); e != nil {
			return "", "", e
		}
	}
	return filepath.Join(state, "home"), filepath.Join(state, "scratch"), nil
}
