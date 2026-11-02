package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/cimonitor"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/submission"
)

// CIProber is restricted to configured-target reads and transport timing writes.
type CIProber interface {
	Probe(context.Context) (cimonitor.ProbeResult, error)
}

func (s *ResultService) ciAccess(ctx context.Context, payload []byte) (json.RawMessage, error) {
	var input struct{}
	if err := submission.DecodeJSON(payload, &input); err != nil {
		return nil, errors.New("CI access accepts no target or options")
	}
	if s.ciProbe == nil {
		return nil, errors.New("configured CI access diagnostic unavailable")
	}
	if !ciFactoryContext(s.binding.Task, s.ciRepository) {
		return nil, errors.New("CI access diagnostic requires the configured factory repository context")
	}
	scan, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result, err := s.ciProbe.Probe(scan)
	if err != nil && result.Reason == "" {
		return nil, errors.New("CI access diagnostic unavailable; inspect saved CI status and next retry")
	}
	switch result.WorkflowState {
	case "", "active", "disabled_manually", "disabled_inactivity", "disabled_fork", "deleted":
	default:
		result.WorkflowState = "unknown"
	}
	return json.Marshal(result)
}

func ciFactoryContext(task, repository string) bool {
	ref, err := naming.ParseRef(task)
	return err == nil && ref.Forge == naming.ForgeGitHub && ref.Qualification == "" && repository != "" && strings.EqualFold(ref.Owner+"/"+ref.Repo, repository)
}
