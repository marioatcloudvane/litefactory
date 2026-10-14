package providerprocess

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ConversationProbe performs only local capability checks, never a model call.
// Output and runtime are bounded and ambient secrets/settings are removed.
func ConversationProbe(ctx context.Context, binary string, args, env []string, directory string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	//nolint:gosec // Adapter-owned executable and local capability arguments.
	cmd := exec.CommandContext(probeCtx, binary, args...)
	cmd.Env = env
	cmd.Dir = directory
	cmd.Stderr = io.Discard
	var output bytes.Buffer
	cmd.Stdout = &limitedProbeWriter{target: &output}
	if err := cmd.Run(); err != nil {
		return "", errors.New("conversation capability probe failed")
	}
	return output.String(), nil
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
