package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

const researchStageOneJSON = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Investigation finished.","payload":{"research":{"request":"Inspect boundaries","findings":[{"id":"F-01","text":"stage-one-finding: SQL stays in the store."}],"decisions":[{"id":"O-01","text":"stage-one-decision: keep the store boundary."}],"assumptions":[{"id":"S-01","text":"stage-one-assumption: change mode, small."}]}}}}`

// Shaping revises the instance's one shared result, so its own passes can
// replace what investigation found. Every shaping pass must still see the
// saved stage-1 result.
func TestResearchShapingReceivesStageOneFindings(t *testing.T) {
	stageOne := []string{"stage-one-finding", "stage-one-decision", "stage-one-assumption"}
	shapePasses := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Graph.Node == "investigate" {
			if strings.Contains(req.Prompt, "Saved earlier-stage Research findings") {
				t.Error("first stage received an earlier-stage note")
			}
			submitLiveResult(t, req, researchStageOneJSON)
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchStageOneJSON, FinishedAt: time.Now()}, nil
		}
		shapePasses++
		start := strings.Index(req.Prompt, "Saved earlier-stage Research findings")
		if start < 0 {
			t.Fatalf("shaping pass %d lacks the stage-1 note", shapePasses)
		}
		note, _, closed := strings.Cut(req.Prompt[start:], "</note>")
		if !closed {
			t.Fatal("stage-1 note is not closed")
		}
		for _, want := range append(stageOne, `"node":"investigate"`) {
			if !strings.Contains(note, want) {
				t.Fatalf("shaping pass %d stage-1 note lacks %q", shapePasses, want)
			}
		}
		if shapePasses > 1 && !strings.Contains(req.Prompt, "Saved cumulative same-stage Research findings") {
			t.Fatal("later shaping pass lost its own continuation note")
		}
		// Replace the shared findings, as a shaping pass may.
		submitContinuationUpdate(t, req)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: researchContinueJSON, FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledResearchFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	for shapePasses < 2 {
		due, err := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 10)
		mustProgress(t, err)
		if len(due) != 1 || due[0].Node != "shape" {
			t.Fatalf("next stage: %+v", due)
		}
		_, reserved, err := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
		mustProgress(t, err)
		if !reserved {
			t.Fatal("dispatch not reserved")
		}
		req.Trigger.CommentID = due[0].TriggerID()
		req.Trigger.Body = ""
		req.Command = ParseCommand("/research inspect boundaries")
		_, err = Execute(t.Context(), f.opts, req)
		mustProgress(t, err)
	}
}
