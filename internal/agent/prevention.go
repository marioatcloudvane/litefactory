package agent

import (
	"errors"
	"slices"
)

// PreventionRequest is a standalone advisory invocation with no workflow authority.
type PreventionRequest struct {
	Invocation DoctorRequest
	MaxTurns   int
}

// PreventionToolNames is the complete retained-data-only analyst catalog.
func PreventionToolNames() []string {
	return []string{"communication_preflight", "communication_receipt", "communication_submit", "evidence_read", "standards_categories", "standards_read", "standards_select", "standards_status"}
}

// Validate forbids inherited tools and unbounded provider turns.
func (r PreventionRequest) Validate() error {
	if err := r.Invocation.validateContext(); err != nil {
		return err
	}
	got := r.Invocation.MCPLaunch.PermittedTools()
	slices.Sort(got)
	if !slices.Equal(got, PreventionToolNames()) || r.MaxTurns < 1 || r.MaxTurns > 24 {
		return errors.New("invalid prevention analyst tool or turn ceiling")
	}
	return nil
}
