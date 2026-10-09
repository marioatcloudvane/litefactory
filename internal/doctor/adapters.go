package doctor

import (
	"context"
	"errors"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

// Adapters connects provider-owned isolation to the shared doctor broker.
// Only factory wiring installs adapters; no configuration or agent text can
// manufacture an isolation attestation or substitute an ordinary workflow runner.
type Adapters struct {
	Providers  map[string]agent.DoctorRunner
	Paths      config.Paths
	Executable string
}

// IsolationEvidence refuses unknown providers and delegates version certification.
func (a *Adapters) IsolationEvidence(ctx context.Context, provider string) (string, error) {
	runner := a.Providers[provider]
	if runner == nil {
		return "", ErrIsolationUnavailable
	}
	evidence, err := runner.DoctorIsolation(ctx)
	if err != nil {
		return "", errors.Join(ErrIsolationUnavailable, err)
	}
	if evidence == "" {
		return "", ErrIsolationUnavailable
	}
	return evidence, nil
}

// Run owns the broker until the provider has ended; Close joins every tool call.
// Only a retained receipt supplies application identity, never provider terminal text.
func (a *Adapters) Run(ctx context.Context, invocation Invocation, results *ResultService) (_ InvocationOutcome, err error) {
	runner := a.Providers[invocation.Provider]
	if runner == nil {
		return InvocationOutcome{}, ErrIsolationUnavailable
	}
	if _, err := a.IsolationEvidence(ctx, invocation.Provider); err != nil {
		return InvocationOutcome{}, err
	}
	if err := os.MkdirAll(a.Paths.SubmissionDir(), 0700); err != nil {
		return InvocationOutcome{}, err
	}
	broker, err := StartResultsBroker(ctx, a.Paths.SubmissionDir(), invocation.ID, results)
	if err != nil {
		return InvocationOutcome{}, err
	}
	defer func() { err = errors.Join(err, broker.Close()) }()
	spec, err := agent.NewMCPLaunchSpec(a.Executable, broker.DescriptorPath(), invocation.ID, agent.DoctorToolNames()...)
	if err != nil {
		return InvocationOutcome{}, err
	}
	operatorHome, err := os.UserHomeDir()
	if err != nil {
		return InvocationOutcome{}, err
	}
	profile := &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: invocation.WorkingDirectory, ReadableRoots: []string{"/"}, DeniedReadPaths: a.Paths.DoctorCredentialPaths(operatorHome)}
	request := agent.DoctorRequest{Attempt: invocation.ID, Model: invocation.ConfiguredModel, Prompt: invocation.Prompt, WorkingDirectory: invocation.WorkingDirectory, UsageAttemptID: invocation.MeterAttempt.ID, MCPLaunch: spec, Usage: invocation.Usage, Sandbox: profile}
	if err := request.Validate(); err != nil {
		return InvocationOutcome{}, err
	}
	outcome, err := runner.RunDoctor(ctx, request)
	native := InvocationOutcome{ObservedModel: outcome.ObservedModel, ProviderVersion: outcome.ProviderVersion, Usage: outcome.Usage}
	if err != nil {
		return native, err
	}
	id, err := results.completedRequest(ctx)
	if err != nil {
		return native, err
	}
	native.RequestID = id
	return native, nil
}

func (s *ResultService) completedRequest(ctx context.Context) (string, error) {
	refs, err := s.db.ResultRequestReferences(ctx, s.binding)
	if err != nil {
		return "", err
	}
	for _, ref := range refs {
		if ref.InputState != "accepted" {
			continue
		}
		receipt, err := s.Receipt(ctx, ref.RequestID)
		if err != nil {
			return "", err
		}
		if receipt.Attempt == s.binding.Attempt {
			return receipt.RequestID, nil
		}
	}
	return "", errors.New("doctor provider ended without an accepted invocation result")
}
