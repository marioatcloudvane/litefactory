package turn

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// FeedbackService routes proposed input to its existing validation owner. It
// neither retains a proposal nor allocates domain submissions, proof or grants.
type FeedbackService struct {
	application MCPResultService
	evidence    *EvidenceService
}

// NewFeedbackService composes shared validators with matching optional checkpoint scope.
func NewFeedbackService(application MCPResultService, evidence *EvidenceService) (*FeedbackService, error) {
	if application == nil {
		return nil, errors.New("feedback requires shared application validator")
	}
	b := application.Binding()
	if b.Mode == "implement" || b.Mode == "validate" {
		if evidence == nil || evidence.Binding() != b {
			return nil, errors.New("feedback requires matching checkpoint validator")
		}
	} else if evidence != nil {
		return nil, errors.New("checkpoint validator is not permitted in this mode")
	}
	return &FeedbackService{application: application, evidence: evidence}, nil
}

// newFeedbackServiceWithoutEvidence is the feedback service of an Implement story
// worker of the graph, which collects no engineering evidence: its result is
// checked once, when it is submitted, against its one story.
func newFeedbackServiceWithoutEvidence(application MCPResultService) (*FeedbackService, error) {
	if application == nil || application.Binding().Mode != "implement" {
		return nil, errors.New("feedback without checkpoints is only for an Implement story worker")
	}
	return &FeedbackService{application: application}, nil
}

// Binding returns the immutable application scope used by the family dispatcher.
func (s *FeedbackService) Binding() store.ResultBinding { return s.application.Binding() }

// PermissionMaterial binds the shared application and checkpoint validation scope.
func (s *FeedbackService) PermissionMaterial() ([]byte, error) {
	var checkpoint []byte
	var err error
	if s.evidence != nil {
		checkpoint, err = s.evidence.PermissionMaterial()
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(struct {
		Application string
		Checkpoint  json.RawMessage
	}{s.application.MCPPermissionDigest(), checkpoint})
}

// HandlePreflight strictly decodes only routing. Each domain validates its own
// input again at submission; successful preflight conveys no authority.
func (s *FeedbackService) HandlePreflight(ctx context.Context, _ string, arguments []byte) (json.RawMessage, error) {
	var q struct {
		Kind  string          `json:"kind"`
		Input json.RawMessage `json:"input"`
	}
	if err := decodeMCPArguments(arguments, &q); err != nil {
		return nil, err
	}
	if len(q.Input) == 0 || string(q.Input) == "null" {
		return nil, errors.New("feedback input is required")
	}
	switch q.Kind {
	case "application":
		result, err := s.application.Preflight(ctx, q.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "checkpoint":
		if s.evidence == nil {
			return nil, errors.New("checkpoint preflight is not permitted in this mode")
		}
		result, err := s.evidence.Preflight(ctx, q.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "creation":
		var envelope submission.CreationEnvelope
		if err := decodeMCPArguments(q.Input, &envelope); err != nil {
			return nil, err
		}
		_, _, validationErr := submission.CanonicalCreation(envelope)
		if validationErr == nil {
			ref, err := naming.ParseRef(s.Binding().Task)
			if err != nil {
				return nil, err
			}
			if envelope.SourceTask != s.Binding().Task || envelope.Repository != ref.Owner+"/"+ref.Repo {
				validationErr = errors.New("creation must match the bound source and repository")
			}
		}
		result := struct {
			Valid       bool     `json:"valid"`
			Diagnostics []string `json:"diagnostics"`
		}{Valid: validationErr == nil, Diagnostics: []string{}}
		if validationErr != nil {
			result.Diagnostics = append(result.Diagnostics, validationErr.Error())
		}
		return json.Marshal(result)
	default:
		return nil, errors.New("unknown feedback kind")
	}
}
