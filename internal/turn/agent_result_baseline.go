package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// AgentResultBaseline is copied authorized data, never execution authority.
// Empty is explicit on cold launches. Retired identities are included only for
// a complete read grant, so a narrow reader cannot discover sibling identities.
type AgentResultBaseline struct {
	Empty      bool                           `json:"empty"`
	Projection agentresult.Projection         `json:"projection"`
	Requests   []store.ResultRequestReference `json:"requests"`
	Retired    agentresult.RetiredIDs         `json:"retired,omitempty"`
}

func resultBaselineTargets(grants []agentresult.Target) []agentresult.Target {
	targets := make([]agentresult.Target, 0, len(grants))
	for _, target := range grants {
		// Generic nested Plan grants are templates for selected stories. The full
		// stories field already includes these fields and their stable item IDs.
		if strings.HasPrefix(target.Field, "payload.plan.stories.") && target.Story == "" {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

func (s *AgentResultService) completeReadGrant() bool {
	for _, target := range resultBaselineTargets(agentresult.EditTargets(s.binding.Mode)) {
		if !slices.Contains(s.readTargets, target) {
			return false
		}
	}
	return true
}

// Baseline returns the current editable content within the explicit read scope.
// It reads persisted state on every call and never relies on provider sessions.
func (s *AgentResultService) Baseline(ctx context.Context) (AgentResultBaseline, error) {
	baseline := AgentResultBaseline{Empty: true, Projection: agentresult.Projection{ResultID: fmt.Sprintf("result-%d", s.binding.ResultID), Selections: []agentresult.ProjectedValue{}}}
	refs, err := s.db.ResultRequestReferences(ctx, s.binding)
	if err != nil {
		return baseline, err
	}
	baseline.Requests = refs
	retained, err := s.db.GetResultRevision(ctx, s.binding, 0)
	if errors.Is(err, store.ErrNotFound) {
		return baseline, nil
	}
	if err != nil {
		return baseline, err
	}
	snapshot, retired, err := decodeRetainedResult(retained)
	if err != nil {
		return baseline, err
	}
	projection, diagnostics := agentresult.Project(snapshot, resultBaselineTargets(s.readTargets), s.readTargets, agentresult.Context{Mode: s.binding.Mode, SchemaVersion: s.binding.SchemaVersion})
	if !diagnostics.Valid || !diagnostics.Complete {
		return baseline, resultDiagnosticError(diagnostics)
	}
	baseline.Empty, baseline.Projection = false, projection
	if s.completeReadGrant() {
		baseline.Projection.CompleteSnapshot = true
		baseline.Retired = retired
	}
	return baseline, nil
}
