package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestNormalResultRetainsPreparationProposal(t *testing.T) {
	t.Parallel()
	rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 6000})
	if err != nil {
		t.Fatal(err)
	}
	proposal := previewProposal()
	proposal.TargetPhase, proposal.TargetRef = "task", "origin/lf/123-change"
	body, err := rep.RenderRun(t.Context(), report.Run{Ref: testRef, Ordinal: 1,
		PreparationCaveats: []string{"Pending refinements remain pending."},
		TaskSync:           &report.Sync{Phase: "task", Outcome: report.SyncBlocked, Behind: 1},
		ConflictProposal:   &proposal,
		Result:             &agent.RunResult{Outcome: agent.OutcomeFailed, ResultText: strings.Repeat("Unfinished original work. ", 1000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := body.String()
	for _, want := range []string{"**Preparation caveat**", "Pending refinements remain pending.", "task synchronization", "Suggested resolution", "proposal 42", "Task-branch baseline SHA", "`/confirm 42 apply this resolution`", "Diff content omitted", "litefactory has not applied it"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Count(text, "**Preparation caveat**") != 1 || strings.Contains(text, report.ReplyMarker(testRef)) || strings.Contains(text, "stopped before starting the agent") {
		t.Fatalf("proposal became a separate refusal: %s", text)
	}
	if body.Len() > 6000 {
		t.Fatal("normal proposal report exceeded limit")
	}
}
