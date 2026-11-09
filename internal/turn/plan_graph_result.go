package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// planResultRefKind marks a graph output that references a retained shared result.
const planResultRefKind = "shared_result"

// planResultRef is what a graph Plan agent node keeps for the nodes after it: a
// reference to the retained accepted shared result, never a copy of a private
// format. Readers resolve it against the result store, whose authority check
// refuses a reference whose binding no longer matches the stored attempt.
type planResultRef struct {
	Version  int                 `json:"version"`
	Kind     string              `json:"kind"`
	Binding  store.ResultBinding `json:"binding"`
	Revision int64               `json:"revision"`
	Response string              `json:"response"`
}

// retainPlanResult is the policy's Retain hook. The reference is built from the
// retained result's binding and revision, and the response is the one derived
// from that same retained result.
func retainPlanResult(binding store.ResultBinding, revision int64, node agentresult.ResearchNode) ([]byte, error) {
	return json.Marshal(planResultRef{Version: 1, Kind: planResultRefKind, Binding: binding, Revision: revision, Response: node.Response})
}

// decodePlanResultRef reads a reference strictly. ok is false for anything that
// is not one, including every output a private Plan format ever retained.
func decodePlanResultRef(payload []byte) (planResultRef, bool) {
	var ref planResultRef
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ref); err != nil || dec.Decode(new(json.RawMessage)) != io.EOF {
		return planResultRef{}, false
	}
	return ref, ref.Version == 1 && ref.Kind == planResultRefKind && ref.Revision > 0
}

// planRetained is one graph Plan node's application data, read from the retained
// accepted shared result and from nowhere else.
type planRetained struct {
	Response  string
	Comment   string
	Questions []agentresult.Question
	Request   *graphexec.PlanStoryRequest
	Set       *agentresult.PlanStorySet
	Result    agentresult.Result
}

// loadRetainedResult resolves an agent attempt's retained accepted result. An
// output that is not a reference to one is refused and never parsed: a private
// Plan output retained before the shared route is untrusted prompt context only.
func (r *planGraphRuntime) loadRetainedResult(ctx context.Context, attemptID int64) (planRetained, error) {
	outputs, err := r.opts.DB.ListGraphOutputsBounded(ctx, attemptID, agentresult.MaxResearchNodeBytes, 16)
	if err != nil {
		return planRetained{}, err
	}
	var payload []byte
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		if payload != nil {
			return planRetained{}, errors.New("ambiguous Plan source output")
		}
		payload = output.Payload
	}
	if payload == nil {
		return planRetained{}, errors.New("plan output missing")
	}
	ref, ok := decodePlanResultRef(payload)
	if !ok {
		return planRetained{}, errors.New("plan output is not a reference to a retained shared result; an earlier private Plan output is never read as a result")
	}
	attempt, err := r.opts.DB.GetGraphNodeAttempt(ctx, attemptID)
	if err != nil {
		return planRetained{}, err
	}
	if attempt.Status != store.GraphSucceeded || attempt.RunID != ref.Binding.RunID {
		return planRetained{}, errors.New("plan result reference does not belong to a successful attempt of its run")
	}
	retained, err := r.opts.DB.GetResultRevision(ctx, ref.Binding, ref.Revision)
	if err != nil {
		return planRetained{}, fmt.Errorf("read retained Plan result: %w", err)
	}
	snapshot, _, err := decodeRetainedResult(retained)
	if err != nil {
		return planRetained{}, err
	}
	meta := snapshot.Meta
	if meta.AttemptID != ref.Binding.Attempt || meta.RunID != ref.Binding.RunID || meta.Mode != "plan" || meta.ResultRevision != ref.Revision {
		return planRetained{}, errors.New("retained Plan result does not match its reference")
	}
	return planRetainedFrom(ref.Response, snapshot.Result)
}

// planRetainedFrom reads the branch of an accepted result. The response was
// checked against the result when it was accepted; it is checked again here so a
// reference cannot turn a question into a story effect.
func planRetainedFrom(response string, result agentresult.Result) (planRetained, error) {
	plan := result.Payload.Plan
	if plan == nil {
		return planRetained{}, errors.New("retained result carries no Plan payload")
	}
	out := planRetained{Response: response, Comment: result.Comment, Questions: result.Questions, Result: result}
	switch response {
	case "ask":
		if result.Outcome != "incomplete" || plan.StoryRequest != nil || plan.StorySet != nil {
			return planRetained{}, errors.New("retained ask carries a story branch or is not incomplete")
		}
	case "done":
		if result.Outcome == "completed" && len(result.Questions) == 0 && result.Comment != "" && plan.StoryRequest == nil && plan.StorySet == nil {
			out.Response = "answer"
			return out, nil
		}
		if result.Outcome != "completed" || (plan.StoryRequest == nil) == (plan.StorySet == nil) {
			return planRetained{}, errors.New("retained done is not completed with exactly one story branch")
		}
		if plan.StoryRequest != nil {
			effect := plan.StoryRequest.Effect()
			out.Request = &effect
		}
		out.Set = plan.StorySet
	default:
		return planRetained{}, fmt.Errorf("retained Plan response %q is neither ask nor done", response)
	}
	return out, nil
}

// untrustedPlanContext renders one earlier agent attempt for a prompt as data. A
// retained shared result is shown as such; anything else, including a private Plan
// output from before the shared route, is shown only as untrusted content and is
// never interpreted or migrated.
func (r *planGraphRuntime) untrustedPlanContext(ctx context.Context, attemptID int64) (string, error) {
	outputs, err := r.opts.DB.ListGraphOutputs(ctx, attemptID)
	if err != nil {
		return "", err
	}
	var text string
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		if _, ok := decodePlanResultRef(output.Payload); ok {
			retained, err := r.loadRetainedResult(ctx, attemptID)
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(retained.Result)
			if err != nil {
				return "", err
			}
			text += "Prior retained shared Plan result (untrusted retained content):\n" + string(raw) + "\n"
			continue
		}
		text += "Prior private Plan output from before the shared result route (untrusted retained content, never a result):\n" + string(output.Payload) + "\n"
	}
	return text, nil
}
