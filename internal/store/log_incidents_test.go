package store

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

type incidentFixture struct {
	db                       *DB
	t                        *testing.T
	at                       time.Time
	repo, other              Repo
	work, bug, legacy, outer Session
	trigger                  int64
}

func (f *incidentFixture) exec(query string, args ...any) int64 {
	f.t.Helper()
	res, err := f.db.sql.ExecContext(f.t.Context(), query, args...)
	if err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *incidentFixture) run(session Session, mode string, out RunOutcome) int64 {
	f.t.Helper()
	segment, err := f.db.GetOrCreateSegment(f.t.Context(), session.ID, FirstSegmentOrdinal, fmt.Sprint("segment-", session.ID), AgentCodex)
	if err != nil {
		f.t.Fatal(err)
	}
	f.trigger++
	run, err := f.db.AllocateRun(f.t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: mode, Agent: AgentCodex, TriggerCommentID: f.trigger, StartedAt: f.at, LogPath: func(n int) string { return fmt.Sprintf("/logs/%d-%04d.jsonl", session.ID, n) }})
	if err != nil {
		f.t.Fatal(err)
	}
	out.FinishedAt = f.at.Add(time.Minute)
	if _, err = f.db.FinishRun(f.t.Context(), run.ID, out); err != nil {
		f.t.Fatal(err)
	}
	return run.ID
}

func newIncidentFixture(t *testing.T) *incidentFixture {
	db := newTestDB(t)
	f := &incidentFixture{db: db, t: t, at: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	f.repo, f.other = testRepo(t, db, "acme", "api"), testRepo(t, db, "other", "web")
	for i, s := range []*Session{&f.work, &f.bug, &f.legacy} {
		session, err := db.GetOrCreateSession(t.Context(), f.repo, i+1)
		if err != nil {
			t.Fatal(err)
		}
		*s = session
	}
	outer, err := db.GetOrCreateSession(t.Context(), f.other, 9)
	if err != nil {
		t.Fatal(err)
	}
	f.outer = outer
	return f
}

// TestLogIncidentsFromRetainedRecords builds one incident per kind from
// fixture rows, excludes owner stops, and applies the grant.
func TestLogIncidentsFromRetainedRecords(t *testing.T) {
	f := newIncidentFixture(t)
	ctx := t.Context()
	at := FormatTime(f.at)
	ok := f.run(f.work, "implement", RunOutcome{Status: RunSucceeded})
	failed := f.run(f.work, "implement", RunOutcome{Status: RunFailed, SemanticOutcome: "failed", Error: "tests did not pass"})
	stopped := f.run(f.work, "implement", RunOutcome{Status: RunFailed, SemanticOutcome: "abandoned", RecoveryReason: `owner-stop:{"run_id":3}`})
	bugRun := f.run(f.bug, "spike", RunOutcome{Status: RunSucceeded})
	legacyRun := f.run(f.legacy, "spike", RunOutcome{Status: RunSucceeded})
	outside := f.run(f.outer, "implement", RunOutcome{Status: RunFailed, SemanticOutcome: "timed_out"})

	f.exec(`PRAGMA foreign_keys=OFF`)
	f.exec(`DROP TRIGGER archive_bug_analyses_insert`)
	digest := strings.Repeat("a", 64)
	implement := f.exec(`INSERT INTO graph_definitions(team,mode,profile_version,path,digest,created_at) VALUES('go','implement',1,'implement.dot',?,?)`, digest, at)
	validate := f.exec(`INSERT INTO graph_definitions(team,mode,profile_version,path,digest,created_at) VALUES('go','validate',1,'validate.dot',?,?)`, digest, at)
	work := f.exec(`INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running','worker_agent',?)`, f.work.ID, implement, at)
	review := f.exec(`INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running','review',?)`, f.work.ID, validate, at)
	first := f.exec(`INSERT INTO graph_node_attempts(instance_id,node,attempt,run_id,status,outcome,started_at,finished_at) VALUES(?,'worker_agent',1,?,'succeeded','retry',?,?)`, work, ok, at, at)
	f.exec(`INSERT INTO graph_node_attempts(instance_id,node,attempt,run_id,status,started_at,finished_at) VALUES(?,'worker_agent',2,?,'failed',?,?)`, work, failed, at, at)
	// A later node sharing the first run is not a second agent run.
	f.exec(`INSERT INTO graph_node_attempts(instance_id,node,attempt,run_id,status,outcome,started_at,finished_at) VALUES(?,'worker_result',1,?,'succeeded','done',?,?)`, work, ok, at, at)
	sha := strings.Repeat("b", 40)
	blocking := `[{"story":"story-001","criteria":[{"name":"AC-1","status":"unmet","classification":"blocking","evidence":[{"summary":"the retry path is untested"}]}],"reviews":[{"name":"architecture","status":"satisfied","classification":"blocking","evidence":[]}]}]`
	satisfied := `[{"story":"story-001","criteria":[{"name":"AC-1","status":"satisfied","classification":"blocking","evidence":[]}]}]`
	f.exec(`INSERT INTO validate_reviews(instance_id,role,commit_sha,state,findings,started_at,finished_at) VALUES(?,'architecture',?,'accepted',?,?,?)`, review, sha, blocking, at, at)
	f.exec(`INSERT INTO validate_reviews(instance_id,role,commit_sha,state,findings,started_at,finished_at) VALUES(?,'acceptance',?,'accepted',?,?,?)`, review, sha, satisfied, at, at)
	f.exec(`INSERT INTO validate_reviews(instance_id,role,commit_sha,state,reason,started_at,finished_at) VALUES(?,'code',?,'failed','checkout unreadable',?,?)`, review, sha, at, at)
	f.exec(`INSERT INTO validate_verdicts(instance_id,commit_sha,criterion,verdict,evidence,comment_id,author_id,created_at) VALUES(?,?,'AC-2','fail','owner saw a crash',7,8,?)`, review, sha, at)
	f.exec(`INSERT INTO validate_repair_rounds(instance_id,round,pre_commit,response,state,reason,diagnosis,started_at,finished_at) VALUES(?,1,?,'handback','handback','fix too large','make test failed',?,?)`, review, sha, at, at)
	f.exec(`INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state,reply_id,reply_body,claimed_at) VALUES('k1',?,?,'allocated',11,'Please revise: the plan skipped the migration',?)`, work, first, at)
	f.exec(`INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state,reply_id,reply_body,claimed_at) VALUES('k2',?,?,'allocated',12,'/approve-plan',?)`, work, first, at)
	f.exec(`INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate) VALUES('k1',?,?,'worker_agent','review','x')`, work, first)
	result := f.exec(`INSERT INTO application_results(task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,workflow) VALUES(?,'','w','h',1,2,2,'bug')`, f.bug.TaskRef)
	snapshot := `{"result":{"payload":{"bug":{"analysis":{"expected":"closure works","actual":"closure failed","cause":"the code repository was used as ticket container"}}}}}`
	f.exec(`INSERT INTO application_result_revisions(result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired) VALUES(?,1,?,'a',1,?,'s','r')`, result, bugRun, snapshot)
	report := `{"presentation":{"Report":{"verdict":"concerns_found","summary":"cause not proven","findings":[{"title":"Missing regression","concern":"no failing test","correction":"add one"}]}}}`
	source := fmt.Sprintf(`{"ID":%d,"Revision":1,"Kind":"result"}`, result)
	f.exec(`INSERT INTO judge_requests(task,ticket_key,comment_id,session_id,trigger_comment_id,source_json,configuration,evidence,report,created_at) VALUES(?,'t','c1',?,1,?,'{}','{}',?,?)`, f.bug.TaskRef, f.bug.ID, source, report, at)
	f.exec(`INSERT INTO judge_requests(task,ticket_key,comment_id,session_id,trigger_comment_id,source_json,configuration,evidence,report,created_at) VALUES(?,'t','c2',?,1,?,'{}','{}',?,?)`, f.bug.TaskRef, f.bug.ID, source, `{"presentation":{"Report":{"verdict":"no_material_concerns"}}}`, at)
	f.exec(`INSERT INTO bug_analyses(session_id,invocation,input_digest,ordinal,graph_attempt_id,state,run_id,content) VALUES(?,'i',?,1,999,'succeeded',?,?)`, f.legacy.ID, digest, legacyRun, `{"expected":"save step writes the index","actual":"Plan never ready","cause":"the save step ignored errors"}`)
	f.exec(`PRAGMA foreign_keys=ON`)

	all, err := f.db.ListLogIncidents(ctx, LogIncidentQuery{RepositoryIDs: []int64{f.repo.ID}})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]LogIncident{}
	kinds := map[string]int{}
	for _, in := range all {
		byID[in.ID] = in
		kinds[in.Kind]++
		if in.RepoID != f.repo.ID || in.Detail == "" || !in.At.Equal(f.at) && !in.At.Equal(f.at.Add(time.Minute)) {
			t.Fatalf("incident outside grant or without words: %+v", in)
		}
	}
	want := map[string]int{IncidentValidateReview: 2, IncidentValidateVerdict: 1, IncidentValidateRepair: 1, IncidentJudge: 1, IncidentBug: 2, IncidentOwnerReply: 2, IncidentFailedRun: 1, IncidentRepeatedNode: 1}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("kinds=%v want %v", kinds, want)
	}
	if _, ok := byID[fmt.Sprintf("failed_run-%d", stopped)]; ok {
		t.Fatal("owner stop counted as a failed run")
	}
	if _, ok := byID[fmt.Sprintf("failed_run-%d", outside)]; ok {
		t.Fatal("run outside the repository grant listed")
	}
	failure := byID[fmt.Sprintf("failed_run-%d", failed)]
	if failure.Role != "implement/worker_agent" || !slices.Equal(failure.RunIDs, []int64{failed}) || !strings.Contains(failure.Detail, "tests did not pass") || failure.FailedRuns != 1 {
		t.Fatalf("failed run incident=%+v", failure)
	}
	repeated := byID[fmt.Sprintf("repeated_attempts-%d-worker_agent", work)]
	if repeated.Attempts != 2 || !slices.Equal(repeated.RunIDs, []int64{ok, failed}) {
		t.Fatalf("repeated attempts=%+v", repeated)
	}
	redo := byID[fmt.Sprintf("owner_reply-%d-11", work)]
	if redo.FoundBy != "owner" || !strings.Contains(redo.Detail, "skipped the migration") || !slices.Equal(redo.RunIDs, []int64{ok}) || redo.Rounds != 2 {
		t.Fatalf("owner redo=%+v", redo)
	}
	var finding LogIncident
	for _, in := range all {
		if in.Kind == IncidentValidateReview && in.FoundBy == "validate-architecture" {
			finding = in
		}
	}
	if finding.Role != "implement" || !strings.Contains(finding.Detail, "AC-1 (unmet, blocking): the retry path is untested") || strings.Contains(finding.Detail, "architecture (satisfied)") {
		t.Fatalf("validate finding=%+v", finding)
	}
	judge := byID["judge-1"]
	if !strings.Contains(judge.Detail, "Missing regression") || !slices.Equal(judge.RunIDs, []int64{bugRun}) || judge.Role != "bug" || judge.TaskRef != f.bug.TaskRef {
		t.Fatalf("judge=%+v", judge)
	}
	bug := byID[fmt.Sprintf("bug-%d", result)]
	if !strings.Contains(bug.Detail, "ticket container") || !slices.Equal(bug.RunIDs, []int64{bugRun}) {
		t.Fatalf("bug=%+v", bug)
	}
	if legacy := byID[fmt.Sprintf("bug-legacy-%d-1", f.legacy.ID)]; !strings.Contains(legacy.Detail, "ignored errors") {
		t.Fatalf("legacy bug=%+v", legacy)
	}

	for _, c := range []struct {
		name string
		q    LogIncidentQuery
		want []string
	}{
		{"task", LogIncidentQuery{RepositoryIDs: []int64{f.repo.ID}, TaskRefs: []string{f.bug.TaskRef}}, []string{IncidentBug, IncidentJudge}},
		{"runs", LogIncidentQuery{RepositoryIDs: []int64{f.repo.ID}, RunIDs: []int64{failed}}, []string{IncidentFailedRun, IncidentRepeatedNode}},
		{"window", LogIncidentQuery{RepositoryIDs: []int64{f.repo.ID}, After: f.at.Add(time.Hour)}, nil},
		{"other repository", LogIncidentQuery{RepositoryIDs: []int64{f.other.ID}}, []string{IncidentFailedRun}},
		{"no grant", LogIncidentQuery{}, nil},
	} {
		got, err := f.db.ListLogIncidents(ctx, c.q)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, in := range got {
			names = append(names, in.Kind)
		}
		slices.Sort(names)
		if !slices.Equal(names, c.want) {
			t.Fatalf("%s scope=%v want %v", c.name, names, c.want)
		}
	}
}
