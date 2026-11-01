// Package process owns private native process mechanics, without factory authority.
package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ErrProviderExit marks an observed nonzero provider exit.
var ErrProviderExit = errors.New("provider exited unsuccessfully")

// FailureObserver receives a nonzero native exit and bounded stderr tail. The caller owns redaction.
type FailureObserver func(exitCode int, stderrTail []byte)

// OversizedObserver reports dropped judge records after bounded transport completes.
type OversizedObserver func(kind string, largestDroppedBytes, totalBytes int)

const (
	outputLineBudget  = 1024 * 1024
	outputTotalBudget = 8 * 1024 * 1024
	stderrTailLimit   = 8 << 10
)

type stderrTail struct{ data []byte }

func (t *stderrTail) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= stderrTailLimit {
		t.data = append(t.data[:0], p[len(p)-stderrTailLimit:]...)
		return n, nil
	}
	if over := len(t.data) + len(p) - stderrTailLimit; over > 0 {
		t.data = append(t.data[:0], t.data[over:]...)
	}
	t.data = append(t.data, p...)
	return n, nil
}

// Run executes bounded line transport with operation-specific output handling.
func Run(ctx context.Context, binary string, args, env []string, prompt, directory string, timeout time.Duration, observe func([]byte), failure FailureObserver, cancelProcess func(int) error, judge bool, oversized OversizedObserver) error {
	output := &Lines{observe: observe, judge: judge}
	err := runScopedOutputProcess(ctx, binary, args, env, prompt, directory, timeout, output, failure, cancelProcess)
	if output.discarding > 0 {
		output.endDiscard()
	}
	if oversized != nil {
		if output.dropped {
			oversized("line_dropped", output.largestDropped, output.total)
		} else if output.total > outputTotalBudget {
			oversized("total_exceeded", 0, output.total)
		}
	}
	return err
}

// OutputPolicy bounds public prepared invocation output. Zero fields use the
// existing scoped transport limits; DiscardOversizedLines retains judge policy.
type OutputPolicy struct {
	LineBytes             int
	TotalBytes            int
	DiscardOversizedLines bool
}

// NewLinesWithPolicy creates a line writer with explicit per-call bounds.
func NewLinesWithPolicy(observe func([]byte), cancel context.CancelFunc, policy OutputPolicy) *Lines {
	return &Lines{observe: observe, cancel: cancel, judge: policy.DiscardOversizedLines, lineLimit: policy.LineBytes, totalLimit: policy.TotalBytes}
}

// NewLines builds the bounded line writer used by owned conversation transport.
func NewLines(observe func([]byte), cancel context.CancelFunc) *Lines {
	return &Lines{observe: observe, cancel: cancel}
}
func runScopedOutputProcess(ctx context.Context, binary string, args, env []string, prompt, directory string, timeout time.Duration, output *Lines, failure FailureObserver, cancelProcess func(int) error) error {
	if timeout <= 0 {
		return errors.New("doctor provider timeout must be positive")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	//nolint:gosec // Resolved provider executable and adapter-owned arguments; owner text is stdin only.
	cmd := exec.CommandContext(runCtx, binary, args...)
	cmd.Dir = directory
	cmd.Env = env
	cmd.Stdin = strings.NewReader(prompt)
	output.cancel = cancel
	cmd.Stdout = output
	var stderr *stderrTail
	if failure != nil {
		stderr = &stderrTail{}
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = io.Discard
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		// Native shells may leave this process group. The provider adapter can
		// supply its existing subtree shutdown, bound only to this owned PID.
		if cancelProcess != nil {
			return cancelProcess(cmd.Process.Pid)
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	err := cmd.Run()
	if err == nil {
		err = output.Finish()
	}
	// Do not return native executable paths or stderr through this boundary.
	// A guard that cancelled the run reports itself, not the cancel it caused.
	if output.err != nil {
		return output.err
	}
	if runCtx.Err() != nil {
		return runCtx.Err()
	}
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			if failure != nil {
				failure(exited.ExitCode(), stderr.data)
			}
			return ErrProviderExit
		}
		return errors.New("doctor provider process or bounded output failed")
	}
	return nil
}

// Lines assembles bounded newline records and preserves the judge discard policy.
type Lines struct {
	lineLimit, totalLimit int
	pending               []byte
	total                 int
	observe               func([]byte)
	cancel                context.CancelFunc
	// err is the guard's own error once it has cancelled the run.
	err error
	// judge discards oversized lines and ignores the total budget.
	judge          bool
	discarding     int
	dropped        bool
	largestDropped int
}

func (s *Lines) stop(err error) (int, error) {
	if s.err == nil {
		s.err = err
	}
	if s.cancel != nil {
		s.cancel()
	}
	return 0, err
}

func (s *Lines) Write(data []byte) (int, error) {
	lineLimit, totalLimit := s.lineLimit, s.totalLimit
	if lineLimit <= 0 {
		lineLimit = outputLineBudget
	}
	if totalLimit <= 0 {
		totalLimit = outputTotalBudget
	}
	if !s.judge && len(data) > totalLimit-s.total {
		return s.stop(errors.New("doctor output byte budget exceeded"))
	}
	s.total += len(data)
	count := len(data)
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		size := len(data)
		if end >= 0 {
			size = end
		}
		if s.judge && (s.discarding > 0 || len(s.pending)+size > lineLimit) {
			// Drop the oversized line and ignore input up to its newline.
			if s.discarding == 0 {
				s.discarding = len(s.pending)
				s.pending = s.pending[:0]
			}
			s.discarding += size
			if end < 0 {
				break
			}
			s.endDiscard()
			data = data[end+1:]
			continue
		}
		if len(s.pending)+size > lineLimit {
			return s.stop(errors.New("doctor output line budget exceeded"))
		}
		s.pending = append(s.pending, data[:size]...)
		if end < 0 {
			break
		}
		s.observe(s.pending)
		s.pending = s.pending[:0]
		data = data[end+1:]
	}
	return count, nil
}
func (s *Lines) endDiscard() {
	s.dropped = true
	s.largestDropped = max(s.largestDropped, s.discarding)
	s.discarding = 0
}

// Finish rejects a nonempty incomplete record at process exit.
func (s *Lines) Finish() error {
	if s.discarding > 0 {
		s.endDiscard()
	}
	if len(bytes.TrimSpace(s.pending)) != 0 {
		return errors.New("doctor provider ended with an incomplete output record")
	}
	return nil
}
