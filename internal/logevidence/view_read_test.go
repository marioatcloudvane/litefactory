package logevidence

import (
	"github.com/cloudvane/litefactory/internal/redact"
	"os"
	"strings"
	"testing"
)

func TestPreparedRunKeepsImmutableRevisionWithRestoredMetadata(t *testing.T) {
	f := newEvidenceFixture(t)
	original := "{\"type\":\"assistant\",\"text\":\"one\"}\n{\"type\":\"assistant\",\"text\":\"two\"}\n"
	run := f.run(t, f.repo, 1, original, false)
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	broker := evidenceBroker(t, f.service(t, scope, nil))
	first := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID, Limit: 1})
	if first.Remaining == nil || len(first.Records) != 1 {
		t.Fatalf("no retained position: %+v", first)
	}
	info, err := os.Stat(run.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.LogPath, []byte(strings.Replace(original, "two", "bad", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(run.LogPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID, Cursor: first.NextCursor, Limit: 1})
	if len(second.Records) != 1 || !strings.Contains(second.Records[0].Text, "two") || second.Records[0].Citation.SourceRevision != first.Records[0].Citation.SourceRevision {
		t.Fatalf("mixed revisions: %+v", second)
	}
	newPass := evidenceBroker(t, f.service(t, scope, nil))
	changed := requireEvidenceCall(t, newPass, "run_logs_read", ReadRequest{RunID: run.ID, Locator: first.Remaining})
	if changed.Status != "source_changed" || len(changed.Records) != 0 {
		t.Fatalf("new source silently substituted: %+v", changed)
	}
}

func TestFullViewCitationRejectsPartialCodePointAndChangedPolicy(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"assistant\",\"text\":\"äö🙂\"}\n", false)
	service := f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, nil)
	broker := evidenceBroker(t, service)
	first := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID})
	if len(first.Records) != 1 {
		t.Fatalf("no record: %+v", first)
	}
	citation := first.Records[0].Citation
	citation.ViewEnd = strings.Index(first.Records[0].Text, "ä") + 1
	citation.ExcerptBytes = citation.ViewEnd
	broken := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID, Citation: &citation})
	if broken.Status != "source_changed" || len(broken.Records) != 0 {
		t.Fatalf("split code point accepted: %+v", broken)
	}
	service.redactor = redact.New(redact.Options{Environ: []string{"EVIDENCE_API_TOKEN=different-secret-value"}})
	citation = first.Records[0].Citation
	changed := requireEvidenceCall(t, broker, "run_logs_read", ReadRequest{RunID: run.ID, Citation: &citation})
	if changed.Status != "policy_changed" || len(changed.Records) != 0 {
		t.Fatalf("old policy disclosed: %+v", changed)
	}
}

func TestPreparationTransfersOnlyAfterBudgetExhaustion(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"assistant\",\"text\":\""+strings.Repeat("ä", 700000)+"\"}\n", false)
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	budget, err := NewBudget(Limits{Calls: 1})
	if err != nil {
		t.Fatal(err)
	}
	service := f.service(t, scope, budget)
	first := requireEvidenceCall(t, evidenceBroker(t, service), "run_logs_read", ReadRequest{RunID: run.ID})
	if first.Status != "budget_exhausted" || first.NextCursor != "" || first.Remaining == nil || first.Remaining.CacheID == "" || len(first.Records) != 0 {
		t.Fatalf("missing bounded preparation handoff: %+v", first)
	}
	snapshotKey := digestJSON([]any{run.ID, first.Remaining.Fingerprint})
	if budget.snapshots[snapshotKey] != nil {
		t.Fatal("old invocation retained transferred ownership")
	}
	secondBudget, err := NewBudget(Limits{Calls: 1})
	if err != nil {
		t.Fatal(err)
	}
	secondService := f.service(t, scope, secondBudget)
	second := requireEvidenceCall(t, evidenceBroker(t, secondService), "run_logs_read", ReadRequest{RunID: run.ID, Locator: first.Remaining})
	if second.Remaining == nil || second.Remaining.PreparationOffset <= first.Remaining.PreparationOffset {
		t.Fatalf("preparation restarted instead of resuming: first %+v second %+v", first.Remaining, second.Remaining)
	}
	if second.Remaining.CacheID != "" {
		state, ok := pausedViews.Take(second.Remaining.CacheID)
		if ok {
			state.close()
		}
	}
}

func TestOuterBudgetPausePreservesPreviouslyIssuedPreparation(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"assistant\",\"text\":\""+strings.Repeat("ä", 700000)+"\"}\n", false)
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	firstService := f.service(t, scope, nil)
	firstBroker := evidenceBroker(t, firstService)
	first := requireEvidenceCall(t, firstBroker, "run_logs_read", ReadRequest{RunID: run.ID})
	if first.Status != "preparing" || first.Remaining == nil || first.Remaining.CacheID == "" {
		t.Fatalf("no stable preparation identity: %+v", first)
	}
	if err := firstService.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := evidenceCall(t.Context(), firstBroker, "run_logs_read", ReadRequest{RunID: run.ID}); err == nil {
		t.Fatal("paused old service remained usable")
	}
	newBudget, err := NewBudget(Limits{Calls: 1})
	if err != nil {
		t.Fatal(err)
	}
	secondService := f.service(t, scope, newBudget)
	second := requireEvidenceCall(t, evidenceBroker(t, secondService), "run_logs_read", ReadRequest{RunID: run.ID, Locator: first.Remaining})
	if second.Remaining == nil || second.Remaining.PreparationOffset <= first.Remaining.PreparationOffset {
		t.Fatalf("outer pause lost progress: first %+v second %+v", first.Remaining, second.Remaining)
	}
	if second.Remaining.CacheID != "" {
		state, ok := pausedViews.Take(second.Remaining.CacheID)
		if ok {
			state.close()
		}
	}
}
