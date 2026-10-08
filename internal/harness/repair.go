package harness

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
	"time"
)

// DoctorRepairIsolation stays closed until the typed repair route is wired.
// The scoped launcher disables native tools, but transport alone is not readiness.
func (r *Runner) DoctorRepairIsolation(context.Context) (string, error) {
	return "", agent.ErrDoctorIsolationUnverified
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
	if operation == "" {
		return request.Validate()
	}
	return (agent.RepairRequest{Invocation: request, Operation: operation}).Validate()
}

func runScopedDoctorProcess(ctx context.Context, binary string, args, env []string, request agent.DoctorRequest, operation string, timeout time.Duration, observe func([]byte)) error {
	if operation == "" {
		return providerprocess.RunDoctorProcess(ctx, binary, args, env, request, timeout, observe)
	}
	return providerprocess.RunRepairProcess(ctx, binary, args, env, agent.RepairRequest{Invocation: request, Operation: operation}, timeout, observe)
}
