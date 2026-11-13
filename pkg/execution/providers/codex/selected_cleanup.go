package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// waitSelectedProcess waits independently from output: native descendants can
// retain pipe descriptors after the CLI exits. Only the group created for this
// command is signalled; detached shell commands retain the existing cancellation
// sweep boundary. The captured direct-child exit status remains authoritative.
func waitSelectedProcess(cmd *exec.Cmd, guardian *selectedGroupGuardian, stdout io.ReadCloser, stderr *os.File, capture io.Writer) (<-chan error, func()) {
	result := make(chan error, 1)
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() { _, _ = io.Copy(capture, stderr); _ = stderr.Close(); close(stderrDone) }()
	go func() {
		err := cmd.Wait()
		err = errors.Join(err, guardian.stop())
		// A detached process can retain output descriptors despite group cleanup.
		// Bound the drain without changing the ordinary provider path.
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		outDone, errDone := stdoutDone, stderrDone
		for outDone != nil || errDone != nil {
			select {
			case <-outDone:
				outDone = nil
			case <-errDone:
				errDone = nil
			case <-timer.C:
				_ = stdout.Close()
				_ = stderr.Close()
				if outDone != nil {
					<-outDone
				}
				if errDone != nil {
					<-errDone
				}
				outDone, errDone = nil, nil
				err = errors.Join(err, errors.New("native output remained open after process shutdown"))
			}
		}
		_ = stdout.Close()
		result <- err
	}()
	return result, func() { close(stdoutDone) }
}

// The unreaped group leader reserves its PID until every selected-group signal
// has been sent, preventing normal CLI exit from turning cleanup into a signal
// to a newly reused process-group ID.
type selectedGroupGuardian struct {
	command *exec.Cmd
	input   *os.File
}

func startSelectedGroupGuardian(ctx context.Context) (*selectedGroupGuardian, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := Command(context.WithoutCancel(ctx), "/bin/sh", "-c", "read -r unused")
	cmd.Env = []string{}
	cmd.Stdin = reader
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, err
	}
	_ = reader.Close()
	return &selectedGroupGuardian{command: cmd, input: writer}, nil
}

func (g *selectedGroupGuardian) stop() error {
	self, err := syscall.Getpgid(0)
	if err == nil && g.command.Process.Pid > 1 && g.command.Process.Pid != self {
		err = syscall.Kill(-g.command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			err = nil
		}
	}
	_ = g.input.Close()
	// Waiting only now preserves the process-group identity until its last signal.
	_ = g.command.Wait()
	return err
}
