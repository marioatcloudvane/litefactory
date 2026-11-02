package daemon

import (
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestJudgeRecordedBugProviderPreservesOtherWorkflowDefaults(t *testing.T) {
	d, _, _, _, parent := bugAgentTestDaemon(t, bugAgentCase{}, nil)
	// The latest run selected Codex; the mutable conversation field still says
	// Claude. Non-Bug defaults retain their existing interpretation of that field.
	bugAgentRecordParent(t, d.db, parent, []string{"codex"})
	session, err := d.db.GetSessionByTicket(t.Context(), parent.Identity)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.recordedJudgeProvider(t.Context(), session.ID)
	if err != nil || got != config.KindClaude {
		t.Fatalf("non-Bug default=%s err=%v", got, err)
	}
	repo, err := d.db.GetRepo(t.Context(), session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	bug, err := d.db.GetOrCreateSession(t.Context(), repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: bug.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	segment, err := d.db.GetOrCreateSegment(t.Context(), bug.ID, store.FirstSegmentOrdinal, "mutable-conversation", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: bug.ID, SegmentID: segment.ID, Agent: store.AgentCodex, Workflow: "bug", LogPath: func(int) string { return filepath.Join(t.TempDir(), "bug.jsonl") }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed}); err != nil {
		t.Fatal(err)
	}
	got, err = d.recordedJudgeProvider(t.Context(), bug.ID)
	if err != nil || got != config.KindCodex {
		t.Fatalf("recorded Bug default=%s err=%v", got, err)
	}
}
