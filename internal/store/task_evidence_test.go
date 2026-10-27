package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func TestTaskEvidenceRecordedWaitApplicabilityAndIsolation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	parent := testSession(t, db, 8800)
	repo, err := db.GetRepo(ctx, parent.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.GetOrCreateSession(ctx, repo, 8801)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReservePlanStory(ctx, parent.TaskRef, "story-002"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE plan_story_drafts SET issue_number=? WHERE parent=? AND story_key='story-002'`, child.IssueNumber, parent.TaskRef); err != nil {
		t.Fatal(err)
	}
	state := staleWaitState(t, false)
	// A recorded source supplies the original choices independently of a later
	// current draft or shared review change.
	attempt := resultRun(t, db, parent, "plan", "discussion", 1)
	binding, err := db.BindResultAttempt(ctx, attempt)
	if err != nil {
		t.Fatal(err)
	}
	snap := agentresult.Snapshot{Result: agentresult.Result{Comment: "original owner question", Questions: []agentresult.Question{{ID: "choose", Text: "Pick an option", Options: []agentresult.Option{{ID: "a", Text: "First"}, {ID: "b", Text: "Second"}}, RecommendedOption: "b"}}, Payload: agentresult.Payload{Plan: &agentresult.Plan{StoryRequest: &agentresult.PlanStoryRequest{Candidate: "c2", SharedRevision: "shared-1", Draft: state.Stories[1].Draft}}}}}
	upstream, err := state.UpstreamReferences("story-002", "technical")
	if err != nil {
		t.Fatal(err)
	}
	snap.Result.Payload.Plan.StoryRequest.Upstream = upstream
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(binding, "question", 0)
	record.Snapshot = raw
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	var saved int64
	cases := []struct {
		name   string
		change func(*planbrief.PlanState)
		want   string
	}{
		{"current", func(*planbrief.PlanState) {}, "current"},
		{"candidate_changed", func(s *planbrief.PlanState) {
			s.Stories[1].Candidate = "new-candidate"
			s.Stories[1].Draft.Questions = []string{"new question"}
		}, "stale"},
		{"stage_changed", func(s *planbrief.PlanState) { s.Stories[1].Stage = "acceptance" }, "stale"},
		{"shared_changed", func(s *planbrief.PlanState) { s.SharedRevision = "shared-2" }, "stale"},
		{"upstream_candidate_changed", func(s *planbrief.PlanState) { s.Stories[0].Candidate = "new-upstream" }, "stale"},
		{"absent_binding", func(s *planbrief.PlanState) { s.Stories[1].Wait.Revision = "" }, "unknown"},
		{"absent_recorded_candidate", func(s *planbrief.PlanState) { s.Stories[1].Wait.Candidate = "" }, "unknown"},
		{"absent_current_candidate", func(s *planbrief.PlanState) { s.Stories[1].Candidate = "" }, "unknown"},
		{"incomparable_binding", func(s *planbrief.PlanState) { s.Stories[0].EffectHold = "unresolved" }, "unknown"},
		{"stale_upstream_approval", func(s *planbrief.PlanState) { s.Stories[1].Approvals["story"] = "stale" }, "stale"},
		{"absent_recorded_upstream", func(*planbrief.PlanState) {}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := staleWaitState(t, false)
			if tc.name == "absent_recorded_upstream" {
				snap.Result.Payload.Plan.StoryRequest.Upstream = nil
				missingRaw, err := json.Marshal(snap)
				if err != nil {
					t.Fatal(err)
				}
				missing := resultRecord(binding, "missing-source-upstream", 1)
				missing.Snapshot = missingRaw
				if _, err = db.RecordResult(ctx, missing); err != nil {
					t.Fatal(err)
				}
			}
			tc.change(&s)
			var err error
			saved, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, saved, s)
			if err != nil {
				t.Fatal(err)
			}
			var before string
			if err = db.sql.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, parent.TaskRef).Scan(&before); err != nil {
				t.Fatal(err)
			}
			p, err := db.ReadTaskEvidence(ctx, child.ID, "questions", 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Items) != 1 {
				t.Fatalf("questions: %+v", p)
			}
			q := p.Items[0]
			if q["recorded_stage"] != "technical" || q["current_stage"] != s.Stories[1].Stage {
				t.Fatalf("stage bindings lost: %+v", q)
			}
			sourceFound := false
			for _, source := range p.Sources {
				if source["source"] == "persisted_plan_state" && source["task_ref"] == parent.TaskRef {
					sourceFound = true
				}
			}
			if !sourceFound {
				t.Fatal("parent source provenance lost")
			}

			if q["applicability"] != tc.want || q["recorded_waiting"] != true {
				t.Fatalf("question applicability: %+v", q)
			}
			if tc.name == "upstream_candidate_changed" {
				recorded, ok := q["recorded_upstream"].([]string)
				if !ok || !slices.Contains(recorded, "story-001:c1") {
					t.Fatalf("original dependency binding lost: %+v", q)
				}
				current, ok := q["current_upstream"].([]string)
				if !ok || !slices.Contains(current, "story-001:new-upstream") {
					t.Fatalf("current dependency binding lost: %+v", q)
				}
			}
			if tc.name == "absent_recorded_upstream" {
				if q["recorded_upstream"] != nil {
					refs, ok := q["recorded_upstream"].([]string)
					if !ok || refs != nil {
						t.Fatalf("invented recorded upstream: %+v", q)
					}
				}
				gaps, ok := q["gaps"].([]string)
				if !ok || !slices.Contains(gaps, "recorded_upstream_not_separately_retained") {
					t.Fatalf("missing upstream gap: %+v", q)
				}
			}
			text, _ := json.Marshal(q)
			if (tc.name != "absent_recorded_candidate" && (!strings.Contains(string(text), `"recommended_option":"b"`) || !strings.Contains(string(text), `"id":"a"`))) || strings.Contains(string(text), "Upstream") {
				t.Fatalf("lost source choices or leaked sibling: %s", text)
			}
			var after string
			if err = db.sql.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, parent.TaskRef).Scan(&after); err != nil || before != after {
				t.Fatal("discussion changed persisted Plan")
			}
			plan, err := db.ReadTaskEvidence(ctx, child.ID, "plan", 0, 50)
			if err != nil || len(plan.Items) != 1 || plan.Items[0]["story_key"] != "story-002" {
				t.Fatalf("story isolation: %+v %v", plan, err)
			}
		})
	}
}

func TestTaskEvidenceResultsRetainIndependentStatesAndFinitePages(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 8810)
	attempt := resultRun(t, db, session, "research", "pages", 1)
	binding, err := db.BindResultAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 117 {
		r := resultRecord(binding, fmt.Sprintf("page-%d", i), int64(i))
		r.Snapshot = []byte(fmt.Sprintf(`{"result":{"comment":"result-%d","payload":{"research":{"findings":[{"id":"finding-%d","text":"fact"}]}}}}`, i, i))
		if _, err = db.RecordResult(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	var revision string
	offset := 0
	seen := map[string]bool{}
	for pages := 0; ; pages++ {
		if pages > 4 {
			t.Fatal("paging did not terminate")
		}
		p, err := db.ReadTaskEvidence(t.Context(), session.ID, "results", offset, 50)
		if err != nil {
			t.Fatal(err)
		}
		if revision != "" && revision != p.Revision {
			t.Fatal("page revision varies without evidence change")
		}
		revision = p.Revision
		for _, item := range p.Items {
			id, ok := item["id"].(string)
			if !ok {
				t.Fatal("missing record identity")
			}
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
			if item["observation_time_basis"] != "run_started_or_finished" {
				t.Fatalf("result time provenance lost: %+v", item)
			}
			if item["input_state"] != "accepted" || item["engineering_state"] != "pending" || item["acceptance_state"] != "pending" || item["publication_state"] != "pending" {
				t.Fatalf("conflated result states: %+v", item)
			}
		}
		if !p.HasMore {
			break
		}
		if p.NextOffset <= offset {
			t.Fatal("non-progress")
		}
		offset = p.NextOffset
	}
	if len(seen) != 117 {
		t.Fatalf("got %d records", len(seen))
	}
	research, err := db.ReadTaskEvidence(t.Context(), session.ID, "research", 0, 50)
	if err != nil || len(research.Items) != 50 {
		t.Fatalf("research facts absent: %+v %v", research, err)
	}
}

func TestTaskEvidenceApprovalFreshnessSeparatesMissingAndStale(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 8820)
	state := staleWaitState(t, false)
	for i := range state.Stories {
		revision, err := state.ReviewRevision(state.Stories[i].Key, "technical")
		if err != nil {
			t.Fatal(err)
		}
		state.Stories[i].Approvals["technical"] = revision
	}
	saved, err := db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, state)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		p, err := db.ReadTaskEvidence(t.Context(), session.ID, "approvals", 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range p.Items {
			if item["id"] == "story-002:technical" && item["applicability"] != want {
				t.Fatalf("technical approval: %+v", item)
			}
			if item["id"] == "story-002:acceptance" && item["applicability"] != "unknown" {
				t.Fatalf("missing approval invented: %+v", item)
			}
		}
	}
	check("current")
	state.SharedRevision = "new-shared"
	saved, err = db.SavePlanWorkflowState(t.Context(), session.TaskRef, saved, state)
	if err != nil {
		t.Fatal(err)
	}
	check("stale")
	state.Stories[0].EffectHold = "unresolved"
	if _, err = db.SavePlanWorkflowState(t.Context(), session.TaskRef, saved, state); err != nil {
		t.Fatal(err)
	}
	check("unknown")
}

func TestTaskEvidenceHistoricalAnswersAndRevisionChanges(t *testing.T) {
	db, reply := ownerReplyFixture(t, true, "plan", "agent", "story_ask", "question")
	var sessionID int64
	if err := db.sql.QueryRowContext(t.Context(), `SELECT id FROM sessions WHERE task_ref=?`, reply.TaskRef).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Candidate: "new-candidate", Draft: planbrief.PlanDraft{Key: "story-001"}, Wait: &planbrief.PlanWait{ID: "new-wait", Stage: "story", Candidate: "new-candidate", Revision: "recorded", Kind: "question"}}}}
	if _, err := db.SavePlanWorkflowState(t.Context(), reply.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	before, err := db.ReadTaskEvidence(t.Context(), sessionID, "questions", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Items) != 2 {
		t.Fatalf("current wait hid history: %+v", before)
	}
	// The real owner reply path records the answer; discussion never claims it.
	if _, err = db.ClaimGraphOwnerReply(t.Context(), reply); err != nil {
		t.Fatal(err)
	}
	after, err := db.ReadTaskEvidence(t.Context(), sessionID, "questions", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision == after.Revision {
		t.Fatal("recorded owner answer did not invalidate paging revision")
	}
	found := false
	for _, item := range after.Items {
		if item["id"] == "history:"+reply.DeliveryKey {
			found = true
			if item["recorded_answer"] != reply.Body || item["recorded_state"] != "claimed" {
				t.Fatalf("lost recorded answer: %+v", item)
			}
		}
	}
	if !found {
		t.Fatal("missing historical answer")
	}
	wait, err := db.GetGraphOwnerWait(t.Context(), reply.InstanceID)
	if err != nil || wait.State != "claimed" {
		t.Fatalf("discussion changed reply state: %+v %v", wait, err)
	}
}

func TestTaskEvidenceAllowsTypedNarrativeFieldsOnly(t *testing.T) {
	raw := `{"comment":{"raw_prompt":"private"},"questions":[{"id":"q","text":"choice","raw_prompt":"private","options":[{"id":"a","text":"A","environment":"private"}]}],"payload":{"research":{"findings":[{"id":"f","text":"fact","logs":"private"}],"decisions":[{"id":"d","text":"decision","artifact_paths":"private"}],"raw_prompt":"private"}},"raw_prompt":"private"}`
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	safe, err := json.Marshal(allowEvidenceDocument(doc))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(safe), "private") || strings.Contains(string(safe), "raw_prompt") || !strings.Contains(string(safe), "choice") || !strings.Contains(string(safe), "fact") {
		t.Fatalf("incorrect evidence allowlist: %s", safe)
	}
}

func TestTaskEvidencePlanAskSourceAndMappedReplyRevision(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	parent := testSession(t, db, 8830)
	repo, err := db.GetRepo(ctx, parent.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.GetOrCreateSession(ctx, repo, 8831)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReservePlanStory(ctx, parent.TaskRef, "story-001"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE plan_story_drafts SET issue_number=? WHERE parent=?`, child.IssueNumber, parent.TaskRef); err != nil {
		t.Fatal(err)
	}
	run := resultRun(t, db, parent, "plan", "ask", 1)
	binding, err := db.BindResultAttempt(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := agentresult.Snapshot{Result: agentresult.Result{Outcome: "incomplete", Comment: "Need owner choice", Questions: []agentresult.Question{{ID: "choose", Text: "Recorded question", Options: []agentresult.Option{{ID: "a", Text: "Safe A"}, {ID: "b", Text: "Safe B"}}, RecommendedOption: "b"}}, Payload: agentresult.Payload{Plan: &agentresult.Plan{}}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(binding, "ask", 0)
	record.Snapshot = raw
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	at := graphTestTime()
	definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "discussion", Mode: "plan", ProfileVersion: 1, Path: "plan.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: parent.ID, DefinitionID: definition.ID, CurrentNode: "story_agent", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "story_agent", RunID: run.RunID, Limit: 3, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := json.Marshal(struct {
		Kind     string        `json:"kind"`
		Binding  ResultBinding `json:"binding"`
		Revision int64         `json:"revision"`
	}{"shared_result", binding, 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "ask", FinishedAt: at, Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: reference, CreatedAt: at}}}); err != nil {
		t.Fatal(err)
	}
	if err = db.RetainGraphComment(ctx, GraphCommentIntent{Key: "ask-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "story_ask", Kind: "question", Candidate: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	candidate := fmt.Sprintf("plan-%d-%d", instance.ID, attempt.ID)
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Candidate: candidate, Draft: planbrief.PlanDraft{Key: "story-001", Questions: []string{"Current draft text"}}}}}
	revision, err := state.ReviewRevision("story-001", "story")
	if err != nil {
		t.Fatal(err)
	}
	state.Stories[0].Wait = &planbrief.PlanWait{ID: "ask-wait", Stage: "story", Candidate: candidate, Revision: revision, Kind: "question"}
	if _, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	before, err := db.ReadTaskEvidence(ctx, child.ID, "questions", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(before.Items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"recommended_option":"b"`) || !strings.Contains(string(encoded), "Recorded question") || strings.Contains(string(encoded), "Current draft text") {
		t.Fatalf("did not resolve exact ask source: %s", encoded)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE graph_owner_waits SET state='claimed',reply_id=102,reply_body='Owner chose B',claimed_at=? WHERE delivery_key='ask-question'`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	after, err := db.ReadTaskEvidence(ctx, child.ID, "questions", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision == after.Revision || after.Items[0]["recorded_answer"] != "Owner chose B" {
		t.Fatalf("mapped reply revision or recorded answer missing: %+v", after)
	}
}
