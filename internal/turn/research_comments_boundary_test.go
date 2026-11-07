package turn

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func researchCommentFixture(t *testing.T, failed bool) (*fixture, store.GraphNodeAttempt, agentresult.Result, agentresult.Context) {
	t.Helper()
	f := newFixture(t, nil)
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "research", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 22, LogPath: func(int) string { return "research.log" }})
	mustProgress(t, err)
	at := time.Now().UTC()
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: commentBriefProfile(t).Digest, CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: "work", StartedAt: at, NextDueAt: at})
	mustProgress(t, err)
	source, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", RunID: run.ID, StartedAt: at})
	mustProgress(t, err)
	status, outcome := store.GraphSucceeded, "success"
	if failed {
		status, outcome = store.GraphFailed, ""
		code := int64(store.SignalExitCode)
		_, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, ExitCode: &code, Error: "timeout"})
		mustProgress(t, err)
	}
	completion := store.GraphCompletion{Status: status, Outcome: outcome, FinishedAt: at, InstanceStatus: store.GraphRunning}
	if !failed {
		completion.Transition = &store.GraphTransition{InstanceID: instance.ID, FromNode: "work", ToNode: "work", OnOutcome: "success", At: at}
	}
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), source.ID, completion))
	source, err = f.db.GetGraphNodeAttempt(t.Context(), source.ID)
	mustProgress(t, err)
	candidate := agentresult.Result{Outcome: "completed", Comment: "Retained findings.", Payload: agentresult.Payload{Research: &agentresult.Research{Request: "Inspect boundaries"}}}
	if failed {
		candidate.Outcome = "incomplete"
		candidate.Reason = "Agent timed out; findings retained."
	}
	return f, source, candidate, agentresult.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "research", QuestionsAllowed: true}
}

func TestResearchCommentHTTPReceiptLossAndCorrection(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	if source.Status == store.GraphSucceeded && len(candidate.Questions) == 0 {
		retainCommentBrief(t, f, source, candidate, scope)
	}
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	before, err := f.db.GetRun(t.Context(), source.RunID)
	mustProgress(t, err)
	forge.afterPost = func() { mustProgress(t, f.db.Close()) }
	posted, key, err := reporter.ReportWorkflowCandidate(t.Context(), source, "report", "final", candidate, scope, "", ResearchWorkflowPolicy())
	if err == nil || posted.CommentID == 0 || key == "" {
		t.Fatalf("receipt loss hidden: %+v %q %v", posted, key, err)
	}
	original := forge.only(t).body
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	mustProgress(t, reopened.Migrate(t.Context()))
	*f.db = *reopened
	forge.afterPost = nil
	intent, err := f.db.GetGraphComment(t.Context(), key)
	mustProgress(t, err)
	raw, err := json.Marshal(candidate)
	mustProgress(t, err)
	if string(intent.Candidate) != string(raw) {
		t.Fatal("authored bytes changed")
	}
	retained, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if retained.CommentID != 0 {
		t.Fatal("unacknowledged effect confirmed")
	}
	recovered := newReporter(t, f, forge, stubRenderer{err: errors.New("must not render retained effect")})
	for range 2 {
		got, e := recovered.RecoverWorkflowComment(t.Context(), key, scope, ResearchWorkflowPolicy())
		mustProgress(t, e)
		if got.CommentID != posted.CommentID {
			t.Fatal("recovery changed receipt")
		}
	}
	if forge.attemptCount() != 1 || forge.only(t).body != original {
		t.Fatal("recovery reposted or changed bytes")
	}
	after, err := f.db.GetRun(t.Context(), source.RunID)
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) || watermark(t, f) != 0 {
		t.Fatal("graph publication finalized run or consumed owner input")
	}
	old, err := f.db.GetGraphComment(t.Context(), key)
	mustProgress(t, err)
	if string(old.Candidate) != string(raw) {
		t.Fatal("correction rewrote original")
	}
}

func TestResearchCommentRenderFailureRecoveryPreservesProcess(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, true)
	if source.Status == store.GraphSucceeded && len(candidate.Questions) == 0 {
		retainCommentBrief(t, f, source, candidate, scope)
	}
	forge := newFakeForge(t)
	before, err := f.db.GetRun(t.Context(), source.RunID)
	mustProgress(t, err)
	reporter := newReporter(t, f, forge, stubRenderer{err: errors.New("renderer unavailable")})
	_, key, err := reporter.ReportWorkflowCandidate(t.Context(), source, "failure-report", "failure", candidate, scope, "", ResearchWorkflowPolicy())
	if err == nil || !strings.Contains(err.Error(), "renderer unavailable") || key == "" {
		t.Fatalf("render cause lost: %q %v", key, err)
	}
	if forge.attemptCount() != 0 {
		t.Fatal("render failure posted")
	}
	if _, err = f.db.GetGraphComment(t.Context(), key); err != nil {
		t.Fatal("render failure lost authored candidate", err)
	}
	reporter = newReporter(t, f, forge, nil)
	_, err = reporter.RecoverWorkflowComment(t.Context(), key, scope, ResearchWorkflowPolicy())
	mustProgress(t, err)
	_, err = reporter.RecoverWorkflowComment(t.Context(), key, scope, ResearchWorkflowPolicy())
	mustProgress(t, err)
	after, err := f.db.GetRun(t.Context(), source.RunID)
	mustProgress(t, err)
	attempt, err := f.db.GetGraphNodeAttempt(t.Context(), source.ID)
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) || attempt.Status != store.GraphFailed || after.Error != "timeout" || forge.attemptCount() != 1 || watermark(t, f) != 0 {
		t.Fatal("report-only recovery changed process, duplicated publication or consumed input")
	}
}

// The kind/outcome/status matrix was removed (#474): a question is published
// once from the retained candidate; deduplication and input are unchanged.
func TestResearchCommentQuestionIsPublishedOnce(t *testing.T) {
	f, source, candidate, scope := researchCommentFixture(t, false)
	candidate.Outcome = "incomplete"
	candidate.Reason = "Scope needed"
	candidate.Questions = []agentresult.Question{{ID: "Q-1", Audience: "business", Text: "Which boundary?"}}
	retainCommentBrief(t, f, source, candidate, scope)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	_, key, err := reporter.ReportWorkflowCandidate(t.Context(), source, "question", "question", candidate, scope, "", ResearchWorkflowPolicy())
	mustProgress(t, err)
	_, same, err := reporter.ReportWorkflowCandidate(t.Context(), source, "question", "question", candidate, scope, "", ResearchWorkflowPolicy())
	mustProgress(t, err)
	if key != same || forge.attemptCount() != 1 || watermark(t, f) != 0 {
		t.Fatal("question repeated or consumed reply")
	}
}

func TestResearchCommentRecoveryRejectsForeignReceipt(t *testing.T) {
	for _, kind := range []string{"author", "destination", "content"} {
		t.Run(kind, func(t *testing.T) {
			f, source, candidate, scope := researchCommentFixture(t, false)
			if source.Status == store.GraphSucceeded && len(candidate.Questions) == 0 {
				retainCommentBrief(t, f, source, candidate, scope)
			}
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			forge.afterPost = func() { mustProgress(t, f.db.Close()) }
			_, key, err := reporter.ReportWorkflowCandidate(t.Context(), source, "report", "final", candidate, scope, "", ResearchWorkflowPolicy())
			if err == nil || key == "" {
				t.Fatal("expected lost receipt")
			}
			reopened, err := store.Open(t.Context(), f.paths.DBPath())
			mustProgress(t, err)
			*f.db = *reopened
			forge.mu.Lock()
			forge.afterPost = nil
			switch kind {
			case "author":
				forge.posts[0].authorID = 999
			case "destination":
				forge.posts[0].htmlURL = "https://github.com/other/repo/issues/1#issuecomment-5001"
			case "content":
				forge.posts[0].body = "Changed body\n" + forge.posts[0].body
			}
			forge.mu.Unlock()
			_, err = reporter.RecoverWorkflowComment(t.Context(), key, scope, ResearchWorkflowPolicy())
			if err == nil {
				t.Fatal("foreign receipt confirmed")
			}
			delivery, err := f.db.GetReportDelivery(t.Context(), key)
			mustProgress(t, err)
			if delivery.CommentID != 0 || forge.attemptCount() != 1 || watermark(t, f) != 0 {
				t.Fatal("untrusted receipt confirmed or replayed")
			}
		})
	}
}

func commentBriefProfile(t *testing.T) *graphexec.Profile {
	t.Helper()
	p, err := graphexec.ParseProfile([]byte(`digraph {team=builtin;mode=research;profile_version=1;start=work;
 work [lf_action=lf_agent,lf_outcomes=success];brief [lf_action=lf_comment,lf_kind=final,lf_source=work,lf_outcomes=published];end [lf_terminal=true];
 work -> brief [on=success];brief -> end [on=published];}`))
	mustProgress(t, err)
	return p
}

// retainCommentBrief records the source attempt's accepted result, which is
// all a publication reads.
func retainCommentBrief(t *testing.T, f *fixture, source store.GraphNodeAttempt, candidate agentresult.Result, _ agentresult.Context) {
	t.Helper()
	response := "done"
	if len(candidate.Questions) > 0 {
		response = "ask"
	}
	raw, err := json.Marshal(agentresult.ResearchNode{Version: 1, Response: response, Result: candidate})
	mustProgress(t, err)
	_, err = f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: source.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: source.StartedAt})
	mustProgress(t, err)
}
