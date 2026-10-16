package store

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"strings"
	"testing"
)

func TestJudgePinnedRequestAndIsolatedResultRecovery(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 809)
	sourceAttempt := resultRun(t, db, session, "research", "source", 1)
	sourceBinding, err := db.BindResultAttempt(ctx, sourceAttempt)
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(sourceBinding, "research-save", 0)
	record.Snapshot = []byte(`{"result":{"outcome":"completed","payload":{"research":{"findings":[{"id":"f1","text":"saved finding"}]}}}}`)
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	source, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	req, err := db.AdmitJudgeRequest(ctx, JudgeRequest{SessionID: session.ID, TriggerCommentID: 9, Task: session.TaskRef, TicketKey: "qualified", CommentID: "comment-9", Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartJudgeAttempt(ctx, req.ID, "codex", "model", "high")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindJudgeResultAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CheckResultWriter(ctx, sourceBinding); err != nil {
		t.Fatalf("judge revoked source writer: %v", err)
	}
	meter, err := db.StartJudgeMeterAttempt(ctx, attempt.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if meter.RunID != 0 || meter.JudgeAttemptID != attempt.ID {
		t.Fatalf("bad attribution %+v", meter)
	}
	receipt, err := db.RecordResult(ctx, resultRecord(binding, "judge-report", 0))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AppliedRevision != 1 {
		t.Fatal(receipt)
	}
	accepted, err := db.JudgeAcceptedResult(ctx, req.ID)
	if err != nil || !bytes.Contains(accepted, []byte("judge-report")) {
		t.Fatalf("recovery %s %v", accepted, err)
	}
	if err = db.FinishJudgeAttempt(ctx, attempt.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	if err = db.CheckResultWriter(ctx, binding); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("finished writer survived: %v", err)
	}
	if err = db.CheckResultWriter(ctx, sourceBinding); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveJudgeReport(ctx, req.ID, accepted); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveJudgeReport(ctx, req.ID, accepted); err != nil {
		t.Fatal(err)
	}
	replay := req
	replay.Source.Reference = "new-latest"
	replay.Configuration = []byte(`{"model":"new"}`)
	again, err := db.AdmitJudgeRequest(ctx, replay)
	if err != nil {
		t.Fatal(err)
	}
	if again.Source.Reference != source.Reference || string(again.Configuration) != "{}" {
		t.Fatal("command redelivery retargeted request")
	}
	sources, err := db.ListJudgeSources(ctx, session.TaskRef)
	if err != nil || len(sources) != 1 {
		t.Fatalf("judge report became source: %+v %v", sources, err)
	}
}

func TestJudgeSourceEligibilityAndScope(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 810)
	a := resultRun(t, db, session, "research", "writer", 1)
	b, err := db.BindResultAttempt(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range []string{`{"result":{"outcome":"failed","payload":{"research":{"findings":[{"text":"failed"}]}}}}`, `{"result":{"outcome":"completed","payload":{"research":{"request":"question only"}}}}`, `{"result":{"outcome":"completed","payload":{"research":{"findings":[{"text":"saved"}]}}}}`} {
		q := resultRecord(b, string(rune('a'+i)), int64(i))
		q.Snapshot = []byte(raw)
		if _, err = db.RecordResult(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := db.ListJudgeSources(ctx, session.TaskRef)
	if err != nil || len(sources) != 1 || sources[0].Revision != 3 {
		t.Fatalf("eligibility %+v %v", sources, err)
	}
	if _, err = db.ResolveJudgeSource(ctx, "github:other/repo#1", sources[0].Reference); err == nil {
		t.Fatal("cross-ticket target accepted")
	}
	if _, err = db.ResolveJudgeSource(ctx, session.TaskRef, "bad-reference"); err == nil {
		t.Fatal("invalid reference substituted latest")
	}
}

func TestJudgePlanDraftHistoryStoryScopeAndPin(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	parent := testSession(t, db, 811)
	repo, err := db.GetRepo(t.Context(), parent.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	story, err := db.GetOrCreateSession(t.Context(), repo, 812)
	if err != nil {
		t.Fatal(err)
	}
	draft := planbrief.PlanDraft{Key: "selected", Name: "Selected draft", UserStory: planbrief.UserStoryContent{Want: "challenge this draft"}}
	sibling := draft
	sibling.Key = "sibling"
	sibling.Name = "Sibling draft"
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number) VALUES(?,'selected',1,812,812),(?,'sibling',2,813,813)`, parent.TaskRef, parent.TaskRef); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: draft.Key, Draft: draft, Candidate: "first"}, {Key: sibling.Key, Draft: sibling, Candidate: "sibling"}}}
	if _, err := db.SavePlanWorkflowState(ctx, parent.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	source, err := db.ResolveJudgeSource(ctx, story.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(source.Snapshot, []byte("Sibling draft")) || !bytes.Contains(source.Snapshot, []byte("Selected draft")) {
		t.Fatal("story projection leaked sibling", string(source.Snapshot))
	}
	draft.Name = "New selected revision"
	state.Stories[0].Draft = draft
	if _, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, 1, state); err != nil {
		t.Fatal(err)
	}
	selected, err := db.ResolveJudgeSource(ctx, story.TaskRef, source.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(selected.Snapshot, source.Snapshot) {
		t.Fatal("historical story projection changed")
	}
	latest, err := db.ResolveJudgeSource(ctx, story.TaskRef, "")
	if err != nil || latest.Reference == source.Reference {
		t.Fatalf("latest failed %+v %v", latest, err)
	}
	parentSource, err := db.ResolveJudgeSource(ctx, parent.TaskRef, "")
	if err != nil || !bytes.Contains(parentSource.Snapshot, []byte("Sibling draft")) {
		t.Fatalf("parent assembly missing %+v %v", parentSource, err)
	}
}

func TestJudgeLegacyPlanEligibilityRequiresSubstantiveContent(t *testing.T) {
	for _, test := range []struct {
		payload  string
		eligible bool
	}{
		{`{"storyrequest":{"operation":"retire","draft":{"key":"selected"}}}`, false},
		{`{"storyset":{"entries":[]}}`, false},
		{`{"stories":[{"key":"selected"}]}`, false},
		{`{"content":{"transport":"published"}}`, false},
		{`{"storyrequest":{"operation":"update","draft":{"key":"selected","name":"Saved draft","user_story":{"want":"review the saved draft"}}}}`, true},
		{`{"storyset":{"entries":[{"name":"Saved story","intent":"review the saved decomposition"}]}}`, true},
		{`{"stories":[{"title":"Saved legacy draft","user_story":{"want":"review the saved draft"}}]}`, true},
	} {
		raw := []byte(`{"result":{"outcome":"completed","payload":{"plan":` + test.payload + `}}}`)
		if got := judgeSubstantive(raw, "plan"); got != test.eligible {
			t.Fatalf("eligible=%v want %v for %s", got, test.eligible, test.payload)
		}
	}
}

func TestJudgeInterruptedRecoverySettlesOnlyItsUnfinishedMeter(t *testing.T) {
	for _, priorFailed := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously_failed_%v", priorFailed), func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			session := testSession(t, db, 817)
			sourceAttempt := resultRun(t, db, session, "research", "source-meter", 1)
			sourceWriter, err := db.BindResultAttempt(ctx, sourceAttempt)
			if err != nil {
				t.Fatal(err)
			}
			sourceMeter, err := db.StartMeterAttempt(ctx, sourceAttempt.RunID, "claude")
			if err != nil {
				t.Fatal(err)
			}
			source := JudgeSource{Task: session.TaskRef, Kind: "result", Reference: "pinned-source", Snapshot: []byte(`{"saved":"source"}`)}
			request, err := db.AdmitJudgeRequest(ctx, JudgeRequest{Task: session.TaskRef, TicketKey: "qualified", CommentID: "interrupted-9", SessionID: session.ID, TriggerCommentID: 9, Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartJudgeAttempt(ctx, request.ID, "codex", "model", "high")
			if err != nil {
				t.Fatal(err)
			}
			writer, err := db.BindJudgeResultAttempt(ctx, attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			meter, err := db.StartJudgeMeterAttempt(ctx, attempt.ID, "codex")
			if err != nil {
				t.Fatal(err)
			}
			if err = db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: meter.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Status: "partial"}); err != nil {
				t.Fatal(err)
			}
			if err = addMeterFixture(ctx, db, meterObservation(meter, "before-interruption"), 7); err != nil {
				t.Fatal(err)
			}
			if priorFailed {
				if err = db.FinishJudgeAttempt(ctx, attempt.ID, "failed", "meter settlement failed"); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.FinishJudgeAttempt(ctx, attempt.ID, "interrupted", "process ended before settlement"); err != nil {
				t.Fatal(err)
			}
			if err = db.FinishJudgeAttempt(ctx, attempt.ID, "interrupted", "process ended before settlement"); err != nil {
				t.Fatal(err)
			}
			got, err := db.GetMeterAttempt(ctx, meter.ID)
			if err != nil || got.Outcome != "cancelled" || got.ProviderReportedZeroCostRefusal {
				t.Fatalf("interrupted meter %+v %v", got, err)
			}
			var finished, status, codes string
			if err = db.sql.QueryRowContext(ctx, `SELECT finished_at,collection_status,diagnostic_codes_json FROM meter_attempts WHERE id=?`, meter.ID).Scan(&finished, &status, &codes); err != nil {
				t.Fatal(err)
			}
			if finished == "" || status != "unknown" || codes != `["judge_execution_interrupted"]` {
				t.Fatalf("interruption coverage finished=%q status=%q codes=%s", finished, status, codes)
			}
			var tokens int64
			if err = db.sql.QueryRowContext(ctx, `SELECT output_tokens FROM meter_measurements WHERE attempt_id=?`, meter.ID).Scan(&tokens); err != nil || tokens != 7 {
				t.Fatalf("recorded usage lost: %d %v", tokens, err)
			}
			got, err = db.GetMeterAttempt(ctx, sourceMeter.ID)
			if err != nil || got.Outcome != "running" {
				t.Fatalf("source meter changed %+v %v", got, err)
			}
			if err = db.CheckResultWriter(ctx, sourceWriter); err != nil {
				t.Fatalf("source writer fenced %v", err)
			}
			if err = db.CheckResultWriter(ctx, writer); !errors.Is(err, ErrResultAuthority) {
				t.Fatalf("interrupted writer survived %v", err)
			}

		})
	}
}

func TestJudgePlainResolveChoosesNewestListedLegacySource(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 940)
	attempt := resultRun(t, db, session, "research", "legacy", 1)
	binding, err := db.BindResultAttempt(ctx, attempt)
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(binding, "legacy-research", 0)
	record.Snapshot = []byte(`{"result":{"outcome":"completed","payload":{"research":{"findings":[{"id":"f1","text":"saved finding"}]}}}}`)
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "s1", Draft: planbrief.PlanDraft{Key: "s1", Name: "Saved Plan", UserStory: planbrief.UserStoryContent{Want: "inspect saved work"}}}}}
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	// Legacy saves predate the commit ledger.
	if _, err = db.sql.ExecContext(ctx, `DELETE FROM judge_source_commits`); err != nil {
		t.Fatal(err)
	}
	sources, err := db.ListJudgeSources(ctx, session.TaskRef)
	if err != nil || len(sources) != 2 || sources[0].Kind == sources[1].Kind || sources[1].Sequence != 0 {
		t.Fatalf("legacy fixture %+v %v", sources, err)
	}
	first := strings.Fields(judgeSourceChoices(sources))[3]
	got, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != first || !got.NewestLegacy {
		t.Fatalf("plain resolve=%+v, want newest listed %s", got, first)
	}
	explicit, err := db.ResolveJudgeSource(ctx, session.TaskRef, sources[0].Reference)
	if err != nil || explicit.Reference != sources[0].Reference || explicit.NewestLegacy {
		t.Fatalf("explicit legacy target %+v %v", explicit, err)
	}
}
