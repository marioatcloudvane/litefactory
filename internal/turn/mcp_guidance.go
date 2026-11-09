package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// mcpGuidance renders only installed operations, as data. Provider
// configuration and this prompt use the same immutable catalog; neither invents
// permissions. How to use the operations is in the result-protocol skill.
func mcpGuidance(ctx context.Context, spec *agent.MCPLaunchSpec, service *AgentResultService, session *SubmissionSession) (string, error) {
	if spec == nil {
		return "", nil
	}
	if service == nil {
		return "", errors.New("MCP guidance requires bound result service")
	}
	names := slices.Sorted(slices.Values(spec.PermittedTools()))
	has := func(tools ...string) bool {
		for _, name := range tools {
			if !slices.Contains(names, name) {
				return false
			}
		}
		return true
	}
	var b strings.Builder
	b.WriteString("\n\n## Factory operations for this attempt\n\n")
	for _, name := range names {
		b.WriteString("- " + name + "\n")
	}
	application := has("communication_preflight", "communication_submit", "communication_receipt")
	checkpoint := has("feedback_preflight", "evidence_submit", "receipts_get") && session != nil && len(session.scope.Stories) > 0
	if checkpoint {
		contract, err := mcpCheckpointExample(session)
		if err != nil {
			return "", err
		}
		b.WriteString("\nCheckpoint C example (illustrative data):\n```json\n")
		b.Write(contract)
		b.WriteString("\n```\n")
	} else if application {
		contract, baseline, err := mcpApplicationExample(ctx, service)
		if err != nil {
			return "", err
		}
		if len(baseline) > 0 {
			b.WriteString("\nCurrent authorized comment baseline (data): ")
			b.Write(baseline)
			b.WriteByte('\n')
		}
		b.WriteString("\nApplication P example (illustrative data):\n```json\n")
		b.Write(contract)
		b.WriteString("\n```\n")
	}
	return b.String(), nil
}

func mcpCheckpointExample(session *SubmissionSession) ([]byte, error) {
	if session == nil || len(session.scope.Stories) == 0 {
		return nil, errors.New("MCP checkpoint guidance requires frozen scope")
	}
	keys := slices.Sorted(maps.Keys(session.scope.Stories))
	key := keys[0]
	c := agent.Contribution{Stage: session.scope.Stage, Story: key, Outcome: "blocked", Reason: "Describe the actual blocker; do not submit this example unchanged."}
	if c.Stage == "validate" {
		source := session.scope.Stories[key]
		findings := func(names []string) []agent.ContributionFinding {
			out := make([]agent.ContributionFinding, 0, len(names))
			for _, name := range names {
				out = append(out, agent.ContributionFinding{Name: name, Status: "unverified", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Replace with findings from supplied retained evidence."}}})
			}
			return out
		}
		c.Outcome, c.Reason = "assessed", ""
		c.Assessment = &agent.AssessmentContribution{SnapshotID: source.SnapshotID, Criteria: findings(source.Criteria), Reviews: findings([]string{"code", "architecture", "plan"})}
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("encode MCP checkpoint example: %w", err)
	}
	return data, nil
}

func mcpApplicationExample(ctx context.Context, service *AgentResultService) (contract, baseline []byte, err error) {
	projection, diagnostics, err := service.Read(ctx, 0, []agentresult.Target{{Field: "comment"}})
	if err == nil {
		if !diagnostics.Valid || !diagnostics.Complete || len(projection.Selections) != 1 {
			return nil, nil, errors.New("MCP guidance baseline is unavailable")
		}
		value, marshalErr := json.Marshal("Replace with the updated factual result summary.")
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		data, marshalErr := json.Marshal(agentresult.UpdateInput{ExpectedRevision: projection.Revision, Operations: []agentresult.Operation{{Op: "set", Target: agentresult.Target{Field: "comment"}, Value: value}}})
		return data, projection.Selections[0].Value, marshalErr
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("read MCP guidance baseline: %w", err)
	}
	r := agentresult.Result{Outcome: "incomplete", Comment: "Replace with the actual result summary.", Reason: "Describe what remains."}
	switch service.binding.Mode {
	case "research":
		r.Payload.Research = &agentresult.Research{Request: "Describe the actual research request."}
	case "spike":
		r.Payload.Spike = &agentresult.Spike{Request: "Describe the bounded exploration."}
	case "bug":
		r.Payload.Bug = &agentresult.Spike{Request: "Describe the reported bug."}
	case "doctor":
		r.Payload.Doctor = &agentresult.Spike{Request: "Describe the stuck task and what the owner asked."}
	case "plan":
		// A graph Plan asks with questions and no story branch. The done shape
		// (storyrequest or storyset) is in the node-result contract.
		r.Payload.Plan = &agentresult.Plan{}
		r.Questions = []agentresult.Question{{ID: "q-1", Audience: "business", Text: "Replace with the question that blocks the story."}}
	case "implement":
		r.Payload.Implement = &agentresult.Implement{}
	case "validate":
		r.Payload.Validate = &agentresult.Validate{}
	default:
		return nil, nil, errors.New("MCP guidance has unsupported mode")
	}
	data, err := json.Marshal(agentresult.CreateInput{Result: r})
	return data, nil, err
}
