// Package providerprocess owns bounded subprocess I/O shared by isolated provider adapters.
// It does not grant or certify provider capabilities.
package providerprocess

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

// ErrProviderExit marks a completed native process that returned a nonzero exit status.
// It does not imply availability; adapters must examine native terminal diagnostics.
var ErrProviderExit = errors.New("provider exited unsuccessfully")

// RunDoctorProcess is transport only, not an isolation attestation. Provider
// adapters must establish their tool ceiling before calling it. Output is
// consumed as bounded JSON lines; stderr is discarded rather than disclosing
// provider diagnostics that may contain credentials or unrelated local paths,
// unless the caller supplies a [FailureObserver] (see the Observed variants).
// An optional adapter-owned cancellation callback handles native shells that
// escape the provider's process group; it receives only the owned provider PID.
func RunDoctorProcess(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, timeout time.Duration, observe func([]byte), cancelProcess ...func(int) error) error {
	var cancel func(int) error
	if len(cancelProcess) > 0 {
		cancel = cancelProcess[0]
	}
	return RunDoctorProcessObserved(ctx, binary, args, env, request, timeout, observe, nil, cancel)
}

// RunDoctorProcessObserved is [RunDoctorProcess] with a failure observer.
func RunDoctorProcessObserved(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, timeout time.Duration, observe func([]byte), failure FailureObserver, cancelProcess func(int) error) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request, timeout, observe, failure, cancelProcess)
}

// OversizedObserver receives, once per judge run and after the process ends,
// the judge transport's report of output it skipped instead of ending the run:
// kind is "line_dropped" when at least one line over the line budget was
// discarded, otherwise "total_exceeded" when the run's stdout passed the
// non-judge total budget. Sizes are in bytes.
type OversizedObserver func(kind string, largestDroppedBytes, totalBytes int)

const (
	outputLineBudget  = 1024 * 1024
	outputTotalBudget = 8 * 1024 * 1024
)

// RunRepairProcess shares bounded I/O while validating the separate exact repair
// contract. Calling this function does not prove provider isolation or approval.
func RunRepairProcess(ctx context.Context, binary string, args, env []string, request agent.RepairRequest, timeout time.Duration, observe func([]byte)) error {
	return RunRepairProcessObserved(ctx, binary, args, env, request, timeout, observe, nil)
}

// RunRepairProcessObserved is [RunRepairProcess] with a failure observer.
func RunRepairProcessObserved(ctx context.Context, binary string, args, env []string, request agent.RepairRequest, timeout time.Duration, observe func([]byte), failure FailureObserver) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request.Invocation, timeout, observe, failure, nil)
}

// RunJudgeProcess validates the isolated advisory catalog before bounded I/O.
// An advisory judge does not need oversized output: a line over the line
// budget is discarded rather than ending the run, and the total budget does
// not apply; the timeout remains the bound. A non-nil oversized observer is
// told once, after the process ends, when output was skipped or passed the
// old total budget.
func RunJudgeProcess(ctx context.Context, binary string, args, env []string, request agent.JudgeRequest, timeout time.Duration, observe func([]byte), oversized OversizedObserver) error {
	return RunJudgeProcessObserved(ctx, binary, args, env, request, timeout, observe, nil, oversized)
}

// RunJudgeProcessObserved is [RunJudgeProcess] with a failure observer.
func RunJudgeProcessObserved(ctx context.Context, binary string, args, env []string, request agent.JudgeRequest, timeout time.Duration, observe func([]byte), failure FailureObserver, oversized OversizedObserver) error {
	if err := request.Validate(); err != nil {
		return err
	}
	output := &doctorLines{observe: observe, judge: true}
	err := runScopedOutputProcess(ctx, binary, args, env, request.Invocation.Prompt, request.Invocation.WorkingDirectory, timeout, output, failure, nil)
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

// FailureObserver receives a nonzero exit status and the bounded tail of the
// provider's standard error. Stderr never crosses this package boundary in a
// returned error; an observer that keeps it must redact it first and must not
// persist it. A nil observer discards stderr, as before.
type FailureObserver func(exitCode int, stderrTail []byte)

// stderrTailLimit bounds the stderr retained for a [FailureObserver].
const stderrTailLimit = 8 << 10

// stderrTail keeps the last stderrTailLimit bytes written to it.
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

// RunChatProcess shares bounded provider I/O for a response-only request. The
// provider adapter owns the verified gate and explicit no-tools launch flags.
func RunChatProcess(ctx context.Context, binary string, args, env []string, request agent.ChatRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, agent.DoctorRequest{Prompt: request.Prompt, WorkingDirectory: request.WorkingDirectory}, timeout, observe, nil, nil)
}

// RunDriftProcess is bounded transport; the provider owns restricted admission.
func RunDriftProcess(ctx context.Context, binary string, args, env []string, request agent.DriftRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request.Invocation, timeout, observe, nil, nil)
}

func runScopedDoctorProcess(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, timeout time.Duration, observe func([]byte), failure FailureObserver, cancelProcess func(int) error) error {
	return runScopedProcessObserved(ctx, binary, args, env, request.Prompt, request.WorkingDirectory, timeout, observe, failure, cancelProcess)
}

// runScopedProcess owns bounded I/O and process-group cancellation without a
// domain request or authority. The public entry point validates its own contract.
func runScopedProcess(ctx context.Context, binary string, args, env []string, prompt, directory string, timeout time.Duration, observe func([]byte), cancelProcess ...func(int) error) error {
	var cancel func(int) error
	if len(cancelProcess) > 0 {
		cancel = cancelProcess[0]
	}
	return runScopedProcessObserved(ctx, binary, args, env, prompt, directory, timeout, observe, nil, cancel)
}

func runScopedProcessObserved(ctx context.Context, binary string, args, env []string, prompt, directory string, timeout time.Duration, observe func([]byte), failure FailureObserver, cancelProcess func(int) error) error {
	return runScopedOutputProcess(ctx, binary, args, env, prompt, directory, timeout, &doctorLines{observe: observe}, failure, cancelProcess)
}

func runScopedOutputProcess(ctx context.Context, binary string, args, env []string, prompt, directory string, timeout time.Duration, output *doctorLines, failure FailureObserver, cancelProcess func(int) error) error {
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
		err = output.finish()
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

type doctorLines struct {
	pending []byte
	total   int
	observe func([]byte)
	cancel  context.CancelFunc
	// err is the guard's own error once it has cancelled the run.
	err error
	// judge discards oversized lines and ignores the total budget.
	judge          bool
	discarding     int
	dropped        bool
	largestDropped int
}

func (s *doctorLines) stop(err error) (int, error) {
	if s.err == nil {
		s.err = err
	}
	if s.cancel != nil {
		s.cancel()
	}
	return 0, err
}

func (s *doctorLines) Write(data []byte) (int, error) {
	if !s.judge && len(data) > outputTotalBudget-s.total {
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
		if s.judge && (s.discarding > 0 || len(s.pending)+size > outputLineBudget) {
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
		if len(s.pending)+size > outputLineBudget {
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
func (s *doctorLines) endDiscard() {
	s.dropped = true
	s.largestDropped = max(s.largestDropped, s.discarding)
	s.discarding = 0
}

func (s *doctorLines) finish() error {
	if s.discarding > 0 {
		s.endDiscard()
	}
	if len(bytes.TrimSpace(s.pending)) != 0 {
		return errors.New("doctor provider ended with an incomplete output record")
	}
	return nil
}

// DoctorEnvironment discards ambient provider settings, factory/forge secrets,
// proxies and user paths. A provider may explicitly append its own auth secret;
// neither the result broker nor prepared context receives that credential.
// USER and LOGNAME stay: Claude names its keychain login after the user, and
// without them it keeps a separate login under "unknown" that workflow runs
// and an owner's /login never refresh.
func DoctorEnvironment(parent []string, privateHome string) []string {
	result := []string{"HOME=" + privateHome, "TMPDIR=" + privateHome}
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if ok && (key == "PATH" || key == "LANG" || key == "LC_ALL" || key == "USER" || key == "LOGNAME") {
			result = append(result, item)
		}
	}
	return result
}
