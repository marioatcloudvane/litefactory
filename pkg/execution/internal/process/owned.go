package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// OwnedOptions keeps durable ownership in the caller. OnStarted acknowledges
// the exact PID before the inherited exec barrier can open. OnExited receives
// unverified cleanup so the caller can preserve its existing recovery fence.
type OwnedOptions struct {
	Prompt, Directory string
	Timeout           time.Duration
	Lock              *os.File
	OnStarted         func(int) error
	OnExited          func(bool)
	CancelProcess     func(int) error
}

// RunOwned runs the inherited lock and PID acknowledgment protocol.
func RunOwned(ctx context.Context, binary string, args, env []string, options OwnedOptions, observe func([]byte)) (phase string, started bool, failure error) {
	phase = "launch"
	if options.Lock == nil || options.OnStarted == nil || options.OnExited == nil || options.Timeout <= 0 {
		return "validation", false, errors.New("invalid owned process coordination")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return phase, started, err
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	runCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	command := append([]string{"-c", `read -r ready <&4 || exit 1; exec "$@"`, "conversation-provider", binary}, args...)
	//nolint:gosec // Fixed shell barrier; executable and arguments are positional, prompt is stdin.
	cmd := exec.CommandContext(runCtx, "/bin/sh", command...)
	cmd.Dir = options.Directory
	cmd.Env = env
	cmd.Stdin = strings.NewReader(options.Prompt)
	cmd.ExtraFiles = []*os.File{options.Lock, reader}
	output := NewLines(observe, cancel)
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	var cancellationUnverified atomic.Bool
	cmd.Cancel = func() error {
		if options.CancelProcess != nil {
			e := options.CancelProcess(cmd.Process.Pid)
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
		return phase, started, fmt.Errorf("conversation provider launch failed: %w", err)
	}
	started = true
	phase = "ownership_persistence"
	e := options.OnStarted(cmd.Process.Pid)
	if e == nil {
		_, e = writer.WriteString("ready\n")
	}
	_ = writer.Close()
	if e != nil {
		cancel()
	}
	err = cmd.Wait()
	options.OnExited(cancellationUnverified.Load())
	if e != nil {
		return phase, started, fmt.Errorf("conversation ownership persistence failed: %w", e)
	}
	phase = "process_wait"
	if runCtx.Err() != nil {
		return phase, started, runCtx.Err()
	}
	if err != nil {
		return phase, started, fmt.Errorf("conversation provider process failed: %w", err)
	}
	return phase, started, output.Finish()
}
