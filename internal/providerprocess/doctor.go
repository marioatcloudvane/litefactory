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
// provider diagnostics that may contain credentials or unrelated local paths.
// An optional adapter-owned cancellation callback handles native shells that
// escape the provider's process group; it receives only the owned provider PID.
func RunDoctorProcess(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, timeout time.Duration, observe func([]byte), cancelProcess ...func(int) error) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request, timeout, observe, cancelProcess...)
}

// RunRepairProcess shares bounded I/O while validating the separate exact repair
// contract. Calling this function does not prove provider isolation or approval.
func RunRepairProcess(ctx context.Context, binary string, args, env []string, request agent.RepairRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request.Invocation, timeout, observe)
}

// RunJudgeProcess validates the isolated advisory catalog before bounded I/O.
func RunJudgeProcess(ctx context.Context, binary string, args, env []string, request agent.JudgeRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request.Invocation, timeout, observe)
}

// RunChatProcess shares bounded provider I/O for a response-only request. The
// provider adapter owns the verified gate and explicit no-tools launch flags.
func RunChatProcess(ctx context.Context, binary string, args, env []string, request agent.ChatRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, agent.DoctorRequest{Prompt: request.Prompt, WorkingDirectory: request.WorkingDirectory}, timeout, observe)
}

// RunDriftProcess is bounded transport; the provider owns restricted admission.
func RunDriftProcess(ctx context.Context, binary string, args, env []string, request agent.DriftRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request.Invocation, timeout, observe)
}

func runScopedDoctorProcess(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, timeout time.Duration, observe func([]byte), cancelProcess ...func(int) error) error {
	if timeout <= 0 {
		return errors.New("doctor provider timeout must be positive")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	//nolint:gosec // Resolved provider executable and adapter-owned arguments; owner text is stdin only.
	cmd := exec.CommandContext(runCtx, binary, args...)
	cmd.Dir = request.WorkingDirectory
	cmd.Env = env
	cmd.Stdin = strings.NewReader(request.Prompt)
	output := &doctorLines{observe: observe, cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		// Native shells may leave this process group. The provider adapter can
		// supply its existing subtree shutdown, bound only to this owned PID.
		if len(cancelProcess) > 0 && cancelProcess[0] != nil {
			return cancelProcess[0](cmd.Process.Pid)
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
	if runCtx.Err() != nil {
		return runCtx.Err()
	}
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
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
}

func (s *doctorLines) Write(data []byte) (int, error) {
	if len(data) > 8*1024*1024-s.total {
		if s.cancel != nil {
			s.cancel()
		}
		return 0, errors.New("doctor output byte budget exceeded")
	}
	s.total += len(data)
	count := len(data)
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		size := len(data)
		if end >= 0 {
			size = end
		}
		if len(s.pending)+size > 1024*1024 {
			if s.cancel != nil {
				s.cancel()
			}
			return 0, errors.New("doctor output line budget exceeded")
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
func (s *doctorLines) finish() error {
	if len(bytes.TrimSpace(s.pending)) != 0 {
		return errors.New("doctor provider ended with an incomplete output record")
	}
	return nil
}

// DoctorEnvironment discards ambient provider settings, factory/forge secrets,
// proxies and user paths. A provider may explicitly append its own auth secret;
// neither the result broker nor prepared context receives that credential.
func DoctorEnvironment(parent []string, privateHome string) []string {
	result := []string{"HOME=" + privateHome, "TMPDIR=" + privateHome}
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if ok && (key == "PATH" || key == "LANG" || key == "LC_ALL") {
			result = append(result, item)
		}
	}
	return result
}

// RunPreventionProcess validates the exact advisory catalog before bounded I/O.
func RunPreventionProcess(ctx context.Context, binary string, args, env []string, request agent.PreventionRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return runScopedDoctorProcess(ctx, binary, args, env, request.Invocation, timeout, observe)
}
