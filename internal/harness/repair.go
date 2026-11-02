package harness

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
	"log/slog"
	"time"
)

// DoctorRepairIsolation identifies the scoped launcher, which disables native tools.
func (r *Runner) DoctorRepairIsolation(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "typed-repair-broker-only", nil
}

// RunDoctorRepair validates the factory-bound manifest operation before the
// repair-specific isolation gate. It cannot fall back to a workflow invocation.
func (r *Runner) RunDoctorRepair(ctx context.Context, request agent.RepairRequest) (agent.DoctorRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if _, err := r.DoctorRepairIsolation(ctx); err != nil {
		return agent.DoctorRunResult{}, err
	}
	return r.runRepairTransport(ctx, request)
}

func (r *Runner) runRepairTransport(ctx context.Context, request agent.RepairRequest) (agent.DoctorRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	return r.runScopedDoctorTransport(ctx, request.Invocation, request.Operation)
}

func validateScopedDoctorRequest(request agent.DoctorRequest, operation string) error {
	if operation == "judge" {
		return (agent.JudgeRequest{Invocation: request, Thinking: request.Thinking}).Validate()
	}
	if operation == "" {
		return request.Validate()
	}
	return (agent.RepairRequest{Invocation: request, Operation: operation}).Validate()
}

func runScopedDoctorProcess(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, operation string, timeout time.Duration, observe func([]byte), failure execution.FailureObserver, oversized execution.OversizedObserver, logger *slog.Logger) error {
	if err := validateScopedDoctorRequest(request, operation); err != nil {
		return err
	}
	if operation == "" {
		if logger == nil {
			logger = slog.New(slog.DiscardHandler)
		}
		return native.RunBounded(ctx, binary, args, env, request.Prompt, request.WorkingDirectory, timeout, observe, failure, func(pid int) error { return execution.KillSubtree(context.WithoutCancel(ctx), pid, logger) }, false, nil)
	}
	return native.RunBounded(ctx, binary, args, env, request.Prompt, request.WorkingDirectory, timeout, observe, failure, nil, operation == "judge", oversized)
}
