package harness

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
	"log/slog"
	"time"
)

func validateScopedDoctorRequest(request agent.DoctorRequest, operation string) error {
	if operation == "judge" {
		return (agent.JudgeRequest{Invocation: request, Thinking: request.Thinking}).Validate()
	}
	if operation == "" {
		return request.Validate()
	}
	return errors.New("unsupported scoped provider operation")
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
