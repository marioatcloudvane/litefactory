package agent

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// ErrDoctorIsolationUnverified forbids launching a diagnosis with unknown tools.
var ErrDoctorIsolationUnverified = errors.New("doctor provider isolation lacks authentic versioned verification")

// DoctorRequest is mode-free: no workflow run, session resume or permissions from
// ordinary work can be carried into the private diagnosis invocation.
type DoctorRequest struct {
	Attempt          string
	Model            string
	Prompt           string
	WorkingDirectory string
	UsageAttemptID   string
	Usage            *UsageCapture
	MCPLaunch        *MCPLaunchSpec
	Sandbox          *DoctorSandbox
}

// Validate enforces the only permitted diagnosis communication surface.
func (r DoctorRequest) Validate() error {
	if err := r.validateContext(); err != nil {
		return err
	}
	if err := r.Sandbox.Validate(r.WorkingDirectory); err != nil {
		return err
	}
	return validateDoctorTools(r.MCPLaunch, DoctorToolNames())
}

// DoctorToolNames is the exact diagnosis-only broker catalog.
func DoctorToolNames() []string {
	return []string{"communication_preflight", "communication_receipt", "communication_submit", "database_query", "issue_comments"}
}

func (r DoctorRequest) validateContext() error {
	if r.Attempt == "" || len(r.Attempt) > 256 || strings.TrimSpace(r.Model) == "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 524288 || !filepath.IsAbs(r.WorkingDirectory) || filepath.Clean(r.WorkingDirectory) != r.WorkingDirectory {
		return errors.New("invalid doctor invocation context")
	}
	if err := r.MCPLaunch.Validate(); err != nil {
		return err
	}
	if r.MCPLaunch.AttemptReference() != r.Attempt {
		return errors.New("doctor MCP attempt mismatch")
	}
	if r.Usage != nil && (r.UsageAttemptID == "" || r.Usage.AttemptID != r.UsageAttemptID) {
		return errors.New("doctor usage attempt mismatch")
	}
	return nil
}

func validateDoctorTools(launch *MCPLaunchSpec, want []string) error {
	got := launch.PermittedTools()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return errors.New("doctor requires exactly its scoped tool catalog")
	}
	return nil
}

// DoctorRunResult contains provider observations, never an application diagnosis.
type DoctorRunResult struct {
	ObservedModel, ProviderVersion string
	Usage                          *UsageCoverage
}

// DoctorRunner is the provider-owned isolated execution boundary. Missing
// verification refuses before process launch; callers cannot request a downgrade.
type DoctorRunner interface {
	DoctorIsolation(context.Context) (string, error)
	RunDoctor(context.Context, DoctorRequest) (DoctorRunResult, error)
}
