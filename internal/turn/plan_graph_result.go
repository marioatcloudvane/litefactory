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

// planNodeGuidance is what every graph Plan invocation carries in its persistent
// instruction channel: the terminal envelope in one line, the two branch rules the
// validator enforces, and the fact that the terminal object confirms a submitted
// result and never supplies one.
const planNodeGuidance = "\nFactory invocation: private Plan graph node. Submit the complete application result with the shared result helper or permitted communication tools, then return exactly one JSON object {\"version\":1,\"response\":\"ask\"|\"done\",\"result\":{...}} whose result is exactly the one you submitted. ask requires an incomplete result with a nonblank reason and nonempty owner questions, and neither storyrequest nor storyset. done requires a completed result without questions and exactly one of result.payload.plan.storyrequest (create, update or retire the factory-selected story) or result.payload.plan.storyset (propose several stories). Never supply stage, candidate, request_id, upstream or shared_revision: the factory fills them. If a submit reply is lost, look up its receipt by request_id before retrying, and resend identical bytes only when no receipt exists: identical bytes keep the request identity and never create a second result. A corrected payload after a refusal gets a new request identity from the helper, so fix the reported diagnostics rather than resending. Do not change accepted content just to force a retry. The terminal object only confirms the retained accepted result and never supplies it; one with no accepted submission, or with a different result, fails the attempt. The entire object is limited to 1 MiB. Role output cannot approve a stage, promote anything or choose authority.\n"

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
// retained result's binding and revision; only the response, already checked for
// consistency with that result, comes from the envelope.
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
	Questions []string
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
	out := planRetained{Response: response, Comment: result.Comment, Result: result}
	for _, q := range result.Questions {
		out.Questions = append(out.Questions, q.Text)
	}
	switch response {
	case "ask":
		if result.Outcome != "incomplete" || plan.StoryRequest != nil || plan.StorySet != nil {
			return planRetained{}, errors.New("retained ask carries a story branch or is not incomplete")
		}
	case "done":
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
