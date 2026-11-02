package providerprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
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
func RunConversationProcess(ctx context.Context, binary string, args, env []string, request agent.ConversationRequest, scratch string, timeout time.Duration, observe func([]byte), cancelProcess ...func(int) error) (failure error) {
	phase, started := "validation", false
	defer func() { failure = agent.ConversationError(failure, phase, !started) }()
	if err := request.Validate(); err != nil {
		return err
	}
	if timeout <= 0 {
		return errors.New("conversation timeout must be positive")
	}
	phase = "ownership"
	lock, err := conversationLock(request.StateDirectory)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	phase = "before_launch"
	if err = request.BeforeLaunch(ctx); err != nil {
		return err
	}
	phase = "launch"
	phase, started, err = execution.RunOwnedProcess(ctx, binary, args, env, execution.OwnedOptions{
		Prompt: request.Prompt, Directory: scratch, Timeout: timeout, Lock: lock,
		OnStarted: func(pid int) error {
			marker := filepath.Join(request.StateDirectory, "provider.pid")
			//nolint:gosec // Fixed marker filename below the locked, validated private state directory.
			markerFile, e := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if e == nil {
				_, e = markerFile.WriteString(strconv.Itoa(pid))
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
			return e
		},
		OnExited: func(unverified bool) {
			marker := filepath.Join(request.StateDirectory, "provider.pid")
			if unverified {
				_ = os.WriteFile(marker, []byte("unverified-process-exit"), 0600)
			} else {
				_ = os.Remove(marker)
			}
		},
		CancelProcess: func(pid int) error {
			if len(cancelProcess) > 0 && cancelProcess[0] != nil {
				return cancelProcess[0](pid)
			}
			return execution.CancelConversationTree(context.WithoutCancel(ctx), pid)
		},
	}, observe)
	return err
}
