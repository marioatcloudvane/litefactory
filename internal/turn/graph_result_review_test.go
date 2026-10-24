package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestGraphPlanFailurePublishesRetainedRejectedSubmission(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		data, err := json.Marshal(map[string]any{"result": map[string]any{"outcome": "incomplete", "reason": "Review evidence remains uncertain", "comment": strings.Repeat("Useful findings and uncertainty stay visible. λ\n", 1800) + "Final retained caveat sentinel", "payload": map[string]any{"plan": map[string]any{}}}, "unknown": "A formatting defect does not authorize promotion"})
		if err != nil {
			t.Fatal(err)
		}
		rejected := mcpDecodeSubmission(t, mcpToolCall(t, submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, "communication_submit", data))
		if rejected.Validation.Valid || rejected.Receipt == nil || rejected.Receipt.AppliedRevision != 0 {
			t.Fatalf("invalid graph submission not retained as rejection: %+v", rejected)
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "TERMINAL DOES NOT SUPPLY REVIEW CONTENT", FinishedAt: time.Now()}, nil
	})
	f, _, request, forge := setupGraphPlanFixture(t, runner, true)
	result, err := Execute(t.Context(), f.opts, request)
	if err == nil || result.Run.Status != store.RunFailed {
		t.Fatalf("rejected graph result advanced workflow: %+v %v", result, err)
	}
	var delivered strings.Builder
	for _, post := range forge.all() {
		delivered.WriteString(post.body)
		delivered.WriteString("\n")
	}
	body := delivered.String()
	for _, want := range []string{"unaccepted, for review only", "Useful findings and uncertainty stay visible", "Final retained caveat sentinel", "Validation limitations", "all parts confirmed"} {
		if !strings.Contains(body, want) {
			t.Errorf("graph review lacks %q", want)
		}
	}
	if strings.Contains(body, "TERMINAL DOES NOT SUPPLY REVIEW CONTENT") {
		t.Fatal("terminal text became graph review")
	}
	if len(forge.all()) < 3 {
		t.Fatalf("large graph review not delivered in linked parts: %d posts", len(forge.all()))
	}
	count := len(forge.all())
	_, err = Execute(t.Context(), f.opts, request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(forge.all()) != count {
		t.Fatalf("graph replay repeated work or delivery: calls=%d posts=%d before=%d", calls, len(forge.all()), count)
	}
}
