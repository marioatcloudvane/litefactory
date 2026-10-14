package providerprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func conversationProcessAlive(state string) error {
	//nolint:gosec // Fixed ownership filename below a validated, symlink-free private state directory.
	data, err := os.ReadFile(filepath.Join(state, "provider.pid"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return agent.ErrConversationBusy
	}
	err = syscall.Kill(pid, 0)
	if !errors.Is(err, syscall.ESRCH) {
		return agent.ErrConversationBusy
	}
	return os.Remove(filepath.Join(state, "provider.pid"))
}

// LockConversation rejects live recorded processes even after the daemon exits.
// PID reuse conservatively blocks recovery rather than risking a concurrent turn.
func LockConversation(state string) (func(), error) {
	f, err := conversationLock(state)
	if err != nil {
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
func conversationLock(state string) (*os.File, error) {
	if !filepath.IsAbs(state) || filepath.Clean(state) != state {
		return nil, errors.New("invalid conversation state path")
	}
	if err := os.MkdirAll(state, 0700); err != nil {
		return nil, err
	}
	for path := state; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("invalid conversation state directory")
		}
		if path == "/" {
			break
		}
	}
	for _, name := range []string{"provider.lock", "provider.pid"} {
		info, err := os.Lstat(filepath.Join(state, name))
		if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return nil, errors.New("invalid conversation ownership file")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	//nolint:gosec // Fixed lock filename below a validated, symlink-free private state directory.
	f, err := os.OpenFile(filepath.Join(state, "provider.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, agent.ErrConversationBusy
	}
	if err = conversationProcessAlive(state); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// ConversationIdle establishes recorded provider exit without killing unrelated processes.
func ConversationIdle(state string) error {
	unlock, err := LockConversation(state)
	if err != nil {
		return err
	}
	unlock()
	return nil
}

// RunConversationProcess journals the exact owned PID before allowing the provider
// to exec. The pipe barrier prevents a daemon crash from launching an unrecorded
// provider. An inherited lock also covers the provider's process lifetime.
func RunConversationProcess(ctx context.Context, binary string, args, env []string, request agent.ConversationRequest, scratch string, timeout time.Duration, observe func([]byte), cancelProcess ...func(int) error) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if timeout <= 0 {
		return errors.New("conversation timeout must be positive")
	}
	lock, err := conversationLock(request.StateDirectory)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	if err = request.BeforeLaunch(ctx); err != nil {
		return err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := append([]string{"-c", `read -r ready <&4 || exit 1; exec "$@"`, "conversation-provider", binary}, args...)
	//nolint:gosec // Fixed shell barrier; executable and arguments are positional, prompt is stdin.
	cmd := exec.CommandContext(runCtx, "/bin/sh", command...)
	cmd.Dir = scratch
	cmd.Env = env
	cmd.Stdin = strings.NewReader(request.Prompt)
	cmd.ExtraFiles = []*os.File{lock, reader}
	output := &doctorLines{observe: observe, cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	var cancellationUnverified atomic.Bool
	cmd.Cancel = func() error {
		if len(cancelProcess) > 0 && cancelProcess[0] != nil {
			e := cancelProcess[0](cmd.Process.Pid)
			if e != nil {
				cancellationUnverified.Store(true)
			}
			return e
		}
		e := CancelConversationTree(context.WithoutCancel(ctx), cmd.Process.Pid)
		if e != nil {
			cancellationUnverified.Store(true)
		}
		return e
	}
	if err = cmd.Start(); err != nil {
		return errors.New("conversation provider launch failed")
	}
	marker := filepath.Join(request.StateDirectory, "provider.pid")
	//nolint:gosec // Fixed marker filename below the locked, validated private state directory.
	markerFile, e := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e == nil {
		_, e = markerFile.WriteString(strconv.Itoa(cmd.Process.Pid))
		if e == nil {
			e = markerFile.Sync()
		}
		e = errors.Join(e, markerFile.Close())
	}
	if e == nil {
		dir, openErr := os.Open(request.StateDirectory)
		if openErr != nil {
			e = openErr
		} else {
			e = errors.Join(dir.Sync(), dir.Close())
		}
	}
	if e == nil {
		_, e = writer.WriteString("ready\n")
	}
	_ = writer.Close()
	if e != nil {
		cancel()
	}
	err = cmd.Wait()
	if cancellationUnverified.Load() {
		_ = os.WriteFile(marker, []byte("unverified-process-exit"), 0600)
	} else {
		_ = os.Remove(marker)
	}
	if e != nil {
		return errors.New("conversation ownership persistence failed")
	}
	if runCtx.Err() != nil {
		return runCtx.Err()
	}
	if err != nil {
		return errors.New("conversation provider process failed")
	}
	return output.finish()
}
