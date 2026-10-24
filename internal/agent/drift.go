package agent

import (
	"context"
	"errors"
)

// DriftRequest is a standalone, one-shot evidence analyst, never a workflow writer.
type DriftRequest struct {
	Invocation DoctorRequest
	// OnInitialized admits broker work only after the exact native catalog is observed.
	OnInitialized func(DoctorRunResult) error
}

// DriftToolNames is the complete analyst catalog. No native tool is permitted.
func DriftToolNames() []string {
	return []string{"communication_preflight", "communication_receipt", "communication_submit", "drift_evidence_list", "drift_evidence_read", "standards_categories_list", "standards_read", "standards_select"}
}

// Validate enforces the standalone context and exact analyst tool catalog.
func (r DriftRequest) Validate() error {
	if err := r.Invocation.validateContext(); err != nil {
		return err
	}
	return validateDoctorTools(r.Invocation.MCPLaunch, DriftToolNames())
}

// ErrDriftIsolation refuses an unverified restricted provider launch.
var ErrDriftIsolation = errors.New("drift analyst provider cannot verify the restricted launch and catalog")

// DriftRunner must establish the dedicated profile independently of doctor admission.
type DriftRunner interface {
	DriftIsolation(context.Context) (string, error)
	RunDrift(context.Context, DriftRequest) (DoctorRunResult, error)
}
