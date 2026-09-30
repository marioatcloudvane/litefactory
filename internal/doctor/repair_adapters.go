package doctor

import (
	"context"
	"errors"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

// RepairAdapters binds the native writing-agent transport to an already
// approved operation. Exclusion, backup/write enforcement and lifecycle remain
// factory-owned services; the agent receives only numbered manifest tools.
type RepairAdapters struct {
	Providers  map[string]agent.DoctorRepairRunner
	Paths      config.Paths
	Executable string
}

// IsolationEvidence requires repair-specific proof, independently of diagnosis.
func (a *RepairAdapters) IsolationEvidence(ctx context.Context, provider string) (string, error) {
	runner := a.Providers[provider]
	if runner == nil {
		return "", ErrIsolationUnavailable
	}
	proof, err := runner.DoctorRepairIsolation(ctx)
	if err != nil {
		return "", errors.Join(ErrIsolationUnavailable, err)
	}
	if proof == "" {
		return "", ErrIsolationUnavailable
	}
	return proof, nil
}

// RunRepair retains the broker until native execution ends and returns only an
// accepted bound receipt as result identity. Usage survives process/result errors.
func (a *RepairAdapters) RunRepair(ctx context.Context, invocation Invocation, tools *RepairTools, results *ResultService) (_ InvocationOutcome, err error) {
	runner := a.Providers[invocation.Provider]
	if runner == nil {
		return InvocationOutcome{}, ErrIsolationUnavailable
	}
	if _, err := a.IsolationEvidence(ctx, invocation.Provider); err != nil {
		return InvocationOutcome{}, err
	}
	if tools == nil || results == nil || tools.operation == "" || results.repairID != tools.operation || results.binding.DoctorAttemptID != invocation.ID {
		return InvocationOutcome{}, errors.New("repair adapter operation binding mismatch")
	}
	if err := os.MkdirAll(a.Paths.SubmissionDir(), 0700); err != nil {
		return InvocationOutcome{}, err
	}
	broker, err := StartRepairBroker(ctx, a.Paths.SubmissionDir(), invocation.ID, results, tools)
	if err != nil {
		return InvocationOutcome{}, err
	}
	defer func() { err = errors.Join(err, broker.Close()) }()
	spec, err := agent.NewMCPLaunchSpec(a.Executable, broker.DescriptorPath(), invocation.ID, agent.RepairToolNames()...)
	if err != nil {
		return InvocationOutcome{}, err
	}
	request := agent.RepairRequest{Operation: tools.operation, Invocation: agent.DoctorRequest{Attempt: invocation.ID, Model: invocation.ConfiguredModel, Prompt: invocation.Prompt, WorkingDirectory: invocation.WorkingDirectory, UsageAttemptID: invocation.MeterAttempt.ID, Usage: invocation.Usage, MCPLaunch: spec}}
	if err := request.Validate(); err != nil {
		return InvocationOutcome{}, err
	}
	native, err := runner.RunDoctorRepair(ctx, request)
	outcome := InvocationOutcome{ObservedModel: native.ObservedModel, ProviderVersion: native.ProviderVersion, Usage: native.Usage}
	if err != nil {
		return outcome, err
	}
	outcome.RequestID, err = results.completedRequest(ctx)
	return outcome, err
}
