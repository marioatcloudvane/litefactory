package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// RepairRequest is bound by the factory to one approved operation. Its catalog
// differs from diagnosis, but adds no native filesystem, SQL or lifecycle tool.
// The broker resolves every numbered step from the immutable approved manifest.
type RepairRequest struct {
	Invocation DoctorRequest
	Operation  string
}

// TypedRepairToolKinds is the closed catalog of approved factory repairs.
func TypedRepairToolKinds() []string {
	return []string{"cancel_schedule", "consume_comment", "park_story", "release_instance", "requeue_schedule", "resume_story", "stop_run", "sync_branch"}
}

// RepairToolNames returns exactly the typed repairs and scoped communication.
func RepairToolNames() []string {
	names := append(TypedRepairToolKinds(), "communication_preflight", "communication_receipt", "communication_submit")
	slices.Sort(names)
	return names
}

// Validate binds repair transport to a bounded operation identity and exact tools.
func (r RepairRequest) Validate() error {
	if strings.TrimSpace(r.Operation) == "" || len(r.Operation) > 256 || strings.ContainsAny(r.Operation, "\x00\r\n\t") {
		return errors.New("invalid doctor repair operation identity")
	}
	if err := r.Invocation.validateContext(); err != nil {
		return err
	}
	return validateDoctorTools(r.Invocation.MCPLaunch, RepairToolNames())
}

// DoctorRepairRunner has a distinct verification gate: diagnosis certification
// never authorizes writing tools or constitutes approval of a repair operation.
type DoctorRepairRunner interface {
	DoctorRepairIsolation(context.Context) (string, error)
	RunDoctorRepair(context.Context, RepairRequest) (DoctorRunResult, error)
}
